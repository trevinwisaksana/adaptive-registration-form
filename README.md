# Adaptive Registration Form — POC

Server-driven trading-app onboarding flow: the Go backend owns the flow definition, ordering,
validation, and state machine; iOS and web are **thin renderers** that only know how to draw six
step `type`s. The original design doc is [`plan.md`](plan.md), for readers who want more depth.
The exact wire format both the iOS app and the web renderer are built against is
[`docs/contract.md`](docs/contract.md), which is the binding API spec for this repo.

![System design overview](system-design.gif)

## What this demonstrates

| Concept | What to look at |
|---|---|
| Server drives the form | `backend/internal/engine` decides `next_step`; `StepRegistry`/`app.js` just dispatch on `type` |
| Native vs WebView hybrid | iOS renders camera/signature/pin natively, loads form/document/external via `WKWebView` at `/web/` |
| Flow definition + branching | `seed/flows/flow_v14.json`, `flow_v15.json` — 10 pages, `transitions[]` FATCA branch on `us_person` |
| Reference data, localization | `backend/internal/store`, seeded from `seed/refdata.json` / `seed/translations.json` |
| Architecture & data model | `backend/migrations/0001_init.sql`, `backend/internal/{flowdef,engine,store,media}` |
| Banners & maintenance mode | `seed/announcements.json`, `system` envelope on every response, `GET /system` |
| Reconciliation, T&C re-acceptance | reconciliation in `backend/internal/engine` on `GET /sessions/{id}`; `seed/legal_docs.json` |
| POC scope / smoke coverage | `backend/scripts/smoke.sh` drives the whole 10-page flow with `curl` |

## Architecture

```
   iOS App (SwiftUI)          Web renderer (vanilla JS)
   native: camera/sig/pin     form/document/external + stand-ins for camera/sig/pin
   webview: form/doc/external
        \___________________________/
                     | REST + Bearer <token>
                     v
      Go API (backend/cmd/api, stdlib router in backend/internal/httpapi)
          |             |               |
       flowdef        engine          media
     (flow JSON,   (state machine,  (presigned uploads:
      ResolveOrder)  reconciliation,  MinIO, else local disk)
                      idempotency,
                      mock KYC)
          |             |               |
          +------+------+------+--------+
                 v             v
             Postgres      MinIO / local disk
    (flow_versions, sessions, step_submissions,
     documents, consents, announcements,
     legal_docs, idempotency_keys, ...)
```

The same Go binary also serves static `web/` at `/web/` — no separate web server for the POC.

## Running it

### Option A — go run against a local Postgres (primary path)

```sh
cd backend
export DATABASE_URL="postgres://postgres:postgres@localhost:5432/registration?sslmode=disable"
export MIGRATIONS_DIR="./migrations" SEED_DIR="../seed" WEB_DIR="../web" UPLOADS_DIR="./uploads"
export BASE_URL="http://localhost:8080"
go run ./cmd/api
```

If `MINIO_ENDPOINT` (default `localhost:9000`) is unreachable, uploads fall back automatically
to `UPLOADS_DIR` on local disk — no MinIO needed to walk the whole flow.

### Option B — docker compose (postgres + minio + api)

```sh
docker compose up --build
```

Starts Postgres, MinIO (+ a one-shot `minio-init` job that creates the `registration` bucket),
and the API on `http://localhost:8080`. Migrations run and seed data (flows, T&C, refdata,
translations, announcements) upserts on every boot.

### Opening the web renderer

With the API running, open `http://localhost:8080/web/` — a plain static page calling the same
REST API the iOS app calls. EN/ID buttons (top-right) demo mid-flow locale switching; `?api=` /
`?locale=` query params point it at a different backend or locale.

### iOS app (Xcode)

```sh
brew install xcodegen        # if you don't have it
cd ios && xcodegen generate
open AdaptiveRegistrationForm.xcodeproj
```

Scheme **AdaptiveRegistrationForm**, any iPhone simulator (iOS 16+), Run. Set the backend URL
via **Settings → Backend → Base URL** in-app (defaults to `http://localhost:8080`). See
`ios/README.md` for the renderer registry design and per-screen notes.

## Smoke test

Drives one full 10-page flow with `curl` (personal details → contact → employment → trading
experience → bank info → ID card → selfie → T&C → signature → PIN → mock KYC), plus idempotency
replay and stale-T&C-hash checks:

```sh
cd backend
BASE_URL=http://localhost:8080 ./scripts/smoke.sh   # requires jq + curl
```

Waits for `GET /system`, submits every page, uploads a valid 1x1 PNG for the camera/signature
slots, then waits ~12s for the mock KYC adapter's delayed webhook.

## The money demo: flow v14 → v15 mid-flight repair

Both flow versions are seeded at startup and new sessions always start on the **latest** one —
so to observe "a user drops off, then v15 publishes while they're away", v15 must not exist yet
when the session starts:

1. Temporarily rename `seed/flows/flow_v15.json` and the two `"version": "2026-08"` entries in
   `seed/legal_docs.json` out of the way.
2. Start the API, `POST /sessions` (pins to v14), submit pages 1–7 (personal details → selfie),
   then stop touching the session.
3. Restore the held-back files and restart the API — the upsert seeder re-adds flow v15
   (requires `tax_id`) and T&C `2026-08` (`reacceptance: "required"`).
4. `GET /sessions/{id}` with the same token. `repairs` now includes a `collect_fields` entry for
   `tax_id`, and (once the flow reaches the T&C page) a `reaccept_document` repair for the new
   version — `next_step` points at the first unresolved item; nothing already submitted is re-asked.

There's no in-process "publish" admin endpoint in this POC (`flow_versions.published_at` exists
but isn't gated against current time) — the seed-holdback above demos it without one. The exact
JSON shape of each repair entry (`collect_fields`, `reaccept_document`, `redo_step`) is defined in
`docs/contract.md`.

## Banners & maintenance-mode toggle

Banners and maintenance status are ops data in the `announcements` table, seeded from
`seed/announcements.json` on boot (upsert by `id`). Seeded rows: a global "high demand" banner
(active), a `bank_info`-scoped outage banner (active), a global maintenance banner (**inactive**).

To flip maintenance mode on, either edit `seed/announcements.json` (`"active": false` → `true`
on `maint-2026-07-06`) and restart the API, or update Postgres directly (no restart needed):

```sql
UPDATE announcements SET active = true WHERE id = 'maint-2026-07-06';
```

Any subsequent call now returns `"system": { "status": "maintenance", "retry_after": 3600 }`.
Flip back to restore service — session state is server-side, so a session resumes exactly where
it left off once maintenance clears.

## Known limitations (POC scope)

- **No real auth or KMS.** Bearer tokens are stub values and PII columns are stored in the clear,
  with TODOs marking where AES-GCM/KMS encryption-at-rest would go. PIN and session token on iOS use
  `UserDefaults` rather than Keychain/Secure Enclave, for the same reason.
- **No automated tests.** Correctness is exercised via `backend/scripts/smoke.sh` (full 10-page
  flow, idempotency replay, stale-T&C checks) and manual exploration — no `go test` or XCUITest
  suite yet.
- **Rate limiter, idempotency cache, and translation cache are in-process/in-memory** — fine for
  a single instance, not for multiple replicas. TODOs point at Redis. Idempotency records are
  also persisted to Postgres, so replay protection survives a restart even though the cache doesn't.
- **Camera, signature, and PIN are native-only by design** — these steps need device hardware
  (camera, biometrics-backed secure input) that a browser can't provide, so the web renderer ships
  simple stand-ins (file picker / canvas / plain input) instead, so the whole flow can still be
  walked in a browser. On iOS, blur detection in the camera step is a stub (always "not blurry");
  face presence via Vision is real.
- **T&C HTML is injected via `innerHTML`** with no sanitizer allow-list yet — acceptable while
  legal copy is same-origin and ops-controlled, worth revisiting if that source ever changes.
- **Web config (base URL, session, token) lives in `localStorage`** for standalone demo use.
  Production design has the native shell inject a short-lived token via a JS bridge on every
  load, with nothing persisted — see the comment in `web/js/config.js`.
- **No in-process "publish" endpoint for flow versions** — `flow_versions.published_at` exists
  but isn't gated against current time; the v14→v15 repair demo above uses a seed-holdback
  instead.
- **Some wire-format names are POC inventions**, made up while turning the design into JSON rather
  than carried over from an existing spec. `docs/contract.md` is the binding source of truth for
  names like repair kinds (`reaccept_document`/`collect_fields`/`redo_step`), error codes
  (`validation_failed`/`stale_document`/`idempotency_key_reused`), and the `force_update`
  capability-gate shape.
- **iPhone-only** — no iPad or rotation support, by design.
