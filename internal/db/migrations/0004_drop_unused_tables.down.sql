-- Recreates the structures dropped by 0004.
--
-- This restores the SCHEMA only. Any rows that existed are gone — a DROP TABLE
-- is not recoverable from a down migration. Both tables were unpopulated by the
-- application, but if anything was inserted by hand it must be restored from a
-- backup, not from here.

CREATE TABLE modules (
    id         BIGSERIAL PRIMARY KEY,
    key        TEXT NOT NULL UNIQUE,
    label      TEXT NOT NULL,
    synced_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);

ALTER TABLE tool_groups ADD COLUMN synced_module_key TEXT REFERENCES modules(key);

CREATE TABLE connector_user_mappings (
    id               BIGSERIAL PRIMARY KEY,
    connector_api_id BIGINT NOT NULL REFERENCES connector_apis(id) ON DELETE CASCADE,
    user_id          BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    meta_data        JSONB NOT NULL DEFAULT '{}',
    created_at       TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (connector_api_id, user_id)
);
