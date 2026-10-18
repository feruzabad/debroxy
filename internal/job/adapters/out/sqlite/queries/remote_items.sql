-- name: UpsertRemoteItem :exec
INSERT INTO remote_items (
    id, provider_id, provider_item_id, provider_hash, kind, fingerprint, source_json, name, size_bytes, state,
    failure_code, failure_detail, bundle_format, bundle_range_supported, per_file_delivery, expires_at,
    telemetry_json, last_progress_at, last_observation_at, submission_json, charge_holder_job_id,
    charge_holder_released, created_at, deleted_at
) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
ON CONFLICT (id) DO UPDATE SET
    provider_item_id = excluded.provider_item_id, provider_hash = excluded.provider_hash,
    source_json = excluded.source_json, name = excluded.name, size_bytes = excluded.size_bytes,
    state = excluded.state, failure_code = excluded.failure_code, failure_detail = excluded.failure_detail,
    bundle_format = excluded.bundle_format, bundle_range_supported = excluded.bundle_range_supported,
    per_file_delivery = excluded.per_file_delivery, expires_at = excluded.expires_at,
    telemetry_json = excluded.telemetry_json, last_progress_at = excluded.last_progress_at,
    last_observation_at = excluded.last_observation_at, submission_json = excluded.submission_json,
    charge_holder_job_id = excluded.charge_holder_job_id, charge_holder_released = excluded.charge_holder_released,
    deleted_at = excluded.deleted_at;

-- name: RemoteItemByID :one
SELECT * FROM remote_items WHERE id = ?;

-- name: ClaimableItemByFingerprint :one
SELECT * FROM remote_items
WHERE provider_id = ? AND fingerprint = ?
  AND state IN ('submitting', 'queued', 'resolving_metadata', 'awaiting_file_selection', 'downloading', 'processing', 'ready');

-- name: RemoteItemIDOfJob :one
SELECT remote_item_id FROM jobs WHERE id = ?;

-- name: RemoteItemsOfProvider :many
SELECT * FROM remote_items WHERE provider_id = ? AND state != 'deleted' ORDER BY created_at;

-- name: RemoteItemsInStates :many
SELECT * FROM remote_items WHERE state IN (sqlc.slice('states')) ORDER BY created_at;

-- name: FilesOfItem :many
SELECT * FROM remote_files WHERE remote_item_id = ? ORDER BY position;

-- name: DeleteFilesOfItem :exec
DELETE FROM remote_files WHERE remote_item_id = ?;

-- name: InsertRemoteFile :exec
INSERT INTO remote_files (remote_item_id, position, file_id, path, size_bytes) VALUES (?, ?, ?, ?, ?);

-- name: JobsOfItem :many
SELECT * FROM jobs WHERE remote_item_id = ? ORDER BY claimed_at, id;

-- name: DeleteJobsOfItem :exec
DELETE FROM jobs WHERE remote_item_id = ?;

-- name: InsertJob :exec
INSERT INTO jobs (id, remote_item_id, owner_id, owner_username, display_name, claimed_at, origin) VALUES (?, ?, ?, ?, ?, ?, ?);

-- name: InsertHistoryEntry :exec
INSERT INTO job_history (
    job_id, owner_id, owner_username, display_name, remote_item_id, provider_id, kind, fingerprint, name,
    final_state, failure_code, failure_detail, claimed_at, released_at
) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?);

-- name: InsertOutboxMessage :exec
INSERT INTO outbox_messages (kind, remote_item_id, created_at, next_attempt_at) VALUES (?, ?, ?, ?);

-- name: InsertSubmitWindow :exec
INSERT INTO submit_rate_windows (user_id, class, accepted_at) VALUES (?, ?, ?);

-- name: CountActiveChargedItems :one
-- Same predicate as RemoteItem.IsChargedTo (ADR 0006 rule 10).
SELECT count(DISTINCT r.id) FROM remote_items r
JOIN jobs j ON j.remote_item_id = r.id
WHERE j.owner_id = ?
  AND r.state IN ('submitting', 'queued', 'resolving_metadata', 'awaiting_file_selection', 'downloading', 'processing')
  AND (r.charge_holder_job_id = j.id OR r.charge_holder_released = 1);

-- name: SlotQuotaOf :one
SELECT * FROM slot_quotas WHERE user_id = ?;

-- name: UpsertSlotQuota :exec
INSERT INTO slot_quotas (user_id, max_active_jobs) VALUES (?, ?)
ON CONFLICT (user_id) DO UPDATE SET max_active_jobs = excluded.max_active_jobs;

-- name: CountSubmitsSince :one
SELECT count(*) FROM submit_rate_windows WHERE user_id = ? AND class = ? AND accepted_at > ?;

-- name: OldestSubmitSince :one
SELECT accepted_at FROM submit_rate_windows WHERE user_id = ? AND class = ? AND accepted_at > ? ORDER BY accepted_at ASC LIMIT 1;

-- name: ItemsHoldingProviderItem :many
SELECT id, state, created_at, deleted_at FROM remote_items WHERE provider_id = ? AND provider_item_id = ?;

-- name: ClaimableItemsMatching :many
SELECT id, state, created_at, deleted_at FROM remote_items
WHERE provider_id = sqlc.arg(provider_id)
  AND (fingerprint = sqlc.arg(fingerprint) OR (sqlc.arg(provider_hash) != '' AND provider_hash = sqlc.arg(provider_hash)))
  AND state IN ('submitting', 'queued', 'resolving_metadata', 'awaiting_file_selection', 'downloading', 'processing', 'ready');

-- name: CountParkedDeletions :one
SELECT count(*) FROM outbox_messages WHERE remote_item_id = ? AND parked_at IS NOT NULL AND processed_at IS NULL;
