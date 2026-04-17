-- Append-only, per-entry-signed audit log (spec §3.7).
-- Partitioned by month on `timestamp` so old months can be pruned or
-- archived as whole partitions. Concrete monthly partitions are created
-- at gateway startup by internal/audit.EnsurePartitions.

CREATE TABLE IF NOT EXISTS audit_events (
    id            UUID        NOT NULL,
    timestamp     TIMESTAMPTZ NOT NULL,
    actor         TEXT        NOT NULL,
    on_behalf_of  TEXT,
    action        TEXT        NOT NULL,
    resource      TEXT,
    decision      TEXT        NOT NULL, -- 'allow' | 'deny' | 'error'
    context       JSONB,
    signature     TEXT        NOT NULL, -- base64 Ed25519 signature
    key_id        TEXT        NOT NULL, -- first 8 bytes of the pubkey, hex
    PRIMARY KEY (timestamp, id)
) PARTITION BY RANGE (timestamp);

CREATE INDEX IF NOT EXISTS audit_events_actor_idx  ON audit_events (actor);
CREATE INDEX IF NOT EXISTS audit_events_action_idx ON audit_events (action);
CREATE INDEX IF NOT EXISTS audit_events_resource_idx ON audit_events (resource);
