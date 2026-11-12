# Amendment: references Stripe returns as the whole object

## Executive Summary

At ten places a request sends another object's ID and Stripe's response returns that object whole (a subscription item's `price`, an invoice's `default_tax_rates`). The provider holds the ID there, as this ADR's D3 already says, but the copied object's 221 members have no state in the provider's coverage report, so the owner's coverage list counts them as attributes awaiting proof. This amendment adds rule X7: the members leave the target because the copied object's own data source reads the same leaves. The register leaves out the members of a top-up's `source`, a deprecated Source that no data source reads.

## Status

Status: Accepted, 2026-11-12, by the product owner. It amends `ADR.md` beside this file: D1 (a new exclusion rule, X7), D3 ("Expandable references", which names these paths, and "Embedded copies of other objects") and D5 (data sources hold the ID at these paths); the changes are listed under "Changes to ADR.md and CONTRIBUTING.md" below.

Decided by the product owner on 2026-11-12, from the proposal's questions:

1. Option (c), rule X7, at all ten places.
2. X7 does not wait on the suite's copy claims: it takes leaves out of the target rather than exposing them, so proof before exposure does not apply, and the spec makes each property the copied object's own schema. The ten copy claims are queued on the suite as a reliability item.
3. The top-up's `source`: a register entry citing Stripe's deprecation of Sources, not a restoration of the 218 leaves on the data source (no current top-up carries one).
4. The count of 488 in-scope rows with no key in `coverage.json` ("Rows the coverage list cannot key") supersedes the owner's earlier estimate.

Evidence is current as of 2026-11-12: provider commit `92ec5eb`, the pinned spec (`stripe/openapi` tag `v2585`, API version `2026-09-30.endive`), the pinned fake release `v0.41.0` with the suite record it embeds (suite `fcc9d56`, run 42411686853, `internal/generated/coverage.json` header at `92ec5eb`), and the conformance suite at commit `32938a0`. The owner's coverage list is `attributes.csv` as generated 2026-11-12 05:50 UTC from the suite's scope pin (`coverage/provider-scope/PIN.json` in the suite: this folder's `handoff-leaves.tsv` at provider commit `4b16f5e`, byte for byte).

## Background Context

### Why the leaves read "awaiting proof"

The scope pin marks 439 response leaves `ID in, object out: held as the ID` (`handoff-leaves.tsv`, column `rule`): the 221 of the ten places below and 218 of a top-up's `source` (below, "The top-up's `source`"). `handoff.md` told the suite these were not leaves the provider would expose ("every row whose `rule` does not start with `X`, `excluded` or `ID in`", `handoff.md:52`, `92ec5eb`), and D3 says "the attribute keeps the ID" (`ADR.md:194`, `92ec5eb`).

The provider's coverage report does not walk into them: "A nested API object is one leaf, unless the table's request writes it as a hash" (`CONTRIBUTING.md:45`, `92ec5eb`). So `coverage.json` holds one record at each place (`price`, `default_tax_rates`), in state `exposed`, and none for its members. The coverage list's generator joins the suite's evidence to scope rows through `coverage.json` keys only, and treats as excluded only rows whose scope rule starts `X1` to `X4` or `excluded:`. A row with no key therefore reads `awaiting proof` and `not reported`, whatever the evidence says, and its provider column falls back to the scope pin's `exposed_today` (the generator's `exposed()` and the loop in `main()`).

### The ten places

The owner's brief names eight places; a subscription schedule's `phases` holds three, so there are ten paths. By the copied object: a tax rate at seven paths (19 leaves each, 133), a price (41), an Issuing cardholder (40), an entitlements feature (7).

|Type|Path|Leaves|Copied object|Resource and data source today|Stripe returns|Leaves in the pinned `attributes.json`|The copied object's data source|
|-|-|-|-|-|-|-|-|
|`stripe_invoice`|`default_tax_rates`|19|`tax_rate`|list of IDs|always, every tax rate whole|19 `verified`|`stripe_tax_rate`: 18, all `verified`|
|`stripe_invoiceitem`|`tax_rates`|19|`tax_rate`|list of IDs|always (nullable)|19 `verified`|`stripe_tax_rate`: 18, all `verified`|
|`stripe_subscription`|`default_tax_rates`|19|`tax_rate`|list of IDs|always (nullable)|19 `verified`|`stripe_tax_rate`: 18, all `verified`|
|`stripe_subscription_item`|`tax_rates`|19|`tax_rate`|list of IDs|always (nullable)|19 `verified`|`stripe_tax_rate`: 18, all `verified`|
|`stripe_subscription_item`|`price`|41|`price`|ID|always; its `tiers` and `currency_options` only when expanded|41 `verified`|`stripe_price`: 40, all `verified`|
|`stripe_subscription_schedule`|`phases.default_tax_rates`|19|`tax_rate`|list of IDs|always (nullable)|19 `verified`|`stripe_tax_rate`: 18, all `verified`|
|`stripe_subscription_schedule`|`phases.items.tax_rates`|19|`tax_rate`|list of IDs|always (nullable)|19 `verified`|`stripe_tax_rate`: 18, all `verified`|
|`stripe_subscription_schedule`|`phases.add_invoice_items.tax_rates`|19|`tax_rate`|list of IDs|always (nullable)|19 `verified`|`stripe_tax_rate`: 18, all `verified`|
|`stripe_issuing_card`|`cardholder`|40|`issuing.cardholder`|ID|always|39 `verified`; `company.tax_id_provided` `unreachable`|`stripe_issuing_cardholder`: 39 exposed; 38 `gap_proven_not_served`, `company.tax_id_provided` `proven_by_test`|
|`stripe_product_feature`|`entitlement_feature`|7|`entitlements.feature`|ID|always|7 `proven_by_test` (suite status `unproven`)|`stripe_entitlements_feature`: 6, all `verified`|

How each column was established:

- *Today.* The generated tables hold a string or a list of strings, on the resource and the data source alike, because one table serves both: `price` is `Shape: fields.String` (`internal/generated/subscription_item/fields.go:31`, `92ec5eb`), `tax_rates` is `fields.ScalarList` of `fields.String` (line 47), and the same holds at `internal/generated/invoice/fields.go:82`, `invoiceitem/fields.go:78`, `subscription/fields.go:102`, `subscription_schedule/fields.go:148,167,216`, `issuing_card/fields.go:12` and `product_feature/fields.go:10`. The generator makes them so when the request sends a string or list of strings and the response returns an object or list of objects (`reconcile`, `isStringOrObject` and `isIDListOrObjects`, `tools/schemagen/build.go:607-625,1078-1094`, `92ec5eb`). The data source pages say so ("`price` - The ID of the price object.", `docs/data-sources/subscription_item.md:41`, `92ec5eb`). `coverage.json` has the one record per place, `exposed`, and no member records. The provider exposes the object at none of the ten places.
- *Stripe returns.* Every one of the ten properties is a direct `$ref` to the object's schema, or an array of them, with no `anyOf` of an ID string and no `x-expansionResources` (`spec/openapi.json`, `components.schemas.invoice.properties.default_tax_rates`, `subscription_item.properties.price`, `issuing.card.properties.cardholder`, `product_feature.properties.entitlement_feature`, `subscription_schedule_phase_configuration.properties.default_tax_rates`, `subscription_schedule_configuration_item.properties.tax_rates`, `subscription_schedule_add_invoice_item.properties.tax_rates`, pin `v2585`). Stripe returns the object on every read; none of them is an expandable reference, so D3's paragraph "Expandable references" and X3's reasoning ("returned only when expanded") describe them only by analogy. Inside the copied price, `tiers` and `currency_options` are returned only when expanded: the price resource asks for them (`Expand: []string{"currency_options", "tiers"}`, `internal/resources/price/resource.go:28`, `92ec5eb`), and the suite's subscription item test asks for `price.currency_options`, `price.currency_options.eur.tiers` and `price.tiers` ("A price returns these only when they are expanded; `eur` is the map key the fixtures use.", `subscription_item_test.go:542-543`, suite `32938a0`). That is 16 of the price's 41 leaves.
- *Pinned evidence.* `spec/attributes.json` (fake `v0.41.0`) keys each place by its members, with `operation` empty, and classes 213 of the 221 `verified` (status `proven`, served, passing on the fake), the 7 of `entitlement_feature` `proven_by_test`, and the cardholder's `company.tax_id_provided` `unproven` (status `unreachable`). The suite already treats the ten places as IDs sent and objects returned (`ObjectIDs: []string{"price", "tax_rates"}`, `subscription_item_test.go:541`, suite `32938a0`).
- *The copied object's data source.* Every copied object has a resource and a data source (`stripe_tax_rate`, `stripe_price`, `stripe_issuing_cardholder`, `stripe_entitlements_feature`), each looked up by `id` (`docs/data-sources/tax_rate.md`, `price.md`, `issuing_cardholder.md`, `entitlements_feature.md`, "Argument Reference", `92ec5eb`). Matching each place's member paths against the copied table's response records in `coverage.json` finds every member but `object`, which the architecture ADR omits from every table ("`object` is omitted", `adrs/1-pending/2026-10-17-provider-architecture/ADR.md:198`, `92ec5eb`; `tools/schemagen/build.go:405`). A copy's `object` is the constant name of the copied type. The cardholder's own leaves are exposed (decision 11 keeps them while the suite audits) but not verified: the fake does not yet serve 38 that the suite proved there.

The architecture ADR already chose how users read a referenced object: "A reference attribute holds the ID string and nothing else ... Data sources also return IDs; expansion is a convenience users can get by referencing the related data source." (`adrs/1-pending/2026-10-17-provider-architecture/ADR.md:129`, `92ec5eb`).

### The suite can prove that a copy is the object

The suite's record has `embedded_copies`, one entry per place one object embeds another: `object`, `path`, `copied_object`, `condition`, `equals_copied` and `tests` (`internal/evidence/record.go:362-371`, suite `32938a0`). "A copy `equals_copied` when the claim's matcher is the value read from the copied object's own endpoint", and the claim runs inside an attribute test's state, so it fails per leaf on the fake (suite ADR `adrs/1-pending/2026-10-29-attribute-coverage/ADR.md:663`, "Batch 3b", suite `32938a0`). The pinned record holds five entries; one is a tax rate copied into a checkout session's `total_details.breakdown.taxes.rate` (`internal/evidence/compile_test.go:1359`, suite `32938a0`). It holds none for the ten places.

### Rows the coverage list cannot key

The brief's related item, measured: of the scope pin's in-scope rows (rule not `X1` to `X4` or `excluded:`), 488 have no `(table, side, path)` key in `coverage.json` at `92ec5eb`, joined as the coverage list's generator joins them (its `provider_table` for the `/v2` renames).

|Rows|What they are|What the list shows|What is true|
|-|-|-|-|
|221|The ten places' members|`not exposed`, `awaiting proof`, `not reported`|Not exposed; 213 `verified` in `attributes.json`|
|218|A top-up's `source.*`, an embedded copy of a (deprecated) Source held as the ID|207 `exposed`, 11 unreachable (`payment-method:sofort`, `payment-method:giropay`)|Not exposed since `6e2287b` (below); all 218 `unproven`|
|44|The members of 11 list envelopes (`lines.data`, `lines.has_more`, `lines.object`, `lines.url`; an invoice's `lines`, a subscription's `items`, a checkout session's `line_items` ...)|`exposed` or not, by the pin's `exposed_today`|`coverage.json` keys each list once, as `CONTRIBUTING.md:45` says ("a list envelope is one leaf"): 10 exposed, and a payment intent's `amount_details.line_items` awaiting proof|
|4|A connected account's external account `account_number`, `country`, `currency`, `routing_number` at the top level|`exposed`|Keyed today as `bank_account.*`, the hash form of a token-or-hash parameter (D3)|
|1|A balance settings `payments.payouts.automatic_transfer_rules_by_currency`, a map|not exposed|Keyed today by its members (`.payout_method`, `.transfer_up_to_amount`, `.type`), awaiting proof|

So "mostly leaves inside embedded copies held as an ID" holds: 439 of the 488 are the ten places and the top-up's `source`. The remaining 49 are a difference of keying between the scope pin (provider `4b16f5e`) and today's `coverage.json`, not missing attributes.

### The top-up's `source`

Until `6e2287b` (provider PR #94, 2026-11-08) `stripe_topup` had no `source` argument, and its resource and data source read the response's `source` as an object of 218 leaves (`internal/generated/topup/fields.go:28`, `0d8b254`: `{Name: "source", Shape: fields.Object, Response: true, ...}`). PR #94 exposed the `verified_accepted_only` request leaf `source` (decision of 2026-11-07, E3), and `reconcile` then made the response's object the ID, on the data source too (`internal/generated/topup/fields.go:28`, `92ec5eb`). The spec describes the field as "The source field is deprecated. It might not always be present in the API response." (`components.schemas.topup.properties.source`, pin `v2585`). No provider data source reads a Source. Decision 11 says an exposed leaf the audit cannot prove "is proven in its wave, never removed"; these 218 were exposed and `unproven`, and are now removed.

## Options

Costs are in sessions of work, each a single focused change in one repository. "Made available" counts leaves exposed and `verified` at the place itself.

|Option|Made available at the place|Suite|Fake|Provider|What a Terraform user meets|
|-|-|-|-|-|-|
|(a) Resource keeps the ID; the holding type's data source returns the object|197 at once; 213 with the price's expansions; 220 once the suite proves `entitlement_feature`|1 session for `entitlement_feature`'s 7|none: the 213 are verified|About 2 sessions: one table serves the resource and the data source today, so the generator needs a per-side shape at these paths; the 16 expand-only price leaves need expansion on the subscription item, which D5 allows only where the resource expands too; one key of them is per currency|`data.stripe_subscription_item.x.price` changes from a string to an object, and `data.stripe_invoice.x.default_tax_rates` from a list of IDs to a list of objects: a breaking change to ten data source attributes, against the architecture ADR's "Data sources also return IDs". The same name holds an ID on the resource and an object on the data source.|
|(b) A Computed read-only copy beside the ID, on the resource and the data source|213 at once (with the price's expansions); 220 after the suite proves `entitlement_feature`|1 session for `entitlement_feature`'s 7|none|About 3 sessions: a new attribute name at each place, which is a second named exception to the architecture ADR's verbatim names (D7), as `<name>_data` is the first; decoding the object into it where the decoder now takes its `id`; the subscription item's expansions of `price.tiers` and `price.currency_options`, and of `price.currency_options.<currency>.tiers` per currency in the response; the full-surface test's assertions|Every subscription item's state carries its price, every invoice, subscription and schedule phase its tax rates. The copy is response-only, so it is unknown on any change (E5): one schedule update plans every phase's copied tax rates as "(known after apply)". A change to a tax rate or price elsewhere (archiving it) appears in every holding resource on refresh. The copy duplicates what the copied object's data source reads.|
|(c) The members leave the target: the copied object's data source reads them|none at the place; 211 equivalents are on the copied objects' data sources, 172 of them `verified` and the cardholder's 39 not yet|none for X7; ten `embedded_copies` claims that the copy equals the copied object's own endpoint are a reliability item (1 session)|none; 0 or 1 session if a claim later fails on the fake|1 session: rule X7 in the coverage report, the docs line|Unchanged: a reference holds the ID; the user reads the object with its data source, as the architecture ADR chose. One docs line per place says where to read it.|
|(d) A mix by place|by place|by place|by place|by place|Two answers to the same shape in one provider, which the docs must explain|

What X3 reasoned for paged lists returned only when expanded holds here more strongly: the object is the copied object's own, read whole by its own data source, and a resource that holds the ID is already its Terraform reference. Unlike an embedded copy at a path the request does not write (an error hash's `payment_method`), which D3 reads in full because nothing else in the holding object says which object it is, here the ID attribute says it.

## Decision

Option (c), as rule X7, for all ten places, without waiting on the suite's copy claims. The top-up's `source` takes a register entry.

Count moved: the 221 rows leave the coverage list's "awaiting proof" for "excluded (X7)", and the in-scope count falls from 10,876 to 10,655. No attribute is added or removed, so nothing breaks. Of the 221, 211 have a counterpart on the copied object's data source, counted per place: 172 `verified` today (the tax rate's 18 at seven places, 126; the price's 40; the feature's 6), and the cardholder's 39, exposed but not yet verified (38 wait on the fake's own work on `stripe_issuing_cardholder`, and `company.tax_id_provided` is `proven_by_test`). The 10 `object` leaves have no counterpart, by the architecture ADR's omission of `object`. The top-up's 218 `source.*` rows are excluded by the register.

Ranked by rows resolved per session, (c) resolves 221 in one provider session, (b) exposes 213 in about four across the suite and the provider, (a) 197 to 213 in about three with a breaking change. Ranked by what a Terraform user expects, (c) is the convention the architecture ADR chose (an ID attribute, and a data source for the object), and the only option that adds no state, no plan noise and no second name for one value.

## Approach Details

### Rule X7

A new row of D1's exclusion rules:

|Rule|Leaves|Reason|
|-|-|-|
|X7. Copies at a reference|a response leaf inside an object (`x-resourceId`) Stripe returns whole at a path to which the request sends that object's ID, where the table holds the ID (`isStringOrObject`, `isIDListOrObjects`): 221 leaves at ten places, such as a subscription item's `price`, an invoice's `default_tax_rates` and an Issuing card's `cardholder`|The copied object's data source reads the same leaves (the reference's docs line names it); excluded from resources and data sources alike. A copy whose object no data source reads needs a register entry instead|

X6 stays withdrawn and is not reused.

### The coverage report

- `tools/schemagen/coverage.go` has `RuleReferenceCopy = "X7"` and its `ruleText`. The walk records the component schema of each embedded copy (`Leaf.Schema`). Where the response holds a copy at a path the request writes, which the table therefore holds as an ID or a list of IDs, `referenceMembers` walks the copied schema into its members, keyed as the suite and the fake key them, and each member gets a record: side `response`, state `excluded`, rule `X7`, its class from `spec/attributes.json`, and `read_by`, the data source type of the copied object (`stripe_tax_rate`). The record at the path itself stays as it is, `exposed`. Generation fails where the table would hold such a copy as an object.
- `annotateReferences` finds the reader from the copied object's name through the tables of `tools/schemagen/resources.go`, as `annotateCopies` does for a copy read in full, and sets `read_by` on the members and `ReadBy` on the field. Generation fails, naming the path and the register entry that would leave the members out, unless exactly one table reads the object, so a new place gets a decision rather than a silent exclusion.
- The register takes a third form for this: a dotted path followed by `.*`, which leaves out the leaves below the path but not the path itself. The top-up's entry is `"source.*"`, citing Stripe's deprecation ("We've deprecated the Sources API and plan to remove support.", https://docs.stripe.com/sources); its 218 members read `excluded`, rule `register`, and the `source` argument and attribute stay exposed.
- `TestReferenceCopyReadersHoldTheirMembers` checks, on the checked-in report, that each X7 member's `read_by` table exposes the same path below the copy, but for `object`. `coverage.md` lists the X7 members under their rule, each with its reader.
- X7 does not wait on the suite's copy claims (decision 2). A gate on them can be added later without redesign: `annotateReferences` would leave a member `awaiting_proof` until the pinned `spec/evidence.json` holds an `embedded_copies` entry with `equals_copied: true` for its place.

### The docs line

The reference attribute's line, on the resource and the data source page, carries one generated sentence after Stripe's description, beside the paged-list sentence's precedent (`readByNote` in `internal/engine/schema.go`):

~~~~~markdown
* `price` - (Optional) The ID of the price object. Stripe returns the whole price here; the provider holds its ID. Read the price with the `stripe_price` data source. Removing it from the configuration stops sending it, and the next apply records the value Stripe holds.

* `default_tax_rates` - The tax rates that will apply to any line item that does not have `tax_rates` set. Stripe returns each tax rate whole here; the provider holds their IDs. Read each with the `stripe_tax_rate` data source.
~~~~~

Reading a list's objects in configuration is a `for_each` over the IDs:

```hcl
data "stripe_tax_rate" "invoice" {
  for_each = toset(stripe_invoice.example.default_tax_rates)
  id       = each.value
}
```

### Nothing changes in the engine's behaviour

The engine already sends the ID and takes `id` from the returned object. No attribute, type, state or plan changes; only descriptions gain the sentence.

### Changes to ADR.md and CONTRIBUTING.md

- `ADR.md`, Status: the amendment of 2026-11-12, naming this file.
- `ADR.md`, Approach Overview: six rules (X1 to X5 and X7) and a register.
- `ADR.md`, D1, "Exclusion rules": the X7 row above.
- `ADR.md`, D3, "Expandable references": the paragraph's last sentence now reads "Where the request sends an ID and the response returns the whole object at the same path (a subscription item's `price`, an invoice's `default_tax_rates`; none of them expandable), the attribute keeps the ID, as `isStringOrObject` and `isIDListOrObjects` do (`tools/schemagen/build.go:607-625`, `92ec5eb`), and the copied object's members fall under X7: its own data source reads them."
- `ADR.md`, D3, "Embedded copies of other objects": "A copy at a path the request writes as an ID is X7's, not this rule's."
- `ADR.md`, D5: "except at a reference, which holds the ID as the resource does (X7)".
- `CONTRIBUTING.md`, "Generated code": X7 in the list of rules; a paragraph on X7, `read_by` and the docs sentence after the one on `PagedLists`; the register's `.*` form.

The measurement's "IDs sent where the response returns the object (221)" (`ADR.md:91`) and the hand-off's "439 response leaves" (`handoff.md:45`) stay as measured; they are this file's 221 and 221 plus the top-up's 218.

## Implementation Roadmap

### Step 1: Provider, rule X7

Rule X7, the member records with `read_by`, the failure where no data source reads a copied object, the register entry for the top-up's `source` and the register's `.*` form, the docs sentence, the tests, the changes to `ADR.md` and `CONTRIBUTING.md`, and the regenerated tables, report and docs.

Acceptance criteria: `coverage.json` holds the 221 members excluded by X7 with their `read_by`, and the top-up's 218 `source.*` members excluded by the register; generation fails on a reference whose copied object no data source reads; the ten docs lines name the data source.
Status: Built, 2026-11-12.
Dependencies: none.

### Step 2: The owner's coverage list

Owner tooling, outside this repository: see "The coverage list" below.

Acceptance criteria: the list shows the 221 rows excluded by X7 and the 218 top-up rows excluded by the register, and no row's provider column comes from the scope pin's `exposed_today` where `coverage.json` has no key.
Status: Not started.
Dependencies: Step 1.

### Queued: the suite's copy claims

A reliability item, not a step of this amendment (decision 2): in the existing attribute tests of the invoice, invoice item, subscription, subscription item, subscription schedule, Issuing card and product feature, an `attrs.Claim` with `Fact.Copied` at each of the ten paths, so the record gains ten `embedded_copies` entries. A claim of `equals_copied: false` would mean a place's copy is a snapshot rather than the object, and would come back as a further amendment, where (b) is the likely answer for that place.

### The coverage list

The scope pin itself needs no change. The generator (`attributes.py` in the owner's `maintaining-coverage-checklist` skill) would:

1. Treat a row as excluded when `coverage.json` holds an `excluded` record for its key with a `rule` (`X1` to `X7`, or `register`), not only when the scope pin's rule starts `X1` to `X4`, so that the provider's rules reach the list without a new scope pin.
2. For a row with no `coverage.json` key, report it as "not keyed" in its own count, instead of falling back to `exposed_today`. The 439 rows of the ten places and the top-up's `source` now have keys; this makes the remaining 49 keying differences visible until the scope is re-pinned.
3. Optionally, for a paged list's envelope members (`<list>.data`, `.has_more`, `.object`, `.url`), take the state of the list's own record, which is how `coverage.json` keys them.

## Architecture Documentation Updates

The repository has no `architecture/` directory; `ADR.md` names `CONTRIBUTING.md` as the place for the contributor-facing parts until it does. The changes are those listed under "Changes to ADR.md and CONTRIBUTING.md".

## Alternatives Considered

Options (a), (b) and (d) are weighed in the table under "Options".

- **(a)** is rejected because it breaks ten data source attributes users can reference today, reverses the architecture ADR's rule that data sources return IDs, and gives one name two types. Reconsider if users ask for the holding object's data source to carry the object, and then as a new attribute rather than a changed one.
- **(b)** is rejected because it duplicates what the copied object's data source reads, needs a second naming exception, and adds state and plan noise to the subscription, invoice and schedule resources. It is the fallback for a place whose copy the suite shows is a snapshot rather than the object.
- **(d)** is the shape the decision takes only if a copy claim fails; no place has a reason for it today.
- **Restoring the top-up's `source` on the data source** in the response's shape (D3, "Same name, different shape") is rejected by the product owner (decision 3): it would wait on the suite's proof of a deprecated object whose payment-method variants the sandboxes mostly cannot reach.
