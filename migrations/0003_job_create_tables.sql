-- +goose Up
-- The job context (spec §5.8). Timestamps are Unix milliseconds (UTC).
CREATE TABLE remote_items (
    id                     TEXT    PRIMARY KEY,
    provider_id            TEXT    NOT NULL,
    provider_item_id       TEXT,
    provider_hash          TEXT    NOT NULL DEFAULT '',
    kind                   TEXT    NOT NULL,
    fingerprint            TEXT    NOT NULL,
    source_json            TEXT,
    name                   TEXT    NOT NULL DEFAULT '',
    size_bytes             INTEGER NOT NULL DEFAULT 0,
    state                  TEXT    NOT NULL,
    failure_code           TEXT    NOT NULL DEFAULT '',
    failure_detail         TEXT    NOT NULL DEFAULT '',
    bundle_format          TEXT,
    bundle_range_supported INTEGER NOT NULL DEFAULT 0,
    per_file_delivery      INTEGER NOT NULL DEFAULT 0,
    expires_at             INTEGER,
    telemetry_json         TEXT    NOT NULL DEFAULT '{}',
    last_progress_at       INTEGER NOT NULL,
    last_observation_at    INTEGER NOT NULL DEFAULT 0,
    submission_json        TEXT,
    charge_holder_job_id   TEXT,
    charge_holder_released INTEGER NOT NULL DEFAULT 0,
    created_at             INTEGER NOT NULL,
    deleted_at             INTEGER
) STRICT;

-- Uniqueness only among claimable items (ADR 0007 rule 2).
CREATE UNIQUE INDEX remote_items_claimable_fingerprint ON remote_items (provider_id, fingerprint)
    WHERE state IN ('submitting', 'queued', 'resolving_metadata', 'awaiting_file_selection', 'downloading', 'processing', 'ready');
CREATE UNIQUE INDEX remote_items_claimable_provider_hash ON remote_items (provider_id, provider_hash)
    WHERE provider_hash != '' AND state IN ('submitting', 'queued', 'resolving_metadata', 'awaiting_file_selection', 'downloading', 'processing', 'ready');
CREATE UNIQUE INDEX remote_items_claimable_provider_item ON remote_items (provider_id, provider_item_id)
    WHERE provider_item_id IS NOT NULL AND state IN ('submitting', 'queued', 'resolving_metadata', 'awaiting_file_selection', 'downloading', 'processing', 'ready');
CREATE INDEX remote_items_provider_item ON remote_items (provider_id, provider_item_id);
CREATE INDEX remote_items_state ON remote_items (state);

CREATE TABLE remote_files (
    remote_item_id TEXT    NOT NULL REFERENCES remote_items (id) ON DELETE CASCADE,
    position       INTEGER NOT NULL,
    file_id        TEXT    NOT NULL,
    path           TEXT    NOT NULL,
    size_bytes     INTEGER NOT NULL,
    PRIMARY KEY (remote_item_id, file_id)
) STRICT;

-- Unreleased jobs: the claims (ADR 0004).
CREATE TABLE jobs (
    id             TEXT    PRIMARY KEY,
    remote_item_id TEXT    NOT NULL REFERENCES remote_items (id) ON DELETE CASCADE,
    owner_id       TEXT    NOT NULL,
    owner_username TEXT    NOT NULL,
    display_name   TEXT    NOT NULL,
    claimed_at     INTEGER NOT NULL,
    origin         TEXT    NOT NULL,
    UNIQUE (remote_item_id, owner_id)
) STRICT;
CREATE INDEX jobs_owner ON jobs (owner_id);

-- Released jobs (spec §5.1).
CREATE TABLE job_history (
    job_id         TEXT    PRIMARY KEY,
    owner_id       TEXT    NOT NULL,
    owner_username TEXT    NOT NULL,
    display_name   TEXT    NOT NULL,
    remote_item_id TEXT    NOT NULL,
    provider_id    TEXT    NOT NULL,
    kind           TEXT    NOT NULL,
    fingerprint    TEXT    NOT NULL,
    name           TEXT    NOT NULL,
    final_state    TEXT    NOT NULL,
    failure_code   TEXT    NOT NULL DEFAULT '',
    failure_detail TEXT    NOT NULL DEFAULT '',
    claimed_at     INTEGER NOT NULL,
    released_at    INTEGER NOT NULL
) STRICT;
CREATE INDEX job_history_owner ON job_history (owner_id, released_at);

CREATE TABLE slot_quotas (
    user_id         TEXT    PRIMARY KEY,
    max_active_jobs INTEGER NOT NULL
) STRICT;

-- One row per accepted submit (ADR 0013 rule 13).
CREATE TABLE submit_rate_windows (
    id          INTEGER PRIMARY KEY AUTOINCREMENT,
    user_id     TEXT    NOT NULL,
    class       TEXT    NOT NULL CHECK (class IN ('uncached', 'cached')),
    accepted_at INTEGER NOT NULL
) STRICT;
CREATE INDEX submit_rate_windows_user ON submit_rate_windows (user_id, class, accepted_at);

CREATE TABLE provider_sync_state (
    provider_id                TEXT    PRIMARY KEY,
    last_authoritative_sync_at INTEGER NOT NULL
) STRICT;

CREATE TABLE unmanaged_items (
    provider_id      TEXT    NOT NULL,
    provider_item_id TEXT    NOT NULL,
    kind             TEXT    NOT NULL,
    name             TEXT    NOT NULL,
    size_bytes       INTEGER NOT NULL,
    state            TEXT    NOT NULL,
    first_seen_at    INTEGER NOT NULL,
    last_seen_at     INTEGER NOT NULL,
    PRIMARY KEY (provider_id, provider_item_id)
) STRICT;

-- Transactional outbox (ADR 0008).
CREATE TABLE outbox_messages (
    id              INTEGER PRIMARY KEY AUTOINCREMENT,
    kind            TEXT    NOT NULL,
    remote_item_id  TEXT    NOT NULL,
    created_at      INTEGER NOT NULL,
    attempts        INTEGER NOT NULL DEFAULT 0,
    next_attempt_at INTEGER NOT NULL,
    last_error      TEXT    NOT NULL DEFAULT '',
    parked_at       INTEGER,
    processed_at    INTEGER
) STRICT;
CREATE INDEX outbox_messages_due ON outbox_messages (processed_at, parked_at, next_attempt_at);
CREATE INDEX outbox_messages_item ON outbox_messages (remote_item_id);

-- +goose Down
DROP TABLE outbox_messages;
DROP TABLE unmanaged_items;
DROP TABLE provider_sync_state;
DROP TABLE submit_rate_windows;
DROP TABLE slot_quotas;
DROP TABLE job_history;
DROP TABLE jobs;
DROP TABLE remote_files;
DROP TABLE remote_items;
