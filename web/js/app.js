// app.js — orchestrator: boots/resumes a session, renders the system
// envelope (banners, maintenance, progress) on every response, and dispatches
// each `next_step` to the right step renderer. This is the only file that
// knows how the pieces fit together; step renderers know nothing about each
// other or about the envelope.

import { config } from "./config.js";
import { t, chromeLocale } from "./i18n.js";
import * as api from "./api.js";
import { ApiError, MaintenanceError } from "./api.js";
import { renderFormStep, clearDraft } from "./steps/form.js";
import { renderDocumentStep } from "./steps/document.js";
import { renderCameraStep } from "./steps/camera.js";
import { renderSignatureStep } from "./steps/signature.js";
import { renderPinStep } from "./steps/pin.js";
import { renderExternalStep } from "./steps/external.js";

const el = {
  progressFill: document.getElementById("progress-fill"),
  progressLabel: document.getElementById("progress-label"),
  bannersGlobal: document.getElementById("banners-global"),
  bannersStep: document.getElementById("banners-step"),
  stepContainer: document.getElementById("step-container"),
  maintenanceScreen: document.getElementById("maintenance-screen"),
  maintenanceBody: document.getElementById("maintenance-body"),
  maintenanceRetry: document.getElementById("maintenance-retry"),
  offlineBanner: document.getElementById("offline-banner"),
  localeButtons: document.querySelectorAll("[data-locale]"),
  debugApiBase: document.getElementById("debug-api-base"),
  debugApiBaseSave: document.getElementById("debug-api-base-save"),
  debugDropOff: document.getElementById("debug-drop-off"),
  debugForget: document.getElementById("debug-forget"),
  debugEnvelope: document.getElementById("debug-envelope"),
  appHeader: document.getElementById("app-header"),
};

const state = {
  sessionId: null,
  locale: config.locale,
  lastEnvelope: null,
  maintenanceTimer: null,
};

// --- Banners & system status -------------------------------------------------

function severityClass(sev) {
  return { info: "banner-info", warning: "banner-warning", critical: "banner-error" }[sev] || "banner-info";
}

function renderBanner(banner) {
  const div = document.createElement("div");
  div.className = `banner ${severityClass(banner.severity)}`;
  div.setAttribute("role", banner.severity === "critical" ? "alert" : "status");
  const text = document.createElement("span");
  text.textContent = banner.text;
  div.append(text);
  const dismiss = document.createElement("button");
  dismiss.type = "button";
  dismiss.className = "banner-dismiss";
  dismiss.setAttribute("aria-label", t("banner_dismiss", state.locale));
  dismiss.textContent = "×";
  dismiss.addEventListener("click", () => div.remove());
  div.append(dismiss);
  return div;
}

function renderBanners(system, currentStepId) {
  el.bannersGlobal.innerHTML = "";
  el.bannersStep.innerHTML = "";
  for (const banner of system?.banners ?? []) {
    if (banner.scope === "global") {
      el.bannersGlobal.append(renderBanner(banner));
    } else if (currentStepId && banner.scope === currentStepId) {
      el.bannersStep.append(renderBanner(banner));
    }
  }
}

function stopMaintenancePoll() {
  if (state.maintenanceTimer) {
    clearTimeout(state.maintenanceTimer);
    state.maintenanceTimer = null;
  }
}

function showMaintenance(retryAfter) {
  el.maintenanceScreen.hidden = false;
  el.appHeader.hidden = true;
  el.stepContainer.hidden = true;
  el.maintenanceBody.textContent = t("maintenance_body", state.locale);
  const seconds = retryAfter && retryAfter > 0 ? retryAfter : 30;
  let remaining = seconds;
  el.maintenanceRetry.textContent = t("maintenance_retry_in", state.locale, remaining);
  stopMaintenancePoll();
  const tick = () => {
    remaining -= 1;
    if (remaining <= 0) {
      el.maintenanceRetry.textContent = t("loading", state.locale);
      boot({ silent: true }).catch(() => {
        remaining = seconds;
        state.maintenanceTimer = setTimeout(tick, 1000);
      });
      return;
    }
    el.maintenanceRetry.textContent = t("maintenance_retry_in", state.locale, remaining);
    state.maintenanceTimer = setTimeout(tick, 1000);
  };
  state.maintenanceTimer = setTimeout(tick, 1000);
}

function hideMaintenance() {
  stopMaintenancePoll();
  el.maintenanceScreen.hidden = true;
  el.appHeader.hidden = false;
  el.stepContainer.hidden = false;
}

function showOffline(show) {
  el.offlineBanner.hidden = !show;
}

// --- Progress -----------------------------------------------------------------

function renderProgress(progress) {
  if (!progress) return;
  const { completed, total } = progress;
  const pct = total > 0 ? Math.min(100, Math.round((completed / total) * 100)) : 0;
  el.progressFill.style.width = `${pct}%`;
  el.progressLabel.textContent = t("progress_label", state.locale, completed, total);
}

// --- Step dispatch --------------------------------------------------------

const RENDERERS = {
  form: renderFormStep,
  document: renderDocumentStep,
  camera: renderCameraStep,
  signature: renderSignatureStep,
  pin: renderPinStep,
  external: renderExternalStep,
};

function renderCompletion() {
  el.stepContainer.innerHTML = "";
  const wrap = document.createElement("div");
  wrap.className = "step-complete";
  wrap.append(
    Object.assign(document.createElement("h1"), { className: "step-title", textContent: t("complete_title", state.locale) }),
    Object.assign(document.createElement("p"), { textContent: t("complete_body", state.locale) })
  );
  const refreshBtn = document.createElement("button");
  refreshBtn.type = "button";
  refreshBtn.className = "btn btn-secondary";
  refreshBtn.textContent = t("checkStatus", state.locale);
  refreshBtn.addEventListener("click", () => resumeExisting());
  wrap.append(refreshBtn);
  el.stepContainer.append(wrap);
}

async function submitStep(stepId, body) {
  const key = api.newIdempotencyKey();
  try {
    const envelope = await api.submitStep(state.sessionId, stepId, body, key);
    clearDraft(state.sessionId, stepId);
    await handleEnvelope(envelope);
  } catch (err) {
    if (err instanceof MaintenanceError) {
      showMaintenance(err.retryAfter);
      return;
    }
    if (err instanceof ApiError && err.status === 401) {
      await handleAuthExpired();
      return;
    }
    throw err; // step renderer may recognize this (validation_failed, stale_document)
  }
}

function dispatchStep(step, repairs) {
  el.stepContainer.hidden = false;
  const renderer = RENDERERS[step.type];
  if (!renderer) {
    // Unknown step type is a hard error, never rendered (contract.md §3.4):
    // capability negotiation means the server should never have served it.
    el.stepContainer.textContent = `Unsupported step type "${step.type}" — please update the app.`;
    console.error(`[app] server sent undeclared step type "${step.type}" for step "${step.id}"`);
    return;
  }
  renderer(step, el.stepContainer, {
    sessionId: state.sessionId,
    locale: state.locale,
    onSubmit: (body) => submitStep(step.id, body),
    repairs,
  });
}

// --- Envelope handling ------------------------------------------------------

async function handleEnvelope(envelope) {
  state.lastEnvelope = envelope;
  if (el.debugEnvelope) el.debugEnvelope.textContent = JSON.stringify(envelope, null, 2);

  if (envelope.session?.id) state.sessionId = envelope.session.id;
  if (envelope.token) config.token = envelope.token;
  if (state.sessionId) config.sessionId = state.sessionId;

  const system = envelope.system;
  if (system?.status === "maintenance") {
    showMaintenance(system.retry_after);
    return;
  }
  hideMaintenance();
  showOffline(false);

  renderProgress(envelope.progress);
  renderBanners(system, envelope.next_step?.id ?? null);

  if (envelope.repairs?.length) {
    // Surfaced inline by the step renderer that owns `repairs[].step_id`; here
    // we just keep them attached to state for anything else that wants them.
    state.repairs = envelope.repairs;
  } else {
    state.repairs = [];
  }

  if (!envelope.next_step) {
    renderCompletion();
    return;
  }

  dispatchStep(envelope.next_step, state.repairs);
}

async function handleAuthExpired() {
  config.clearSession();
  state.sessionId = null;
  await boot({ silent: true });
}

// --- Boot / resume ----------------------------------------------------------

async function resumeExisting() {
  try {
    const envelope = await api.getSession(config.sessionId, { locale: state.locale });
    await handleEnvelope(envelope);
    return true;
  } catch (err) {
    if (err instanceof MaintenanceError) {
      showMaintenance(err.retryAfter);
      return true;
    }
    if (err instanceof ApiError && err.status === 401) {
      config.clearSession();
      return false;
    }
    throw err;
  }
}

async function startFresh() {
  const envelope = await api.startOrResumeSession({
    locale: state.locale,
    resumeToken: config.token, // POC: reuse the last bearer token as the "resume token" (see config.js note)
  });
  await handleEnvelope(envelope);
}

async function boot({ silent = false } = {}) {
  if (!silent) {
    el.stepContainer.innerHTML = `<p class="loading-message">${t("resuming", state.locale)}</p>`;
  }
  try {
    if (config.sessionId && config.token) {
      const resumed = await resumeExisting();
      if (resumed) return;
    }
    await startFresh();
  } catch (err) {
    if (err instanceof MaintenanceError) {
      showMaintenance(err.retryAfter);
      return;
    }
    console.error("[app] boot failed:", err);
    el.stepContainer.innerHTML = `<div class="fatal-error"><h1>${t("offline_title", state.locale)}</h1><p>${t("offline_body", state.locale)}</p></div>`;
    showOffline(true);
  }
}

// --- Locale switcher --------------------------------------------------------

function setLocale(locale) {
  state.locale = locale;
  config.locale = locale;
  for (const btn of el.localeButtons) btn.classList.toggle("active", btn.dataset.locale === locale);
  document.documentElement.lang = chromeLocale(locale);
  // Re-render static chrome strings immediately for responsiveness...
  applyStaticStrings();
  // ...then ask the server to re-resolve the current step in the new locale.
  // NOTE (assumption): contract.md only documents `locale` at session
  // creation (§2.1); it doesn't define a dedicated mid-flow locale-change
  // endpoint. Per plan.md ("Locale is session state... switchable
  // mid-flow... a language switch just re-serves the current step
  // re-resolved") we re-fetch via GET /sessions/{id}?locale=, extending the
  // resume endpoint with a query param. A real backend should confirm/adjust
  // this exact mechanism.
  //
  // Routed through boot() (not called inline) so a failed refresh — e.g. a
  // 401 because the resume token has since expired — falls back to starting
  // a fresh session and re-rendering, instead of resumeExisting() silently
  // wiping the persisted session (contract.md §4) while the UI is left
  // showing stale content with no recovery path.
  boot({ silent: true }).catch((err) => console.error("[app] locale refresh failed:", err));
}

function applyStaticStrings() {
  document.title = t("appName", state.locale);
  el.debugDropOff.textContent = t("debug_drop_off", state.locale);
  el.debugForget.textContent = t("debug_forget", state.locale);
  document.getElementById("debug-title").textContent = t("debug_title", state.locale);
  document.getElementById("debug-api-base-label").textContent = t("debug_api_base", state.locale);
  document.getElementById("debug-locale-label").textContent = t("debug_locale", state.locale);
  document.getElementById("debug-envelope-label").textContent = t("debug_last_envelope", state.locale);
}

for (const btn of el.localeButtons) {
  btn.addEventListener("click", () => setLocale(btn.dataset.locale));
}

// --- Debug panel: drop-off / resume demo, config -----------------------------

el.debugApiBase.value = config.apiBase;
el.debugApiBaseSave.addEventListener("click", () => {
  config.apiBase = el.debugApiBase.value.trim() || undefined;
  window.location.reload();
});

el.debugDropOff.addEventListener("click", () => {
  // Simulates the app being killed mid-page: the in-progress (unsubmitted)
  // on-device draft for whatever step is currently showing is intentionally
  // left in place (that's the point of the draft — it *should* survive a
  // simple reload), but we drop all in-memory JS state and force a full
  // reload, which re-resumes purely from GET /sessions/{id}. Any pages
  // already submitted are never re-asked; `repairs[]` (if the backend has
  // meanwhile bumped a flow/T&C version) are recomputed fresh and surfaced
  // inline on the step that needs them — contract.md §4.
  window.location.reload();
});

el.debugForget.addEventListener("click", () => {
  config.clearSession();
  window.location.reload();
});

// --- Global safety net --------------------------------------------------------

window.addEventListener("unhandledrejection", (event) => {
  const err = event.reason;
  console.error("[app] unhandled error:", err);
  if (err instanceof MaintenanceError) {
    showMaintenance(err.retryAfter);
    event.preventDefault();
    return;
  }
  if (err instanceof ApiError && err.code === "idempotency_key_reused") {
    // Shouldn't happen (we mint a fresh key per attempt) — surface and let
    // the user retry rather than silently failing.
    alert(t("retry", state.locale));
    event.preventDefault();
  }
});

window.addEventListener("online", () => showOffline(false));
window.addEventListener("offline", () => showOffline(true));

// --- Init ---------------------------------------------------------------------

el.debugApiBase.value = config.apiBase;
document.querySelectorAll("[data-locale]").forEach((btn) => {
  btn.classList.toggle("active", btn.dataset.locale === state.locale);
});
applyStaticStrings();
document.documentElement.lang = chromeLocale(state.locale);
boot();
