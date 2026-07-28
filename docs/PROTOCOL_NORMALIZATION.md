# Protocol normalization in the connector engine

This covers two gaps in how the engine treats protocols asymmetrically, and closes
both so REST, SOAP, and GraphQL (and anything added later) are handled the same way
from both directions: engine → caller, and author → engine.

**Status.** Part B (native-descriptor tool authoring) is implemented —
`WSDLParser.Parse` and the new `graphql-sdl` importer, both in
`internal/parsers/`. Part A (SOAP response XML→JSON normalization) is still design
only; nothing in `internal/engines/soap.go` has changed yet.

## Problem statement

**Response side.** REST and GraphQL responses are JSON-decoded before a tool's
caller ever sees them (`internal/engines/rest.go:94-100`,
`internal/engines/graphql.go:62-65`). SOAP responses are not — `SoapEngine.Execute`
reads the raw HTTP body and hands it back untouched:

```go
// internal/engines/soap.go:59-68
raw, err := io.ReadAll(resp.Body)
...
return &Result{
	StatusCode: resp.StatusCode,
	Headers:    flattenHeaders(resp.Header),
	Body:       string(raw),
}, nil
```

`applyResponseMapping` (`internal/dispatcher/response_shaper.go:5-46`) type-asserts
the body to `map[string]any` via `dotGet`. That assertion always fails for SOAP,
so response mapping silently no-ops for every SOAP tool today — the caller gets
literal XML text and has to convert it to JSON themselves. The engine should own
that translation instead, the same way it already does for REST/GraphQL.

**Authoring side.** `EndpointMapping["envelopeTemplate"]` (SOAP) and
`EndpointMapping["query"]` (GraphQL) are already raw native-format strings, not
JSON-converted — that part is fine as-is. The actual gap is `Parameters`, the JSON
Schema exposed verbatim as MCP `inputSchema`
(`internal/api/mcp_routes.go:86-96`), which every engine type requires equally.
Nothing derives it from the native artifact, so whoever defines a SOAP or GraphQL
tool has to hand-write `Parameters` and separately keep it in sync with the
`{{param}}` placeholders in their envelope template or the variables in their
GraphQL query. REST already has an escape hatch for this via OpenAPI import
(`internal/parsers/openapi.go`, one tool per operation, `Parameters` derived from
the spec). SOAP and GraphQL don't.

Notably, the plumbing for a SOAP escape hatch already exists —
`internal/parsers/parser.go`'s `Registry` registers `"wsdl": &WSDLParser{}`, and
`internal/parsers/wsdl.go` is a stub:

```go
func (p *WSDLParser) Parse(raw []byte) (*ParseResult, error) {
	return nil, fmt.Errorf("parsers: wsdl: not yet implemented — author SOAP tools via the manual tool-mapping editor for now")
}
```

So closing this gap for SOAP means implementing an existing stub, not building new
plumbing — `POST /connectors/{id}/import?format=wsdl` already routes here.

## Part A — Response normalization (engine → caller)

**Contract.** `Result.Body` (`internal/engines/engine.go:11-15`) should always be
JSON-shaped (`map[string]any` / `[]any` / a scalar) by the time `Execute` returns —
never a raw native-protocol string when the protocol has structure. This needs no
change to the `Engine` interface itself and no change to `response_shaper.go` —
`dotGet`'s `map[string]any` assumption becomes valid for SOAP too, once
normalization happens at the engine boundary instead of downstream.

**Where it lives.** A conversion helper alongside `soap.go` in the `engines`
package, called at the point currently marked `Body: string(raw)`
(`soap.go:64-68`). After parsing, unwrap the SOAP envelope so the tool receives the
operation's response element — not the whole `<soap:Envelope><soap:Body>...`
wrapper.

**XML → JSON convention** (documented here explicitly as a lossy, non-reversible
choice — not "the correct" mapping, just the one this engine commits to):

| XML shape | JSON shape |
|---|---|
| Element, text only | Scalar — numeric/bool coercion attempted, else string |
| Element, repeated child tag | JSON array |
| Attribute | `@name` key alongside siblings |
| Mixed text + child elements | Text under a `#text` key |
| Namespace-prefixed tag/attribute | Prefix stripped from the JSON key; URI dropped |

On XML parse failure, fall back to `Body: string(raw)` — the same
graceful-degradation pattern REST already uses when a response isn't valid JSON.

**Open question — SOAP faults.** A `<Fault>` element is often returned inside a
`200 OK` response. Should the engine detect it and surface it as a
dispatcher-level error (consistent with what a REST 4xx/5xx would produce), or
just pass it through as a normal — if oddly shaped — body? Not resolved here;
flagging for a decision before Phase 1 implementation.

**Open question — SOAP 1.1 vs 1.2.** Envelope/fault namespaces differ between the
two versions. Phase 1 needs to decide whether both are handled or only 1.1 for
now.

## Part B — Native-descriptor tool authoring (author → engine)

**Implemented.** Reuses the existing `POST /connectors/{id}/import?format=...`
endpoint and `parsers.Registry` — no new endpoint was needed.

**SOAP** — `internal/parsers/wsdl.go`, `WSDLParser.Parse`, mirroring
`OpenAPIParser`'s one-tool-per-operation loop (`internal/parsers/openapi.go:40-92`).
For each `<operation>` in the WSDL's `portType`:
- `Parameters` (JSON Schema) is derived from the operation's input message XSD
  element types. Primitive mapping: `xs:int`/`long`/`short`/`unsigned*` → integer,
  `xs:decimal`/`float`/`double` → number, `xs:boolean` → boolean, everything else
  (string, date, dateTime, anyURI, unmapped/complex) → string; `maxOccurs="unbounded"`
  → array.
- `EndpointMapping.envelopeTemplate` is a generated envelope skeleton with
  `{{param}}` placeholders for each input element, wrapped in the request element
  under the schema's target namespace.
- `EndpointMapping.soapAction` comes from the matching `<binding>` operation's
  `soapAction`.

*Resolved — WSDL/XSD parsing.* Went with a hand-rolled parser
(`encoding/xml`, no new dependency) covering a deliberately minimal subset:
document/literal style only, a single inline `<types>` schema (no external
imports), and one level of complex-type resolution (a message part's element
either has an inline `complexType`/`sequence` of simple children, or references a
named top-level `complexType` — nested complex types inside a sequence aren't
expanded, they fall back to `string`). rpc/encoded-style WSDL falls back to typing
each message part directly. This covers the common modern SOAP style; a genuinely
legacy/nested WSDL may need hand-editing of the generated `Parameters` after
import — which was already the required fallback path before this change.

**GraphQL** — `internal/parsers/graphql_sdl.go`, `GraphQLSDLParser`, registered as
`"graphql-sdl"` in `internal/parsers/parser.go`. Accepts either raw SDL text
(detected as the non-`{`-prefixed input, parsed via `gqlparser.LoadSchema`) or a
standard introspection query JSON result (`{"data":{"__schema":{...}}}` or a bare
`{"__schema":{...}}`, decoded by hand — no dependency needed for this path since
introspection JSON is already fully structured). One `DraftTool` per `Query`/
`Mutation` field (GraphQL's own introspection meta-fields, `__schema`/`__type`,
are filtered out); `Parameters` derived from the field's arguments, with one level
of `INPUT_OBJECT` expansion before falling back to a generic `object`.

*Resolved — GraphQL dependency.* Added `github.com/vektah/gqlparser/v2` (used by
`gqlgen`; well-maintained) for SDL parsing, per the doc's original recommendation
— hand-rolling a correct SDL tokenizer (lists, non-null, directives, block
strings) was judged higher-risk than one focused dependency.

*Resolved — response selection.* The generated `query`/`mutation` string's
selection set defaults to the return type's scalar/enum fields only, falling back
to `{ __typename }` if the return type has none (e.g. a bare scalar-returning
field needs no selection at all, and is left with an empty set). This is a
starting point, not a claim of completeness — an author whose tool needs nested
object fields edits the generated `query` after import, same as any other
auto-derived field.

**Manual authoring stays intact.** `CreateToolInput`
(`internal/controlplane/tool_service.go:24-65`) has no engine-specific validation
today — creating a tool by hand, without a WSDL/SDL import, keeps working exactly
as-is. Import is purely additive.

**No lock-in.** Auto-derived `Parameters`/`EndpointMapping` are plain fields,
editable after creation like any hand-authored tool. Import always creates new
tools rather than mutating existing ones, so a re-import can never silently
overwrite a tool someone has already customized.

## Phasing

1. **SOAP response XML→JSON (Part A).** Not started. No new dependencies, no API
   surface changes, no data-model changes required — still the next piece to
   pick up.
2. **`WSDLParser.Parse` (Part B, SOAP).** Done — `internal/parsers/wsdl.go`.
3. **`graphql-sdl` import (Part B, GraphQL).** Done —
   `internal/parsers/graphql_sdl.go`, `internal/parsers/parser.go`.

Tests: `internal/parsers/wsdl_test.go`, `internal/parsers/graphql_sdl_test.go`
(covers both SDL text and introspection JSON input).

## Open questions / risks (consolidated)

**Still open (Part A, not yet implemented):**
- Exact XML→JSON convention — irreversible once SOAP tools start depending on it
  for `OutputSchema`/`ResponseMapping` authoring.
- SOAP Fault handling: dispatcher-level error vs. normal body.
- SOAP 1.1 vs 1.2 support scope for Phase 1.

**Resolved (Part B, implemented):**
- WSDL/XSD parsing: hand-rolled minimal subset, no new dependency (see Part B).
- GraphQL importer's response-field-selection default: scalar/enum-only,
  `__typename` fallback (see Part B).
- GraphQL SDL parsing dependency: `github.com/vektah/gqlparser/v2`.

**New, surfaced during Part B implementation:**
- The hand-rolled WSDL parser's known coverage gaps (rpc/encoded style, WSDL
  `<import>`, nested complex types beyond one level) mean some real-world WSDLs
  will import with partial/best-effort `Parameters` that need manual cleanup —
  not a regression (manual authoring was already required for all SOAP tools
  before this change), but worth knowing before pointing it at an arbitrary
  legacy WSDL.
