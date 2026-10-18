-- name: PruneJobHistory :execrows
DELETE FROM job_history WHERE released_at < ?;

-- name: PruneUnmanagedItems :execrows
DELETE FROM unmanaged_items WHERE last_seen_at < ?;

-- name: PruneProcessedOutboxMessages :execrows
DELETE FROM outbox_messages WHERE processed_at IS NOT NULL AND processed_at < ?;

-- name: PruneDeletedRemoteItems :execrows
DELETE FROM remote_items WHERE state = 'deleted' AND deleted_at < ?;

-- name: PruneSubmitRateWindows :execrows
DELETE FROM submit_rate_windows WHERE accepted_at < ?;
