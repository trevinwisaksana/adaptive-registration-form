-- Initial schema for the adaptive registration form POC.
-- Applied once by the migration runner in internal/dbx on startup.

CREATE EXTENSION IF NOT EXISTS pgcrypto; -- for gen_random_uuid()

CREATE TABLE IF NOT EXISTS flow_versions (
    flow_key     text NOT NULL,
    version      integer NOT NULL,
    definition   jsonb NOT NULL,
    published_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (flow_key, version)
);

CREATE TABLE IF NOT EXISTS sessions (
    id                  uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    token               text NOT NULL UNIQUE,
    flow_key            text NOT NULL,
    flow_version        integer NOT NULL,      -- current pinned version; may be soft-upgraded on reconcile
    original_flow_version integer NOT NULL,    -- version the session was created on; never changes
    locale              text NOT NULL DEFAULT 'en-US',
    status              text NOT NULL DEFAULT 'in_progress', -- in_progress|verifying|approved|rejected|expired
    pin_set             boolean NOT NULL DEFAULT false,
    blocked_min_version text,       -- set when client capabilities can't render this flow (force_update gate)
    device_id           text,
    platform            text,
    app_version          text,
    created_at          timestamptz NOT NULL DEFAULT now(),
    updated_at          timestamptz NOT NULL DEFAULT now(),
    expires_at          timestamptz NOT NULL,
    FOREIGN KEY (flow_key, flow_version) REFERENCES flow_versions (flow_key, version)
);

CREATE TABLE IF NOT EXISTS step_submissions (
    id         uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    session_id uuid NOT NULL REFERENCES sessions (id) ON DELETE CASCADE,
    step_id    text NOT NULL,
    payload    jsonb NOT NULL DEFAULT '{}',
    status     text NOT NULL DEFAULT 'submitted', -- submitted|invalidated
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    UNIQUE (session_id, step_id)
);

CREATE TABLE IF NOT EXISTS idempotency_keys (
    session_id      uuid NOT NULL REFERENCES sessions (id) ON DELETE CASCADE,
    step_id         text NOT NULL,
    idempotency_key text NOT NULL,
    request_hash    text NOT NULL,
    status_code     integer NOT NULL,
    response_body   text NOT NULL, -- raw response bytes, verbatim (NOT jsonb: jsonb re-serializes and
                                    -- reorders keys, which would break "replay returns the original
                                    -- response verbatim", contract §2.3)
    created_at      timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (session_id, step_id, idempotency_key)
);

CREATE TABLE IF NOT EXISTS documents (
    session_id    uuid NOT NULL REFERENCES sessions (id) ON DELETE CASCADE,
    kind          text NOT NULL, -- id_card|selfie|signature
    upload_ref    text NOT NULL UNIQUE,
    object_key    text NOT NULL,
    content_type  text,
    size_bytes    bigint,
    sha256        text,
    review_status text NOT NULL DEFAULT 'pending', -- pending|checked|rejected
    uploaded_at   timestamptz,
    created_at    timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (session_id, kind)
);

CREATE TABLE IF NOT EXISTS consents (
    id          uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    session_id  uuid NOT NULL REFERENCES sessions (id) ON DELETE CASCADE,
    doc_kind    text NOT NULL,
    doc_version text NOT NULL,
    doc_locale  text NOT NULL,
    doc_sha256  text NOT NULL,
    accepted_at timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS audit_log (
    id         bigserial PRIMARY KEY,
    session_id uuid REFERENCES sessions (id) ON DELETE CASCADE,
    event      text NOT NULL,
    detail     jsonb NOT NULL DEFAULT '{}',
    at         timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS on_complete_runs (
    session_id  uuid NOT NULL REFERENCES sessions (id) ON DELETE CASCADE,
    adapter     text NOT NULL,
    attempt     integer NOT NULL DEFAULT 1,
    status      text NOT NULL DEFAULT 'pending', -- pending|approved|rejected
    reason      text,
    started_at  timestamptz NOT NULL DEFAULT now(),
    finished_at timestamptz,
    PRIMARY KEY (session_id, adapter)
);

CREATE TABLE IF NOT EXISTS ref_datasets (
    key     text PRIMARY KEY,
    version integer NOT NULL DEFAULT 1
);

CREATE TABLE IF NOT EXISTS ref_items (
    id          bigserial PRIMARY KEY,
    dataset_key text NOT NULL REFERENCES ref_datasets (key) ON DELETE CASCADE,
    code        text NOT NULL,
    parent_code text,
    labels      jsonb NOT NULL DEFAULT '{}', -- {"en-US": "Bekasi", "id-ID": "Bekasi"}
    active      boolean NOT NULL DEFAULT true,
    sort        integer NOT NULL DEFAULT 0,
    UNIQUE (dataset_key, code)
);
CREATE INDEX IF NOT EXISTS idx_ref_items_dataset_parent ON ref_items (dataset_key, parent_code);

CREATE TABLE IF NOT EXISTS translations (
    key    text NOT NULL,
    locale text NOT NULL,
    text   text NOT NULL,
    PRIMARY KEY (key, locale)
);

CREATE TABLE IF NOT EXISTS legal_docs (
    kind         text NOT NULL,
    version      text NOT NULL,
    locale       text NOT NULL,
    sha256       text NOT NULL,
    content_type text NOT NULL DEFAULT 'text/html',
    content      text NOT NULL,
    effective_at timestamptz NOT NULL,
    reacceptance text NOT NULL DEFAULT 'required', -- required|editorial
    PRIMARY KEY (kind, version, locale)
);

CREATE TABLE IF NOT EXISTS announcements (
    id             text PRIMARY KEY,
    severity       text NOT NULL DEFAULT 'info',
    scope          text NOT NULL DEFAULT 'global', -- global | a step id
    status_override text NOT NULL DEFAULT 'ok',    -- ok|degraded|maintenance
    retry_after    integer,
    active         boolean NOT NULL DEFAULT true,
    starts_at      timestamptz,
    ends_at        timestamptz,
    text_by_locale jsonb NOT NULL DEFAULT '{}'
);
