-- AI identities (spec §3.4 + §3.5). Each agent has its own X.509 cert
-- signed by the Dominion internal CA; the SHA-256 thumbprint of the DER
-- encoding is the stable principal id the audit log and ACL engine see.

CREATE TABLE IF NOT EXISTS agents (
    id            UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    display_name  TEXT NOT NULL,
    thumbprint    TEXT UNIQUE NOT NULL,                     -- hex SHA-256 of cert DER
    cert_pem      TEXT NOT NULL,
    owner_user_id UUID REFERENCES users(id) ON DELETE SET NULL,
    active        BOOLEAN NOT NULL DEFAULT TRUE,
    created_at    TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at    TIMESTAMPTZ NOT NULL DEFAULT now(),
    revoked_at    TIMESTAMPTZ
);

CREATE INDEX IF NOT EXISTS agents_thumbprint_idx ON agents (thumbprint);
CREATE INDEX IF NOT EXISTS agents_owner_idx      ON agents (owner_user_id);
