<p align="center">
  <img width="1200" height="400" alt="image" src="https://github.com/user-attachments/assets/ef3f96dc-da95-4075-a6aa-b209b91b9b74" />
</p>

<p align="center">
  <img alt="Go" src="https://img.shields.io/badge/Go-1.26-00ADD8?style=for-the-badge&logo=go&logoColor=white">
  <img alt="PostgreSQL" src="https://img.shields.io/badge/PostgreSQL-GORM-4169E1?style=for-the-badge&logo=postgresql&logoColor=white">
  <img alt="Protocol" src="https://img.shields.io/badge/Model_Context_Protocol-ready-8b5cf6?style=for-the-badge">
  <img alt="PRs" src="https://img.shields.io/badge/PRs-welcome-10b981?style=for-the-badge">
</p>

<p align="center">
  <b>GridHook wraps any REST, SOAP, or GraphQL API and hands it to an AI agent as a callable tool</b> —
  encrypted credentials, per-org tenancy, tool grouping, and a full audit trail, out of the box.
</p>

<br/>

## What is this, actually

You have APIs. You want an LLM to be able to call them safely, without gluing together
auth headers and prompt-engineering a tool schema by hand every time.

GridHook sits in between:

1. **Point it at an API** — paste an OpenAPI/Swagger spec, a Postman collection, a WSDL file,
   or a raw `curl` command. It parses the operations into callable tools automatically.
2. **It handles the boring, dangerous parts** — OAuth2/Bearer/API-key/Basic credentials are
   encrypted at rest (AES-256-GCM, envelope encryption) and only decrypted in memory at call time.
3. **You group tools and expose them** — bundle tools from one or many connectors into a
   **Tool Group**, attach it to an **MCP Server**, and hand the generated endpoint + API key to
   any MCP-speaking AI client.
4. **Every call is logged** — who called what, which connector, how long it took, what came back.

<br/>

## How a call actually flows

<p align="center">
  <img width="1200" height="340" alt="image" src="https://github.com/user-attachments/assets/e36c7559-34cf-4a17-a698-2e8f5381c8ad" />
</p>

An AI agent calls the MCP server → the **dispatcher** resolves the tool, the **auth broker**
decrypts and injects the right credentials (with short-lived token caching for OAuth2), the
right **engine** (REST / SOAP / GraphQL) executes the call against your real API, and the
outcome — success or failure — lands in the audit log. The agent never sees a credential.

<br/>

## Why it doesn't feel like "yet another CRUD app"

|  |  |
|---|---|
| 🔐 **Credentials never touch disk in plaintext** | `client_secret`, `bearer_token`, `api_key_value`, `basic_password` are sealed with AES-256-GCM before they hit Postgres, and opened only in memory when a call is dispatched. |
| 🧩 **Three protocol engines, one interface** | REST, SOAP, and GraphQL connectors are all just "tools" to the dispatcher — the engine differences are isolated behind one small interface. |
| 🏢 **Multi-tenant from day one** | `Company → Tenant → Organization → User`, with role-based access (`owner`/`admin`/`developer`/`viewer`) and org-scoped everything. |
| 📦 **Import instead of hand-write** | Drop in an OpenAPI spec, Postman collection, WSDL, or a `curl` snippet — tools get generated, not typed. |
| 🗂️ **Tool Groups, not one giant tool list** | Group tools by connector, by team, or by whatever makes sense — an MCP server only ever exposes the groups assigned to it. |
| 🕵️ **An audit trail that's actually queryable** | Every dispatch — status, HTTP code, duration, input, output — is written asynchronously so it never slows down the live call. |

<br/>

## Quick start

```bash
git clone https://github.com/AadiVerma/Gridhook-Backend.git
cd Gridhook-Backend

cp .env.example .env        # then edit DATABASE_URL etc.

psql "$DATABASE_URL" -f internal/db/migrations/0001_init.up.sql

go run ./cmd/server          # admin API + MCP runtime, one process
go run ./cmd/worker          # background: session sweep, health checks
```

The server reads `.env` automatically (via `godotenv`) — no shell exports required for local dev.

<br/>

## Project layout

```
cmd/
  server/          admin API + MCP runtime (one process)
  worker/           background jobs: session sweep, connector health checks

internal/
  config/           env + .env loading
  db/               Postgres connection (GORM) + the Sealer encryption boundary
  models/           domain types — Connector, MCPTool, ToolGroup, ToolInvocation, ...
  identity/         auth, sessions, users & roles
  controlplane/     CRUD for connectors, connector APIs, tool groups, tools, MCP servers
  auth/             pluggable credential schemes (OAuth2, Bearer, API key, Basic) + token cache
  engines/          REST, SOAP, GraphQL execution engines
  dispatcher/       resolves a tool call → engine → shapes the response
  parsers/          OpenAPI, Postman, WSDL, curl → connector import
  audit/            append-only tool_invocations log
  api/              chi routes for the admin surface
```

<br/>

## Tech stack

<p align="center">
  <img alt="Go" src="https://img.shields.io/badge/-Go-000000?style=flat-square&logo=go&logoColor=00ADD8">
  <img alt="chi" src="https://img.shields.io/badge/-chi_router-000000?style=flat-square">
  <img alt="GORM" src="https://img.shields.io/badge/-GORM-000000?style=flat-square">
  <img alt="PostgreSQL" src="https://img.shields.io/badge/-PostgreSQL-000000?style=flat-square&logo=postgresql&logoColor=4169E1">
  <img alt="golangci-lint" src="https://img.shields.io/badge/-golangci--lint-000000?style=flat-square">
</p>
