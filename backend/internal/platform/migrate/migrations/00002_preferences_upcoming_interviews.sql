-- OfferLog migration 00002: P0/P1 closure foundations.
--
--   * user_preferences: account profile + reminder prefs (settings previously
--     wrote only to localStorage). week_start Sunday=0..Saturday=6; keep the
--     full set so prefs survive renames and can be shown in UI.
--   * notifications: in-app reminders (overdue / interview-eve / stale-14d).
--     Generated server-side by the worker; read/unread and dismissable so the
--     same event cannot notify twice. Generic object (application_id) — for a
--     single-account app nothing else links yet; owner_id scoping preserved.
--   * schedule_links: per-interview scheduling metadata. Fields are nullable,
--     ISO-8601 with explicit original timezone; `cancelled` records a
--     cancellation (改期/取消) without deleting the row so dashboards can
--     exclude it and reminders stop.
--
-- Pure additive: nothing existing is altered or dropped.

CREATE TABLE IF NOT EXISTS user_preferences (
    user_id           BIGINT PRIMARY KEY REFERENCES users(id) ON DELETE CASCADE,
    display_name      TEXT NOT NULL DEFAULT '',
    timezone          TEXT NOT NULL DEFAULT 'Europe/Dublin',
    week_start        INTEGER NOT NULL DEFAULT 1,          -- 0=Sunday .. 6=Saturday
    remind_overdue    BOOLEAN NOT NULL DEFAULT TRUE,
    remind_interview  BOOLEAN NOT NULL DEFAULT TRUE,       -- 面试前一天
    remind_stale_days INTEGER NOT NULL DEFAULT 14,         -- 投递满 N 天未回复
    remind_weekly     BOOLEAN NOT NULL DEFAULT FALSE,
    locale            TEXT NOT NULL DEFAULT 'zh-CN',
    created_at        TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at        TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS notifications (
    id            BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    owner_id      BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    kind          TEXT NOT NULL,           -- overdue | interview | stale | weekly
    title         TEXT NOT NULL,
    body          TEXT NOT NULL DEFAULT '',
    application_id BIGINT REFERENCES applications(id) ON DELETE CASCADE,
    idempotency_key TEXT NOT NULL DEFAULT '',
    read_at       TIMESTAMPTZ,
    dismissed_at  TIMESTAMPTZ,
    created_at    TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS notifications_owner_unread_idx
    ON notifications(owner_id, read_at, dismissed_at, id DESC);
CREATE INDEX IF NOT EXISTS notifications_idem_idx
    ON notifications(owner_id, kind, application_id, idempotency_key)
    WHERE dismissed_at IS NULL AND read_at IS NULL;

CREATE TABLE IF NOT EXISTS schedule_links (
    id             BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    interview_id   BIGINT NOT NULL REFERENCES interviews(id) ON DELETE CASCADE,
    owner_id       BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    meeting_url    TEXT NOT NULL DEFAULT '',
    location       TEXT NOT NULL DEFAULT '',
    contact_name   TEXT NOT NULL DEFAULT '',
    contact_email  TEXT NOT NULL DEFAULT '',
    notes          TEXT NOT NULL DEFAULT '',
    cancelled      BOOLEAN NOT NULL DEFAULT FALSE,
    cancelled_reason TEXT NOT NULL DEFAULT '',
    original_timezone TEXT NOT NULL DEFAULT '',
    created_at     TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at     TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS schedule_links_owner_idx ON schedule_links(owner_id);
CREATE UNIQUE INDEX IF NOT EXISTS schedule_links_interview_uidx ON schedule_links(interview_id);

-- actions gain priority + source columns for the unified todo list (plan
-- §5.3). priority is a plain scalar (default medium); source marks rows that
-- were migrated from an application's legacy next_action so they can be told
-- apart and the migration can stay idempotent (never re-creating rows that
-- already exist).
ALTER TABLE actions ADD COLUMN IF NOT EXISTS priority TEXT NOT NULL DEFAULT 'medium';
ALTER TABLE actions ADD COLUMN IF NOT EXISTS source TEXT NOT NULL DEFAULT 'manual';

-- Idempotent backfill: every application that still has a legacy next_action
-- and no standalone action at all gets one open action carrying the content
-- and due date, marked source='next_action'. Re-running the migration cannot
-- create duplicates because the INSERT ... SELECT ... WHERE NOT EXISTS guard
-- is per-application.
INSERT INTO actions (application_id, owner_id, title, due_date, due_ts, done_at, remind_me, priority, source, created_at, updated_at)
SELECT ap.id, ap.owner_id,
       left(btrim(ap.next_action), 500),
       ap.next_action_due_at, ap.next_action_due_ts, NULL, FALSE, 'medium', 'next_action', now(), now()
FROM applications ap
WHERE ap.deleted_at IS NULL
  AND btrim(ap.next_action) <> ''
  AND NOT EXISTS (
      SELECT 1 FROM actions a
      WHERE a.application_id = ap.id AND a.owner_id = ap.owner_id
        AND a.source = 'next_action'
  );

-- v1 compatibility view over the reminders shape so the frontend can read
-- upcoming interviews + notifications in one dashboard call later.
CREATE OR REPLACE VIEW upcoming_interviews AS
SELECT i.id, i.application_id, i.owner_id, i.round_name, i.format,
       i.scheduled_at, i.timezone, i.duration_minutes, i.result, i.notes AS interview_notes,
       a.company_name, a.position,
       COALESCE(sl.cancelled, FALSE) AS cancelled,
       sl.meeting_url, sl.location, sl.contact_name
FROM interviews i
JOIN applications a ON a.id = i.application_id AND a.owner_id = i.owner_id
LEFT JOIN schedule_links sl ON sl.interview_id = i.id AND sl.owner_id = i.owner_id
WHERE i.owner_id IS NOT NULL;
