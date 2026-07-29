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
| 🔐 **Credentials never touch disk in plaintext** | `client_secret`, `bearer_token`, `api_key_value`, `basic_password` are sealed with AES-256-GCM before they hit Postgres, opened only in memory at dispatch time, stripped from requests that redirect off-host, and redacted from errors before they reach a response or the audit log. |
| 🧩 **Three protocol engines, one interface** | REST, SOAP, and GraphQL connectors are all just "tools" to the dispatcher — the engine differences are isolated behind one small interface. |
| 🏢 **Multi-tenant from day one** | `Company → Tenant → Organization → User`. Every repository method takes an organization ID and applies it as a SQL predicate — tenancy is enforced in the statement, not by the caller. |
| 📦 **Import instead of hand-write** | Drop in an OpenAPI spec, Postman collection, WSDL, or a `curl` snippet — tools get generated, not typed. |
| 🗂️ **Tool Groups, not one giant tool list** | Group tools by connector, by team, or by whatever makes sense — an MCP server only ever exposes the groups assigned to it. |
| 🕵️ **An audit trail that's actually queryable** | Every dispatch — status, HTTP code, duration, input, output — is written asynchronously so it never slows down the live call. |

<br/>

## Quick start

```bash
git clone https://github.com/AadiVerma/Gridhook-Backend.git
cd Gridhook-Backend

cp .env.example .env        # then edit DATABASE_URL etc.

make migrate                 # applies every migration in order

make run-server              # admin API + MCP runtime, one process
make run-worker              # background: session sweep
```

Health probes live at `/healthz` (liveness) and `/readyz` (readiness — checks
the database). Point a supervisor at the first and a load balancer at the
second; conflating them turns a database blip into a restart loop.

The server reads `.env` automatically (via `godotenv`) — no shell exports required for local dev.

<br/>

## Project layout

```
cmd/
  server/           admin API + MCP runtime (one process)
  worker/           background jobs: session sweep

internal/
  models/           domain types — Connector, MCPTool, ToolGroup, ToolInvocation, ...
  config/           the only package that reads the environment; typed + validated
  secrets/          AES-256-GCM sealer and credential redaction helpers
  slug/             URL-safe identifier generation

  db/               Postgres connection and pool tuning
  httpx/            shared outbound client — retry, SSRF guard, size caps, redirects
  observability/    structured logging, request correlation, panic recovery

  identity/         auth, sessions, users & roles
  controlplane/     org-scoped repositories for connectors, APIs, tool groups, tools, servers
  parsers/          OpenAPI, Postman, WSDL, curl, GraphQL SDL → connector import

  auth/             credential broker + token cache
  auth/schemes/     one strategy per credential style (OAuth2, Bearer, API key, Basic)
  engines/          REST, SOAP, GraphQL execution engines
  dispatcher/       resolves a tool call → credentials → engine → audit record

  audit/            append-only tool_invocations log (Recorder writes, Reader queries)
  api/              chi routes, DTOs and error mapping
```

`internal/models` imports nothing; `cmd/` is the only place that constructs
dependencies. See [ARCHITECTURE.md](ARCHITECTURE.md) for the full dependency
graph, the security model, and known gaps.

<br/>

## Development

```bash
make dev          # live reload (air) — rebuilds and restarts on save
make check        # vet + lint + race tests — the gate a change must pass
make test-race    # tests under the race detector
make cover        # coverage summary
make build        # binaries into bin/
make migrate      # apply migrations (needs DATABASE_URL)
make help         # everything else
```

`make dev` is the Go equivalent of nodemon. It watches `.go` files and `.env`,
and reloads with **SIGINT rather than SIGKILL** so the graceful shutdown path
runs on every save — otherwise each reload would silently discard whatever was
still queued in the audit buffer.

Development logs use a compact colourised format; production defaults to JSON.
Set `LOG_FORMAT=json` locally to see exactly what a log shipper will receive,
or `LOG_COLOR=never` (or `NO_COLOR=1`) to strip escape codes.

<br/>

## Tech stack

<p align="center">
  <img alt="Go" src="https://img.shields.io/badge/-Go-000000?style=flat-square&logo=go&logoColor=00ADD8">
  <img alt="chi" src="https://img.shields.io/badge/-chi_router-000000?style=flat-square">
  <img alt="GORM" src="https://img.shields.io/badge/-GORM-000000?style=flat-square">
  <img alt="PostgreSQL" src="https://img.shields.io/badge/-PostgreSQL-000000?style=flat-square&logo=postgresql&logoColor=4169E1">
  <img alt="golangci-lint" src="https://img.shields.io/badge/-golangci--lint-000000?style=flat-square">
</p>
