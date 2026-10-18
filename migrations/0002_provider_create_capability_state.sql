-- +goose Up
-- Learned capability state per Provider Account (DEC-21, ADR 0003 rule 9).
-- A rebuildable cache: deleting it costs one extra provider call per capability.
CREATE TABLE provider_capability_state (
    provider_id TEXT    NOT NULL,
    capability  TEXT    NOT NULL,
    state       TEXT    NOT NULL CHECK (state IN ('available', 'unavailable', 'unverified')),
    reason      TEXT,
    since       INTEGER,
    PRIMARY KEY (provider_id, capability)
) STRICT;

-- +goose Down
DROP TABLE provider_capability_state;
