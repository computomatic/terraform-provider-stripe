# Every attribute the API offers, with documented defaults

## Executive Summary

The provider exposes 61% of the request parameters and 48% of the response fields the pinned spec lists, because its field tables were trimmed to what the conformance suite proves. The owner has directed that every resource and data source cover every attribute the API offers (provider issue #65), proven by the suite against Stripe and served faithfully by the fake, and that the docs state every argument's default and who sets it (issue #64). This ADR defines "every attribute" and its few exclusions, settles how the provider represents the shapes its tables cannot hold today and how state treats them, takes defaults from the suite's observations, and plans the work so that each wave of resources follows the suite's proof and the fake's support for it, with every breaking change landing before `v0.1.0`.

## Status

Status: Accepted, 2026-10-29, by the product owner, after a revision the same day to the owner's direction on proof (below). It amends `adrs/1-pending/2026-10-17-provider-architecture/ADR.md` (D1's per-resource standard; D2's server-default, clearing and write-only rules; D7's naming rule, by the named exception of `<name>_data` for the hash form of a token-or-hash parameter, D3 below) and `adrs/1-pending/2026-10-22-documentation-layout/ADR.md` (D2's argument line, D3a's remedy for oversized pages). The ADR stays in `adrs/1-pending/` until it is built.

Evidence is current as of 2026-10-28: provider commit `0832bd8`, the pinned spec (`stripe/openapi` tag `v2585`, API version `2026-09-30.endive`, `spec/PIN.json`), the fake at commit `19079c2` and the conformance suite at commit `9212b25`. Beside this file:

- `measurement.md`: the gap, measured, with the script that produced it (`measure_test.go.txt`).
- `handoff.md` and `handoff-leaves.tsv`: per Stripe object, every attribute and shape the provider needs the suite to prove and the fake to serve, with the hard cases, for those repositories' ADR sessions; produced by `handoff_test.go.txt`.

Resource type names use the `/v2` renames of provider issue #66: `stripe_account_v2` (today `stripe_core_account`), `stripe_account_person_v2`, `stripe_event_destination_v2` and `stripe_event_v2`.

Decided by the product owner on 2026-10-29, from the first draft's questions (recorded here so that the decisions below need no re-arguing):

1. Embedded copies of other objects are read in full; their docs link to the copied object's data source page.
2. No doc marks for unproven fields: there are no unproven fields.
3. Write-only secrets stay in state as Sensitive; Terraform's write-only arguments are a later option.
4. Paged lists returned only when expanded are excluded; their own resources and data sources manage them.
5. `confirm` is excluded wherever a `status` attribute models the same action; the other one-off instructions are arguments kept from configuration.
6. Both forms of a token-or-hash parameter are supported; the token form is never dropped (test tokens such as `btok_us_verified` onboard sandboxes).
7. Every change that would break users once released lands before `v0.1.0`, across all waves.
8. Pages over the docs budget link embedded copies, then move their largest nested blocks to per-resource guide pages; never to Stripe's reference.
9. Country-specific fields are proven by the suite, first through connected accounts created in those countries from the US platform.
10. The product owner approves each new entry in the exclusion register in review; object-level exclusions go to the owner.

Decided by the product owner at acceptance, 2026-10-29, from the revised draft's questions:

11. Today's attributes stay exposed while the suite audits them against `handoff.md`; one the audit cannot prove yet is proven in its wave, never removed (D1).
12. `<name>_data` for the hash form of a token-or-hash parameter is accepted as a named exception to the architecture ADR's verbatim-names rule (D7), because it mirrors Stripe's own `price` and `price_data` and keeps today's string attributes (D3).
13. Lists returned only when expanded are excluded from data sources too, because their own data sources read them (D1, D5).
14. Lists Stripe returns a page at a time on every read (`lines`, `items`, `reversals`) are kept as today; the docs say the attribute holds the first page Stripe returns and where to read the rest (D3).
15. Changes that break only behaviour (removing an argument no longer clears it; state read back from Stripe) count as breaking, so `v0.1.0` waits on the suite's minimal creates and unsetting proofs for the 57 objects of Step 6 (D6).
16. A leaf is exposed as soon as the pinned fake release's `attributes.json` lists it as `verified` (amended: see below and D1). Waves order the work; they are not release gates (D6).

Amended before merge, 2026-10-29, to match the suite's ADR (`stripe-api-conformance`, `adrs/1-pending/2026-10-29-attribute-coverage/ADR.md`, suite PR #71) and the fake's ADR (`fake-stripe-api`, `adrs/1-pending/2026-10-29-attribute-parity/ADR.md`, fake PR #65), both accepted by the product owner: the provider gates on the pinned fake release's `attributes.json` (D1, "The evidence gate"), vendors the suite's per-run evidence file that `attributes.json` names, and does not expose hosted-flow leaves the suite proves against Stripe only.

Also accepted: the measuring and hand-off scripts and `handoff-leaves.tsv` stay beside this ADR until the Step 1 coverage report supersedes them.

The decisions most worth scrutiny:

1. **Every exposed attribute is proven (D1, D6).** The generator exposes a leaf only when the pinned fake release's `attributes.json` lists it as `verified`: proven against Stripe by an assertion that also ran on the fake, served, and passing the release's fidelity run. Each leaf waits on the suite and the fake, however long that takes; waves only order the work.
2. **Optional and Computed unless the suite shows otherwise (D2, E2).** An optional argument that Stripe returns keeps Stripe's value when removed from the configuration, unless the suite shows Stripe leaves it empty and proves how to unset it. 801 argument leaves exposed today are affected, so the suite's minimal creates for 57 objects come before `v0.1.0`.
3. **Apply keeps the plan; refresh reads Stripe (D2, E4).** A defence for users when Stripe answers differently from what the suite recorded: a warning instead of "Provider produced inconsistent result after apply", and an error in the acceptance tests.
4. **The hash form of a token-or-hash parameter is `<name>_data` (D3).** A named exception to the architecture ADR's rule of Stripe's names verbatim, chosen because it keeps every existing string attribute and follows Stripe's own `price` and `price_data` pattern.

## Background Context

### The owner's direction

Provider issue #65, "Support missing Account (v1) company properties" (per the owner, 2026-10-24):

> The `stripe_account` resource has a `company` attribute which only supports `company.name` currently. The stripe API docs list 25 other supported properties under `company` (`company.address`, `company.address_kana`, etc.), and more nested properties under many of those. The resource needs to support all attributes which are available in the API. This rule extends to all attributes on all resources and data sources.

Provider issue #64, "Document default values":

> The documentation should indicate the default value which will be used for every default field, and whether that default comes from the provider implementation or the Stripe API.

On proof (per the owner, 2026-10-29):

> To be clear about my expectations: I expect the fake api to behave like the official stripe API, including attribute parity. That implies the conformance suite also has full coverage of these attributes.

> I'd like the fake stripe API to be so complete and faithful that I could potentially license it to customers who need a fake API to test their own projects against.

Per the product owner, 2026-10-29: every attribute the provider exposes is proven by the suite against real Stripe and served faithfully by the fake; the suite and the fake each design how in their own ADR; this ADR stays about the provider and hands them what it needs.

### What the provider does today

- The architecture ADR promised "a resource and a data source covering every writable and readable field the spec lists" (`adrs/1-pending/2026-10-17-provider-architecture/ADR.md:32`, `0832bd8`). The generator instead takes per-resource `Keep`, `Omit`, `ReadOnly` and `NoResponse` lists (`tools/schemagen/build.go:31-74`, `0832bd8`) that trim tables to what the suite proves. `stripe_account` keeps `company.name` alone and does not read back `capabilities`, `company` or `individual` (`tools/schemagen/resources.go:107-121`, `0832bd8`). `discounts` is omitted from five billing objects because "no attribute holds both ways" (`tools/schemagen/resources.go:44-45`).
- The generator fails on some shapes: an `anyOf` of several non-string schemas, nesting deeper than 12 levels, maps of unsupported value shapes (`tools/schemagen/build.go:463-580`, `0832bd8`). `Omit` lists hold them.
- An optional argument is Computed only when the generator marks it server-defaulted (the response schema requires it and it is not nullable) or the declaration lists it (`internal/engine/model.go:720-728`, `0832bd8`). Otherwise it is Optional alone, and removing it from the configuration sends `""`, Stripe's unset (architecture ADR, line 113).
- A field the response does not carry keeps the configured value (`internal/engine/decode.go:96-116`, `0832bd8`). A field Stripe returns in another shape than the table's fails the read ("Unexpected Stripe response", `internal/engine/decode.go:159-162`).
- `UseStateForUnknown` is attached to `id`, `created`, `livemode`, create-only fields, once-returned fields and Optional nested objects outside lists (`internal/engine/model.go:752-754`, `0832bd8`). A plan that changes nothing configured is rewritten to the state (`internal/engine/resource.go:112-128`, `0832bd8`), so plan noise appears only when something changes.
- The docs say nothing structured about defaults. Descriptions are Stripe's text, which sometimes says "Defaults to" (documentation layout ADR D1, line 147). The provider's own defaults are on provider arguments (`max_retries`, "Defaults to 3.") and timeouts; no resource attribute has a framework default.

### The measurement

From `measurement.md`, counting leaves (scalars, scalar arrays, free-form maps) at every nesting level, every leaf of every embedded copy included:

|Side|Spec leaves|Exposed|Missing|
|-|-|-|-|
|Request parameters (62 resources)|3,238|1,971|1,267|
|Response fields (121 tables)|18,875|9,120|9,940|

Of the missing, 725 request and 740 response leaves were trimmed for lack of proof, 7,694 response leaves sit inside embedded copies of other objects (an invoice's `last_finalization_error.payment_intent` alone holds about 1,000), and the rest are unions (63 and 1,171), write-only values (151), inline data for other objects (123), one-off instructions (28), `expand` and `include` (61), hashes with no keys (97 and 106), IDs sent where the response returns the object (221), `discounts` (23), recursion (2) and deprecated fields (2).

The provider already exposes 3,502 leaves inside embedded copies in 29 tables (a Treasury transaction's `flow_details`, 651; a terminal reader's `action`, 402).

Counted as the suite will need to prove them (`handoff.md`: embedded copies collapsed to the 82 places they appear, exclusions applied), the provider will expose 3,177 request leaves (1,206 new) and 7,180 response leaves of the objects themselves (1,974 new). Of those, 2,111 concern payment methods a US account may not offer, 331 are filled only by a customer's action in a hosted page, 254 depend on the account's country, 244 on onboarding, 229 need a file and 63 appear only after a failure.

On defaults: of 1,450 exposed optional argument leaves, 147 descriptions mention a default, and the spec has no `default` keyword on any parameter. The spec cannot document defaults; the suite's observations must.

On size: the provider's whole schema (62 resources, 121 data sources) builds in 68 ms and is 6,786,116 bytes as protocol JSON, measured on `0832bd8` by calling `GetProviderSchema` in process. The largest page is 164,148 bytes against the 250,000-byte budget (`tools/docgen/render_test.go:279`, `0832bd8`).

### The chain of guarantees

The suite proves Stripe's behaviour against a real sandbox, single-threaded against one account. The fake serves what the suite proves and refuses the rest: "A registered operation also answers `fake_not_implemented` when the request carries a spec-valid parameter that is not in `Implements`, naming the parameter, so a parameter is never silently dropped" (`architecture/fake-stripe-api/resources/resources.md:22`, fake `19079c2`). The fake's CI runs the suite at a pinned commit (`conformance/SUITE_REF`, `architecture/fake-stripe-api/fake-stripe-api.md`, "Conformance in CI"). The provider's CI runs against a fake release pinned by `.fake-version` (`CONTRIBUTING.md`, "CI", `0832bd8`).

One request can prove many fields: the suite's record of a payment link created with one line item lists its defaults at once ("`customer_creation: "if_required"` (not `always`), `payment_method_collection: "if_required"`, `billing_address_collection: "auto"` ...", `docs/stripe-api-behaviour.md:77`, suite `9212b25`).

## Approach Overview

- **Every attribute (D1).** Every leaf of the create request, update request and response schemas is an attribute of the resource, and every response leaf one of the data source, except by five rules and a register whose entries carry a reason and the product owner's approval. A leaf is exposed once the pinned fake release's `attributes.json` lists it as `verified`. A generated coverage report shows every leaf's state, and CI fails on a leaf that is neither exposed, excluded nor awaiting proof.
- **State (D2).** Six engine rules: sent as configured; Optional and Computed unless the suite shows Stripe leaves the argument empty; write-only values and instructions kept from configuration, Sensitive where Stripe reports `<name>_provided`; apply keeps the plan and warns where Stripe answers differently; unset settings keep their prior value where the suite shows Stripe does not change them; an unexpected shape warns on read.
- **Shapes (D3).** Response unions become one object with every variant's members; a token-or-hash parameter keeps its string attribute and gains `<name>_data`; embedded copies are read in full; recursion stops at the repeat; `discounts` maps between its request and response shapes.
- **Defaults (D4).** Every optional argument's line ends with one default statement: the value and whether Stripe or the provider sets it, or none. Stripe's defaults come from the suite's minimal creates.
- **Data sources (D5).** Every response field, including nested, polymorphic and embedded ones.
- **Work plan (D6).** Five engine and generator steps, a step that makes every breaking change on today's attributes before `v0.1.0`, then six waves, each gated on the suite and the fake.

## Approach Details

### D1. What "every attribute" means

**The universe.** For a table built from a create (or read, or list) operation and an optional update operation, the spec leaves are every path reached by walking the create request body, the update request body and the response object through `$ref`, `anyOf`, `oneOf`, `allOf`, arrays and maps, as `measurement.md` defines them. A resource covers every request leaf as an argument (or a member of one) and every response leaf as an attribute; a data source covers every response leaf. This replaces the architecture ADR's sentence on line 32 with a definition the generator checks.

**Exclusion rules.** Only these leave a leaf out without a register entry; the coverage report names the rule for each leaf it removes:

|Rule|Leaves|Reason|
|-|-|-|
|X1. Request controls|top-level `expand` and `include`|They shape the response, not the object; the engine sends them from the declaration (architecture ADR D2, `/v2` ADR D4)|
|X2. Type markers|a response hash with no keys, such as a payment method's `affirm: {}`|It carries no value; `type` says the same (`tools/schemagen/build.go:294-299`, `0832bd8`)|
|X3. Paged lists returned only when expanded|a response list envelope (`object: list`) that the object's schema does not require and no request writes: 14 in the provider's objects, such as a customer's `subscriptions`, `sources` and `tax_ids`, a charge's `refunds` and an account's `external_accounts`|Their own resources and data sources manage and read them (`stripe_subscription`, `stripe_customer_card`, `stripe_tax_id`, `stripe_refund`, `stripe_account_external_account`); excluded from resources and data sources alike, per the product owner|
|X4. The recursion cut|a schema met again on its own path|Held as the repeat's `id`, or left out where it has none (D3)|
|X5. `confirm` where `status` confirms|`confirm` on a resource whose declaration has a `status` transition that confirms (payment and setup intents)|The transition is the resource's one way to confirm (money-moving ADR D5); per the product owner|

Everything else is in, including deprecated fields (marked `Deprecated` in the schema and the docs, `tools/docgen/render.go:91-116`, `0832bd8`), inline data for other objects (`price_data`), one-off instructions (`proration_behavior`, `off_session`, `return_url`), write-only values and embedded copies.

The schema marks a list returned only when expanded by leaving it out of the object's `required` list: a customer's `subscriptions`, `sources` and `tax_ids` and a charge's `refunds` are not required, while an invoice's `lines` and a subscription's `items`, returned on every read, are (`spec/openapi.json`, `components.schemas.customer.required` and `components.schemas.invoice.required`, pin `v2585`).

**The register.** Any other exclusion is an entry in `Resource.Exclude`, a map from dotted path to reason, in `tools/schemagen/resources.go`. An entry names its evidence (a suite test, a Stripe doc URL, or a framework limit). The product owner approves each new entry in review; excluding a whole resource or data source goes to the owner. `Keep`, `Omit`, `ReadOnly` and `NoResponse` stop being ways to trim for lack of proof: the evidence gate below does that job. `CreateOnly`, `Require`, `Response`, `CreateHash` and `DocsFrom` stay, because they describe Stripe.

**The evidence gate.** The provider gates on the fake's own manifest, not on the suite's record alone. Each fake release attaches `attributes.json`, which names the release (`fake_version`), the suite commit it embedded (`suite_ref`), the evidence run (`run_id`), `api_version` and `spec_sha256`, and classes every leaf ("`attributes.json`", A7 of the fake's ADR (`fake-stripe-api`, `adrs/1-pending/2026-10-29-attribute-parity/ADR.md`, fake PR #65)). The generator exposes a leaf only when that file lists it as `verified`: its pinned record says `proven` with `targets` including `fake`, the release binary serves it, and every leaf test the record cites passed on the fake in the release's fidelity run (same section, "Classes"). Every other class (`served_failing`, `served_broken`, `served_unverified`, `gap_proven_not_served`, `gap_stripe_only`, `gap_placeholder_null`, `proven_by_test`, `unproven`) leaves it unexposed; `served_broken` and `proven_by_test` are the fake's additions to its ADR's seven (`architecture/ci.md`, "Attribute fidelity", fake `v0.26.0`). A leaf the suite proves against Stripe only (`"targets": ["stripe"]`, the hosted-flow leaves, decision 6 of the fake's ADR and the suite's `targets`) is therefore not exposed until the fake serves it and a release verifies it.

The provider vendors two files beside `spec/PIN.json`, with their own pin (`spec/EVIDENCE_PIN.json`, named so because `spec/EVIDENCE.json` is the same file as `spec/evidence.json` on a case-insensitive file system: the fake release, `suite_ref`, `run_id`, and the size and SHA-256 of each file):

- `spec/attributes.json`: the release asset of the fake release named in `.fake-version`.
- `spec/evidence.json`: the suite's per-run file `evidence/<suite_ref>/<run_id>.json` from its `results` branch, at the `suite_ref` and `run_id` that `attributes.json` names. Not `evidence/<sha>.json`, which is a copy of the commit's current run and changes when the same commit passes again ("Where it lives and how it is pinned", the suite's ADR (`stripe-api-conformance`, `adrs/1-pending/2026-10-29-attribute-coverage/ADR.md`, suite PR #71)). The per-run file is immutable, and it is the bytes the fake embedded (F-5 of the fake's ADR).

Defaults, normalised forms, side effects, unset results and refusals for the docs and the engine (D2, D4) come from `spec/evidence.json`. The generator refuses either file when its `api_version` or `spec_sha256` differs from `spec/PIN.json`, or when their `suite_ref` and `run_id` disagree. Moving to a new fake release re-pins both in the same change, as `.fake-version` moves today. What the record holds is listed in `handoff.md` ("The evidence record the provider consumes") and specified by the suite's ADR.

Today's attributes were trimmed to what the suite proves, so they stay exposed while the suite audits them against `handoff.md`; one the audit cannot prove is proven in its resource's wave, never removed, because removing it would break users (per the product owner).

**The coverage report.** `tools/schemagen` writes `internal/generated/coverage.json` (one record per spec leaf: table, path, side, state, rule or register reason, the test that proves it, the default statement and its source) and `internal/generated/coverage.md` (per table totals, then excluded and awaiting leaves). A leaf's state is `exposed`, `excluded` (a rule or the register) or `awaiting proof`. They are generated files, so the existing generated-drift check keeps them current, and the owner's checklist tooling can publish `coverage.md` beside the coverage checklist. A unit test fails on a leaf with no state, naming the path. The report's count of `awaiting proof` is the measure of the remaining work.

### D2. State and the engine's rules

The suite proves every exposed field; these rules say what the engine assumes, from that proof, and how it defends users when Stripe still answers otherwise.

- **E1. Sent as configured.** Unchanged: a configured value is sent on create, and on update when it changes; an unset one is never sent. A one-off instruction (`proration_behavior`) is sent with create, or with the change it qualifies, as `SendWithUpdate` does today (`internal/engine/declaration.go:397-403`, `0832bd8`).
- **E2. Optional and Computed unless the suite shows otherwise.** An optional argument that the response also carries is Optional and Computed. It is Optional alone, so that removing it clears the value (architecture ADR line 113), only when the evidence shows Stripe leaves it null on a minimal create and the suite proves unsetting for its family (`handoff.md`, P4 and D-1 to D-3). Response-only fields stay Computed. This amends architecture ADR D2's "server_defaulted list is generated for every Optional field the response schema marks non-nullable": the spec's nullability does not say whether Stripe fills a field, and a wrong guess fails the apply.
- **E3. Read back when returned; kept from configuration when not.** A request leaf whose path the response lacks (write-only values, inline data, instructions) keeps the configured value, as today (`internal/engine/decode.go:104-108`, `0832bd8`). It is Sensitive when the response has a `<name>_provided` boolean beside it (`company.tax_id` and `company.tax_id_provided`), which the suite proves per value; the flag is a Computed attribute. The Import section lists these arguments automatically: an import cannot recover them, so a configuration that sets one plans an update after import, or a replacement where the argument is create-only.
- **E4. Apply keeps the plan; refresh reads Stripe.** After a create or update, every attribute whose planned value is known keeps it in the new state. If Stripe returned something else for a configured attribute, the apply succeeds with a warning: "Stripe stored `company.phone` as `+15555550100`, not as configured (`+1 555 555 0100`). The next plan will show the difference; configure Stripe's form to avoid it." For an unconfigured attribute the difference is logged at DEBUG. The next refresh records Stripe's value. Unknown planned values are filled from the response as today. The acceptance harness sets the engine's strict mode, in which the warning is an error, so a test fails where Stripe or the fake answers otherwise than the suite recorded.
- **E5. Unset settings keep their prior value only where Stripe leaves them alone.** An Optional and Computed attribute the configuration leaves unset keeps the state's value (`UseStateForUnknown`) when the evidence shows Stripe does not change it as a side effect of an update (`handoff.md`, P5). A field Stripe fills from another keeps the existing `FilledFrom` treatment (`internal/engine/declaration.go:307-314`, `0832bd8`), and any other field the evidence shows changing stays unknown on update. Response-only attributes stay unknown on any change, as the architecture ADR requires ("Fields that can change server-side stay unknown on update", line 111).
- **E6. An unexpected shape warns on read.** When Stripe returns a field in a shape other than the schema's, the read keeps that field's prior value and warns, naming the field and what came back, instead of failing the read; strict mode makes it an error. One changed field never stops every plan of a resource.

E4 and E6 are a defence for users when Stripe changes after the suite proved it, not a substitute for proof.

**Normalised values.** Where the suite records that Stripe returns a configured value in another form (P2), the declaration keeps the configured value in state (`PreserveConfig`, `internal/engine/declaration.go:319-321`) or, where the rule is simple (lower-casing), refuses the unnormalised form at validation (`LowerCase`, lines 337-341). Each needs the suite's record of the form.

**What can still go wrong for users.**

|Risk|When|What the user sees|Remedy|
|-|-|-|-|
|A diff after apply|Stripe normalises or changes a value the suite did not record|A warning at apply (E4), then the difference at the next plan|A suite test of the form, then `PreserveConfig` or a validator|
|Stale value until refresh|Stripe starts changing an unset setting as a side effect, unlike the evidence|The old value in state after apply, and in anything that references it in the same run|The next refresh; a suite test, then `FilledFrom`|
|Refusal at apply|A field Stripe accepts only for some countries or account types (`company.address_kana`)|Stripe's error, mapped to the attribute (architecture ADR D5)|The description names the countries; the suite records the refusal|
|Lost write-only value|Import, or a value changed outside Terraform|An update plan after import (E3); no drift for a change made outside|The generated import note; `ignore_changes`|
|No way to clear|An Optional and Computed argument removed from the configuration (E2)|The value stays|Set the empty value explicitly where Stripe takes one; the docs line says removing it keeps the value (D4)|

### D3. Shapes the tables cannot hold today

**Response unions and polymorphic objects.** An `anyOf` or `oneOf` of several object variants becomes one object attribute holding every variant's members, merged by name (a charge's `source` with the members of a card, a bank account and a source, beside its `object`). Members of the same name and shape merge; the generator fails when two variants give one name different shapes, and each such case becomes a declaration override or a register entry. The suite proves each variant (`handoff.md`, P9). The integer-or-string and reference-or-object rules stay (`tools/schemagen/build.go:355-370`, `0832bd8`).

**Request unions of a string and a hash.** A parameter that takes a string or a hash (`stripe_account.bank_account`: a token such as `btok_us_verified`, or the account details; a customer's `bank_account`, `card` and `source`; a charge's `card` and `source`; a plan's `product`: a product ID, or the hash that creates one) becomes two arguments, of which a configuration sets at most one:

- `<name>`, a string, Stripe's name, for the token or ID, exactly as today.
- `<name>_data`, an object, for the hash, sent on the wire under `<name>`.

This is a named exception to the architecture ADR's rule that attribute names are Stripe's verbatim (D7). It keeps every string attribute shipped today, so no union is a breaking change, and it follows Stripe's own naming where Stripe splits the two forms itself (`price` and `price_data`, `payment_method` and `payment_method_data`). The generator fails when `<name>_data` is already a field at that level. The resource reads back what the response returns at `<name>` into `<name>` only. A request union of two hashes merges as a response union does.

**Lists returned a page at a time.** A list envelope the object's schema requires, and so returns on every read, holds only the first page of its elements: an invoice's and a credit note's `lines`, a subscription's `items`, a transfer's `reversals`, an application fee's `refunds`, a Radar value list's `list_items`, a Financial Connections session's `accounts`. These stay as today (per the product owner). A declaration names, for each one, the resource or data source that reads every element (`PagedLists`, a map from dotted path to type name, such as `lines` to `stripe_invoice_line`); the generator fails on a required list envelope the declaration does not name. The docs line of each says: "Holds the first page of the list Stripe returns with the object; `has_more` is true when there are more. Read every element with the `stripe_invoice_line` data source."

**Recursion.** The walk stops at a schema already on the path (X4). The repeat is held as its `id` when it has one: a payment intent's `last_payment_error.payment_intent` is the payment intent's own ID.

**Embedded copies of other objects.** A nested object that is itself a Stripe object (`x-resourceId`), not expandable, such as an invoice's `last_finalization_error.payment_intent` or a Treasury transaction's `flow_details.outbound_payment`, is read in full, as 3,502 leaves already are. The proof of the copied object covers its fields; the suite proves where and when the copy appears (`handoff.md`, P10, which lists the 82 places). Its docs subsection is one line linking to the data source page of the copied object.

**Expandable references.** Unchanged (architecture ADR D2): an ID, never expanded on resource reads, except fields a declaration expands (`Expand`). Where the response returns the object at a path to which the request sends an ID (a subscription item's `price`, 439 leaves), the attribute keeps the ID, as `isStringOrObject` already does (`tools/schemagen/build.go:359-365`).

**Objects in, IDs out (`discounts`).** The request takes a list of hashes, each with one of `coupon`, `discount` or `promotion_code`; the response returns discount IDs, or discount objects when expanded. The attribute takes the request shape. The engine expands `discounts` on every request that returns the object and maps each discount back by key, never by position, as `MatchBy` does: a configured `coupon` matches a discount with that `coupon.id` and no `promotion_code`, a configured `promotion_code` matches by `promotion_code`, a configured `discount` by `id`. A discount that matches no configured element is drift. The suite proves the three forms on each object with `discounts` (P11) before the engine relies on this mapping; where it shows another relation, the mapping follows the evidence.

**Write-only values.** E3: kept from the configuration, Sensitive where Stripe reports `<name>_provided`. Terraform's write-only arguments are a later option, not adopted now: each would need a `<name>_version` companion attribute to trigger an update (architecture ADR D2, line 108) and Terraform 1.11 or later (documentation layout ADR, line 160).

**Request hashes with no members.** A request hash with no members selects an option with Stripe's defaults (a payment method type under `payment_method_options`). It becomes a nested attribute with no members, configured as `{}`. Step 2 proves that the framework and both CLIs accept it; if they do not, it becomes a Bool attribute and this ADR is amended. `/v2` sends it as `{}`. On `/v1`, where an empty value unsets a field, the provider refuses at plan, naming the attribute, any object (not a map) whose configuration would encode to nothing, `{}` or every argument null, whether or not the schema gives it members, judging a create at plan by the arguments a create sends, and the encoder refuses an object it would send empty, naming the arguments the request sends and those it never sends, until the suite records what Stripe takes for one (suite Step 3); the rule then follows the evidence (per the product owner, 2026-10-29). No resource test or example relied on sending one: a probe of the form encoder over the whole acceptance suite, run against the fake, found none.

**Same name, different shape.** A field the request and response both name with different shapes (an account's `capabilities`: `requested` flags in, statuses out) takes the request's shape in the resource and the response's shape in the data source. The resource keeps the configured value (E3); the data source reads Stripe's.

**Maps.** String, integer and object maps are supported (`internal/fields/fields.go`, `0832bd8`). A map of another value shape fails generation, naming the path, and becomes a register entry.

**Very large objects.**

- *Plan noise.* E5 keeps unset settings out of "(known after apply)" where the evidence shows Stripe leaves them alone; response-only fields still show on any change, and a plan that changes nothing configured is still empty (`internal/engine/resource.go:112-128`).
- *Schema size and start-up.* Exposing every leaf adds about 1,200 request and 9,800 response leaves to today's 11,100, so the schema roughly doubles. Today's 68 ms and 6.8 MB suggest about 150 ms and 15 MB; Step 2 measured about 105 ms and 14.0 MB (Step 2, below). Terraform asks for the schema once per provider process. No lazy schema is needed unless Step 2 measures more than a second.
- *Docs size.* Embedded copies link to their data source page instead of repeating it. A page still over the 250,000-byte budget moves its largest nested blocks to per-resource guide pages under `docs/guides/` (copied from `docs-src/guides/` by the renderer, documentation layout ADR D3a), linked from the block's line; each argument there keeps its default statement. Our pages never defer to Stripe's reference. This replaces D3a's unbuilt remedy of omitting a subtree.

### D4. Defaults in the docs

**The line.** Every optional argument's line, at any depth, ends with exactly one default statement, after Stripe's description and the replacement sentence. Required arguments and read-only attributes have none.

|Case|Statement|
|-|-|
|Stripe applies a fixed value|`Default (Stripe): \`once\`.`|
|Stripe's value depends on the account or another argument|`Default (Stripe): <rule>.`, with the value observed on the suite's US account in parentheses|
|The provider applies a value|`Default (provider): \`3\`.`|
|Unset stays unset|`Default: none.`|
|An update-only argument|`Default: not sent.`|

An Optional and Computed argument also says `Removing it from the configuration leaves the current value.` (E2).

**Rendered, for real arguments** (the descriptions are today's):

~~~~~markdown
* `duration` - (Optional) Specifies how long the discount will be in effect if used on a subscription. Defaults to `once`. Possible values: `forever`, `once`, `repeating`. Changing it replaces the resource. Default (Stripe): `once`.

* `customer_creation` - (Optional) Configures whether [checkout sessions](https://docs.stripe.com/api/checkout/sessions) created by this payment link create a [Customer](https://docs.stripe.com/api/customers). Possible values: `always`, `if_required`. Changing it replaces the resource. Default (Stripe): `if_required`. Removing it from the configuration leaves the current value.

* `currency` - (Optional) The currency to create this invoice in. Defaults to that of `customer` if not specified. Changing it replaces the resource. Default (Stripe): the customer's currency, or else the account's default currency (`usd` on a US account). Removing it from the configuration leaves the current value.

* `max_retries` - (Optional) Retries per request on transport errors, 429 and 5xx responses. Defaults to 3. Default (provider): `3`.
~~~~~

The evidence behind them: `stripe_coupon.duration` ("`POST /v1/coupons` with only `percent_off=10` returns 200 with `duration: "once"`", `docs/stripe-api-behaviour.md:7`, suite `9212b25`); `stripe_payment_link.customer_creation` (line 77 of the same file); `stripe_invoice.currency` (line 169: "An invoice created with `POST /v1/invoices` for a customer with no currency takes the account default"; that the customer's currency comes first is the spec's text, which the suite's minimal creates confirm before the line ships); the provider's `max_retries`. Stripe's own "Defaults to" sentences stay, because descriptions are verbatim (documentation layout ADR D1); the structured statement is the one CI checks.

**Where each value comes from.**

1. *The provider's schema.* A framework `Default` on an attribute, or a provider argument's documented fallback: `Default (provider)`.
2. *The suite's evidence.* What the suite must record is in `handoff.md` ("Defaults: what the suite must record"): the whole response of a minimal create per creatable object (D-1), per variant that changes defaults (D-2), with each optional hash set to its required members only (D-3), and, where the value depends on the account or another argument, that rule with the observed value (D-4). A value gives `Default (Stripe): <value>`, null gives `Default: none.`, a rule gives `Default (Stripe): <rule>`.
3. *The declaration*, only to word a rule the suite recorded: `Declaration.Defaults`, a map from dotted path to the rule's text, citing the suite test in a comment.

The spec's "Defaults to" text is not a source. The generator compares it with the evidence and lists disagreements in the coverage report for the suite to check.

**Staying correct.** The generator recomputes every statement from the pinned evidence (the per-run file of D1) and spec on each run, so a new fake release, with its evidence run, or a spec bump shows as a diff of the generated files and the docs. Evidence for another API version fails generation. `coverage.json` records each statement's source.

**Completeness in CI.** A leaf is exposed only with evidence (D1), and the minimal creates are part of that evidence, so every exposed optional argument has a statement. `tools/docgen` fails on an optional argument without one, naming the page and the path.

### D5. Data sources

A data source exposes every response leaf, all Computed, under the rules of D1 and the shapes of D3: unions merged, embedded copies in full, recursion cut at the repeat, the response's shape where request and response differ, `discounts` as the response's IDs. Lookup arguments are unchanged (architecture ADR D7). A data source never expands: a field returned only when expanded is read only where the declaration expands it for the resource too, as today, and X3 applies to data sources as to resources, because the excluded lists' own data sources read them (per the product owner).

### D6. The work plan

#### Engine and generator first

Steps 1 to 5: the coverage report, register and evidence gate; the generator's shapes; the engine rules; defaults in the docs; the full-surface test. Steps 1 to 3 need nothing from the suite or the fake. Step 4 needs the evidence record's format. Step 5 needs a fake release that serves a proven resource in full.

#### Before `v0.1.0`: every breaking change on today's attributes

Step 6 makes, on the attributes exposed today, every change of this ADR that would break users once released. From `handoff.md` ("Needed first"):

|Change|Leaves|Objects|Needs from the suite|
|-|-|-|-|
|Optional arguments that become Optional and Computed (E2) unless the evidence shows Stripe leaves them null and proves unsetting|801|57|Minimal creates (D-1 to D-3) and unsetting per family (P4)|
|Arguments read back that state keeps from configuration today: `stripe_account`'s `company.name` and `individual` fields; a Treasury financial account's `features.*.requested`|22|2|What Stripe returns at those paths (P2)|
|Attributes removed by X3: a customer's `tax_ids`, a charge's `refunds`, a file's `links`, an invoice's `payments`, a quote's `computed.upfront.line_items`, a report run's `result.links`|6|6|Nothing|
|The `/v2` renames (issue #66)|n/a|4|Nothing; decided separately|

No change of this ADR makes an existing attribute Sensitive (no exposed argument has a `<name>_provided` flag that is not already Sensitive) or changes its type (D3 keeps every string attribute of a union). A new attribute that is Required inside an object users already configure would break their configurations; the hand-off script found none in today's objects, and the coverage report's test keeps checking it for every wave (any new Required leaf under an exposed object fails the test unless the register approves it).

`v0.1.0` waits on Step 6, and Step 6 waits on the suite's minimal creates and unsetting proofs for the 57 objects (per the product owner: behaviour-only changes are breaking). Additive attributes follow as they are proven and can ship in any release after `v0.1.0`. When to ship is the owner's decision.

#### Waves

A leaf is exposed as soon as the pinned fake release's `attributes.json` lists it as `verified`, whatever its wave (per the product owner; D1). Waves order the work for the suite, the fake and the provider; they are not release gates. A resource is done by the definition below. What each wave needs, in brief (the full lists are in `handoff.md`):

|Wave|Objects|Hard cases the suite meets first|
|-|-|-|
|1|`stripe_account`|`company` and `individual` in three scripts (Japanese kana and kanji), files for verification and documents, onboarding values, `bank_account` in its token and hash forms, `capabilities`' two shapes|
|2|The rest of Connect and `/v2`: `stripe_account_person`, `stripe_account_external_account`, `stripe_account_v2`, `stripe_account_person_v2`, `stripe_event_destination_v2`, the capability, application fee and transfer objects|v2 unsetting with `null` (which `ClearsWithNull` waits on today), persons in other countries|
|3|Payments: payment and setup intents, payment methods, charges, refunds, checkout sessions, payment links, mandates, disputes, payouts, top-ups|Payment method options and details for methods a US account may not offer (2,111 leaves across the provider), hosted flows (`next_action`, `collected_information`), failure states|
|4|Billing and catalogue: customers, invoices, invoice items, subscriptions and their items and schedules, quotes, credit notes, prices, products, coupons, promotion codes and the other billing objects|`discounts` in its three forms, payment settings per method|
|5|Issuing, Treasury and Terminal|Physical cards, shipping and PINs; financial account features; reader actions|
|6|Every other object|Mostly embedded copies and unions|

`stripe_account` is first because it is the owner's example and exercises every rule: trims, write-only values (`company.tax_id`), a shape difference (`capabilities`), a token-or-hash union (`bank_account`), `RefuseChange` on onboarding arguments, and country-specific fields.

#### Definition of done per resource

1. The coverage report shows every leaf of the table exposed or excluded by a rule or an approved register entry; none awaits proof.
2. Every exposed leaf of the table is `verified` in the pinned `spec/attributes.json`, and its facts are in the pinned per-run `spec/evidence.json` that file names.
3. The full-surface test passes against that fake under Terraform and OpenTofu in strict mode.
4. Every optional argument has a default statement from the provider or the evidence.
5. The six per-resource acceptance tests and the import block test pass; the docs are regenerated; every page is within the budget.

## Implementation Roadmap

### Step 1: Coverage report, register and evidence gate

Add the leaf walk of `measurement.md` to `tools/schemagen`, the rules X1 to X5, `Resource.Exclude`, the readers of `spec/attributes.json` and `spec/evidence.json` and their pin `spec/EVIDENCE_PIN.json`, the gate that exposes a `verified` leaf, the generated `internal/generated/coverage.json` and `coverage.md`, and the check that fails on a new Required leaf under an object exposed today unless the register approves it. Today's exposed leaves stay exposed while the suite audits them, and one the audit cannot prove yet is proven in its wave, never removed (per the product owner). Describe the report and the evidence pin in `CONTRIBUTING.md` ("Generated code").

Acceptance criteria: `go generate ./internal/generated` writes both files and changes no field table; a fixture leaf with no state fails the test; a fixture evidence record for another API version fails generation, and so does one whose `run_id` differs from the fixture `attributes.json`; a leaf `verified` there is exposed, and one in any other class (including `gap_stripe_only`) is not.

Status: BUILT. The walk keys leaves as the suite and the fake do, with the owner's decisions of 2026-10-29: a nested API object the table's request writes as a hash (an account's `individual`, a customer's `cash_balance`, a financial account's `features`) is walked into its own leaves, and any other is one embedded-copy leaf; a list envelope is one leaf at its path. Request leaves are counted per operation. With no evidence pinned yet, the first report counts 3,197 request leaves exposed, 124 excluded and 2,262 awaiting proof, and 5,272, 121 and 2,101 on the response side; no field table changed. Ten X3 lists sit at the objects' own level; the other four of the hand-off's 14, the report run's `result.links` among them, are inside embedded copies, so Step 6 removes `result.links` with the copy's leaf in view. Departures: the pin is `spec/EVIDENCE_PIN.json` (above); the pin is optional until a fake release attaches `attributes.json`; X5 follows the declarations, so it excludes the payment intent's `confirm` and not the setup intent's, whose `status` only cancels; the register's first entry is `stripe_file`'s `file`, which the declaration sends from `source`, for the product owner's approval. Fixtures in `tools/schemagen` stand in for `attributes.json` and the evidence record. The first pin, written by `tools/evidencepin`, is fake `v0.26.0` with suite commit `2d60360` and run 40940184416: every leaf it verifies was exposed already, so it changes no attribute, and it gives `stripe_coupon` its first default statements, and the report names four verified arguments with no recorded default: the coupon's `id` and `percent_off`, and the `/v2` event destination's `description` and `metadata`, whose create leaves the fake serves but does not verify.

Dependencies: none. Until a fake release attaches `attributes.json` and the suite publishes a passed run, fixtures stand in for both.

### Step 2: Generator shapes

Response unions merged into one object; `<name>` and `<name>_data` for token-or-hash parameters, refusing a collision; the recursion cut holding the repeat's `id`; request hashes with no members, proven against both CLIs; the same-name-different-shape split between resource and data source; Sensitive from `<name>_provided`; embedded copies in full; `PagedLists` and its check on required list envelopes. Measure the schema's build time and size with every table regenerated without trims, in a throwaway branch, and record them here.

Acceptance criteria: the generator fixture covers each shape; regenerating every table without trims succeeds or names each failure; the measured build time and size are recorded in this ADR.

Status: BUILT. Each shape is in the generator's fixture (`tools/schemagen/testdata/shapes_spec.json`, `shapes_test.go`) and in the engine's (`internal/engine/shapes_test.go`); a request hash with no members, configured `{}`, applies and plans clean under Terraform 1.17.0 and OpenTofu 1.13.2 (`internal/resources/shapes_cli_test.go`, which CI's acceptance job runs under both, as it does a create through `<name>_data` followed by an empty plan), so it stays a nested attribute. With no evidence pinned, no field table and no docs page changed.

Measured on 2026-10-29 in a scratch copy, with every leaf that no rule and no register entry excludes treated as verified, so that the gate lifts every trim: 119 of the 121 tables regenerate; `country_spec` (`supported_bank_account_currencies`) and `balance_settings` (`payments.payouts.automatic_transfer_rules_by_currency`) fail on maps of arrays, which become register entries in their waves. Six request leaves stay unheld: the token form and the hash's own `object` of the external account's `bank_account`, which the table spreads into its top level; the update's `identity.country` and `identity.entity_type` of a v2 account and the update's `billing_cycle_anchor.type` of a subscription, which `CreateOnly` keeps create-only; and an invoice item update's `invoicing_rules`, which takes only `""`. The provider's schema, built in process by `GetProviderSchema` with the 68 resources and 121 data sources, takes a median of about 105 ms (92 to 145 ms over seven runs) and is 13,966,105 bytes as protocol JSON, against 45 to 53 ms and 6,886,123 bytes for the same build of today's tables; no lazy schema is needed. `tools/docgen` then stops at the setup intent's data source page, 596,812 bytes against the Registry's 500,000-byte limit, which Step 4's links from embedded copies and guide pages address.

Departures: `<name>_data` and the Sensitive mark from `<name>_provided` are built only where the gate verifies a leaf of them, so the schema does not change before evidence does; X4 leaves out only a repeat with no `id`, so a payment intent's `last_payment_error.payment_intent` and a setup intent's `last_setup_error.setup_intent` now await proof; X2 leaves out only a response hash with no keys that no request writes; the reference rule holds an ID sent and the object returned, not a hash sent and a string returned, which splits; data sources leave out `<name>_data`; a merged hash requires a member only where every variant does; `PagedLists` is on the generator's `Resource`, beside the register, and names the type for each required list envelope. The token of a token-or-hash parameter the response returns is Optional and Computed beside its hash form, and keeps its state value while neither form changes, except inside a list element, where an insert or a reorder would give it another element's value: there it is unknown on a change and, if create-only, replaces only when the configuration sets another value, unless a field above it forces replacement, where it keeps its state value too; an object sent again for a change of a sibling sends only the form the configuration sets; where the create requires the parameter, both forms are Optional and a configuration sets exactly one, which validation checks at plan and the encoder again at apply, at any depth. Declaration entries name the hash form by its attribute name, `<name>_data`, so that an entry names one form. A `/v1` object whose configuration would encode to nothing is refused at plan (D3).

Dependencies: Step 1.

### Step 3: Engine rules E2 to E6 and `discounts`

E2's Computed rule from the evidence; E3's sensitivity and the generated import note; E4 with the strict mode the acceptance harness sets; E5 from the evidence's side effects; E6; the `discounts` mapping.

Acceptance criteria: engine unit tests show an apply that gets back another value succeeds with the warning and fails in strict mode; an unset setting the evidence marks stable keeps its prior value in an update plan, and one it marks changing does not; a wrong-shaped field warns on read and keeps its prior value; the `discounts` mapping matches by each of the three keys.

Status: BUILT. The generator reads the evidence record of format version 1 strictly (every member its JSON Schema defines is declared, and a member it does not define, or a status, target, default kind or `unset` value outside the schema's sets, fails generation), tested on the suite's own fixture record cut to two objects (suite `c903898`, `tools/schemagen/testdata/evidence/`). E2 marks a request field the gate exposes that the response returns Optional and Computed unless the pinned record shows a null or absent default in the minimal create and every variant, and an update `unset` of `""` on `/v1` or null on `/v2` whose result is null or absent; it decides per leaf, not by the family samples. E5 marks an optional argument an update takes `Stable` when its response leaf is verified, the record proves a leaf of the table's update (whose probes record the side effects) and no `side_effects` or `filled_from` entry names its path; the engine then attaches `UseStateForUnknown`, outside list elements. E4 keeps every known planned value after a create or update and warns for a configured attribute, logging any other at DEBUG; strict mode (`provider.NewStrict`, which the acceptance harness and the CLI shape test serve) makes both an error, because Terraform failed on either before. E6 keeps the prior value of a field in an unexpected shape on any read, the data source's included, and warns; a list envelope with `has_more` and a keyed list that cannot be matched still fail. E3's import note is generated from the arguments the response never carries, leaving out those the declaration's note names in backticks. `discounts` is recognised as a top-level split field whose request elements take only `coupon`, `discount` and `promotion_code`; the resource expands it on every request, maps the expanded discounts back by key (the coupon from `source.coupon`, as the pinned spec places it) and adds an unmatched discount as drift in the first form the element takes of `discount`, `promotion_code` and `coupon`. With no evidence pinned, E2, E5 and `discounts` change no table; E4 and E6 change today's behaviour, and the import note adds a sentence to 19 resource pages.

Departures: E2 applies only to fields the gate exposes, so that today's 801 leaves change in Step 6 with their changelog entries, and to a split field too, since the response returns it in another shape; E5 applies to today's fields as soon as the evidence verifies them, because keeping a value Stripe does not change breaks no configuration; strict mode fails an unconfigured attribute's difference as well as a configured one's; nested `discounts` (a subscription item's, a quote line's) keep the configured value as other split fields do, because the engine expands only top-level paths; the mapping does not yet compare the record's `discounts` relations, which are free text.

Dependencies: Step 2.

### Step 4: Defaults in the docs

The default statement and the removal sentence in `tools/docgen`, `Declaration.Defaults`, the comparison with the spec's text, the completeness check, links from embedded copies to data source pages, the first-page sentence of a list returned a page at a time, and guide pages for blocks of a page over the budget.

Acceptance criteria: docgen golden tests cover every statement form and a guide-page split; an optional argument without a statement fails docgen; every page is within the 250,000-byte budget.

Status: BUILT. The generator puts on each table field an exposed request leaf only the update takes `Default: not sent.`, which needs no evidence, and on each field a verified request leaf holds its default from the pinned record (`fields.Default`), marks each embedded copy of an object exactly one table reads (`Copy`) and each list returned a page at a time (`PagedBy`), lists in the coverage report where the spec's "Defaults to" names another value than the evidence, and counts per table the optional arguments whose default is not yet recorded, naming the verified ones. The engine applies `Declaration.Defaults` and gives docgen each attribute's statement and copy (`AttributeDocs`); the provider gives its own arguments' (`provider.ArgumentDefaults`). Docgen ends each optional argument's line with the statement and, on a resource page, an Optional and Computed argument's with the removal sentence; links a copy to its data source page; and moves the largest top-level nested blocks of a page over the budget to generated guide pages. With no evidence pinned, the seven provider arguments carry a `Default (provider)` statement, 43 update-only arguments on 16 resource pages carry `Default: not sent.`, 595 argument lines on 57 resource pages carry a removal sentence (357 on 45 pages that removing leaves the current value, 29 on 7 pages that Stripe sets it when the list is next sent, 209 on 45 pages that the provider stops sending it), 12 pages carry the first-page sentence, 24 pages link their copies, the largest page (`stripe_checkout_session`'s resource page) falls from 165,227 to 160,138 bytes, no page needs a guide, and the report counts 1,455 optional arguments in 62 tables whose default is not yet recorded. Pinned in a scratch copy with every leaf it proves on both targets verified, the suite's first run (40926522642, which did not pass) gives 15 statements on `stripe_coupon`, `stripe_event_destination_v2` and `stripe_terminal_location`, finds no disagreement with the spec's text, and finds 14 verified arguments with no recorded default: the coupon's `id` and `percent_off`, which the minimal create sets or Stripe generates, and the twelve members of a terminal location's `address_kana` and `address_kanji`, whose hashes the minimal create leaves out.

Departures, each the owner's decision or the smallest reading of the text: until the `v0.1.0` evidence is pinned, docgen requires a statement only of the provider's arguments and the arguments only an update takes (an update-only member that its hash in the update requires included, since the create never sends it); because evidence arrives object by object, a verified argument whose default the record does not give yet does not fail a pin, and the coverage report names it per table, under "Defaults not yet recorded", as "verified, default not recorded", beside the count of unverified ones (`everyArgumentHasADefault` in `tools/docgen` makes the check total for both, per the owner's decision of 2026-10-29); an argument a declaration words in `Declaration.Defaults` counts as recorded, which the generator reads from the declaration's source, because a declaration compiles against the tables the generator writes; a create leaf's default is stated only when that create leaf is verified, so an argument verified only through its update leaf has none; a statement belongs to an argument a leaf holds, so a nested object with members states its members' defaults, not its own; a default the minimal create's variants give differently has no statement until `Declaration.Defaults` words its rule, which the engine accepts only there and for a recorded rule; a rule's observed value is that of the observation with no condition, on the platform country of the run; a value is shown as a configuration writes it, a string bare (`""` when empty) and anything else as compact JSON; the report records a default only where the leaf is verified, as the docs do; a copy links to its data source only where that data source has every attribute the copy has, the copy's `object` aside, so the payment method copies of payment and setup intents and terminal readers, an Issuing authorization's `card` and a subscription's `pending_update.subscription_items` stay in full until those data sources hold every attribute; guide pages hold one top-level block each, named `<type>-<resource|data-source>-<attribute>`; the provider's own descriptions keep their "Defaults to" sentences, as the rendered `max_retries` above does; D4's removal sentence is written only where the plan keeps the prior value (`UseStateForUnknown`, which E5 attaches to a `Stable` argument outside a list), and the engine words what else removing an Optional and Computed argument does: in a list element, whose prior value an insert or a reorder would mismatch, "Removing it from the configuration lets Stripe set it when the list is next sent.", and elsewhere "Removing it from the configuration stops sending it, and the next apply records the value Stripe holds."

Dependencies: Step 1. The evidence record's format, specified by the suite's ADR (format version 1).

### Step 5: Full-surface acceptance test

A helper in `internal/acctest` that builds, from a table and the evidence's sample values, a configuration setting every argument the pinned `attributes.json` lists as `verified`, with values from the pinned evidence, applies it, checks an empty plan, updates every updatable argument, checks an empty plan and imports, in strict mode. `internal/resources/layout_test.go` requires `TestAcc<Name>_fullSurface` of every resource whose wave is done.

Acceptance criteria: the test passes for `stripe_account` against a fake release whose `attributes.json` verifies its leaves, on Terraform and OpenTofu; it fails when the engine drops a field.

Status: PENDING

Dependencies: Step 3. A fake release that serves `stripe_account` in full.

### Step 6: Breaking changes on today's attributes

E2 applied to the 801 argument leaves, the 22 read-backs and the 6 X3 removals of D6, with the changelog entry for each. `v0.1.0` waits on this step.

Acceptance criteria: `coverage.json` shows each of the 801 leaves Optional and Computed or backed by evidence of null and of unsetting; the six attributes are gone; the acceptance suite passes.

Status: PENDING. `stripe_account`'s 13 read-backs are done, with the second pin (fake `v0.27.0`, suite commit `3b86d25`, run 40980203177): `company.name` and the `individual` fields are read back, `individual.phone` keeps the configured form because the record shows Stripe returns another, the four verification document references keep the configured value because the dev sandbox replaces a test token, which the record never sends, with a new `file_...` ID (both `PreserveConfig`), and `company`, `individual` and the objects Stripe returns with null members are server-filled, so that an account that configures them only in part, or not at all, plans nothing. By the owner's decision of 2026-10-29, every member of `individual` and `company` whose update leaf is verified changes in place, and only `individual.id_number` and `individual.ssn_last_4` stay in `RefuseChange`, which a test checks against `coverage.json`. An update sends only the members that changed (`SendChangedMembers`), because the record's `v1-connect` family keeps the others and they include attestations Stripe holds, such as `company.directors_provided`. The `company` fields the record's `filled_from` names are server-filled and `FilledFrom`, so they follow the individual on an account with no `company` sent, the only case the record has, and hold what Stripe answers where a configuration sets part of `company.address` beside an individual's address, which the dev sandbox leaves otherwise null. The first smoke of this work showed what the record does not: an update of `company` or of the individual's name moves `settings` and, for `company.name`, `business_profile.name`, where the record has no side effects and leaves those leaves unproven. They are unknown in such a plan (hand-written `FilledFrom`) until the suite proves them. A Treasury financial account's `features.*.requested`, the 801 leaves and the X3 removals remain.

Dependencies: Steps 1 to 3. The suite's minimal creates and unsetting samples for the 57 objects, and the 22 read-backs, `verified` in a pinned fake release's `attributes.json`, with the facts in the per-run evidence file it names.

### Step 7: Wave 1, `stripe_account`

As in D6.

Acceptance criteria: the definition of done.

Status: PENDING

Dependencies: Steps 1 to 5 for exposing its proven leaves, which can happen before Step 6; Step 6 for its breaking changes. The suite's proof and the fake's support of `stripe_account` as listed in `handoff.md`.

### Step 8: Waves 2 to 6

In wave order, each leaf exposed as soon as the pinned fake release verifies it. When the last wave lands, `Keep`, `Omit`, `ReadOnly` and `NoResponse` are deleted, and the architecture ADR's D1 sentence points here.

Acceptance criteria: the definition of done for every resource; `coverage.md` shows no leaf awaiting proof.

Status: PENDING

Dependencies: Step 1 for exposing proven leaves; Step 7 orders the work before it. Per leaf, the suite and the fake.

## Architecture Documentation Updates

The repo has no `architecture/` directory yet (see the architecture ADR); it should be initialised with `/init-adrs`. When it is, `architecture/provider/mapping.md` takes D1's universe, exclusion rules and evidence gate, D2's rules E1 to E6 and D3's shapes, and `architecture/provider/docs.md` takes D4's default statement and the guide-page split. Until then `CONTRIBUTING.md` carries the contributor-facing parts in Steps 1, 4 and 5.

## Alternatives Considered

### 1. Expose fields before the suite proves them

A tiered standard: fields shipped once the provider's own handling is proven against a fake that stores and returns unmodelled parameters on opt-in accounts, with docs marking fields not yet proven against Stripe.

**Pros:** every attribute reaches users quickly; the suite's work is reduced to samples and snapshots.

**Cons:** users get fields whose behaviour nobody has observed; the fake would serve guesses, against its principle and the owner's aim of a fake faithful enough to license.

**Status:** rejected by the owner, 2026-10-29.

### 2. Expose everything with today's engine

**Pros:** smallest change.

**Cons:** an inconsistent-result error wherever Stripe fills or normalises a value, and a read that fails every plan on one unexpected field.

**Reconsider if:** never; E2 to E6 are cheap.

### 3. A generic passthrough

An `extra_parameters` map of bracket paths and a `raw_response` JSON attribute on every resource.

**Pros:** everything reachable at once.

**Cons:** untyped, undocumented per field, invisible to plan diffs, hides secrets in a map, and not what issue #65 asks for.

**Reconsider if:** never.

### 4. Embedded copies as IDs only

**Pros:** about 7,700 fewer response leaves and a schema near today's size.

**Cons:** removes 3,502 leaves that ship today.

**Status:** rejected by the owner, 2026-10-29. **Reconsider if:** Step 2 measures a schema that slows start-up past a second.

### 5. One attribute for both forms of a token-or-hash parameter

An object with the hash's members and a member for the token, or a dynamic attribute that takes either a string or an object.

**Pros:** Stripe's name only, no `_data` suffix.

**Cons:** both change the type of string attributes that ship today (a customer's `bank_account`, a plan's `product`), so both break users; a dynamic attribute also loses the typed schema the docs are built from.

**Status:** rejected by the product owner, 2026-10-29, in favour of `<name>_data`.

### 6. Terraform write-only arguments for secrets

**Pros:** secrets never reach state.

**Cons:** a `<name>_version` companion per field, Terraform 1.11 or later, and no drift or import of the value at all.

**Status:** a later option, per the owner.

## Resolved questions

All questions of both drafts were decided by the product owner on 2026-10-29 and are listed under Status (decisions 1 to 16).
