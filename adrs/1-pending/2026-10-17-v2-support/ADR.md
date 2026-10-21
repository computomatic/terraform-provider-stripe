# Stripe `/v2` support in the provider engine

## Executive Summary

The accepted architecture (`adrs/1-pending/2026-10-17-provider-architecture/ADR.md`) builds every resource from a generated field table, a small declaration and one engine, and the engine is written for `/v1`. This ADR extends it to `/v2`, starting with `v2.core.event_destination` (wave 2) and ready for `v2.core.account` (wave 4): the generator reads `/v2` operations from the pinned spec, the engine sends and reads JSON, and the client and error mapping learn the `/v2` envelope. Every rule is limited to what the conformance suite has observed; where it has not, the engine refuses or omits rather than guesses, and the gaps are listed as work for the suite.

## Status

Status: Accepted, 2026-10-18, with the four open questions answered as recommended (see "Resolved questions"). The ADR stays in `adrs/1-pending/` until implemented.

Evidence is current as of 2026-10-18: the behaviour of the Stripe API as observed by a black-box conformance suite, and the behaviour of the Stripe API fake the provider is tested against.

## Background Context

**What is already in the repository.** A partial `/v2` path exists. `Declaration.V2` makes the engine set `stripeapi.JSONEncoding` on every call (`internal/engine/declaration.go:116`, `internal/engine/api.go:47`). The client has a JSON body branch (`internal/stripeapi/client.go:197`) and decodes `invalid_fields` into `APIError.InvalidFields` (`internal/stripeapi/errors.go:26`, `:59`). The generator's `requestSchema` falls back to `application/json` when an operation has no form body (`tools/schemagen/openapi.go:125`). Nothing has run against a `/v2` resource; no `/v2` table is generated and no `/v2` test exists.

**What the pinned spec says** (`spec/openapi.json`, tag v2585, API version `2026-09-30.endive`, `spec/PIN.json`):

- It has 32 `/v2` operations. Every one with a body declares only `application/json`; `GET` operations declare none. Event destinations are `GET`/`POST /v2/core/event_destinations`, `GET`/`POST`/`DELETE /v2/core/event_destinations/{id}`, and `POST` to `/enable`, `/disable`, `/ping` under `{id}`.
- The list takes `include`, `limit` and `page` (`page` is a string) and returns `{data, next_page_url, previous_page_url}`, all three required, the two URLs nullable. There is no `has_more`.
- `include` is a request parameter, not an object field: an array of enum on `GET` (`webhook_endpoint.url`), on create (`webhook_endpoint.signing_secret`, `webhook_endpoint.url`) and on update (`webhook_endpoint.url`). The create-response description of `webhook_endpoint.signing_secret` reads "Returned only in the create response when explicitly included; public API clients cannot retrieve it later."
- Update takes `description`, `enabled_events`, `include`, `metadata`, `name`, `webhook_endpoint`. Create additionally takes `type`, `event_payload`, `snapshot_api_version`, `events_from`, `amazon_eventbridge`, `azure_event_grid`. Update's `metadata` has `additionalProperties: {type: string, nullable: true}`; create's does not.
- Delete returns `v2.deleted_object` (`id`, `object`). Errors are `v2.error`: `{error: {code, message, type?, doc_url?, user_message?}}`, with `v2.error.not_found` for 404.
- No `/v2` component schema sets `nullable: true` (counted over every schema whose name starts with `v2.`). Timestamps are `type: string, format: date-time`; `format: decimal` and `format: int64` are strings.

**What the suite observed** (API version `2026-09-30.endive`):

- `POST` takes `application/json`; a form body is rejected with a status in 400 to 499 (subtest "create requires a JSON body", which asserts only the range).
- A v2 error has no `type` and no `param`. Field validation is 400 `code: "invalid_fields"` with `invalid_fields: [{field, message}]` sorted by field. Other validation failures are 400 with a `message` and context fields but no `code` (`field`, `event_destination_type`, `event_type`). A missing object is 404 `code: "not_found"`, message `No such event_destination: '<id>'`.
- IDs are `ed_test_...`, `created` and `updated` are RFC 3339 strings, and `webhook_endpoint.url` is absent unless `include=webhook_endpoint.url` is sent (on create, retrieve, update and list). The suite reads the field as "absent or null", so it does not pin which.
- Update: `metadata` merges and a `null` value removes that key (subtest "update metadata": `{"metadata":{"new":"v","drop":null}}` leaves `{"keep":"me","new":"v"}`). `type`, `event_payload` and `snapshot_api_version` are rejected as unknown fields. An update that sends a changed `name`, `description`, `enabled_events` and `webhook_endpoint` changes each.
- List: `limit` 1 and 20 are sent; the token comes from the `page` query parameter of `next_page_url`; the first page has a null `previous_page_url`.
- Delete returns `id` and `object`; a second delete, and a retrieve after delete, are 404 `not_found`.
- Enable and disable set `status` to `enabled` and `disabled`.

**What the suite does not observe.** A `null` or empty string for a scalar on create or update. An empty array or empty object in a body. Whether a shrinking `enabled_events` replaces or merges (the suite's update only grows the list, so replace and merge look the same). `signing_secret` (the suite never sends `include` on create). An array `include` in the indexed form below. The index form of an array element in `invalid_fields.field`. Any `429` body on `/v2`.

**What the fake serves:** `/v2` is detected by path prefix. A `POST` body must be JSON; a form body is a 400. `GET` parameters come from the query string, and a scalar given for an array parameter becomes a one-element array. Validation returns `invalid_fields` sorted by field, with "Unknown field, did you mean ...?". A `null` for a field the schema types as a string, or any other type mismatch, is `fake_unproven`, except `null` as a value inside a map schema such as `metadata`, which is allowed. Event destination update replaces `enabled_events` and `webhook_endpoint` and merges `metadata`. Only `type=webhook_endpoint` is served, and `include=webhook_endpoint.signing_secret` answers `fake_not_implemented`. Idempotency keys are honoured on `POST` only.

**What Stripe documents** (https://docs.stripe.com/api-v2-overview):

- "Request and responses use JSON encoding (`application/json`)."
- Pagination: "Specify the `page` token for list API requests. Use the `previous_page_url` and `next_page_url` properties in the API response". "You can't change list filters after the first request."
- Arrays in query parameters: "When you make direct API requests (not using server-side SDKs or the CLI), you must always specify the index of the array value using bracket notation, even if you only pass a single value" (example `include[0]=requirements`).
- Metadata: "Remove a key-value pair by setting the value to `null`" (v1: an empty string).
- Idempotency: "All `POST` and `DELETE` API v2 requests accept idempotency keys and behave idempotently"; if the first request failed, "the API re-executes the failed requests"; a replay window is 30 days. v1 "always returns the previously-saved response of the first API request, even if it was an error."
- "All API requests to the API `/v2` namespace must include the `Stripe-Version` header." The `expand` parameter "isn't supported"; the `include` parameter is its counterpart.

## Approach Overview

- **Namespace comes from the path (D1).** A declaration says it is `/v2` by using `/v2/` paths. `Declaration.V2` and `Request.Encoding` are removed; the client picks JSON for any `/v2/` path. The model rejects a declaration whose paths mix namespaces.
- **Generator (D2).** The existing `Resource` paths work unchanged for `/v2`. A `/v2` operation must have a JSON body; `include` is omitted from the table and handled by the declaration; v2 nullability is taken as unknown, so the generator's server-defaulted rule is the only one in play.
- **Encoding (D3).** Create omits null, unknown and empty values. Update sends the changed fields, whole objects and whole arrays, and `metadata` as a key diff with `null` for removals. Clearing any other field is refused until the suite proves `null` for it.
- **Includes (D4).** A declaration lists the `include` values the engine sends on every call that returns the object, so redacted fields are read back and `PreserveConfig` is not needed for them.
- **Decoding and errors (D5).** Missing and `null` read the same. `invalid_fields` entries become attribute-level diagnostics; context fields are kept in the detail.
- **Pagination (D6).** A `/v2` lookup reads one page of 20 and treats a non-null `next_page_url` as "more". No page is followed, because no consumer needs one.
- **Client (D7).** JSON by path prefix, `Stripe-Version` as today, an idempotency key on `POST` only.
- **Tests (D8).** The per-resource acceptance set for `stripe_core_event_destination` against the fake, plus unit tests for the codec, errors, generator and engine, and a list of behaviours the suite and fake must prove first.

## Approach Details

### D1. How a declaration says it is `/v2`

- **Decision.** A declaration is `/v2` when its `Paths` start with `/v2/`. The engine and client derive everything else from that. `Declaration.V2` (`internal/engine/declaration.go:116`) and `stripeapi.Request.Encoding` with `FormEncoding`/`JSONEncoding` (`internal/stripeapi/client.go:30-44`) are deleted.
- **Check.** `newModel` (`internal/engine/model.go`, beside the existing `Paths` checks at `:71-94`) fails when the non-empty paths of one declaration do not all start with `/v2/` or all with `/v1/`. An archive `Path` is held to the same rule.
- **Why.** The fake decides the same way, by path prefix, so a flag can only disagree with the wire. A flag also leaves a trap: `tfacc.Post` (`internal/acctest/tfacc/tfacc.go:34`) builds a `stripeapi.Request` with the default form encoding, so any test that touches a `/v2` path out of band would send a form. Derived encoding makes that test helper correct without a change.
- **Type name and import.** Unchanged from the architecture ADR (D7): `stripe_core_event_destination`, import ID the bare `ed_...` ID. `IDPrefix` is `ed_`; the suite sees `ed_test_...` and the import check is a prefix match that only warns (`internal/engine/resource.go:468`), so `ed_` holds.
- **Parents.** A `/v2` child path uses its own placeholder (`/v2/core/accounts/{account_id}/persons/{id}`). `Declaration.Parent` already names the placeholder, so `Parent: "account_id"` works with no change; the parent attribute is then named `account_id`, which is Stripe's name and is kept verbatim (architecture ADR D7).

### D2. How the generator reads `/v2` operations

- **Operations.** `Resource.CreatePath`, `UpdatePath` and `ReadPath` take `/v2` paths; `Spec.operation` looks them up in `Spec.Paths` as it does now. For event destinations: `CreatePath: "/v2/core/event_destinations"`, `UpdatePath: "/v2/core/event_destinations/{id}"`.
- **Body media type.** `requestSchema` keeps its order (form, then JSON). It gains a check: an operation under `/v2/` must offer `application/json` and no form body, and one under `/v1/` must offer a form body. A violation is a generator error, so a spec release that changes a namespace's encoding fails `go generate` instead of producing a wrong table.
- **Response schema.** The 200 response of the create operation is the object schema, as for `/v1`. The `/v2` object schemas are named `v2.<path>` and resolve through `$ref` the same way.
- **`include` is not a field.** The create and update request schemas carry `include` as a body property, but the object has no such member, so the generator would emit a write-only attribute. `Resource.Omit` gets `"include"` for event destinations (its existing job is leaving paths out of the table). The declaration holds the values (D4).
- **Nullability is unknown in `/v2`.** The `/v1` rule at `tools/schemagen/build.go:199` marks an Optional request field `ServerDefaulted` when the response lists it as required and not nullable. No `/v2` schema declares `nullable`, so every required response field looks non-nullable. The consequence for event destinations is mild and correct: `description` is optional on create and required in the response, so it becomes Optional+Computed, which is also what the fake returns (`""`, `eventdestination.go:209`). The generator keeps the rule unchanged for `/v2`. Where the suite later shows a `/v2` field comes back null, the declaration lists it under `NotServerDefaulted` like any `/v1` exception.
- **Timestamps.** `format: date-time` strings stay `String` attributes holding the RFC 3339 text Stripe returns (architecture ADR D7). `format: decimal` and `format: int64` on `/v2` are strings and stay `String`.
- **Omitted until proven.** `webhook_endpoint.signing_secret` is omitted from the table (`Omit`) until the suite and fake prove it (resolved question 1). `events_from`, `amazon_eventbridge` and `azure_event_grid` are generated like any field; the fake answers `fake_not_implemented` for them and the tests skip with `tfacc.SkipWhenFakeLacks` (`internal/acctest/tfacc/tfacc.go:45`).
- **Create-only.** Derived from the schemas as in architecture ADR D1. For event destinations this yields `type`, `event_payload`, `snapshot_api_version`, `events_from`, `amazon_eventbridge`, `azure_event_grid` as create-only, matching the suite's "update immutable fields rejected" (the suite proves the first three) and the update schema.

### D3. JSON request bodies

The engine builds one `wire.Object` tree for every namespace; the only difference is how the client renders it. `wire.EncodeJSON` already renders `Null`, `Bool`, `Int`, `Decimal` as a bare number literal, `String`, `Array` and `Object` in member order (`internal/wire/json.go:86`).

| Case | `/v1` form | `/v2` JSON (this ADR) |
|-|-|-|
| Unknown or null in plan, create | key omitted | key omitted |
| Empty list, set, object or map in plan, create | key omitted if Optional; sent as given if the table marks it required | same |
| Changed scalar, update | `k=v` | `"k": v` |
| Changed list or set, update | indexed items | the whole new array |
| Changed nested object, update | bracketed members | the whole new object (members unchanged by the user included) |
| `metadata`, update | changed keys; removed keys as `""` | changed keys; removed keys as `null` |
| Removed Optional scalar, update | `k=` | refused (below) |
| Create-only field, update | never sent | never sent |

- **Why create drops empty values.** An empty array or object in a body is unobserved (a gap in the suite, see "What the suite does not observe"), and the fake answers `fake_unproven` for a wrong-typed value. Omitting it produces what the user configured: an Optional collection set to empty and one left unset mean the same thing on create. A required collection is sent as written, so Stripe's own error (`Required request field missing`, `invalid_fields`) reaches the user.
- **Why update sends whole arrays and objects.** Whether `/v2` replaces or merges a changed array or object is unobserved: the suite's update grows `enabled_events` from one entry to two (`event_destination_test.go:353`), which replace and merge both produce, and `webhook_endpoint` has one member. The fake replaces (`eventdestination.go:314`), which the fake's own comment does not claim as proven. Sending the whole new value is correct under replace. Under merge it is correct except for a removed list item or object member, which then stays in Stripe, so the read after apply differs from the plan and the framework reports an inconsistent result. That fails loudly rather than corrupting state, and it is the signal for the suite to settle the question (D8, suite work).
- **Metadata.** `metadataDiff` (`internal/engine/encode.go`) already returns `wire.Null{}` for a removed key and the changed value for a new one; only the form codec turns that null into `""`. The JSON codec sends `null`, which is the proven `/v2` behaviour ("update metadata"). No change to `metadataDiff`. Clear-all (`metadata = {}` or removed) is a diff of `null` per existing key, never a single empty object, for the same reason the architecture ADR gives (D2, Metadata).
- **Removed Optional scalar.** `encodeUpdate` emits `wire.Null{}` for a field removed from configuration (`internal/engine/encode.go:53`). In `/v1` that means "unset". In `/v2` it is unobserved, and the fake rejects it as `fake_unproven` for string fields (`internal/validate/v2.go:40`). On a `/v2` declaration the engine therefore does not send `null` for any field except a `metadata` key. Instead the update fails with the diagnostic "clearing `<path>` is not supported on this resource: Stripe's `/v2` behaviour for null is not yet verified". The declaration lists `ClearsWithNull []string` of dotted paths once the suite has proven each one, and the engine sends `null` for those. Event destinations have no such field: `description` is Optional+Computed and a removed Optional+Computed field keeps its value (architecture ADR D2, Clearing fields); `metadata` keys are proven. The rule matters for `v2.core.account` in wave 4.
- **Unset versus empty versus null in state.** The read side is symmetrical: a missing key and `null` both decode as absent (`isAbsent`, `internal/engine/decode.go`). An empty collection for an Optional field the user left unset decodes to null (`decode.go`), as for `/v1`. A resource whose plan holds a known empty collection keeps it when the response omits the member or returns `null`, because create sends nothing for it. A data source has no plan and reads null. On `/v2`, update sends nothing for a change between no value and an empty collection, in either direction. The suite tolerates both "absent" and "null" for a redacted `webhook_endpoint.url`, so the decoder must not rely on either.
- **Numbers and strings.** Integers are `wire.Int`; floats are `wire.Decimal` text. A `/v2` field typed `string` with `format: decimal` or `int64` is sent as a JSON string, which is what the schema says.
- **Sensitive masking in logs.** Unchanged: `Sensitive` names are masked at any depth (`internal/stripeapi/client.go:293`).

### D4. `include` values

- **Decision.** `Declaration` gains `Include []string`: the values the engine sends as `include` on retrieve, list, create and update. For event destinations: `Include: []string{"webhook_endpoint.url"}`.
- **Why always.** The architecture ADR makes `PreserveConfig` the answer to a redacted field (D2, Read and drift). With `include` the real value is read, so a URL changed out of band is drift. `Expand` already works this way for `/v1` (`Declaration.Expand`, `internal/engine/api.go:61`); `Include` is its `/v2` counterpart (Stripe: `expand` "isn't supported"; `include` is the substitute).
- **Encoding.** On `POST` it is a body member `"include": ["webhook_endpoint.url"]`. On `GET` it is a query parameter in the indexed form `include[0]=webhook_endpoint.url`, which is what Stripe documents for direct API calls and what `wire.EncodeForm` already produces for arrays (`internal/wire/form.go`, `include%5B0%5D=...`). The suite proves the scalar form `include=webhook_endpoint.url` (`event_destination_test.go:339`), and the fake accepts a scalar for an array parameter (`wire/v2.go:64`); the indexed form is documented but unproven by the suite. The fake serves it: the `stripe_core_event_destination` acceptance tests read the URL back and assert the decoded `include` array in the request log. D8 keeps the suite proof on the list.
- **Update.** Each update request carries `include`, so the response has the URL. The create request carries `include` too, so state starts with the URL without a second read.
- **Not a Terraform attribute.** `include` is never in the schema and never in state.
- **Signing secret.** `webhook_endpoint.signing_secret` would be `OnceReturned` (Computed, Sensitive, carried forward) with `include=webhook_endpoint.signing_secret` on create only. It stays out of this ADR's implementation (resolved question 1).

### D5. Decoding responses and errors

- **Responses.** `api.call` already requires an object body (`internal/engine/api.go:57`). `/v2` object responses decode like `/v1` ones: unknown members are ignored, `object` is not an attribute (architecture ADR D7), `id` and RFC 3339 strings are plain strings. The delete response (`{id, object}`) is discarded; a 404 on delete is success (architecture ADR D2). There is no `deleted: true` stub on `/v2`: a retrieve of a deleted object is 404 `not_found` (`event_destination_test.go:476`), so `isDeletedStub` never matches and the 404 path handles it.
- **Error envelope.** `decodeAPIError` reads `error.type`, `code`, `message`, `param` and `invalid_fields` (`internal/stripeapi/errors.go:55-60`), which covers the `/v2` envelope (`type` and `param` stay empty, as the suite shows). Two additions:
  1. `APIError` gains `Context wire.Object`, built for `/v2` responses only (the namespace is passed from `Client.Do`, not guessed from the body) and holding only scalars and arrays of scalars, never an object. A `/v1` error carries members such as `payment_intent` with a `client_secret`, which must not reach a diagnostic. It holds every member of `error` other than `type`, `code`, `message`, `param`, `invalid_fields`, `request_log_url`, `doc_url`, `user_message`. This keeps `field`, `event_destination_type` and `event_type` (suite: "create webhook endpoint without url", "create with unknown event type"), which today are dropped. They are appended to the diagnostic detail as JSON.
  2. `InvalidFields` is parsed into `[]InvalidField{Field, Message string}` (the shape is `[{field, message}]`, `docs/stripe-api-behaviour.md:44`); an entry that does not fit stays in the detail as raw JSON.
- **Diagnostics.** `AddToDiagnostics` (`errors.go`) keeps its summary rule (code, else type, else `HTTP <status>`; the 400s without a code get `HTTP 400`, which is honest). For each parsed invalid field it adds an attribute error when `hasAttribute` finds the attribute, with `Message` as detail; otherwise it joins the diagnostic detail. The field path is dotted (`webhook_endpoint.url`); a segment of digits is a list index. The suite only shows top-level names, so index form for array elements is unobserved: the mapper tries the longest prefix that names an attribute and attaches there. The schema lookup returns the attribute's type, and the walk stops at a set or a map: an element of a set (`enabled_events.0` when declared a set) has no stable index and a map key is data, so both attach to the set or map itself. List indexes are kept.
- **Not found.** `IsNotFound` is status-based (`errors.go:37`), so `not_found` needs no change.
- **Retries.** The retry rules in architecture ADR D5 apply by status and header, not by body. A `/v2` `429` body is unobserved; if `Stripe-Should-Retry` or `Retry-After` is present they win as before.

### D6. Pagination

- **What is needed.** The only list caller is the data source lookup (`internal/engine/datasource.go:166`), which reads one page with `limit=100` and a filter, and decides from `has_more` whether more than one match exists (`:179`). It never follows a page. That is the rule from architecture ADR D7 ("never read more pages than needed to prove zero, one or more than one match") and it holds for `/v2`.
- **Change.** A lookup against a `/v2` list sends `limit=20` (the largest value the suite sends, `event_destination_test.go:171`; `/v2` maximum page size is unobserved) and reads `data`. "More" is `next_page_url` non-null, not `has_more` (absent on `/v2`: suite `:128`, spec `required` list). The result is the same three outcomes: zero, one, or "ambiguous, more than N".
- **No token handling ships.** The page token is the `page` query parameter inside `next_page_url`, as the suite extracts it (`nextPage`, `:151`); Stripe says you may use the URLs directly. Because no engine path follows pages, no code parses `next_page_url` and no `page` parameter is sent. The first consumer that needs to follow pages (a plural data source, which the architecture ADR reserves) takes the token from the URL's `page` parameter and re-sends its own filters; it never requests the URL's host, because the fake returns a relative path (`eventdestination.go:446`) and Stripe's form is unobserved.
- **Event destinations have no lookup.** The list endpoint has no filter (`include`, `limit`, `page` only) and Stripe documents no unique key, so the data source supports `id` only, like `stripe_billing_meter`. The `/v2` lookup branch is therefore not exercised by wave 2. It is specified here so it is not invented later, and is built by the first `/v2` declaration that lists a `Lookup`. Until then no `/v2` list code exists.
- **Correction to the architecture ADR.** D5's sentence "List calls follow `starting_after` ... v2 follows `next_page_url` tokens" describes behaviour the engine does not have and, per the rule above, should not. It should read: lookups read one page and use `has_more` (`/v1`) or `next_page_url` (`/v2`) only to detect more than one match.

### D7. Client changes

| Concern | Change |
|-|-|
| Body codec | Chosen by `strings.HasPrefix(req.Path, "/v2/")` inside `Client.build`. `Request.Encoding` is removed |
| `GET` parameters | Unchanged: `wire.EncodeForm` in the query string, which renders arrays as `k[0]=v`, the form Stripe documents for `/v2` |
| `Stripe-Version` | Unchanged: sent on every request (`client.go:230`); Stripe requires it on `/v2` |
| Idempotency | Unchanged: one key per logical `POST`, reused across retries (`client.go:130`). Stripe accepts keys on `/v2` `DELETE` too, but a repeated delete already ends in 404, which the engine treats as success, so there is nothing to protect. The fake honours keys on `POST` only (`exchange.go:85`) |
| Error decoding | D5: parsed `invalid_fields` and `Context` |
| Retry, limiter, logging | Unchanged |
| `Content-Type` | `application/json` for `/v2` `POST` (set today at `client.go:197-203`) |

A `/v2` request with no parameters (`DELETE`, or a `POST` to `/enable` or `/disable`) sends no body and no `Content-Type`, which is what the suite sends (`rawDoJSON(..., nil)` sends no body, `event_destination_test.go:326`, and its `DoJSON` sets no content type for a nil body). The fake refuses a body on a `DELETE` (`internal/wire/v2.go:27`), so an empty JSON object would break every delete. Whether Stripe accepts `{}` is unobserved, so the client never sends it.

### D8. Testing

**Acceptance tests** (`internal/resources/core_event_destination/resource_test.go`, fake-backed, parallel, a fresh account each; the layout meta-test in `internal/resources/layout_test.go` requires the six by name):

| Test | `/v2` content |
|-|-|
| create and read | create webhook-endpoint destination, thin payload; assert `status`, RFC 3339 `created`/`updated`, and that `webhook_endpoint.url` is in state (proves `include` on create and read) |
| update in place | change `name`, `description`, `enabled_events` (grow and shrink), `webhook_endpoint.url`; add and remove a `metadata` key; request log shows `null` for the removed key and the whole `enabled_events` array |
| replace | change `event_payload` thin to snapshot with `snapshot_api_version`; expect a replace plan; `type` is not changeable on the fake, so it is not exercised |
| import | `ImportStateVerify` on `ed_test_...`; nothing is once-returned yet |
| delete | destroy, then retrieve via the harness is 404; destroy of an already-deleted object succeeds |
| drift | `tfacc.Post(t, acct, "/v2/core/event_destinations/"+id, ...)` changes `name` out of band; expect a non-empty plan, then restore |
| lookup | none: `id` only; a data source test covers `id` and the not-found error |

- **Request-log assertions** (fake-only): every `POST` has an `Idempotency-Key`; update bodies never contain `type`, `event_payload` or `snapshot_api_version`; the update body is JSON with `"metadata":{"drop":null}` for a removed key; the retrieve carries `include`.
- **Skips.** Fields the fake does not serve (`events_from`, EventBridge, Azure, `snapshot` without version) use `tfacc.SkipWhenFakeLacks`; the provider never works around a fake gap.
- **Unit tests.** `internal/wire`: JSON golden output for null, empty array, empty object, nested object, `metadata` diff with `null`. `internal/engine`: a fixture `/v2` declaration encodes create (empty values omitted), update (whole arrays, `null` only under `metadata`, refusal of a removed scalar unless in `ClearsWithNull`), and sends `include`. `internal/stripeapi`: the body is JSON for `/v2/` paths; `decodeAPIError` on the captured bodies quoted in the suite (`invalid_fields`, `event_type` context, `not_found`); `AddToDiagnostics` attaches `webhook_endpoint.url` and falls back for an unknown path. `tools/schemagen`: a fixture spec with a `/v2` create, update and response yields the expected table, and a `/v2` operation with a form body is an error.

**Work the suite and the fake must do first.** These are outside this repository; the provider does not guess them.

| Gap | Needed from | Blocks |
|-|-|-|
| `null` and `""` for `description` and `name` on create and update; what Stripe does | suite, then fake | `ClearsWithNull` for any field; `v2.core.account` |
| An empty array or object in a create or update body | suite, then fake | removing the "omit empty on create" rule |
| `enabled_events` shrinks on update (replace or merge) | suite | confidence in "whole array on update" |
| `include[0]=webhook_endpoint.url` on retrieve and list | suite (fake must accept it) | D4 before the first resource merges; if refused, `include=...` scalar for single values |
| `include=webhook_endpoint.signing_secret` on create | suite, then fake | `signing_secret` attribute |
| `field` form for array elements in `invalid_fields` | suite | exact error-path mapping |
| A `429` body and `Stripe-Should-Retry` on `/v2` | suite | none (status rules suffice) |
| `limit` maximum on `/v2` lists | suite | raising D6's page size |

## Resolved questions

Answered on 2026-10-18, each as recommended.

1. **`webhook_endpoint.signing_secret`.** `stripe_core_event_destination` ships without it, and the generator omits it from the table because neither the suite nor the fake proves it yet. Proving it is a priority, since the secret is what makes a webhook destination usable; the field follows.
2. **Enable and disable.** `status` is read-only Computed. A writable `status` comes once the suite proves the action calls.
3. **Clearing a `/v2` field.** The engine refuses to send `null` for anything but a `metadata` key unless the declaration lists the field in `ClearsWithNull`.
4. **Suite work before wave 4.** The gaps in the D8 table go to the suite before `v2.core.account`.

## Implementation Roadmap

### Step 1: Client and wire for `/v2`

Remove `Request.Encoding` and choose JSON by path prefix (D7). Parse `invalid_fields`, add `Context`, and map invalid fields to attributes (D5). Add the `wire` JSON golden tests.

Acceptance criteria: client tests show a `/v2/` `POST` sends `application/json` and a `/v1/` one sends a form; `decodeAPIError` keeps `field`, `event_destination_type` and `event_type` from the suite's captured bodies; `AddToDiagnostics` attaches an invalid field to `webhook_endpoint.url` when the attribute exists.

Status: DONE

Dependencies: (none)

### Step 2: Declaration and generator

Remove `Declaration.V2`; add the namespace check to `newModel` (D1), `Include` and `ClearsWithNull` (D3, D4). Add the generator's media-type check and the `Omit` of `include` (D2). Add the event destination entry to `Resources`.

Acceptance criteria: `go generate` produces `internal/generated/core_event_destination` twice with no diff; the generator fixture test covers a `/v2` operation and the form-body error; a declaration mixing `/v1/` and `/v2/` paths fails `newModel`.

Status: DONE

Dependencies: Step 1: nothing in step 2 needs it to compile, but the engine tests send through the client.

### Step 3: Engine encoding and includes

Implement the D3 table: empty-value omission on create, whole arrays and objects on update, the `/v2` refusal of a removed scalar outside `ClearsWithNull`, `include` on every call (D4). Switch the lookup "more" test to `next_page_url` for `/v2` paths (D6).

Acceptance criteria: the engine unit tests of D8 pass against a fixture `/v2` declaration; `metadataDiff` behaviour for `/v1` is unchanged (existing tests green).

Status: DONE

Dependencies: Step 1, Step 2.

### Step 4: `stripe_core_event_destination`

Declaration (`IDPrefix: "ed_"`, `Delete`, `Include`, no `Lookups`), docs example, the six acceptance tests and the data source test (D8), against a pinned fake release.

Acceptance criteria: all tests pass with `TF_ACC=1` against the fake; the meta-test is green; skips are limited to the fields listed in D2.

Status: DONE

Dependencies: Steps 1 to 3. Suite proof of the `include[0]` form (D8 table) and a fake release that serves it.

### Step 5: Architecture docs

Write the section for `architecture/provider/mapping.md` and apply the corrections to the architecture ADR (D5 pagination and idempotency sentences; D1 "v1 versus v2" wording).

Acceptance criteria: the section exists under `architecture/` and matches the code.

Status: PARTIAL. The corrections to the architecture ADR (D1, D2, D5) are applied. `architecture/` does not exist yet, so the section waits for the architecture ADR to initialise it.

Dependencies: Step 4.

## Architecture Documentation Updates

The repository has no `architecture/` directory yet (the architecture ADR initialises it). The `/v2` rules in D1 to D7 belong in `architecture/provider/mapping.md` once it exists.

## Alternatives Considered

### 1. Keep the `Declaration.V2` flag

The declaration states the namespace beside its paths.

**Pros:**
- Already in the code; explicit.

**Cons:**
- A flag and a path can disagree, and the wire only honours the path (the fake does the same). `tfacc.Post` would send a form to a `/v2` path.

**Reconsider if:** a `/v2` operation is ever served from a path without the `/v2/` prefix.

### 2. Send `null` for removed fields on `/v2`, as `/v1` sends `""`

**Pros:**
- Uniform with the `/v1` engine; no `ClearsWithNull` list.

**Cons:**
- The suite shows `null` unsets only a `metadata` key. For scalars it is unobserved and the fake treats it as unproven, so a green fake-backed test would say nothing about Stripe.

**Reconsider if:** the suite proves `null` unsets every field type, at which point `ClearsWithNull` becomes the default.

### 3. Send only the changed members of a nested object on update

A JSON-merge style patch.

**Pros:**
- Smaller bodies.

**Cons:**
- Correct only if `/v2` merges nested objects, which is unobserved. Wrong under replace, where it would drop the members left out.

**Reconsider if:** the suite shows `/v2` merges objects and arrays consistently.

### 4. Use `stripe-go` v87's `/v2` client

**Pros:**
- Handles pagination, `include` and errors.

**Cons:**
- Same reasons as architecture ADR Alternative 4: typed per-resource params defeat the generic engine.

**Reconsider if:** the architecture ADR's decision is reopened.

### 5. Follow `next_page_url` in the client for every list

**Pros:**
- One place handles `/v2` pagination.

**Cons:**
- Nothing needs more than one page; the unobserved absolute-or-relative URL form would be handled for no consumer.

**Reconsider if:** plural data sources are built.

### 6. `PreserveConfig` for `webhook_endpoint.url` instead of `include`

**Pros:**
- No `include` support in the engine.

**Cons:**
- A changed URL is invisible to Terraform, and the wave 4 `/v2` objects will need `include` anyway.

**Reconsider if:** an `include` value is expensive to request or unavailable.
