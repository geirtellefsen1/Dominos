CREATE EXTENSION IF NOT EXISTS "pgcrypto";

CREATE TABLE IF NOT EXISTS schemas (
    id          TEXT PRIMARY KEY,
    json_schema JSONB NOT NULL,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS documents (
    id         UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    schema_id  TEXT NOT NULL REFERENCES schemas(id),
    tenant_id  UUID NOT NULL,
    body       JSONB NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    created_by TEXT NOT NULL,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_by TEXT NOT NULL,
    deleted_at TIMESTAMPTZ
);

CREATE INDEX IF NOT EXISTS documents_schema_id_idx ON documents (schema_id);
CREATE INDEX IF NOT EXISTS documents_body_gin_idx ON documents USING GIN (body);
CREATE INDEX IF NOT EXISTS documents_not_deleted_idx
    ON documents (schema_id, created_at DESC)
    WHERE deleted_at IS NULL;
