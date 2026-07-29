-- Drop two tables that were designed but never wired up.
--
-- connector_user_mappings
--   Intended for per-person identity attributes an API needs beyond its
--   tenant-level credentials (the original comment cited Ariba's per-approver
--   user/passwordAdapter). Nothing ever read or wrote it: the only Go that
--   referenced it was the struct and its TableName().
--
--   Two things blocked its intended use, and both need answering before any
--   replacement is designed:
--     1. meta_data was plain JSONB with no encryption, while every secret in
--        connector_credentials is AES-256-GCM sealed. The obvious payload — a
--        per-user password or OAuth refresh token — would have hit disk in
--        plaintext.
--     2. It is keyed by user_id, but the MCP dispatch path has no user. An
--        API key resolves to a *server*, not a person, so the row could never
--        be looked up on the path that matters.
--
-- modules
--   A global (non-tenant) lookup mirroring an external system's feature list,
--   referenced only by tool_groups.synced_module_key for `synced` groups.
--   No importer ever populated it, and GroupService.SyncModules /
--   EnsureSyncedGroup — the only writers — had no call sites anywhere.
--
-- tool_groups.kind is deliberately KEPT. It is part of the API response shape
-- that clients already consume, and 'manual' remains meaningful. Only the
-- unreachable half of the feature is removed here.

-- The FK column goes before the table it references.
ALTER TABLE tool_groups DROP COLUMN IF EXISTS synced_module_key;

DROP TABLE IF EXISTS connector_user_mappings;
DROP TABLE IF EXISTS modules;
