-- OfferLog initial schema.
-- Every business table carries owner_id + timestamps; cross-table references
-- are validated within the owning user's scope (see service layer).

CREATE EXTENSION IF NOT EXISTS pgcrypto;

-- ---------------------------------------------------------------------------
-- Identity
-- ---------------------------------------------------------------------------
CREATE TABLE users (
    id            BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    email         TEXT NOT NULL UNIQUE,
    password_hash TEXT NOT NULL,
    display_name  TEXT NOT NULL DEFAULT '',
    timezone      TEXT NOT NULL DEFAULT 'Europe/Dublin',
    locale        TEXT NOT NULL DEFAULT 'zh-CN',
    is_admin      BOOLEAN NOT NULL DEFAULT FALSE,
    created_at    TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at    TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE sessions (
    id         UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id    BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    token_hash TEXT NOT NULL UNIQUE,
    csrf_token TEXT NOT NULL,
    user_agent TEXT NOT NULL DEFAULT '',
    ip         TEXT NOT NULL DEFAULT '',
    expires_at TIMESTAMPTZ NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    last_seen  TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX sessions_user_id_idx ON sessions(user_id);
CREATE INDEX sessions_expires_at_idx ON sessions(expires_at);

-- ---------------------------------------------------------------------------
-- Companies
-- ---------------------------------------------------------------------------
CREATE TABLE companies (
    id         BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    owner_id   BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    name       TEXT NOT NULL,
    website    TEXT NOT NULL DEFAULT '',
    notes      TEXT NOT NULL DEFAULT '',
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (owner_id, name)
);
CREATE INDEX companies_owner_name_idx ON companies(owner_id, name);

-- ---------------------------------------------------------------------------
-- Applications
-- ---------------------------------------------------------------------------
CREATE TABLE applications (
    id               BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    owner_id         BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    company_id       BIGINT NOT NULL REFERENCES companies(id),
    company_name     TEXT NOT NULL,             -- denormalized snapshot for display
    position         TEXT NOT NULL,
    job_url          TEXT NOT NULL DEFAULT '',
    jd_snapshot      TEXT NOT NULL DEFAULT '',
    location         TEXT NOT NULL DEFAULT '',
    remote_policy    TEXT NOT NULL DEFAULT '',  -- remote / hybrid / onsite
    employment_type  TEXT NOT NULL DEFAULT '',  -- full-time / contract / intern
    salary_min       BIGINT,
    salary_max       BIGINT,
    salary_currency  TEXT NOT NULL DEFAULT '',
    channel          TEXT NOT NULL DEFAULT '',
    status           TEXT NOT NULL DEFAULT 'saved',
    priority         TEXT NOT NULL DEFAULT 'medium',   -- high / medium / low
    tags             TEXT[] NOT NULL DEFAULT '{}',
    custom_values    JSONB NOT NULL DEFAULT '{}',
    notes            TEXT NOT NULL DEFAULT '',
    saved_at         TIMESTAMPTZ,
    submitted_at     TIMESTAMPTZ,
    first_response_at TIMESTAMPTZ,
    deadline         DATE,
    accepted_at      TIMESTAMPTZ,
    rejected_at      TIMESTAMPTZ,
    reason           TEXT NOT NULL DEFAULT '',
    next_action      TEXT NOT NULL DEFAULT '',
    next_action_due_at DATE,
    next_action_due_ts TIMESTAMPTZ,
    version          INTEGER NOT NULL DEFAULT 1,
    archived_at      TIMESTAMPTZ,
    deleted_at       TIMESTAMPTZ,
    previous_application_id BIGINT REFERENCES applications(id),
    created_at       TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at       TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX applications_owner_status_idx ON applications(owner_id, status) WHERE deleted_at IS NULL;
CREATE INDEX applications_owner_submitted_idx ON applications(owner_id, submitted_at) WHERE deleted_at IS NULL;
CREATE INDEX applications_owner_due_idx ON applications(owner_id, next_action_due_at, next_action_due_ts) WHERE deleted_at IS NULL;
CREATE INDEX applications_owner_created_idx ON applications(owner_id, created_at);
CREATE INDEX applications_owner_company_idx ON applications(owner_id, company_id);

-- ---------------------------------------------------------------------------
-- Application events (status timeline; authoritative audit trail)
-- ---------------------------------------------------------------------------
CREATE TABLE application_events (
    id            BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    application_id BIGINT NOT NULL REFERENCES applications(id) ON DELETE CASCADE,
    owner_id      BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    sequence      INTEGER NOT NULL,
    event_type    TEXT NOT NULL DEFAULT 'status_change', -- status_change | correction | note | created
    from_status   TEXT,
    to_status     TEXT,
    note          TEXT NOT NULL DEFAULT '',
    reason        TEXT NOT NULL DEFAULT '',
    occurred_at   TIMESTAMPTZ NOT NULL,
    recorded_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
    corrects_event_id BIGINT REFERENCES application_events(id),
    actor_id      BIGINT REFERENCES users(id),
    UNIQUE (application_id, sequence)
);
CREATE INDEX events_app_occurred_idx ON application_events(application_id, occurred_at, sequence);
CREATE INDEX events_owner_idx ON application_events(owner_id, occurred_at);

-- ---------------------------------------------------------------------------
-- Interviews (rounds are first-class rows, not top-level statuses)
-- ---------------------------------------------------------------------------
CREATE TABLE interviews (
    id            BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    application_id BIGINT NOT NULL REFERENCES applications(id) ON DELETE CASCADE,
    owner_id      BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    round_name    TEXT NOT NULL DEFAULT '',        -- 一面 / 二面 / 技术面 ...
    format        TEXT NOT NULL DEFAULT '',        -- phone / video / onsite / takehome
    scheduled_at  TIMESTAMPTZ,
    timezone      TEXT NOT NULL DEFAULT 'Europe/Dublin',
    duration_minutes INTEGER,
    result        TEXT NOT NULL DEFAULT '',        -- pending / passed / failed
    feedback      TEXT NOT NULL DEFAULT '',
    notes         TEXT NOT NULL DEFAULT '',
    created_at    TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at    TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX interviews_app_idx ON interviews(application_id, scheduled_at);

-- ---------------------------------------------------------------------------
-- Next actions + reminders
-- ---------------------------------------------------------------------------
CREATE TABLE actions (
    id            BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    application_id BIGINT REFERENCES applications(id) ON DELETE CASCADE,
    owner_id      BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    title         TEXT NOT NULL,
    due_date      DATE,
    due_ts        TIMESTAMPTZ,
    done_at       TIMESTAMPTZ,
    remind_me     BOOLEAN NOT NULL DEFAULT FALSE,
    remind_at     TIMESTAMPTZ,
    created_at    TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at    TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX actions_owner_due_idx ON actions(owner_id, done_at, due_date, due_ts);

-- ---------------------------------------------------------------------------
-- Notes
-- ---------------------------------------------------------------------------
CREATE TABLE notes (
    id             BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    application_id BIGINT NOT NULL REFERENCES applications(id) ON DELETE CASCADE,
    owner_id       BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    content_md     TEXT NOT NULL,
    created_at     TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at     TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX notes_app_idx ON notes(application_id, created_at);

-- ---------------------------------------------------------------------------
-- Custom property definitions (type, options, validation)
-- ---------------------------------------------------------------------------
CREATE TABLE property_definitions (
    id            BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    owner_id      BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    name          TEXT NOT NULL,
    key           TEXT NOT NULL,
    data_type     TEXT NOT NULL DEFAULT 'text',   -- text | number | select | multi_select | date | checkbox | url
    options       JSONB NOT NULL DEFAULT '[]',    -- [{id,label}]
    required      BOOLEAN NOT NULL DEFAULT FALSE,
    "order"       INTEGER NOT NULL DEFAULT 0,
    schema_version INTEGER NOT NULL DEFAULT 1,
    created_at    TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at    TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (owner_id, key)
);
CREATE INDEX property_defs_owner_idx ON property_definitions(owner_id);

-- ---------------------------------------------------------------------------
-- Saved views (shared filter/sort/group/column config)
-- ---------------------------------------------------------------------------
CREATE TABLE saved_views (
    id            BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    owner_id      BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    name          TEXT NOT NULL,
    layout        TEXT NOT NULL DEFAULT 'table',  -- table | board | list
    columns       JSONB NOT NULL DEFAULT '[]',    -- [{field,width,visible}]
    filter_ast    JSONB NOT NULL DEFAULT '[]',    -- AND/OR tree {op, conditions}
    sort          JSONB NOT NULL DEFAULT '[]',    -- [{field,dir}]
    group_by      JSONB NOT NULL DEFAULT '{}',    -- {field}
    is_builtin    BOOLEAN NOT NULL DEFAULT FALSE,
    schema_version INTEGER NOT NULL DEFAULT 1,
    created_at    TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at    TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX saved_views_owner_idx ON saved_views(owner_id);

-- ---------------------------------------------------------------------------
-- Files & application-file associations
-- ---------------------------------------------------------------------------
CREATE TABLE files (
    id            UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    owner_id      BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    object_key    TEXT NOT NULL,               -- staging key while pending
    final_key     TEXT NOT NULL DEFAULT '',    -- owners/{id}/files/{uuid}/content
    original_name TEXT NOT NULL,
    content_type  TEXT NOT NULL DEFAULT '',
    size_bytes    BIGINT NOT NULL DEFAULT 0,
    sha256        TEXT NOT NULL DEFAULT '',
    status        TEXT NOT NULL DEFAULT 'pending', -- pending | validating | ready | failed | deleting | deleted
    category      TEXT NOT NULL DEFAULT '',        -- resume | cover_letter | offer | other
    error_message TEXT NOT NULL DEFAULT '',
    used_by_application BIGINT REFERENCES applications(id),
    is_resume_version BOOLEAN NOT NULL DEFAULT FALSE,
    created_at    TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at    TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX files_owner_idx ON files(owner_id, status);
CREATE INDEX files_status_idx ON files(status);

CREATE TABLE application_files (
    application_id BIGINT NOT NULL REFERENCES applications(id) ON DELETE CASCADE,
    file_id        UUID NOT NULL REFERENCES files(id) ON DELETE CASCADE,
    owner_id       BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    purpose        TEXT NOT NULL DEFAULT '',
    is_resume      BOOLEAN NOT NULL DEFAULT FALSE,
    created_at     TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (application_id, file_id)
);
CREATE INDEX app_files_file_idx ON application_files(file_id);

-- ---------------------------------------------------------------------------
-- Persistent jobs / outbox
-- ---------------------------------------------------------------------------
CREATE TABLE jobs (
    id            BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    kind          TEXT NOT NULL,              -- reminder | cleanup | export | import | file_delete
    status        TEXT NOT NULL DEFAULT 'pending', -- pending | running | done | failed | cancelled
    payload       JSONB NOT NULL DEFAULT '{}',
    idempotency_key TEXT NOT NULL DEFAULT '',
    attempts      INTEGER NOT NULL DEFAULT 0,
    max_attempts  INTEGER NOT NULL DEFAULT 5,
    lease_until   TIMESTAMPTZ,
    next_run_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
    last_error    TEXT NOT NULL DEFAULT '',
    run_after     TIMESTAMPTZ,
    created_at    TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at    TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX jobs_lease_idx ON jobs(status, next_run_at) WHERE status IN ('pending','failed');
CREATE UNIQUE INDEX jobs_idem_key_idx ON jobs(kind, idempotency_key) WHERE idempotency_key <> '';

-- ---------------------------------------------------------------------------
-- Import batches
-- ---------------------------------------------------------------------------
CREATE TABLE import_batches (
    id            BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    owner_id      BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    filename      TEXT NOT NULL DEFAULT '',
    status        TEXT NOT NULL DEFAULT 'preview', -- preview | committed
    field_mapping JSONB NOT NULL DEFAULT '{}',
    preview       JSONB NOT NULL DEFAULT '{}',     -- report: rows, errors, duplicates
    created_at    TIMESTAMPTZ NOT NULL DEFAULT now(),
    committed_at  TIMESTAMPTZ
);
CREATE INDEX import_batches_owner_idx ON import_batches(owner_id);

-- ---------------------------------------------------------------------------
-- Analytics snapshot cache (short-lived; token-bound drilldown)
-- ---------------------------------------------------------------------------
CREATE TABLE analytics_snapshots (
    id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    owner_id    BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    mode        TEXT NOT NULL,               -- sankey_current | sankey_history | summary | trends | channel
    payload     JSONB NOT NULL,
    member_ids  BIGINT[] NOT NULL DEFAULT '{}',
    token       TEXT NOT NULL UNIQUE,
    filter_fingerprint TEXT NOT NULL DEFAULT '',
    expires_at  TIMESTAMPTZ NOT NULL,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX snapshots_owner_expiry_idx ON analytics_snapshots(owner_id, expires_at);

CREATE TABLE outbox (
    id          BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    kind        TEXT NOT NULL,
    payload     JSONB NOT NULL DEFAULT '{}',
    idempotency_key TEXT NOT NULL DEFAULT '',
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    processed_at TIMESTAMPTZ
);
CREATE INDEX outbox_processed_idx ON outbox(processed_at) WHERE processed_at IS NULL;
