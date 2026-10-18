-- name: ItemIDsOfOwner :many
SELECT DISTINCT remote_item_id FROM jobs WHERE owner_id = ?;

-- name: ItemIDsWithJobs :many
SELECT DISTINCT remote_item_id FROM jobs;

-- name: HistoryOfOwner :many
SELECT * FROM job_history WHERE owner_id = ? ORDER BY released_at DESC;

-- name: HistoryEntryByJob :one
SELECT * FROM job_history WHERE job_id = ?;

-- name: UnmanagedItemList :many
SELECT * FROM unmanaged_items ORDER BY provider_id, last_seen_at DESC;

-- name: ParkedOutboxMessageList :many
SELECT * FROM outbox_messages WHERE parked_at IS NOT NULL AND processed_at IS NULL ORDER BY parked_at;
