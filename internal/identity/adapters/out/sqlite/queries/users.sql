-- name: InsertUser :exec
INSERT INTO users (id, username, password_hash, oidc_issuer, oidc_subject, role, status, session_generation, created_at)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?);

-- name: UpdateUser :exec
UPDATE users
SET password_hash = ?, oidc_issuer = ?, oidc_subject = ?, role = ?, status = ?, session_generation = ?
WHERE id = ?;

-- name: UserByID :one
SELECT * FROM users WHERE id = ?;

-- name: UserByUsername :one
SELECT * FROM users WHERE username = ?;

-- name: UserByOIDCIdentity :one
SELECT * FROM users WHERE oidc_issuer = ? AND oidc_subject = ?;

-- name: CountUsers :one
SELECT count(*) FROM users;

-- name: CountActiveAdmins :one
SELECT count(*) FROM users WHERE role = 'admin' AND status = 'active';

-- name: ListUsers :many
SELECT * FROM users ORDER BY username;
