-- name: InsertSession :exec
INSERT INTO sessions (id, user_id, token_hash, user_session_generation, created_at, last_seen_at, absolute_expires_at, idle_expires_at, revoked_at)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?);

-- name: UpdateSession :exec
UPDATE sessions
SET user_session_generation = ?, last_seen_at = ?, idle_expires_at = ?, revoked_at = ?
WHERE id = ?;

-- name: SessionByID :one
SELECT * FROM sessions WHERE id = ?;

-- name: SessionForAuthentication :one
SELECT s.id AS session_id, s.user_id, s.user_session_generation, s.last_seen_at, s.absolute_expires_at,
       s.idle_expires_at, s.revoked_at, u.role, u.status, u.session_generation
FROM sessions s
JOIN users u ON u.id = s.user_id
WHERE s.token_hash = ?;

-- name: PruneSessions :execrows
DELETE FROM sessions WHERE revoked_at IS NOT NULL OR absolute_expires_at <= ? OR idle_expires_at <= ?;
