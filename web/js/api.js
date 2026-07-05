// api.js — thin fetch wrapper around the backend contract (docs/contract.md).
// No third-party HTTP client, stdlib fetch only.

import { config } from "./config.js";

export class ApiError extends Error {
  constructor(message, { status, code, payload } = {}) {
    super(message);
    this.name = "ApiError";
    this.status = status;
    this.code = code; // e.g. "validation_failed", "stale_document", "idempotency_key_reused"
    this.payload = payload; // full parsed error body, for §-specific extra fields (current_doc, fields, ...)
  }
}

// Thrown for both `system.status === "maintenance"` envelopes and a bare
// gateway `503 + Retry-After` — plan.md §3.1 says the client should treat
// both the same way.
export class MaintenanceError extends Error {
  constructor(retryAfter, message) {
    super(message || "Service in maintenance");
    this.name = "MaintenanceError";
    this.retryAfter = retryAfter ?? null;
  }
}

function authHeaders() {
  const token = config.token;
  return token ? { Authorization: `Bearer ${token}` } : {};
}

async function request(path, { method = "GET", headers = {}, body, auth = false } = {}) {
  const url = `${config.apiBase}${path}`;
  let res;
  try {
    res = await fetch(url, {
      method,
      headers: {
        "Content-Type": "application/json",
        ...(auth ? authHeaders() : {}),
        ...headers,
      },
      body: body != null ? JSON.stringify(body) : undefined,
    });
  } catch (networkErr) {
    // Backend unreachable entirely — treat like maintenance per plan.md §3.1.
    throw new MaintenanceError(null, "Network error contacting server");
  }

  if (res.status === 503) {
    const retryAfter = Number(res.headers.get("Retry-After")) || null;
    throw new MaintenanceError(retryAfter, "Service unavailable");
  }

  if (res.status === 304) {
    return { notModified: true, etag: res.headers.get("ETag") };
  }

  let json = null;
  const text = await res.text();
  if (text) {
    try {
      json = JSON.parse(text);
    } catch {
      // non-JSON body (shouldn't happen per contract) — surface raw text
      json = { raw: text };
    }
  }

  if (!res.ok) {
    const errObj = json?.error;
    throw new ApiError(errObj?.message || `Request failed (${res.status})`, {
      status: res.status,
      code: errObj?.code,
      payload: json,
    });
  }

  return { data: json, etag: res.headers.get("ETag"), retryAfterHeader: res.headers.get("Retry-After") };
}

// --- Sessions -------------------------------------------------------------

export async function startOrResumeSession({ locale, resumeToken }) {
  const { data } = await request("/sessions", {
    method: "POST",
    body: {
      locale,
      client: config.clientCapabilities,
      device_attestation: null, // POC: no real App Attest / Play Integrity
      resume_token: resumeToken ?? null,
    },
  });
  return data;
}

export async function getSession(sessionId, { locale } = {}) {
  const qs = locale ? `?locale=${encodeURIComponent(locale)}` : "";
  const { data } = await request(`/sessions/${encodeURIComponent(sessionId)}${qs}`, {
    method: "GET",
    auth: true,
  });
  return data;
}

export async function submitStep(sessionId, stepId, body, idempotencyKey) {
  const { data } = await request(
    `/sessions/${encodeURIComponent(sessionId)}/steps/${encodeURIComponent(stepId)}`,
    {
      method: "POST",
      auth: true,
      headers: { "Idempotency-Key": idempotencyKey },
      body,
    }
  );
  return data;
}

export async function requestUploadSlot(sessionId, { kind, contentType, sizeBytes }) {
  const { data } = await request(`/sessions/${encodeURIComponent(sessionId)}/uploads`, {
    method: "POST",
    auth: true,
    body: { kind, content_type: contentType, size_bytes: sizeBytes },
  });
  return data;
}

// Uploads go straight to the presigned URL, never through our API.
export async function putUpload(url, headers, blob) {
  const res = await fetch(url, { method: "PUT", headers, body: blob });
  if (!res.ok) {
    throw new ApiError(`Upload failed (${res.status})`, { status: res.status });
  }
}

// --- Reference data ---------------------------------------------------------

const refdataCache = new Map(); // key -> { etag, data }

export async function getRefdata(dataset, { parent, q } = {}) {
  const params = new URLSearchParams();
  if (parent) params.set("parent", parent);
  if (q) params.set("q", q);
  const qs = params.toString();
  const cacheKey = `${dataset}?${qs}`;
  const cached = refdataCache.get(cacheKey);

  const headers = cached?.etag ? { "If-None-Match": cached.etag } : {};
  const { data, etag, notModified } = await request(
    `/refdata/${encodeURIComponent(dataset)}${qs ? `?${qs}` : ""}`,
    { method: "GET", headers }
  );

  if (notModified && cached) return cached.data;
  if (data) refdataCache.set(cacheKey, { etag, data });
  return data;
}

// --- Legal documents ---------------------------------------------------------

export async function getLegalDoc(kind, version, locale) {
  const qs = locale ? `?locale=${encodeURIComponent(locale)}` : "";
  const { data } = await request(`/legal/${encodeURIComponent(kind)}/${encodeURIComponent(version)}${qs}`, {
    method: "GET",
  });
  return data;
}

// --- System envelope (no session) -------------------------------------------

export async function getSystem() {
  const { data } = await request("/system", { method: "GET" });
  return data;
}

export function newIdempotencyKey() {
  if (window.crypto?.randomUUID) return window.crypto.randomUUID();
  // Fallback for older WKWebView engines without crypto.randomUUID.
  return "xxxxxxxx-xxxx-4xxx-yxxx-xxxxxxxxxxxx".replace(/[xy]/g, (c) => {
    const r = (Math.random() * 16) | 0;
    const v = c === "x" ? r : (r & 0x3) | 0x8;
    return v.toString(16);
  });
}
