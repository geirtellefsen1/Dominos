-- Per-user OAuth accounts for source systems (spec §3.6). Phase 6 only
-- uses provider='graph' (Microsoft 365); the table leaves room for
-- Slack, Drive, Calendar, etc. later.
--
-- refresh_token_ciphertext is opaque bytes: AES-256-GCM(plaintext) with
-- a 12-byte nonce prefixed. Encrypted at rest so a DB leak alone doesn't
-- give the attacker live mailbox access.

CREATE TABLE IF NOT EXISTS connector_accounts (
    id                         UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id                    UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    provider                   TEXT NOT NULL,                 -- 'graph'
    external_account_id        TEXT NOT NULL,                 -- mailbox email
    refresh_token_ciphertext   BYTEA NOT NULL,
    last_synced_at             TIMESTAMPTZ,
    last_error                 TEXT,
    created_at                 TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at                 TIMESTAMPTZ NOT NULL DEFAULT now(),
    revoked_at                 TIMESTAMPTZ,
    UNIQUE (user_id, provider)
);

CREATE INDEX IF NOT EXISTS connector_accounts_provider_active_idx
    ON connector_accounts (provider) WHERE revoked_at IS NULL;
