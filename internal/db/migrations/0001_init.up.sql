-- Gridhook connector platform — initial schema.
-- See ARCHITECTURE.md §2 for the entity model this implements.

-- ---------------------------------------------------------------------------
-- Identity hub: companies -> tenants -> organizations (the CTM row)
-- ---------------------------------------------------------------------------

CREATE TABLE companies (
    id         BIGSERIAL PRIMARY KEY,
    name       TEXT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE tenants (
    id         BIGSERIAL PRIMARY KEY,
    name       TEXT NOT NULL,
    domain     TEXT NOT NULL UNIQUE,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- organizations == trd.md's "company_tenant_mappings": the single tenant
-- identifier used everywhere downstream (connectors, mcp servers, audit log).
CREATE TABLE organizations (
    id         BIGSERIAL PRIMARY KEY,
    company_id BIGINT NOT NULL REFERENCES companies(id) ON DELETE CASCADE,
    tenant_id  BIGINT NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    name       TEXT NOT NULL,
    slug       TEXT NOT NULL UNIQUE,
    timezone   TEXT NOT NULL DEFAULT 'UTC',
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (company_id, tenant_id)
);

CREATE TYPE user_role   AS ENUM ('owner', 'admin', 'developer', 'viewer');
CREATE TYPE user_status AS ENUM ('active', 'invited');

CREATE TABLE users (
    id              BIGSERIAL PRIMARY KEY,
    organization_id BIGINT NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
    email           TEXT NOT NULL UNIQUE,
    name            TEXT NOT NULL,
    password_hash   TEXT NOT NULL,
    role            user_role NOT NULL DEFAULT 'developer',
    status          user_status NOT NULL DEFAULT 'invited',
    last_active_at  TIMESTAMPTZ,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX idx_users_org ON users(organization_id);

-- Opaque bearer token, DB-verified, never reused — a fresh row per login.
CREATE TABLE sessions (
    id           BIGSERIAL PRIMARY KEY,
    user_id      BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    access_token TEXT NOT NULL UNIQUE,
    expires_at   TIMESTAMPTZ NOT NULL,
    revoked_at   TIMESTAMPTZ,
    created_at   TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX idx_sessions_user ON sessions(user_id);
CREATE INDEX idx_sessions_token ON sessions(access_token) WHERE revoked_at IS NULL;

-- ---------------------------------------------------------------------------
-- Connectors: logical grouping only. The protocol-specific detail (base URL,
-- auth, engine type) lives one level down, on connector_apis — a connector can
-- bundle several of these (e.g. a REST API and a SOAP API under one logical
-- "Salesforce" connector).
-- ---------------------------------------------------------------------------

CREATE TYPE connector_status AS ENUM ('active', 'inactive', 'error');

CREATE TABLE connectors (
    id              BIGSERIAL PRIMARY KEY,
    organization_id BIGINT NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
    name            TEXT NOT NULL,
    glyph           TEXT,
    description     TEXT NOT NULL DEFAULT '',
    status          connector_status NOT NULL DEFAULT 'inactive',
    last_sync_at    TIMESTAMPTZ,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX idx_connectors_org ON connectors(organization_id);

CREATE TYPE engine_type AS ENUM ('REST', 'SOAP', 'GRAPHQL');
CREATE TYPE auth_type   AS ENUM ('oauth2', 'bearer', 'api_key', 'basic', 'login_token', 'none');

-- One row per actual callable endpoint under a connector.
CREATE TABLE connector_apis (
    id              BIGSERIAL PRIMARY KEY,
    connector_id    BIGINT NOT NULL REFERENCES connectors(id) ON DELETE CASCADE,
    name            TEXT NOT NULL,
    engine_type     engine_type NOT NULL,
    base_url        TEXT NOT NULL,
    auth_type       auth_type NOT NULL DEFAULT 'none',
    spec_url        TEXT,
    spec_raw        JSONB,
    is_active       BOOLEAN NOT NULL DEFAULT true,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX idx_connector_apis_connector ON connector_apis(connector_id);

-- Secret material for one connector_api. Typed columns per scheme instead of a
-- single JSON blob so each auth scheme's fields are real, visible columns.
-- client_secret/bearer_token/headers are envelope-encrypted at the app layer
-- (see internal/db's Sealer) — this table is never returned from a GET.
CREATE TABLE connector_credentials (
    id              BIGSERIAL PRIMARY KEY,
    connector_api_id BIGINT NOT NULL UNIQUE REFERENCES connector_apis(id) ON DELETE CASCADE,
    auth_type       auth_type NOT NULL,
    token_url       TEXT,
    client_id       TEXT,
    client_secret   TEXT, -- encrypted at rest
    bearer_token    TEXT, -- encrypted at rest
    api_key_header  TEXT,
    api_key_value   TEXT, -- encrypted at rest
    basic_username  TEXT,
    basic_password  TEXT, -- encrypted at rest
    headers         JSONB NOT NULL DEFAULT '{}',
    meta_data       JSONB NOT NULL DEFAULT '{}',
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- Per-person identity attributes a specific API needs beyond its tenant-level
-- credentials (e.g. Ariba's per-approver user/passwordAdapter).
CREATE TABLE connector_user_mappings (
    id               BIGSERIAL PRIMARY KEY,
    connector_api_id BIGINT NOT NULL REFERENCES connector_apis(id) ON DELETE CASCADE,
    user_id          BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    meta_data        JSONB NOT NULL DEFAULT '{}',
    created_at       TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (connector_api_id, user_id)
);

-- ---------------------------------------------------------------------------
-- Modules: optional synced lookup mirroring an external system's own feature
-- list. Global, not per-tenant. Only used when a tool_group is `synced`.
-- ---------------------------------------------------------------------------

CREATE TABLE modules (
    id         BIGSERIAL PRIMARY KEY,
    key        TEXT NOT NULL UNIQUE,
    label      TEXT NOT NULL,
    synced_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- ---------------------------------------------------------------------------
-- Tool groups: generalized "modules" — a named cluster of tools invoked as a
-- unit by an LLM client. `manual` groups are hand-curated; `synced` groups
-- mirror a connector's own module list via the worker's module_sync job.
-- ---------------------------------------------------------------------------

CREATE TYPE tool_group_kind AS ENUM ('manual', 'synced');

CREATE TABLE tool_groups (
    id                 BIGSERIAL PRIMARY KEY,
    organization_id    BIGINT NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
    name               TEXT NOT NULL,
    slug               TEXT NOT NULL,
    description        TEXT NOT NULL DEFAULT '',
    kind               tool_group_kind NOT NULL DEFAULT 'manual',
    synced_module_key  TEXT REFERENCES modules(key),
    created_at         TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at         TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (organization_id, slug)
);

CREATE INDEX idx_tool_groups_org ON tool_groups(organization_id);

-- ---------------------------------------------------------------------------
-- Tools: one row per callable endpoint exposed to the AI. FK'd to a specific
-- connector_api (not the connector) since engine_type/auth is per-API.
-- ---------------------------------------------------------------------------

CREATE TYPE http_method AS ENUM ('GET', 'POST', 'PUT', 'PATCH', 'DELETE');
CREATE TYPE tool_status AS ENUM ('active', 'deprecated');

CREATE TABLE mcp_tools (
    id                BIGSERIAL PRIMARY KEY,
    connector_api_id  BIGINT NOT NULL REFERENCES connector_apis(id) ON DELETE CASCADE,
    group_id          BIGINT REFERENCES tool_groups(id) ON DELETE SET NULL,
    -- denormalized from connector_apis.engine_type at creation time so the
    -- dispatcher never needs a join to pick an engine.
    engine_type       engine_type NOT NULL,
    name              TEXT NOT NULL,
    method            http_method NOT NULL,
    path              TEXT NOT NULL,
    description       TEXT NOT NULL DEFAULT '',
    parameters        JSONB NOT NULL DEFAULT '{}',   -- JSON-schema of inputs
    endpoint_mapping  JSONB NOT NULL DEFAULT '{}',   -- protocol-specific call recipe
    response_mapping  JSONB NOT NULL DEFAULT '{}',   -- output reshape rules
    output_schema     JSONB NOT NULL DEFAULT '{}',
    cached            BOOLEAN NOT NULL DEFAULT false,
    cache_ttl_seconds INTEGER NOT NULL DEFAULT 0,
    status            tool_status NOT NULL DEFAULT 'active',
    version           TEXT NOT NULL DEFAULT '1',
    display_title     TEXT,
    display_on_frontend BOOLEAN NOT NULL DEFAULT true,
    deprecated_at     TIMESTAMPTZ,
    created_at        TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at        TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (connector_api_id, name)
);

CREATE INDEX idx_mcp_tools_api ON mcp_tools(connector_api_id);
CREATE INDEX idx_mcp_tools_group ON mcp_tools(group_id);

-- ---------------------------------------------------------------------------
-- MCP servers: the product's actual "output" — an endpoint AI clients connect
-- to. Assigned tool_groups (not raw connector IDs) so it can bundle a subset
-- of tools, possibly spanning connectors, invoked as one unit.
-- ---------------------------------------------------------------------------

CREATE TYPE mcp_server_status AS ENUM ('running', 'stopped');

CREATE TABLE mcp_servers (
    id                   BIGSERIAL PRIMARY KEY,
    organization_id      BIGINT NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
    name                 TEXT NOT NULL,
    slug                 TEXT NOT NULL,
    description          TEXT NOT NULL DEFAULT '',
    custom_instructions  TEXT NOT NULL DEFAULT '',
    status               mcp_server_status NOT NULL DEFAULT 'stopped',
    created_at           TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at           TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (organization_id, slug)
);

CREATE INDEX idx_mcp_servers_org ON mcp_servers(organization_id);

CREATE TABLE mcp_server_tool_groups (
    mcp_server_id BIGINT NOT NULL REFERENCES mcp_servers(id) ON DELETE CASCADE,
    tool_group_id BIGINT NOT NULL REFERENCES tool_groups(id) ON DELETE CASCADE,
    created_at    TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (mcp_server_id, tool_group_id)
);

CREATE TABLE mcp_server_api_keys (
    id            BIGSERIAL PRIMARY KEY,
    mcp_server_id BIGINT NOT NULL REFERENCES mcp_servers(id) ON DELETE CASCADE,
    label         TEXT NOT NULL,
    key_prefix    TEXT NOT NULL,
    key_hash      TEXT NOT NULL UNIQUE, -- sha256 of the full key; full key shown once on create
    created_at    TIMESTAMPTZ NOT NULL DEFAULT now(),
    revoked_at    TIMESTAMPTZ
);

CREATE INDEX idx_mcp_server_api_keys_server ON mcp_server_api_keys(mcp_server_id);

-- ---------------------------------------------------------------------------
-- Audit log: append-only, one row per real tool call. Denormalized so no
-- joins are needed to filter by org/connector/server/user.
-- ---------------------------------------------------------------------------

CREATE TYPE invocation_status AS ENUM ('success', 'error', 'timeout');

CREATE TABLE tool_invocations (
    id                BIGSERIAL PRIMARY KEY,
    tool_id           BIGINT NOT NULL REFERENCES mcp_tools(id) ON DELETE CASCADE,
    connector_id      BIGINT NOT NULL REFERENCES connectors(id) ON DELETE CASCADE,
    connector_api_id  BIGINT NOT NULL REFERENCES connector_apis(id) ON DELETE CASCADE,
    mcp_server_id     BIGINT REFERENCES mcp_servers(id) ON DELETE SET NULL,
    organization_id   BIGINT NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
    user_id           BIGINT,
    user_email        TEXT,
    status            invocation_status NOT NULL,
    http_code         INTEGER,
    duration_ms       INTEGER NOT NULL,
    input             JSONB NOT NULL DEFAULT '{}',
    output             JSONB NOT NULL DEFAULT '{}',
    error             TEXT,
    created_at        TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX idx_invocations_org_time ON tool_invocations(organization_id, created_at DESC);
CREATE INDEX idx_invocations_connector ON tool_invocations(connector_id);
CREATE INDEX idx_invocations_server ON tool_invocations(mcp_server_id);
CREATE INDEX idx_invocations_tool ON tool_invocations(tool_id);
CREATE INDEX idx_invocations_status ON tool_invocations(status);
