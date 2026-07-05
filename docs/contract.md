# API Contract — Adaptive Registration Form (POC)

Source of truth for every builder (Go backend, iOS renderer, seed data). Derived from `plan.md`.
Server is authoritative for ordering, validation, and localization. Client never decides ordering.

Auth: every endpoint except `POST /sessions`, `GET /refdata/*`, `GET /legal/*`, `GET /system`, and
`POST /webhooks/mock-kyc` requires `Authorization: Bearer <session_token>` (stub token, no real
auth — POC only). Content type is `application/json` unless noted.

---

## 1. The response envelope

Every session-scoped response (POST /sessions, GET /sessions/{id}, POST .../steps/{stepId}) is
wrapped in this envelope. Nothing else is nested outside it.

```jsonc
{
  "system": {
    "status": "ok",              // ok | degraded | maintenance
    "retry_after": null,         // seconds; set when status = maintenance
    "banners": [
      { "id": "high-load-0705", "severity": "warning", "scope": "global",
        "text": "We're seeing high demand — verification may take longer than usual." },
      { "id": "bank-x-outage", "severity": "info", "scope": "bank_info",
        "text": "Bank X connections are currently unstable. You can finish this later." }
    ]
  },
  "progress": { "completed": 4, "total": 10 },
  "next_step": { "id": "bank_info", "type": "form", "title": "Bank information", "fields": [ /* §3 */ ] },
  "repairs": []
}
```

- `system.banners[].scope` is `"global"` or a step `id` — the client shows a banner only when
  `scope == "global"` or `scope == next_step.id`.
- `next_step` is `null` when the flow is complete (state machine moved to `on_complete`/terminal).
- `progress.total` changes when a branch inserts/removes a page (e.g. FATCA) — client never hardcodes it.
- `repairs` is `[]` in the common case; populated on resume/reconciliation (§4) or after an edit
  invalidates downstream answers (§2, cross-page conditions).

---

## 2. Endpoints

### 2.1 `POST /sessions` — start or resume

Request:
```json
{
  "locale": "en-US",
  "client": {
    "platform": "ios",
    "app_version": "1.4.0",
    "supported_types": ["form", "camera", "signature", "document", "pin", "external"],
    "supported_field_kinds": ["text", "date", "select", "multiselect", "money", "bool"]
  },
  "device_attestation": "<app-attest-token>",
  "resume_token": null
}
```

Response `201 Created` (or `200 OK` on resume via `resume_token`):
```json
{
  "session": { "id": "sess_9f2a", "flow": "retail_onboarding", "flow_version": 14, "expires_at": "2026-08-04T00:00:00Z" },
  "token": "stub.<session_id>.<random>",
  "system": { "status": "ok", "banners": [] },
  "progress": { "completed": 0, "total": 10 },
  "next_step": { "id": "personal_details", "type": "form", "fields": [ "…" ] },
  "repairs": []
}
```

If the flow needs step types/field kinds beyond the client's declared capabilities, `next_step`
becomes `{ "id": "force_update", "type": "external", "adapter": "force_update", "min_app_version": "1.6.0" }`
(a capability gate, not an outage — `system.status` stays `"ok"`).

Rate limit: keyed on device + IP, ~5 sessions/day/device. `429` with `Retry-After`.

### 2.2 `GET /sessions/{id}` — resume

Headers: `Authorization: Bearer <token>`.
Response: same envelope shape as §1, computed fresh from reconciliation (§4). No request body.

### 2.3 `POST /sessions/{id}/steps/{stepId}` — submit a step

Headers: `Authorization: Bearer <token>`, **`Idempotency-Key: <uuid>` (required)**.

Request body shape depends on `next_step.type` (the client only ever submits to the `stepId` it was
just served):

```jsonc
// type: form
{ "answers": { "employment_status": "employed", "employer_name": "Acme", "us_person": false } }

// type: camera
{ "upload_ref": "up_3fa1", "client_checks": { "face_present": true, "blurry": false } }

// type: signature
{ "upload_ref": "up_sig7" }

// type: document (T&C accept — echoes what the client displayed)
{ "accept": true, "doc": { "kind": "tnc", "version": "2026-08", "locale": "en-US",
  "sha256": "b94d27b9934d3e08a52e52d7da7dabfa..." } }

// type: pin (never persisted to step_submissions — routed straight to auth service, §5 of plan.md)
{ "pin": "142857" }

// type: external (result handed back from a vendor webview/adapter)
{ "adapter": "vendor_esign", "result": { "status": "completed" } }
```

Response `200 OK`: the envelope (§1), with `next_step` advanced by the state machine.

Idempotency: replaying the same `Idempotency-Key` for the same `stepId` returns the original
cached response verbatim (no reprocessing). Reusing the key with a **different** body is a
`409 Conflict`:
```json
{ "error": { "code": "idempotency_key_reused", "message": "This request key was already used with a different payload." } }
```

Validation error `422 Unprocessable Entity` (localized to the session's locale):
```json
{
  "error": {
    "code": "validation_failed",
    "step_id": "employment_income",
    "fields": [
      { "key": "employer_name", "rule": "required_when", "message": "Employer name is required." },
      { "key": "annual_income", "rule": "min:0", "message": "Annual income cannot be negative." }
    ]
  }
}
```

Stale T&C at accept time (server-verified version/hash mismatch), `409 Conflict` — consent is
**not** recorded; the next `GET`/submit re-serves the document step with `current_doc` as pointer:
```json
{
  "error": { "code": "stale_document", "message": "This document was updated. Please review the new version." },
  "current_doc": { "kind": "tnc", "version": "2026-09", "locale": "en-US",
    "sha256": "1a79a4d60de6718e8e5b326e338ae533...", "url": "https://legal.example.com/tnc/2026-09?locale=en-US" }
}
```

### 2.4 `POST /sessions/{id}/uploads` — presigned upload slot

Request:
```json
{ "kind": "selfie", "content_type": "image/jpeg", "size_bytes": 482301 }
```
`kind` is one of `id_card | selfie | signature` — one object slot per kind per session; re-requesting
overwrites the prior slot.

Response `201 Created`:
```json
{ "upload_ref": "up_3fa1", "url": "https://minio.local/registration/sess_9f2a/selfie?X-Amz-...",
  "method": "PUT", "headers": { "Content-Type": "image/jpeg" }, "expires_at": "2026-07-05T10:05:00Z" }
```
Client `PUT`s the raw bytes directly to `url` (never through the API). Server checks size/content-type
and that the object decodes as an image — no vendor verification here. The resulting `upload_ref` is
passed into the matching `camera`/`signature` step submit (§2.3).

### 2.5 `GET /refdata/{dataset}?parent=&q=`

ETag-cached, paginated, searchable. `parent` filters by `parent_code` (cascading lists, e.g. cities
under a region); `q` is a typeahead search.

```json
{ "dataset": "cities", "version": 12, "items": [ { "code": "JB-BKS", "label": "Bekasi" } ] }
```
Codes are what fields store (`options_ref` fields submit `code`, never `label`). `304 Not Modified`
on matching `If-None-Match`.

### 2.6 `GET /legal/{kind}/{version}?locale=`

Immutable per `(kind, version, locale)` — CDN-cacheable forever, no auth required.
```json
{
  "kind": "tnc", "version": "2026-08", "locale": "en-US",
  "sha256": "b94d27b9934d3e08a52e52d7da7dabfa...",
  "content_type": "text/html",
  "content": "<html>…</html>",
  "effective_at": "2026-08-01T00:00:00Z"
}
```

### 2.7 `POST /webhooks/mock-kyc`

Called by the mock vendor adapter (never by the client). Header: `X-Vendor-Signature: <hmac>`.
```json
{ "session_id": "sess_9f2a", "vendor": "mock_kyc", "verdict": "approved", "reason": null }
```
`verdict` is `approved | rejected`; `reason` is a machine code (e.g. `"blurry_id"`) required when
rejected — targets the repair (redo only the affected camera step). `200 { "received": true }`,
idempotent per `session_id` (duplicate verdict on a terminal session is a no-op).

### 2.8 `GET /system`

No auth, no session. Global banners/maintenance only (step-scoped banners require a session).
```json
{ "system": { "status": "ok", "retry_after": null, "banners": [ { "id": "maint-2026-07-06", "severity": "warning", "scope": "global", "text": "Scheduled maintenance 02:00–03:00 UTC." } ] } }
```

---

## 3. Step-definition schema

`next_step` is always one object of shape `{ id, type, title?, ...type-specific fields }`. Every
field/label the client sees is already server-localized to the session's locale — no `label_key`s
reach the client, only resolved `label`s.

### 3.1 `type: "form"`

```jsonc
{
  "id": "employment_income", "type": "form", "title": "Employment & income",
  "fields": [
    { "key": "employment_status", "kind": "select", "label": "Employment status",
      "options_ref": "employment_statuses", "required": true },
    { "key": "employer_name", "kind": "text", "label": "Employer name",
      "visible_when": "employment_status in ['employed','self_employed']",
      "required_when": "employment_status in ['employed','self_employed']" },
    { "key": "annual_income", "kind": "money", "label": "Annual income", "rules": ["min:0"] },
    { "key": "source_of_funds", "kind": "multiselect", "label": "Source of funds", "options_ref": "fund_sources" },
    { "key": "city", "kind": "select", "label": "City", "options_ref": "cities", "filter_by": { "parent": "region" } },
    { "key": "us_person", "kind": "bool", "label": "US person?", "required": true },
    { "key": "ssn", "kind": "text", "label": "SSN", "visible_when": "answers.contact_address.country == 'US'" }
  ]
}
```

Field object:

| field | meaning |
|---|---|
| `key` | storage key; codes not labels are submitted for `select`/`multiselect` |
| `kind` | `text \| date \| select \| multiselect \| money \| bool` |
| `label` | server-localized display text |
| `required` | static requirement |
| `required_when` | conditional requirement, expression string |
| `visible_when` | conditional visibility, expression string |
| `options_ref` | name of a `/refdata/{dataset}` dataset backing a `select`/`multiselect` |
| `filter_by.parent` | another field `key` on the **same page** whose value filters `options_ref` (cascading list) |
| `rules` | validation rules (e.g. `"min:0"`, `"age>=18"`) — always re-checked server-side |

**Client/server split:** `visible_when`/`required_when` referencing only same-page fields are shipped
as-is and evaluated client-side for live show/hide UX, then re-verified at submit. Conditions
referencing `answers.<other_step_id>.<field>` (cross-page) are **never sent to the client** — the
server resolves them before serving the page, so the field set the client receives is already
correct (e.g. `ssn` present or absent). The client never evaluates or sees `answers.*` syntax.

### 3.2 `type: "camera"` / `"signature"` / `"pin"` — no `fields`, minimal payload

```json
{ "id": "id_card", "type": "camera", "title": "Photograph your ID card", "capture": "id_card" }
{ "id": "sign", "type": "signature", "title": "Sign to continue" }
{ "id": "setup_pin", "type": "pin", "title": "Set up your PIN" }
```
- `camera.capture` is `id_card | selfie` — tells the renderer which on-device checks to run (face
  present, not blurry) before offering the upload slot (§2.4). Submit body: `{ "upload_ref": "…" }`.
- `signature` — client renders a signature pad; submit body: `{ "upload_ref": "…" }` (§2.4).
- `pin` — submit body `{ "pin": "..." }` (§2.3); routed straight to the auth service, stored only as
  an Argon2 hash there, and logged to `audit_log` as `pin_set` with no payload — **never** written
  to `step_submissions`.

### 3.3 `type: "document"`

```json
{
  "id": "tnc", "type": "document", "title": "Terms & Conditions",
  "doc": { "kind": "tnc", "version": "2026-08", "locale": "en-US",
    "sha256": "b94d27b9934d3e08a52e52d7da7dabfa...",
    "url": "https://legal.example.com/tnc/2026-08?locale=en-US" }
}
```
The session API sends only the pointer; content is fetched from `GET /legal/{kind}/{version}` (§2.6).
Accept payload and staleness check are in §2.3.

### 3.4 `type: "external"`

```json
{ "id": "extra_kyc", "type": "external", "title": "Additional verification",
  "adapter": "vendor_kyc_extra", "webview_url": "https://vendor.example.com/session/abc123" }
```
Third-party vendor steps only — the client renders a webview at `webview_url` and reports
completion via the generic `external` submit body (§2.3). There is NO unknown-step-type
fallback anywhere: the server must never serve a step type outside this contract's six types
to a client that didn't declare support for it (capability negotiation → `force_update`).
Clients must treat an unrecognized `type` as a hard error, never render it.

---

## 4. Reconciliation & repairs (resume)

`GET /sessions/{id}` (and any step submit) recomputes `repairs[]` fresh each time by diffing stored
`step_submissions`/`consents`/`documents` against current reality (flow version, T&C version, doc
TTLs, async vendor results):

```jsonc
"repairs": [
  { "kind": "reaccept_document", "step_id": "tnc", "reason": "tnc_version_changed",
    "detail": { "doc": { "kind": "tnc", "version": "2026-08", "locale": "en-US", "sha256": "…" } } },
  { "kind": "collect_fields", "step_id": "personal_details", "reason": "flow_version_bumped",
    "detail": { "fields": ["tax_id"] } },
  { "kind": "redo_step", "step_id": "selfie", "reason": "document_expired",
    "detail": { "ttl_days": 30 } }
]
```

| `kind` | trigger |
|---|---|
| `reaccept_document` | T&C version bumped with `reacceptance: "required"` since last accept |
| `collect_fields` | newer flow version adds a required field the session hasn't answered |
| `redo_step` | uploaded doc past TTL, or vendor verdict `rejected` with a targeted reason |

The **same mechanism drives cross-page edit cascades**: re-submitting an earlier page with a changed
answer re-resolves downstream pages — valid answers are kept, orphaned ones dropped, and a page now
missing a required answer appears as a `collect_fields` repair, routing the user back to only that
page. `next_step` always points at the first unresolved item (a repair's `step_id` or the next fresh
step) — the client never picks among repairs itself; it just renders `next_step`.

---

## 5. Flow definition JSON (plan.md §2)

Stored in `flow_versions.definition` (jsonb), pinned per session at creation time.

```jsonc
{
  "flow": "retail_onboarding",
  "version": 15,
  "steps": [
    { "id": "personal_details", "type": "form", "fields": [
      { "key": "full_name", "kind": "text", "required": true },
      { "key": "dob", "kind": "date", "rules": ["age>=18"] },
      { "key": "tax_id", "kind": "text", "required": true, "since_version": 15 }
    ]},
    { "id": "contact_address", "type": "form", "fields": [ "…" ] },
    { "id": "employment_income", "type": "form", "fields": [ "…" ] },
    { "id": "trading_experience", "type": "form", "fields": [ "…" ] },
    { "id": "fatca_form", "type": "form", "fields": [ "…" ] },
    { "id": "bank_info", "type": "form", "fields": [ "…" ] },
    { "id": "id_card", "type": "camera", "capture": "id_card" },
    { "id": "selfie", "type": "camera", "capture": "selfie" },
    { "id": "tnc", "type": "document", "doc": "tnc" },
    { "id": "sign", "type": "signature" },
    { "id": "setup_pin", "type": "pin" }
  ],
  "transitions": [
    { "from": "trading_experience", "when": "answers.trading_experience.us_person == true",
      "insert": ["fatca_form"], "before": "bank_info" }
  ],
  "on_complete": [
    { "adapter": "vendor_kyc" }
  ]
}
```

- **`steps`** — default linear order; each entry is a step definition per §3, minus parts resolved
  only at serve time (`label`, `options_ref` payload, resolved `doc` pointer, cross-page conditions).
- **`transitions`** — the only place branching lives. `from` + `when` (expression over any earlier
  page's answers) triggers `insert` (step ids spliced in, `before`/`after` an existing step id); no
  match = default order from `steps`. This is how FATCA appears only for `us_person == true` and how
  `progress.total` becomes 11 instead of 10.
- **`on_complete`** — adapters run once, after the last step, never rendered as a page; the state
  machine invokes them idempotently exactly once per session.
- **`since_version`** is informational (changelog); enforcement is just "field exists in this
  version's `steps`" — an older pinned session won't see it until a `collect_fields` repair (§4)
  is generated post-publish.
