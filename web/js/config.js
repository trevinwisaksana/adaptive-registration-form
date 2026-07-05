// config.js — runtime configuration for the form renderer.
//
// POC ONLY: the API base URL and locale are kept in localStorage so this page
// can be opened directly in a desktop browser and still "remember" itself
// across reloads for demo purposes. In production the native shell (WKWebView
// host) would inject configuration (and a short-lived session token) via a JS
// bridge on every page load, and nothing would be persisted in web storage —
// see plan.md §1 ("hardened native↔web token handoff... nothing stored in web
// storage"). That discipline is relaxed here only so the reconciliation /
// resume demo works when this app is opened standalone.

const DEFAULT_API_BASE = "http://localhost:8080";

const LS = {
  apiBase: "arf.apiBase",
  locale: "arf.locale",
  sessionId: "arf.sessionId",
  token: "arf.token",
  resumeToken: "arf.resumeToken",
};

function readQueryParam(name) {
  const params = new URLSearchParams(window.location.search);
  return params.get(name);
}

export const config = {
  get apiBase() {
    return (
      readQueryParam("api") ||
      window.localStorage.getItem(LS.apiBase) ||
      DEFAULT_API_BASE
    );
  },
  set apiBase(value) {
    window.localStorage.setItem(LS.apiBase, value);
  },

  get locale() {
    return readQueryParam("locale") || window.localStorage.getItem(LS.locale) || "en-US";
  },
  set locale(value) {
    window.localStorage.setItem(LS.locale, value);
  },

  get sessionId() {
    return window.localStorage.getItem(LS.sessionId);
  },
  set sessionId(value) {
    if (value == null) window.localStorage.removeItem(LS.sessionId);
    else window.localStorage.setItem(LS.sessionId, value);
  },

  get token() {
    return window.localStorage.getItem(LS.token);
  },
  set token(value) {
    if (value == null) window.localStorage.removeItem(LS.token);
    else window.localStorage.setItem(LS.token, value);
  },

  // What the "app" declares it can render. A real client would hardcode this
  // per its own capabilities; this web renderer supports the full set defined
  // in the contract, plus simple stand-ins for the steps that are native-only
  // in production (camera / signature / pin) so the whole flow can be walked
  // in a plain browser for the demo.
  clientCapabilities: {
    platform: "web",
    app_version: "0.1.0-poc",
    supported_types: ["form", "camera", "signature", "document", "pin", "external"],
    supported_field_kinds: ["text", "date", "select", "multiselect", "money", "bool"],
  },

  clearSession() {
    window.localStorage.removeItem(LS.sessionId);
    window.localStorage.removeItem(LS.token);
  },
};

export { LS };
