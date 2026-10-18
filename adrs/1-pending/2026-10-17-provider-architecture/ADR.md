# Architecture of the Terraform provider for Stripe

## Executive Summary

This ADR decides the architecture of `terraform-provider-stripe` (roadmap deliverable P1.0) before any code exists: a provider on terraform-plugin-framework over protocol 6, built from schema tables generated from the pinned OpenAPI spec plus a small hand-written declaration per resource, talking to Stripe through its own thin HTTP client, pinned to API version `2026-09-30.endive`, and tested in parallel against the fake with a fresh account per test. It fixes how Stripe objects map to Terraform resources (archive versus delete, immutable fields, once-returned secrets, defaults, metadata, nesting, references), the provider configuration, and the naming rules. These choices are expensive to reverse because users' state files and configurations encode them, and 69 manageable resources will copy whatever pattern the first one sets.

## Status and ownership

Status: Accepted, 2026-10-17, by the owner agent, open to the owner's veto. The ADR stays in `adrs/1-pending/` until it is built, then moves to `adrs/2-implemented/`.

Resolutions recorded at acceptance:
- **Raw client.** A raw `net/http` client instead of `stripe-go`: accepted, because a generic engine cannot use per-resource typed structs (D5).
- **Archive and expire.** Destroy archives or expires where Stripe has no DELETE: accepted. What happens when a replace reuses the same unique key is settled per resource by its acceptance tests and documented on that resource (D2).
- **Major releases.** A breaking schema change in an API version bump makes a major release: accepted (D4).
- **Licence.** Stays with the owner (P1.6). Not decided here.

Evidence is current as of: `computomatic/stripe-api-conformance` commit dac9aec (`docs/stripe-api-behaviour.md`), `computomatic/fake-stripe-api` commit c45772d (the fake's architecture ADR, `adrs/1-pending/2026-10-17-fake-architecture/ADR.md`), and the owner's `work/` files read on 2026-10-17 (`brief.md`, `decisions.md`, `roadmap/phase-3-provider.md`, `roadmap/resource-waves.md`).

The decisions below most deserved the owner's attention; each is argued where it is decided:

1. **A raw HTTP client, not `stripe-go` v87 (D5).** The roadmap names `stripe-go` first and a raw client as the alternative. This ADR picks the raw client, because a generic engine over generated tables cannot use per-resource typed SDK structs.
2. **Destroy archives where Stripe offers no delete (D2).** `terraform destroy` on a price, promotion code, shipping rate and similar objects leaves an inactive object in the account. Unique keys on it (a promotion code's `code`, a feature's `lookup_key`) stay taken.
3. **Hybrid generation (D1).** Checked-in generated tables and a generic engine, with hand-written declarations only for what the spec cannot say. This sets the cost of every later resource.
4. **A version bump with a breaking schema change is a major provider release (D4).** Stripe names a version about twice a year, so expect up to two major releases a year.
5. **Provider attribute names (D3).** `api_key`, `base_url`, `files_base_url`, `stripe_account`. No account ID is required.
6. **The licence is not decided here.** P1.6 stays with the owner (`work/roadmap/phase-3-provider.md`).

## Background Context

**Project.** The provider is the third link of the chain of guarantees: the conformance suite is validated against real Stripe, the fake against the suite, the provider against the fake (`work/brief.md`, "Chain of guarantees"). The gate on building it was lifted on 2026-10-17 (owner issue #2, option C, `work/decisions.md`). The code is wholly ours and clean room: no source from `stripe/terraform-provider-stripe` or `lukasaron/terraform-provider-stripe` is read or copied; their docs and behaviour may be observed (`work/decisions.md`, 2026-10-09 "Build the provider from scratch and own the code outright"; this repo's `CLAUDE.md`). The April 2025 spike in this repo is not a base. Its `go.mod` requires terraform-plugin-framework v1.14.1 and Go 1.23.7; the restart (P1.1) replaces it.

**Scale.** The scorecard universe has 145 entries, 69 of them manageable (`work/roadmap/resource-waves.md`, "Waves"). Wave 1 is product, price, coupon, customer, webhook_endpoint and billing_meter, plus the balance data source. Later waves include nested and parent-scoped objects (`accounts.external_accounts`, `products.features`), v2 objects (`v2.core.event_destination`) and objects that cannot be deleted. The P2 standard requires, per resource, a resource and a data source covering every writable and readable field the spec lists, and acceptance tests for create, read, update in place, replace on immutable change, import, delete and drift (`work/roadmap/phase-3-provider.md`, "Per-resource standard (P2)").

**API version policy.** One version for the whole project, pinned by the `stripe-go` major the suite uses: `2026-09-30.endive` (stripe-go v87). The provider sends it explicitly and documents it; it supports only that version; "a version upgrade is a provider release that notes every schema change" (`work/decisions.md`, 2026-10-17 "API version policy, M1.2").

**What the suite has observed** (all on `2026-09-30.endive`, `stripe-api-conformance` dac9aec, `docs/stripe-api-behaviour.md`):

- Immutable fields are rejected on update as `parameter_unknown`, with the object unchanged (lines 8, 18, 28).
- Shipping rates, tax rates and promotion codes cannot be deleted: "they are archived with `active=false`" (line 19). File links, payment method configurations and payment method domains cannot be deleted either: "retired with `expires_at=now`, configurations with `active=false` and domains with `enabled=false`" (line 79). Tax registrations cannot be deleted and are ended by setting `expires_at` a few seconds ahead (line 64). Files cannot be deleted at all, and `POST /v1/files` is served by `https://files.stripe.com` (line 78).
- Archived entitlement features are frozen: updating one is a 400, "A feature cannot be reactivated, and archiving twice is an error", and they still appear in the default list (lines 21 and 22).
- A detached product feature still retrieves as a deleted stub (line 27); deleting a terminal location twice succeeds (line 67).
- A coupon's `duration` defaults to `once` (line 7).
- `POST /v1/promotion_codes` returns `promotion: {type, coupon}` with the coupon as an ID unless expanded (line 20).
- v2 endpoints take `application/json`, page by token, and report errors as `invalid_fields` rather than `param` (lines 44 and 45).
- Concurrent access to one object can fail with 429 `lock_timeout`; "Retry with backoff" (line 76).

**What Stripe documents:**

- The webhook endpoint `secret` is "Only returned at creation" (https://docs.stripe.com/api/webhook_endpoints/object, "Attributes").
- Idempotency: "For API v1, all `POST` requests accept idempotency keys. Don't send idempotency keys in `GET` and `DELETE` requests because it has no effect"; keys are "up to 255 characters long"; keys may be pruned after 24 hours; a key reused with different parameters is an error (https://docs.stripe.com/api/idempotent_requests).
- Rate limits: sandbox "25 requests per second" global and per endpoint; 429 responses carry `Stripe-Rate-Limited-Reason`; "If a request returns a `429` status code without these headers, it wasn't the result of a rate limit. It might be a lock timeout"; retry "on an exponential backoff schedule" with randomness (https://docs.stripe.com/rate-limits).

**Tooling versions** (latest releases, `gh api repos/{owner}/{repo}/releases/latest`, 2026-10-17): terraform-plugin-framework v1.19.0, terraform-plugin-go v0.31.0, terraform-plugin-testing v1.16.0, terraform-plugin-docs v0.25.0, terraform-plugin-mux v0.23.1, stripe-go v87.0.0. terraform-plugin-testing's README: "Go **1.25** or later must be used when including this project as a dependency" (https://github.com/hashicorp/terraform-plugin-testing).

**The fake.** Its admin API lives under `/_fake/` on the same port. `POST /_fake/accounts` creates an isolated account and returns `{"account_id","secret_key","api_version"}`; `DELETE /_fake/accounts/{id}` removes it; `GET /_fake/accounts/{id}/requests` returns a bounded request log of decoded parameters; `GET /_fake/health` reports versions; an optional `-admin-token` bearer protects it (fake ADR D6, c45772d). The fake serves one API version, runs in memory, and releases an image the provider's CI pulls by exact tag (fake ADR D7, K1.5).

**Constraints.** Stripe is only ever called in a sandbox (`sk_test_` keys); the CI sandbox key is never used locally (this repo's `CLAUDE.md`). The owner's global rules forbid `any` types. No licence is set.

## Approach Overview

- **Hybrid generation (D1).** A generator reads the pinned OpenAPI spec and emits checked-in field tables per resource (types, required/optional, create-only versus updatable, nesting). A hand-written declaration per resource states only what the spec cannot: path shapes, archive behaviour, once-returned fields, lookup keys. A generic engine implements CRUD, import and drift for every resource from the two.
- **Mapping rules (D2).** One written rule set for archive versus delete, immutable fields (create-only means replace), once-returned and write-only fields, server defaults, metadata, nested objects and references. Resource code never deviates; an exception is a cited, named entry.
- **Configuration (D3).** `api_key`, `base_url`, `files_base_url`, `stripe_account`, `max_retries`; environment fallbacks; no account ID.
- **API version (D4).** One constant, derived from the same pin file as the generated tables, sent on every request. Upgrades ship as a regenerate plus a schema diff, and a major release when the diff breaks.
- **Raw HTTP client (D5).** Own client with form and JSON codecs, retries on documented signals, an idempotency key on every POST, and a client-side concurrency and rate cap.
- **Testing (D6).** terraform-plugin-testing against the fake, a fresh account per test, parallel by default; the same tests later run serially against a sandbox in a marked smoke subset.
- **Naming (D7).** Stripe's own field names verbatim, singular `stripe_<path>` type names, the Stripe ID (or `parent/child`) as import ID, data sources that look up by documented unique keys with exactly one match required.
- **Framework (D8).** terraform-plugin-framework v1.19.0, protocol 6 only, no mux, tfplugindocs for docs, OpenTofu in the CI matrix.

## Approach Details

### D1. Hand-written, generated, or both

- **Decision.** Generated scaffolding with hand-written behaviour, in three layers:
  1. `tools/schemagen` (a Go program run by `go generate`) reads `spec/openapi.json`, the same pinned file the fake pins (`spec/PIN.json` records tag, commit and sha256), and writes `internal/generated/<resource>/fields.go`: one Go table per resource listing each request field with its Stripe name, wire type, required flag, whether it is accepted on create, on update, and both, its nested fields, and the matching response field. Generated files are checked in and carry a `Code generated` header.
  2. `internal/resources/<resource>/resource.go` is hand-written and short. It declares the resource's identity (type name, ID prefix, path templates for create, retrieve, update, delete, list, including parent segments and v1 versus v2), its delete strategy (D2), its once-returned fields, its server-defaulted fields, its list-lookup keys for the data source, and any field overrides (renamed collisions, set versus list). It may not contain CRUD logic.
  3. `internal/engine` is hand-written once. From a declaration and its table it builds the framework `schema.Schema`, the resource and data source implementations, the request encoder, the response decoder and the plan modifiers.
- **How create-only is derived.** The spec lists a field in the create request schema and not in the update request schema exactly when Stripe will not take it on update. That is the observed behaviour: such fields "are rejected as `parameter_unknown`" (`stripe-api-conformance` dac9aec, `docs/stripe-api-behaviour.md:8`, `:18`, `:28`). The generator therefore marks such fields create-only and the engine attaches `RequiresReplace`. The suite and the fake prove each one, so a spec error shows as a failing acceptance test.
- **Drift control.** A CI check runs `go generate` and fails on any diff. A second check fails when a resource directory lacks a declaration, a docs example or the six acceptance tests of the P2 standard (D6). A third compares the generated tables to `terraform providers schema -json` of the previous release (D4).
- **No untyped values.** The engine moves data between three representations: framework `attr.Value` trees, a closed JSON value type (`internal/wire`: null, bool, int64, string, decimal-string, array, object, built on `json.RawMessage` decoding into that type only), and the form or JSON request body. None uses `any` or `interface{}`. Per-type switches over the closed set replace reflection.
- **Escape hatch.** A resource whose behaviour does not fit the engine (a singleton such as `tax.settings`, a stateful action) may provide a hand-written resource that implements the framework interfaces directly and reuses the codec and client. Each use is recorded in the ADR log with the reason. The engine is not extended for one resource.
- **Why this way.** The 69 manageable entries share a structure the spec already encodes, so field tables are mechanical and a hand-written copy per resource would drift from the spec and from each other. The semantics are not mechanical: which delete strategy applies, which field is returned once, which server default must not show as a diff. Those need a human decision, are few per resource, and sit in the declaration where review can see them.

### D2. How resources map to Stripe objects

Every rule below is implemented once in `internal/engine`; declarations choose among them.

**Delete versus archive.** The declaration names one strategy:

|Strategy|When|Destroy does|
|-|-|-|
|`delete`|The spec has `DELETE` for the object (customer, coupon, webhook endpoint, apple pay domain, terminal location)|Calls `DELETE`. A 404 is success. A 200 with `deleted: true` is success (a second delete returns 200 for terminal locations: `docs/stripe-api-behaviour.md:67`)|
|`archive`|No `DELETE`; the object has an off switch (price `active=false`, shipping rate, tax rate, promotion code, payment method configuration; payment method domain `enabled=false`; file link `expires_at=now`)|Retrieves, then sends the declared update (`field=value`), then removes the object from state. Retrieve first, because a second archive can be an error (`docs/stripe-api-behaviour.md:21`). An object already archived counts as done|
|`expire`|The object ends at a time (tax registration)|Sends `expires_at` a few seconds ahead and waits for status `expired` (`docs/stripe-api-behaviour.md:64`)|
|`none`|Stripe offers no way to remove it (files)|There is no resource; only a data source exists|

- The docs for each archiving resource say in the first paragraph that `terraform destroy` archives and what stays behind.
- `active` (or `enabled`) remains an ordinary Optional+Computed attribute. Read never removes an object from state because it is inactive; only a 404 does. An object archived out of band appears as drift on `active`, and `terraform import` works on archived objects. This keeps "archived" visible and avoids deleting state for something that still exists.
- Unique keys on archived objects (a promotion code's `code`, a feature's `lookup_key`) stay taken. A create that collides surfaces Stripe's error unchanged with a diagnostic suffix naming the archive behaviour. The provider never adopts an existing object silently. Whether a replace of such a resource can succeed is proven per resource by its replace acceptance test; a resource that cannot replace gets a documented `lifecycle` note rather than a hidden workaround.
- Frozen objects (an archived entitlement feature rejects updates) are handled by the `archive` strategy being last: update is never attempted after archive.

**Immutable fields.** Create-only fields (D1) get `RequiresReplace`. The update request never includes a create-only field, even unchanged, because Stripe rejects the name itself (`parameter_unknown`), not only a changed value. Replace is destroy then create under the default lifecycle.

**Write-only and once-returned fields.**

- *Once-returned outputs* (webhook endpoint `secret`: "Only returned at creation"): the declaration lists the field under `once_returned`. The attribute is Computed and Sensitive. Create stores the value in state. Read and update responses that omit it never overwrite it: the engine carries the prior state value forward. Import cannot recover it, so an imported resource has a null secret, and the docs say so. A `terraform apply -replace` is the way to mint a new one. It is not a write-only attribute, because write-only attributes are never stored in state.
- *Write-only inputs* (a secret Stripe accepts and never returns): use the framework's `WriteOnly` attribute flag only where the field is absent from the response schema, the declaration lists it under `write_only`, and a `<name>_version` companion Int64 attribute lets users trigger an update. No wave 1 resource needs it, so none ships until one does. If the oldest supported Terraform or OpenTofu release (D8) lacks write-only support, the field is Optional+Sensitive and stays in state, documented as such.
- Sensitive values never appear in logs or diagnostics (D5).

**Server-set defaults.** A field Stripe fills when the request omits it (a coupon's `duration` defaults to `once`: `docs/stripe-api-behaviour.md:7`) is Optional+Computed, and the engine sends it only when configured. The declaration's `server_defaulted` list is generated for every Optional field the response schema marks non-nullable when absent; reviewers prune it. `UseStateForUnknown` is applied only to fields Stripe never changes by itself (`created`, `livemode`, IDs). Fields that can change server-side stay unknown on update, so the plan is honest.

**Clearing fields.** An Optional (not Computed) string or scalar removed from configuration is sent as the empty string, Stripe's way of unsetting. An Optional+Computed field removed from configuration keeps its value, as is standard.

**Metadata.** `metadata` is `MapAttribute` of strings, Optional (not Computed), on every resource that has it. Create sends the map. Update sends only the diff: each key whose value changed, and each removed key as the empty string. Clear-all as a single empty `metadata` is not used, because the suite has no proof of it yet (the fake ADR lists "metadata clear-all" among behaviours the suite has not proven, c45772d, Cross-repo dependencies, "Suite" row). The provider adds no keys of its own. Stripe's limits (50 keys, 40-character keys, 500-character values) are not duplicated in validators: Stripe's error is the source of truth.

**Nested objects.** Mapped by shape, never flattened or renamed:

|Stripe shape|Terraform attribute|
|-|-|
|Hash with fixed keys|`SingleNestedAttribute`|
|Array of hashes|`ListNestedAttribute`|
|Array of scalars the docs call a set of values (`enabled_events`)|`SetAttribute`; otherwise `ListAttribute`|
|Hash keyed by a variable (currency or country: `currency_options`, `tipping`)|`MapNestedAttribute`|
|Free-form map (`metadata`)|`MapAttribute` of string|

v1 bodies use Stripe's bracket form encoding (`a[b][0][c]=v`). v2 bodies use JSON (`docs/stripe-api-behaviour.md:44`). The resource declaration states which; the encoder implements both from the same tree.

**Expandable references.** A reference attribute holds the ID string and nothing else (`product = stripe_product.main.id`). The provider never sends `expand` on resource reads, so responses carry IDs, which is what configuration holds. Where a response may carry an expanded object, the decoder takes its `id`. There is no `expand` attribute. Data sources also return IDs; expansion is a convenience users can get by referencing the related data source.

**Read and drift.** Read maps every field in the schema from the response and ignores fields the schema does not know (a later minor version of the spec adds them). A 404 removes the resource from state. Write attributes whose response is a redacted or reformatted value (the v2 event destination redacts `url` unless requested: `docs/stripe-api-behaviour.md:45`) are declared `preserve_config` so a redaction is not drift.

### D3. Provider configuration

```hcl
provider "stripe" {
  api_key         = var.stripe_secret_key   # or STRIPE_API_KEY
  base_url        = "http://127.0.0.1:12111" # or STRIPE_API_BASE, default https://api.stripe.com
  files_base_url  = null                     # or STRIPE_FILES_BASE_URL
  stripe_account  = null                     # or STRIPE_ACCOUNT, a connected account ID
  max_retries     = 3                        # default
}
```

|Attribute|Required|Notes|
|-|-|-|
|`api_key`|Yes, by attribute or `STRIPE_API_KEY`|Sensitive. A secret or restricted key (`sk_` or `rk_`). The provider does not refuse live keys: users own that choice. Empty or missing is an error at configure time, not at validate, so `terraform validate` works with unknown values|
|`base_url`|No|Default `https://api.stripe.com`. `http` is accepted only when set explicitly, which is how the provider targets the fake. The path must be empty|
|`files_base_url`|No|Default `https://files.stripe.com` when `base_url` is unset. When `base_url` is set and this is not, files use `base_url`, because the fake serves everything on one port. `POST /v1/files` is the only call to this host (`docs/stripe-api-behaviour.md:78`)|
|`stripe_account`|No|Sent as the `Stripe-Account` header for Connect platforms acting on a connected account. Not needed for a standalone account|
|`max_retries`|No|Retries per request (D5)|

- **No account ID is required.** The secret key identifies the account. The brief leaves "possibly an account ID" open (`work/brief.md`, "Component 3"); the suite uses an account ID only as a test configuration input, and the provider has no use for one. A read-only `stripe_account` data source (the account the key belongs to) may come with the Connect wave.
- The API version is not configurable (D4). There is no `api_version` attribute.
- A `User-Agent` of `terraform-provider-stripe/<version>` and Stripe's `X-Stripe-Client-User-Agent` shape are sent; the key is never logged. The framework's `tflog` masks the `api_key` field.
- Request timeout: per-resource `timeouts` blocks (terraform-plugin-framework-timeouts) for create, update and delete; reads use a fixed provider default.

### D4. API version

- `internal/stripeapi.Version` is the constant `2026-09-30.endive`. It is read from `spec/PIN.json` by a generated file, so the tables and the header cannot disagree. Every request, v1 and v2, files host included, sends `Stripe-Version: <Version>`.
- The provider documents the version on the registry index page and in every release note.
- **Upgrade procedure**, after the suite and the fake have moved (the project's build order, `work/decisions.md` 2026-10-17):
  1. Update `spec/openapi.json` and `spec/PIN.json` to the matching release of `stripe/openapi`.
  2. Run `go generate`. The diff of `internal/generated/` is the schema change.
  3. Run `tools/schemadiff` against the previous release's `terraform providers schema -json`. It lists added, removed and changed attributes and flags breaking ones: a removed or renamed attribute, a type change, an optional that became required, a computed that became required-replace.
  4. Run the full acceptance suite against the matching fake release.
  5. Release. Breaking diff: new major version. Additive only: minor. The release notes embed the schemadiff output.
- Within a named version, Stripe's monthly versions are additive (`work/decisions.md` 2026-10-17). New fields appear as new attributes at the next regenerate, as a minor release.

### D5. HTTP client

- **Decision.** A raw client in `internal/stripeapi` on `net/http`, not `stripe-go` v87. About 600 lines: transport, form and JSON codecs, error decoding, retry, idempotency.
- **Why.** `stripe-go` builds requests from per-resource typed parameter structs. The engine (D1) holds a field table, not a struct per resource, so using the SDK would mean hand-written mapping code for each of 69 resources, which is the drift D1 exists to remove. The SDK's retry policy is also not ours to tune against `lock_timeout` and the fake. The cost is owning the codecs and the error shapes; the suite and the fake already pin both, and the acceptance suite exercises them on every resource.
- **Errors.** Decode Stripe's `{"error": {...}}` (v1) and `{"error": {"code","message","invalid_fields"}}` (v2: `docs/stripe-api-behaviour.md:44`). Map into diagnostics with summary `Stripe API error: <code or type>`, detail the message, the `param` as an attribute path where the attribute exists, and the `Request-Id` header. Fields Stripe omits (`code`, `param`) stay absent, never defaulted: the suite shows many errors carry neither (`docs/stripe-api-behaviour.md:15`, `:23`).
- **Retries.** Retry on transport errors, 429, and 5xx, up to `max_retries`, with exponential backoff, full jitter, a 250 ms base and an 8 s cap, bounded by the operation's timeout. Honour `Retry-After` when present. A 429 is retried whether it is a rate limit or a `lock_timeout`: Stripe says both retry with backoff (https://docs.stripe.com/rate-limits, "Object lock timeouts"). A 4xx other than 429 is never retried. If `Stripe-Should-Retry` is present on the response it overrides the status rule.
- **Idempotency keys.** Every `POST` carries an `Idempotency-Key` header (a v4 UUID, so within 255 characters), generated once per logical operation and reused across that operation's retries. `GET` and `DELETE` carry none, as Stripe says it "has no effect" (https://docs.stripe.com/api/idempotent_requests). A retry after a lost response therefore cannot create a second object. A key is never reused for a different request.
- **Rate limits.** A per-provider-instance limiter caps in-flight requests (default 4, matching a typical Terraform parallelism) and spaces requests below the sandbox's documented "25 requests per second" (https://docs.stripe.com/rate-limits). The limiter is not configurable in the first release. Files use the Files API's limits (20 per second), covered by the same cap.
- **Pagination.** List calls follow `starting_after` with `limit=100` and stop at `has_more=false`; v2 follows `next_page_url` tokens. Data sources never read more pages than needed to prove zero, one or more than one match (D7).
- **Logging.** `tflog` at DEBUG logs method, path, status and `Request-Id`; bodies only at TRACE, with `Authorization` and every field the declaration marks sensitive masked.

### D6. Testing

- **Framework.** `terraform-plugin-testing` v1.16.0 (needs Go 1.25 or later, so the module's Go version is at least that), `ProtoV6ProviderFactories`, `resource.ParallelTest` as the default.
- **Harness** (`internal/acctest`). `TestMain` reads `STRIPE_FAKE_URL` (and `STRIPE_FAKE_ADMIN_TOKEN` when set). `NewAccount(t)` calls `POST /_fake/accounts`, registers `t.Cleanup` calling `DELETE /_fake/accounts/{id}`, and returns a `Config` with `Key` and `BaseURL`. Every test builds its provider block from that, so no test shares state and tests run fully parallel. The admin client is typed to the four calls the harness needs. Acceptance tests require `TF_ACC=1`.
- **Per-resource tests.** Each resource ships the P2 set: create and read, update in place, replace on a create-only change (with `plancheck.ExpectResourceAction(..., ResourceActionReplace)`), import (`ImportStateVerify`, ignoring once-returned fields), delete (and the archive aftermath for archive strategies), and drift. Drift tests change the object out of band through the typed Stripe client in the harness and expect a non-empty plan, then an apply that restores it. A meta-test fails when a resource directory lacks any of these by name.
- **Request log assertions.** Update tests read `GET /_fake/accounts/{id}/requests` to assert the provider never sent a create-only field on update and sent each POST with an idempotency key. This is a fake-only assertion and is skipped on a sandbox.
- **Unit tests.** Codecs (round trip of the nested-shape table), the retry policy (table-driven with a fake transport), the schemadiff tool, and the generator against a small fixture spec.
- **Real sandbox (phase 4).** The same tests run with `STRIPE_ACC_TARGET=sandbox` and a key from the environment. In that mode `NewAccount` makes no admin call and returns the sandbox key, tests marked smoke are the only ones run, parallelism is one (`-parallel 1`, the sandbox is a single shared account), every name carries a random prefix, and fake-only assertions skip. CI uses the CI sandbox key from `StripeProvider/CiSandbox` read at run time (`work/decisions.md`, 2026-10-09); no one runs it locally against that sandbox. A failure there feeds back to the suite, as the brief describes (`work/brief.md`, "Closing the loop").
- **CI shape** (P1.3). Unit tests, generated-code check, docs check and the acceptance suite run on every PR against a pinned fake image by exact tag, in parallel. The Terraform and OpenTofu matrix (D8) runs the acceptance suite once per CLI.

### D7. Naming, import and lookups

- **Resource type names.** `stripe_` plus the universe entry path, singular, dots to underscores: `stripe_product`, `stripe_billing_meter`, `stripe_tax_registration`, `stripe_radar_value_list_item`, `stripe_product_feature` (from `products.features`), `stripe_account_external_account`. The `v2` segment is dropped, so `v2.core.event_destination` is `stripe_core_event_destination` and `v2.core.account` is `stripe_core_account`; a collision with a v1 name (none known) is resolved by keeping `v2_` on the newer one. Legacy objects keep their own name (`stripe_plan`). Data sources use the same type name. The balance probe is the data source `stripe_balance`.
- **Attribute names.** Stripe's field names, verbatim, at every nesting level. No renaming, flattening, singularising or prefixing, so docs map one to one to Stripe's reference and the generator needs no per-field table. `id` is Computed. `object` is omitted. Other read-only fields (`created`, `livemode`, `status`) are Computed. Timestamps stay Int64 Unix seconds on v1 and RFC 3339 strings on v2, as Stripe returns them; amounts stay integers in minor units, with `*_decimal` fields as strings. A field whose name Terraform reserves (`count`, `depends_on`, `for_each`, `lifecycle`, `provider`, `providers`) gets a `_` suffix by an override in the declaration; none exists in the waves read so far.
- **Import IDs.** The Stripe ID for a top-level object (`terraform import stripe_product.x prod_123`). For an object addressed under a parent, `<parent_id>/<child_id>` (`stripe_product_feature`: `prod_123/pf_456`). Singletons (a per-account settings object) import with a constant ID `default`. Each resource also declares a resource identity with the same shape, so import blocks with identity work on Terraform versions that support it. Attributes that Stripe never returns cannot be set by import and are listed in the docs.
- **Data source lookups.** Every data source accepts `id`, or the lookup keys its declaration lists, exactly one form per block (`ExactlyOneOf`). A lookup key is a field that Stripe documents as unique per account (price `lookup_key`, promotion code `code`, entitlement feature `lookup_key`, billing meter `event_name`, radar value list `alias`, apple pay domain `domain_name`) and that appears among the list endpoint's parameters. A tax registration's `country` is not one (the list endpoint filters by `status` only, and an expired registration does not free the country). An apple pay domain's `domain_name` is offered although Stripe accepts duplicates, because an ambiguous match is an error. Rules:
  - Lookups use the List endpoint's filter, never the Search API, because search results lag writes (the fake ADR also lists "search lag" as a known divergence, c45772d).
  - Zero matches is an error "no <type> found for <key>=<value>". More than one match is an error naming how many. The provider never picks the first of many.
  - A key that is not unique in Stripe (`name`, `email`) is not offered. A user who needs those uses `id`.
  - Data sources return every readable field of the object, same attribute names as the resource, all Computed.
- **Plural data sources** (`stripe_products`) are out of scope for this ADR. The names `stripe_<plural>` are reserved for them.

### D8. Framework, protocol and OpenTofu

- terraform-plugin-framework v1.19.0 and terraform-plugin-go v0.31.0 (the latest releases on 2026-10-17); `go.mod` is bumped to the latest at P1.1 and kept current by dependency updates.
- **Protocol 6 only**, served with `providerserver.NewProtocol6WithError`. `terraform-registry-manifest.json` declares `protocol_versions: ["6.0"]`. No SDKv2 and no `terraform-plugin-mux`: all resources are framework resources, so there is nothing to mux.
- **Terraform and OpenTofu.** The minimum supported versions are the oldest releases that both support protocol 6 and the framework features used (resource identity, write-only where used), fixed in P1.3 and listed in the docs. The CI matrix runs the acceptance suite with the Terraform and the OpenTofu CLI (pointing terraform-plugin-testing at a CLI binary; the exact variables are confirmed in P1.3 from the testing module's documentation, since the README read for this ADR does not cover them). Provider code uses no feature absent from the OpenTofu release in the matrix. The provider is published to both registries at P1.6.
- **Docs.** `tfplugindocs` v0.25.0 generates registry docs from the schema and from `examples/` (one example per resource and data source, as P1.5 requires); CI fails if regenerating changes the checked-in output.
- **Go.** The latest stable Go at P1.1, and at least 1.25 for terraform-plugin-testing.
- **Layout:**

```
main.go                       providerserver entry
spec/                         PIN.json, openapi.json
tools/schemagen/              generator
tools/schemadiff/             schema breaking-change check
internal/provider/            provider schema, configure
internal/engine/              schema builder, CRUD, import, plan modifiers
internal/wire/                closed JSON value type, form and JSON codecs
internal/stripeapi/           client, retry, errors, version
internal/generated/<res>/     checked-in field tables
internal/resources/<res>/     declaration, docs example, acceptance tests
internal/acctest/             fake admin client, harness
examples/                     one per resource and data source
docs/                         generated
```

## Implementation Roadmap

### Step 1: Restart scaffold (P1.1)

Remove the spike's code. Create the module, `main.go`, the provider with the D3 configuration, `internal/stripeapi` with the version constant, and the layout in D8. Targets the fake in `terraform init` and `plan`.

Acceptance criteria: an empty provider builds; `terraform init` and `terraform plan` work with `base_url` pointing at a running fake; the provider rejects a missing key at configure time; the repo's `CLAUDE.md` line "add no resources until the owner agent says so" is replaced.

Status: PENDING

Dependencies: (none)

### Step 2: Client and wire (P1.2, part 1)

`internal/wire` and `internal/stripeapi` as in D5: codecs, errors, retry, idempotency, limiter.

Acceptance criteria: unit tests for the nested-shape round trip, the retry table (429 with and without `Stripe-Rate-Limited-Reason`, 5xx, `Stripe-Should-Retry`), and the idempotency key rules pass; a smoke call to the fake's balance endpoint succeeds with a fresh account.

Status: PENDING

Dependencies: Step 1: module and layout.

### Step 3: Generator and engine (P1.2, part 2)

`tools/schemagen`, `tools/schemadiff` and `internal/engine` as in D1 and D2, with the CI checks for generated drift.

Acceptance criteria: running `go generate` twice yields no diff; the generator fixture test passes; the engine builds a schema for a fixture declaration with every D2 shape in the nested-object table.

Status: PENDING

Dependencies: Step 1: module and layout. Step 2: the engine calls the client and codecs.

### Step 4: Test harness and the product resource (P1.2, part 3)

`internal/acctest` and `stripe_product` with its data source, using all the machinery.

Acceptance criteria: product passes the P2 acceptance set against a pinned fake release, with parallel tests each on a fresh account; the meta-test for required tests is green.

Status: PENDING

Dependencies: Step 2, Step 3. The fake's product resource (K1.4, fake ADR step 7) and a fake release (K1.5).

### Step 5: CI and docs (P1.3, P1.5)

Workflows for unit, generate check, docs check and the Terraform and OpenTofu acceptance matrix; `tfplugindocs` wiring and the examples check.

Acceptance criteria: required on PRs; a PR that breaks a generated file or a resource's docs fails; the acceptance suite runs on both CLIs.

Status: PENDING

Dependencies: Step 4. The fake image pull grant (fake ADR step 8, owner-only).

### Step 6: Wave 1 resources (P1.4)

Declarations, examples and tests for price, coupon, customer, webhook_endpoint, billing_meter and the balance data source. Product is done in Step 4.

Acceptance criteria: each meets the P2 standard items 1 to 4; item 5 (the sandbox smoke test, L1) is recorded when it has run.

Status: PENDING

Dependencies: Step 5. Fake wave 1 (K1.4).

### Step 7: Architecture docs

Land the `architecture/` documents drafted in `1-architecture-doc-updates.md` once Steps 1 to 4 are done.

Acceptance criteria: the files exist as drafted and match the code.

Status: PENDING

Dependencies: Step 4.

## Architecture Documentation Updates

The repo has no `architecture/` directory yet; initialise it with `/init-adrs`. The drafted prose is in `1-architecture-doc-updates.md` beside this ADR. It creates `architecture/README.md` (map and reading order) and `architecture/provider/mapping.md` (the D2 rules, D3 configuration, D4 version policy, D7 naming), and the repo's `CLAUDE.md` gains the `architecture/` and `adrs/` lines the fake's `CLAUDE.md` carries.

## Alternatives Considered

### 1. Fully hand-written resources

One Go file per resource, with its own schema and CRUD.

**Pros:**
- Simplest to read for one resource; no generator or engine to learn.
- Full freedom per resource.

**Cons:**
- 69 resources and their data sources drift from each other and from the spec; every rule in D2 gets copied, then diverges.
- A spec upgrade (twice a year) means a manual audit of every resource.

**Reconsider if:** the manageable set shrinks to a handful, or the engine proves unable to express more than a quarter of the resources.

### 2. Fully generated provider

Generate resources, CRUD and docs from the spec with no hand-written declarations.

**Pros:**
- Least human effort; follows Stripe's spec exactly.

**Cons:**
- The spec cannot say which delete strategy applies, which field is returned once, or which defaults are server-set. The suite's record shows these vary by object (`docs/stripe-api-behaviour.md:19`, `:21`, `:64`, `:79`).
- Users would get Stripe's shape without Terraform judgement, the main thing that makes a provider good.

**Reconsider if:** Stripe's spec gains machine-readable lifecycle annotations.

### 3. Interpret the spec at run time

Build the schema from the spec when the provider starts.

**Pros:**
- No generated code in the repo.

**Cons:**
- Docs generation, review of schema changes and the schemadiff all need static schema. Startup cost and a run-time dependency on a large spec file for every `terraform plan`.

**Reconsider if:** none expected.

### 4. `stripe-go` v87 as the client

Use the SDK the suite already uses.

**Pros:**
- Maintained retries, error types and encoding; the same library as the suite, so its behaviour on the wire is already exercised.
- Faster to the first resource.

**Cons:**
- Typed per-resource parameter structs force hand-written mapping per resource, which defeats D1.
- Retry and rate control are less tunable; v2 and files hosts need extra handling.

**Reconsider if:** D1's engine is abandoned for hand-written resources, or the raw client's codecs show repeated divergence from the suite's SDK-based tests.

### 5. Delete-only destroy (leave archive-only objects in the account)

Destroy only removes from state when Stripe has no delete.

**Pros:**
- Never mutates an object on destroy.

**Cons:**
- A destroyed price stays active and keeps taking payments through links that reference it. That is a surprising, unsafe default.

**Reconsider if:** users ask for an opt-out; it can be added as a per-resource attribute later without breaking state.

### 6. Remove an archived object from state on read

Treat `active=false` as gone.

**Pros:**
- Matches what users see in the dashboard.

**Cons:**
- Hides an object that still exists and may be referenced elsewhere; an import of an archived object would be impossible; a user who sets `active = false` on purpose would see their resource disappear.

**Reconsider if:** none expected.

### 7. A `stripe_account_id` required provider attribute

**Pros:**
- Matches the suite's configuration (`work/brief.md`, "Component 1").

**Cons:**
- The key already identifies the account; a second source can only disagree. Connect use is covered by `stripe_account`.

**Reconsider if:** a resource needs the account's own ID as input; a data source gives it without a provider attribute.

### 8. Terraform plugin SDK v2, or a mux with it

**Pros:**
- Older, widely documented.

**Cons:**
- No new resources should use SDKv2; the framework supports nested attributes, write-only attributes and identity, which D2 depends on. Mux adds a second server for no benefit.

**Reconsider if:** none expected.

### 9. Search API for data source lookups

**Pros:**
- Can match any field, including `email` and `name`.

**Cons:**
- Search results lag writes, so a data source can miss an object created seconds earlier in the same apply. It has a lower rate limit (20 per second) and is not guaranteed available on all objects.

**Reconsider if:** Stripe documents read-after-write consistency for search.

### 10. Rename attributes into Terraform idiom

Flatten hashes, singularise arrays, use `_id` suffixes.

**Pros:**
- Looks more native.

**Cons:**
- Every rename is a table entry per field, which defeats generation; docs stop matching Stripe's reference; users read the reference to learn the provider.

**Reconsider if:** none expected.
