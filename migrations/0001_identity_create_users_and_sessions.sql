-- +goose Up
-- Timestamps are Unix milliseconds (UTC).
CREATE TABLE users (
    id                 TEXT    PRIMARY KEY,
    username           TEXT    NOT NULL UNIQUE,
    password_hash      TEXT,
    oidc_issuer        TEXT,
    oidc_subject       TEXT,
    role               TEXT    NOT NULL CHECK (role IN ('admin', 'user')),
    status             TEXT    NOT NULL CHECK (status IN ('active', 'disabled')),
    session_generation INTEGER NOT NULL DEFAULT 0,
    created_at         INTEGER NOT NULL,
    CHECK ((oidc_issuer IS NULL) = (oidc_subject IS NULL)),
    CHECK (password_hash IS NOT NULL OR oidc_issuer IS NOT NULL)
) STRICT;

CREATE UNIQUE INDEX users_oidc_identity ON users (oidc_issuer, oidc_subject) WHERE oidc_issuer IS NOT NULL;

CREATE TABLE sessions (
    id                      TEXT    PRIMARY KEY,
    user_id                 TEXT    NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    token_hash              TEXT    NOT NULL UNIQUE,
    user_session_generation INTEGER NOT NULL,
    created_at              INTEGER NOT NULL,
    last_seen_at            INTEGER NOT NULL,
    absolute_expires_at     INTEGER NOT NULL,
    idle_expires_at         INTEGER NOT NULL,
    revoked_at              INTEGER
) STRICT;

CREATE INDEX sessions_user_id ON sessions (user_id);

-- +goose Down
DROP TABLE sessions;
DROP TABLE users;
