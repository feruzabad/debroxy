-- name: CapabilityStatesOfProvider :many
SELECT * FROM provider_capability_state WHERE provider_id = ?;

-- name: UpsertCapabilityState :exec
INSERT INTO provider_capability_state (provider_id, capability, state, reason, since)
VALUES (?, ?, ?, ?, ?)
ON CONFLICT (provider_id, capability) DO UPDATE SET state = excluded.state, reason = excluded.reason, since = excluded.since;
