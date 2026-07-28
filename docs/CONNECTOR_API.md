# Connector API

All routes below are under `/api/v1` and require `Authorization: Bearer <token>` (see
`AUTH_API.md`). Every write is scoped to the caller's organization.

## The model, in one paragraph

A **Connector** (e.g. "Legacy ERP") is the thing you see in the connector list. **Modules**
(`tool-groups` in the API/DB — "module" is just the product-facing name) are how you organize
a connector's surface area at creation time: a module has a name/description and a list of
**ConnectorAPIs** nested under it (each a concrete REST/GraphQL/SOAP/etc config — base URL,
auth type, credentials). A **Tool** — an individual callable endpoint — belongs to exactly one
`ConnectorAPI`, and inherits that API's module unless you move it into a different one later.
Modules themselves are still **flat, org-level rows** (not literally owned by a connector) —
that's what lets one module span APIs from more than one connector if you want — but every API
you create *from* a connector's page passes that connector's id, and optionally a module id, so
the two are linked from the moment of creation, exactly like the "create module → add API to
this module → add another API to this module" flow in the connector-creation screen.

```
Connector ──< ConnectorAPI >── groupId ──> Module (flat, org-wide, reusable)
                  ╰──< Tool  >── groupId ──> Module  (defaults to its API's module)
```

<br/>

## Connectors — `/connectors`

### `GET /connectors`
Query params: `page`, `pageSize`, `q` (name search), `status` (`active`|`inactive`|`error`),
`type` (`REST`|`SOAP`|`GRAPHQL` — matches connectors that have *at least one* API of that type).

**Response `200`**
```json
{
  "data": [
    {
      "id": 12,
      "name": "Stripe Billing",
      "glyph": "S",
      "description": "Payments, invoicing, and subscription billing.",
      "status": "active",
      "lastSync": "2026-07-25T10:12:00Z",
      "engineTypes": ["REST"],
      "authTypes": ["bearer"],
      "apiCount": 1,
      "toolCount": 14,
      "moduleCount": 0
    },
    {
      "id": 13,
      "name": "Legacy ERP",
      "glyph": "ERP",
      "description": "On-prem ERP covering the full procure-to-pay lifecycle.",
      "status": "error",
      "lastSync": "2026-07-20T04:00:00Z",
      "engineTypes": ["REST", "SOAP"],
      "authTypes": ["basic", "api_key"],
      "apiCount": 2,
      "toolCount": 6,
      "moduleCount": 3
    }
  ],
  "page": 1,
  "pageSize": 50,
  "total": 2
}
```
`engineTypes`/`authTypes` are the **distinct** values across that connector's APIs — frontend
renders a single tag (`REST`, `BEARER`) when the array has length 1, and `MIXED TYPES`/`MIXED
AUTH` when it has more than one. `moduleCount` is the number of distinct modules linked to this
connector, whether the link is on one of its APIs directly or on a tool underneath one — so it's
correct immediately after creation, before any tools exist.

### `POST /connectors`

Two ways to call this, matching the two things the create-connector screen needs to do:

**Simple — no modules, at most one inline API** (unchanged from before):
```json
{ "name": "Stripe Billing", "glyph": "S", "description": "...", "type": "REST", "baseUrl": "https://api.stripe.com", "authType": "bearer" }
```

**With modules — this is the "add module → add API to this module" screen.** Send `modules`
instead of `type`/`baseUrl`/`authType`; each module can have multiple APIs, and you can add as
many modules as the form has:
```json
{
  "name": "Legacy ERP",
  "glyph": "ERP",
  "description": "On-prem ERP covering the full procure-to-pay lifecycle.",
  "modules": [
    {
      "name": "Bidding",
      "description": "What does this module cover?",
      "apis": [
        {
          "name": "Bidding SOAP API",
          "engineType": "SOAP",
          "baseUrl": "https://api.example.com/v1",
          "authType": "oauth2",
          "credentials": { "authType": "oauth2", "tokenUrl": "https://api.example.com/oauth/token", "clientId": "client_9f2a", "clientSecret": "..." }
        }
      ]
    },
    { "name": "Invoicing", "description": "...", "apis": [ { "name": "Invoicing REST API", "engineType": "REST", "baseUrl": "...", "authType": "bearer" } ] }
  ]
}
```
`credentials` per API is optional — omit it and call `PUT /connectors/{id}/apis/{apiId}/credentials`
separately if you'd rather save-as-you-go. No `slug` field needed for a module — the backend
slugifies the name and dedupes it automatically.

**Response `201`** (same envelope either way; `modules` is `[]` for the simple form):
```json
{
  "connector": { "id": 13, "name": "Legacy ERP", "...": "...", "engineTypes": ["SOAP", "REST"], "authTypes": ["oauth2", "bearer"], "apiCount": 2, "toolCount": 0, "moduleCount": 2 },
  "modules": [
    {
      "module": { "id": 3, "organizationId": 7, "name": "Bidding", "slug": "bidding", "description": "...", "kind": "manual", "createdAt": "...", "updatedAt": "..." },
      "apis": [ { "id": 5, "connectorId": 13, "groupId": 3, "name": "Bidding SOAP API", "engineType": "SOAP", "baseUrl": "...", "authType": "oauth2", "isActive": true, "createdAt": "...", "updatedAt": "..." } ]
    },
    { "module": { "id": 4, "name": "Invoicing", "slug": "invoicing", "...": "..." }, "apis": [ { "id": 6, "connectorId": 13, "groupId": 4, "...": "..." } ] }
  ]
}
```
Every API in `modules[].apis` already has `connectorId` set and `groupId` pointing back at its
module — that's the connector→module→API link the frontend needs to send back on later edits.

### `GET /connectors/{id}` — same shape as one list item.

### `PATCH /connectors/{id}`
```json
{ "name": "New name", "description": "...", "status": "inactive" }
```
All fields optional/pointer — send only what changed.

### `POST /connectors/{id}/toggle`
```json
{ "active": false }
```
The on/off switch in the UI. Flips the connector's `status` to `active`/`inactive` **and**
cascades `isActive` to every `ConnectorAPI` under it in one transaction — so the connector and
all its APIs always agree. Returns the updated connector (same shape as `GET`).

### `DELETE /connectors/{id}` — `204`.

### `POST /connectors/{id}/health-check`
Pings every active API's base URL; sets `status` to `active` or `error` accordingly and bumps
`lastSync`. Returns the updated connector.

### `POST /connectors/import?format=openapi&name=...`
Body is the raw spec file. Creates a connector + one API + bulk-imported tools in one shot.
`format` also supports `postman`, `curl`. Returns `201` with `{ connector, api, tools }`.

### `GET /connectors/{id}/export`
Full dump for backup/cloning: `{ connector, apis: [{ api, tools }] }`.

<br/>

## Modules on a connector — `/connectors/{id}/modules`

The connector-scoped view of modules — this is how you render the "module → its APIs" tree for
a connector you already created, and how you add another module to it later without re-running
the whole create-connector flow.

### `GET /connectors/{id}/modules`
```json
[
  {
    "module": { "id": 3, "organizationId": 7, "name": "Bidding", "slug": "bidding", "description": "...", "kind": "manual", "toolCount": 0, "createdAt": "...", "updatedAt": "..." },
    "apis": [ { "id": 5, "connectorId": 13, "groupId": 3, "name": "Bidding SOAP API", "engineType": "SOAP", "baseUrl": "...", "authType": "oauth2", "isActive": true, "createdAt": "...", "updatedAt": "..." } ]
  }
]
```
Includes modules linked either through an API tagged directly (`connector_apis.groupId`) or
through a tool tagged underneath one of this connector's APIs — so it's accurate even for a
brand-new connector that has APIs but no tools imported yet.

### `POST /connectors/{id}/modules`
Same module shape as the nested `modules[]` entries in `POST /connectors` above — use this to
add one more module (with its APIs) to a connector that already exists:
```json
{ "name": "Invoicing", "description": "...", "apis": [ { "name": "Invoicing REST API", "engineType": "REST", "baseUrl": "...", "authType": "bearer" } ] }
```
**Response `201`**: `{ "module": {...}, "apis": [...] }`.

<br/>

## Connector APIs — `/connectors/{id}/apis`

A connector can have multiple APIs (e.g. a REST API and a legacy SOAP API side by side) —
this is what the "MIXED TYPES" badge is telling you. Every API belongs to exactly one connector
(`connectorId`) and, optionally, one module (`groupId`, `0` if unassigned).

### `GET /connectors/{id}/apis` — plain array, no pagination.
```json
[
  { "id": 5, "connectorId": 12, "groupId": 0, "name": "Stripe REST", "engineType": "REST", "baseUrl": "https://api.stripe.com", "authType": "bearer", "isActive": true, "createdAt": "...", "updatedAt": "..." }
]
```

### `POST /connectors/{id}/apis`
```json
{ "name": "Stripe REST", "engineType": "REST", "baseUrl": "https://api.stripe.com", "authType": "bearer", "specUrl": "", "groupId": 3 }
```
`groupId` is optional — pass an existing module's id to tag this API into it from creation
(this is exactly what the nested `POST /connectors` and `POST /connectors/{id}/modules` calls
do under the hood).

### `PATCH /connectors/{id}/apis/{apiId}`
```json
{ "isActive": false, "groupId": 4 }
```
Pointer-optional — send only what changed. `groupId: 0` unassigns the API from its current
module. Use this to move an API to a different module, or to enable/disable a single API
independently of the connector-level toggle.

### `PUT /connectors/{id}/apis/{apiId}/credentials`
Shape depends on `authType` — send only the relevant fields (`bearerToken`, or
`clientId`/`clientSecret`/`tokenUrl`, or `apiKeyHeader`/`apiKeyValue`, or
`basicUsername`/`basicPassword`, plus optional `headers`). `204` on success. Credentials are
never returned by any `GET` — write-only.

<br/>

## Tools — `/connectors/{id}/tools`

Tools belong to a specific `ConnectorAPI`. If a connector has exactly one API you can omit
`apiId`; if it has more than one you must pass `?apiId=` or you'll get `400 ambiguous_api`.

### `GET /connectors/{id}/tools?apiId=5` — plain array.
```json
[
  { "id": 90, "connectorApiId": 5, "groupId": 3, "engineType": "REST", "name": "createInvoice", "method": "POST", "path": "/v1/invoices", "status": "active", "cached": false, "cacheTtlSeconds": 0, "version": "1", "displayOnFrontend": true, "createdAt": "...", "updatedAt": "..." }
]
```
`groupId` (`0` if unassigned) is the tool's module — this is what `moduleCount` on the
connector is derived from.

### `POST /connectors/{id}/tools?apiId=5`
```json
{ "name": "createInvoice", "method": "POST", "path": "/v1/invoices", "description": "...", "parameters": {}, "groupId": 3 }
```
`groupId` is optional — if omitted, the tool defaults to whatever module its `ConnectorAPI` is
tagged into (`0`/no module if the API isn't tagged either). Pass it explicitly to put a tool in
a different module than its API.

### `GET /connectors/{id}/tools/{toolId}` / `PATCH .../{toolId}` / `DELETE .../{toolId}`
`PATCH` body is pointer-optional: `{ "name": "...", "method": "...", "path": "...", "cacheTtlSeconds": 60, "groupId": 4 }`.

### `POST /connectors/{id}/tools/{toolId}/run`
Body is the tool's input params as raw JSON. Dispatches the call live and returns the outcome.

<br/>

## Modules — `/tool-groups`

The org-wide registry of every module, independent of any one connector — use `/connectors/{id}/modules`
above when you specifically want "this connector's modules"; use these routes to list/manage
all of them (e.g. an org-wide modules settings page), or to reuse one module across connectors.

### `GET /tool-groups`
```json
[
  { "id": 3, "organizationId": 7, "name": "Bidding", "slug": "bidding", "description": "...", "kind": "manual", "toolCount": 9, "createdAt": "...", "updatedAt": "..." }
]
```
`kind` is `manual` (hand-curated here) or `synced` (mirrored from a connector's own module list
by a background sync job — has `syncedModuleKey` set). `toolCount` here counts tools only —
it won't include APIs tagged directly with no tools yet (see `moduleCount` on the connector for
that combined view).

### `POST /tool-groups`
```json
{ "name": "Bidding", "description": "..." }
```
`slug` is optional — auto-generated from `name` (and de-duped) if omitted. This is the same
endpoint the nested module-creation calls use internally; call it directly if you just want a
standalone module with no APIs yet.

### `GET /tool-groups/{id}` / `DELETE /tool-groups/{id}`

### `PUT /tool-groups/{id}/tools`
```json
{ "toolIds": [90, 91, 102] }
```
Replaces those tools' `groupId` to point at this module. `204`. (A tool can only belong to one
module at a time — assigning it here moves it out of whatever module it was in before.)
