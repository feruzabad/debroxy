-- name: DueOutboxMessages :many
SELECT * FROM outbox_messages
WHERE processed_at IS NULL AND parked_at IS NULL AND next_attempt_at <= ?
ORDER BY next_attempt_at, id LIMIT ?;

-- name: MarkOutboxMessageProcessed :exec
UPDATE outbox_messages SET processed_at = ? WHERE id = ?;

-- name: RecordOutboxFailure :exec
UPDATE outbox_messages SET attempts = attempts + 1, next_attempt_at = ?, last_error = ?, parked_at = ? WHERE id = ?;

-- name: UpsertSyncState :exec
INSERT INTO provider_sync_state (provider_id, last_authoritative_sync_at) VALUES (?, ?)
ON CONFLICT (provider_id) DO UPDATE SET last_authoritative_sync_at = excluded.last_authoritative_sync_at;

-- name: SyncStateOf :one
SELECT last_authoritative_sync_at FROM provider_sync_state WHERE provider_id = ?;

-- name: UpsertUnmanagedItem :exec
INSERT INTO unmanaged_items (provider_id, provider_item_id, kind, name, size_bytes, state, first_seen_at, last_seen_at)
VALUES (?, ?, ?, ?, ?, ?, ?, ?)
ON CONFLICT (provider_id, provider_item_id) DO UPDATE SET
    kind = excluded.kind, name = excluded.name, size_bytes = excluded.size_bytes, state = excluded.state,
    last_seen_at = excluded.last_seen_at;
