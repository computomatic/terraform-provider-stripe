# Money-moving resources behind a live-mode guard

## Executive Summary

Eleven Stripe objects that move money (payment intents, charges, refunds, invoices, credit notes, checkout sessions, payouts, top-ups, transfers, transfer reversals and application fee refunds) are data sources only today. This ADR makes them resources so users can manage test data in a sandbox. A provider argument `allow_live_money_movement`, false by default, makes every one of them refuse to create or change an object in a live-mode account. The mode comes from Stripe's own `livemode` flag, read once per provider configuration from `GET /v1/balance`, never from the key. The ADR also settles each resource's arguments, lifecycle attributes and destroy, how plans stay quiet while Stripe moves the objects on by itself, and what the fake and the conformance suite must add first.

## Status

Status: Accepted (decided by the owner agent), 2026-10-22, with the eight open questions answered as recommended and the provisional technical choices of Q9 accepted (see "Resolved questions"). Transfer reversals are the 11th resource. The ADR stays in `adrs/1-pending/` until it is built.

It extends three accepted ADRs:

- **The architecture ADR** (`adrs/1-pending/2026-10-17-provider-architecture/ADR.md`). D3 gains the provider argument.
- **The destroy-semantics ADR** (`adrs/1-pending/2026-10-22-destroy-semantics/ADR.md`). Its rule and its lifecycle attributes (D5 there) apply unchanged. The `Transition` mechanism gains `From`, and `RefuseWhen` gains `Except`.
- **The documentation layout ADR** (`adrs/1-pending/2026-10-22-documentation-layout/ADR.md`). Its D3a already plans guides under `docs-src/guides/`; this ADR builds that and adds the first guide.

Evidence is current as of 2026-10-22, from these sources:

- this repository at `b324a4a`;
- the conformance suite (`stripe-api-conformance`) at `49125db`;
- the fake at `v0.13.0` (`3829f2e`), the tag `.fake-version` names;
- the pinned `spec/openapi.json`;
- Stripe's documentation as quoted.

The decisions most worth scrutiny:

1. **The check is `GET /v1/balance` (D2).** A restricted key therefore needs Balance read permission before it can create or change a money-moving object. No cheaper call reports `livemode` without an existing object.
2. **Lifecycle attributes offer only values the object stays at (D5).** A payment intent takes `succeeded` and `canceled` but not `requires_capture`, because Stripe cancels an uncaptured authorization by itself.
3. **Payment intents confirm and capture through `status`, not through a `confirm` argument (D5, D6.1).** Several transitions that share one value run as a chain, each starting only from its `From` states.
4. **Four resources wait on follow-ups (D11).** Charges, invoices and payouts wait for the suite or the fake. Credit notes ship without `memo` and `metadata` updates, which the suite has not proven.

## Background Context

### The owner's direction

Per the owner, 2026-10-22:

- "I was thinking about a few resources that are excluded because they move real money (payments, transfers, etc.). I think we should add these but with some sort of safety. The use case I can imagine is someone using terraform to manage test data, in a sandbox for instance. These resources would support that, while also progressing our mission to be the most complete stripe provider in the world. The safety could be something simple like an attribute to "enable live mode" which is default false. Without that set, the resource will work fine on test environments but error out if attempted on a live account."
- On one provider-level switch: "I agree that's good UX. But never rely on parsing the key - always check against the api which flags test accounts."
- On destroy (already built, destroy-semantics ADR): "follow stripe api semantics. If reversing a transfer is actually a delete on the api, then that makes sense. If it's creating a second transfer, then that's not our place. If there's no delete supported, then we can error and reject the attempt. The only exception I can think of is "archive" where supported." And: "Cancelling a payment or voiding an invoice don't make them go away, they mutate state. This are attributes, not delete actions."

The owner's product decisions, from the brief for this ADR:

- A provider argument `allow_live_money_movement` (bool, default false). Every money-moving resource refuses to plan or apply a create, update or lifecycle transition against a live-mode account unless it is true. Reads (refresh, import, data sources) are never blocked.
- The check is always an API read of Stripe's `livemode` flag, never the key prefix, for every key type. It works with restricted keys of minimal permission. It is made once per provider configuration and cached. It fails closed.
- Scope: charges, payment intents, refunds (also charge refunds), invoices, credit notes, checkout sessions, payouts, top-ups, transfers, application fee refunds. Transfer reversals stay data source only unless argued otherwise.
- Destroy as the destroy-semantics ADR says. Lifecycle actions are attributes, each only where the suite proves it, with `Transition` and `Final`. A change that would force replacement is refused.

### What exists today

- Each of the ten objects is a data source only, with `engine.NoDelete`. The declarations say why:
  - "moving one through its states (confirm, capture, cancel) is an action, so only the data source exists" (`internal/resources/payment_intent/datasource.go:11 (b324a4a)`);
  - "capturing or refunding one is an action" (`internal/resources/charge/datasource.go:11 (b324a4a)`);
  - "expiring one is an action" (`internal/resources/checkout_session/datasource.go:11 (b324a4a)`).
- Their generator entries have a `ReadPath` only, at `tools/schemagen/resources.go (b324a4a)`:
  - credit_note `:45`, invoice `:48`, payment_intent `:63`, charge `:64`, refund `:65`, checkout_session `:67`;
  - payout `:140`, topup `:141`, transfer `:215`, application_fee_refund `:218`.
- The provider's `Configure` fails on any unknown argument ("The provider cannot be configured while %s is unknown"). It hands every resource and data source the bare `*stripeapi.Client` as `ResourceData` and `DataSourceData` (`internal/provider/provider.go:196-231 (b324a4a)`).
- The engine reads that value as its `Client` interface (`internal/engine/resource.go:85-101`, `internal/engine/datasource.go:55 (b324a4a)`).
- `resolveConfig` refuses keys that start with neither `sk_` nor `rk_` (`provider.go:424-436 (b324a4a)`). The architecture ADR says "The provider does not refuse live keys: users own that choice" (D3, line 147 at `b324a4a`).
- `ModifyPlan` (`internal/engine/resource.go:113-170 (b324a4a)`):
  - first refuses a destroy;
  - returns early for a create (`:123-125`);
  - then runs, in order, `refuseLeavingFinal`, the currency-options and fixed-keys replacement checks, `refuseGoingBack`, `refuseChanges` and `forgetFilled`;
  - ends by making a plan that changes nothing equal to the state.
- `Transition` (`internal/engine/declaration.go:107-127 (b324a4a)`) makes a response-only `status` Optional and Computed. `applyTransitions` (`resource.go:611-637`) calls each transition whose `Value` is planned and differs from the object's, after create and after update. `Final` lists values the transition cannot leave, and `refuseLeavingFinal` (`resource.go:217-231`) fails such a plan.
- `RefuseWhen` (`declaration.go:93-105`) refuses destroy and replacement when a field equals one value (`internal/engine/refuse.go:25-36 (b324a4a)`).
- `tools/docgen` renders a `~>` destroy callout after the lead paragraph and a "Removing from State" section (`tools/docgen/render.go:276-305 (b324a4a)`). It reads `DestroyNote()` from each resource (`tools/docgen/main.go:72-81`). It writes no guides.
- The sandbox smoke run builds only the wave 1 packages (`scripts/smoke-bundle/build.sh`, `wave1=...`, `b324a4a`). The destroy-semantics ADR's D4 admits a refusing resource to it only with a sweep entry that ends its objects: "An object nothing ends (a customer balance transaction) stays out of the smoke run."
- Harness helpers for these objects exist (`CONTRIBUTING.md:67 (b324a4a)`): `tfacc.TransfersAccount`, `tfacc.FundAvailableBalance`, `tfacc.Transfer`, `tfacc.DestinationCharge` and `tfacc.SucceededPaymentIntent`.

### What Stripe documents

- **`livemode` on the balance.** Of the spec's `GET` operations that take no path parameter, the only ones whose response carries a top-level `livemode` are:
  - `/v1/balance`;
  - `/v1/tax/settings`;
  - `/v1/billing/credit_balance_summary`;
  - `/v1/apps/secrets/find`;
  - `/v1/credit_notes/preview`.

  This is a `jq` query over `spec/openapi.json` at `b324a4a`. `/v1/account` has no `livemode`, and a list envelope (`object`, `data`, `has_more`, `url`) has none either.
- **Restricted keys** (https://docs.stripe.com/keys/restricted-api-keys):
  - "you select which Stripe resources the key can access and the permissions for each resource: **Read**, **Write**, or **None**";
  - "The default value for all permissions is **None**";
  - "If a restricted API key doesn't have the correct permissions to complete an API request, Stripe returns an invalid request error".
- **Organization keys** (https://docs.stripe.com/keys/organization-api-keys):
  - "Organization API keys are prefixed `sk_org`. Like account-level restricted API keys (RAKs) or secret keys, you can limit their permissions, and they support sandboxes and live mode";
  - "Organization API keys all have the same `sk_org` prefix, regardless of their permission levels";
  - "All API requests made with an organization API key that target a specific account must include the `Stripe-Context` header to identify the affected account".

  The page's example request is `GET /v1/balance` with `Stripe-Context`. An `sk_org` key's prefix therefore does not say whether it is live.
- **Uncaptured authorizations** (https://docs.stripe.com/payments/place-a-hold-on-a-payment-method): "If the authorization expires before you capture the funds, the funds are released and the payment status changes to `canceled`." Card authorizations last up to 7 days.
- **Automatic advancement** (https://docs.stripe.com/invoicing/integration/automatic-advancement-collection): with `auto_advance` true, Stripe is "Attempting payments for auto-charge invoices". With it false, "you're responsible for transitioning the invoice between states".
- **Taint after a failed create** (https://developer.hashicorp.com/terraform/cli/commands/untaint): "Terraform automatically marks an object as "tainted" if an error occurs during a multi-step "create" action, because Terraform can't be sure that the object was left in a fully-functional state."

### What the suite and the fake prove

Each resource's evidence is in D6, with suite subtests cited by name at `49125db` and the fake at `v0.13.0`. In summary:

- **The suite.**
  - It proves payment intents, refunds through either path, checkout sessions, top-ups, transfers, transfer reversals and application fee refunds well.
  - It never calls `POST /v1/charges` or a charge's `/capture`. Every charge comes from a payment intent (`charge_test.go`).
  - It has no test of `POST /v1/invoices/{id}`, of an invoice's delete, or of void, send or mark-uncollectible as subtests.
  - It proves no credit note update.
  - A payout's cancel succeeds only when the payout has not settled yet. Sandbox payouts settle "within seconds" (`docs/stripe-api-behaviour.md:187`).
- **The fake** answers any parameter it does not implement with `fake_not_implemented`, and it never moves an object on by time.
  - It refuses every payout create for want of an external account (`internal/resources/payout/payout.go:84-88`).
  - It does not serve invoice update, send or mark-uncollectible (`internal/resources/invoice/invoice.go:60-76`).
  - It does not serve credit note update (`internal/resources/creditnote/creditnote.go:68-76`).
  - It does not serve charge create or capture (`internal/resources/charge/charge.go:30-35, 66-74`).
  - Its objects are always `livemode: false`. Its keys must be `sk_test_`.
  - It serves a stored balance whole. A balance with `livemode: true` is served as stored (`internal/resources/balance/balance_test.go:55-77`), and the admin seed replaces the balance (`balance.go:110-122`).

### Constraints

- The ten type names are already data sources, and the resources take the same names (architecture ADR D7). Adding a resource changes no data source and no state.
- Users must be able to apply against a sandbox with the default configuration: a sandbox key is `sk_test_` and its balance is `livemode: false` (`docs/stripe-api-behaviour.md:87`; suite `payment_intent_test.go` "create minimal" asserts `livemode` false).
- Code uses no `any` types (`CLAUDE.md`).

## Approach Overview

- **The switch (D1).** `allow_live_money_movement`, Optional Bool, default false, no environment fallback.
- **The check (D2).** `GET /v1/balance` through the provider's own client, so `Stripe-Account` (and any future `Stripe-Context`) applies. It is made lazily, the first time a money-moving resource plans or applies a change, and cached for that provider configuration, failures included. A missing or non-boolean `livemode`, or any error, refuses the change. When the switch is true no check is made.
- **Where (D3).** At the end of `ModifyPlan` for a create, or for any plan that differs from the state, and again in `Create` and `Update` before the first request. Read, import, data sources and destroy are never checked.
- **Engine (D4).** A declaration flag `MovesMoney`. A `ProviderData` value carries the client and a `LiveCheck`. A `MoneyNote()` writes the page callout.
- **Lifecycle (D5).** A `Transition.From` turns transitions that share a value into a chain. After the transitions an object that did not reach the planned value fails the apply with what Stripe left. A lifecycle value is offered only where the object stays at it. `RefuseWhen.Except` refuses every value but one, for the draft invoice.
- **Per resource (D6).** Arguments as the suite proves them. Unproven update fields go in `ReplaceOn`, which on a refusing resource fails the plan. Destroy is refused everywhere except a draft invoice, which deletes.
- **Changes on their own (D7).** Computed fields and an unconfigured `status` follow Stripe with no planned change. A configured lifecycle value that Stripe moves past fails the next plan through `Final`, with a message that says what to do.
- **Tests, smoke, docs (D8 to D10).** One acceptance test proves the guard end to end against a seeded live balance. A unit test pins which resources move money. No money-moving resource joins the smoke run; one smoke test that creates nothing proves the sandbox reports `livemode: false`. A guide, "Managing Test Data", explains the switch, the lifecycle attributes and why destroy is refused.

## Approach Details

### D1. The provider argument

```hcl
provider "stripe" {
  api_key                   = var.stripe_key
  allow_live_money_movement = false # default
}
```

| Attribute | Required | Notes |
|-|-|-|
| `allow_live_money_movement` | No | Bool, default false. When false, the money-moving resources refuse to create or change an object, or set a lifecycle attribute, in a Stripe account in live mode. When true, they do so in any mode. It changes nothing for reads, imports, data sources, destroy or any other resource. No environment variable sets it |

- **Description**, in the schema and on the provider page: "Allows the resources that move money, such as `stripe_payment_intent` and `stripe_transfer`, to create and change objects in a live-mode Stripe account. Defaults to false: they then work in sandboxes and test mode, and fail at plan time in live mode. The provider asks Stripe for the account's mode; it never infers it from the key."
- **No environment fallback.** The other arguments have one (`STRIPE_API_KEY` and so on, `provider.go:424-470 (b324a4a)`). A safety switch that a shell profile or a CI variable can turn on unseen defeats its purpose, so this one must be written in the configuration where review sees it.
- **Unknown.** `Configure` already refuses an unknown argument for every other argument (`provider.go:203-219 (b324a4a)`); `allow_live_money_movement` joins the same loop with the same message. An unknown value therefore never reaches a resource, and the plan fails before any money-moving plan is made. This is the fail-closed outcome. It is also no worse than today for other configurations, because an unknown `api_key` fails the same way.
- **Aliases.** Each provider configuration has its own switch and its own check. A module can hold a sandbox configuration and a live one, and only the configuration that sets the switch may move live money.

### D2. The check

**The call.** `GET /v1/balance`, sent through the same `stripeapi.Client` as every other request, with no parameters, and read for its top-level `livemode` boolean.

- **Why the balance.** Every account has exactly one, so the call answers on an empty sandbox. It is one small object, and it is the only parameter-free retrieve with a top-level `livemode` that every account type serves. Tax settings and the credit balance summary need products the account may not use, the secret finder needs a name and a scope, and the credit note preview needs an invoice. It is also the call Stripe's own organization-key example makes with `Stripe-Context`.
- **Why not a call the resource itself needs.**
  - A create has no object to read yet.
  - The resource's list endpoint answers with an envelope that has no `livemode`, and on an empty account no element to read one from. The check would then fail closed on every fresh sandbox, which is the main use case.
  - An existing object's own `livemode` (in state for every one of these objects except refunds, application fee refunds and transfer reversals, whose schemas lack it) could serve updates. But that is a second rule beside the balance, and the fake cannot be made to produce a live object to test it, while it can serve a live balance.
- **Restricted keys.** A restricted key needs **Balance: Read** for the check. Without it Stripe answers an invalid request error, and the change is refused with the message below, which names the permission. Write on a resource implies its read, so a key that may create payment intents still needs Balance read. The owner accepted this (Q4).
- **Organization keys.** The provider accepts an `sk_org` key today, because it starts with `sk_`, and sends no `Stripe-Context`, so its requests run against the issuing account. The check goes through the same client and headers as the change it guards, so whatever account a request reaches, the balance read answers for that account. A later `stripe_context` argument needs no change to the check.
- **`stripe_account`.** With a connected account, the balance read carries `Stripe-Account` and reports that account's mode, which is the mode the guarded request runs in.
- **Fail closed.** These outcomes refuse the change:
  - a transport error after the client's retries;
  - any Stripe error, including a permission error;
  - a 200 whose body has no `livemode`;
  - a 200 whose `livemode` is not a boolean.

  A non-200 answer is never read as "test".
- **Once per provider configuration.** `Configure` creates the `LiveCheck` (D4). The first money-moving `ModifyPlan`, `Create` or `Update` makes the call. Later ones, including concurrent ones, wait for it and reuse the answer, error included, for the life of that configured provider.

  Terraform configures the provider again for the apply, so the apply makes its own check. That is the backstop for a saved plan applied with a different key. No call is made by a run that plans no money-moving change, so configurations without these resources, and plans with no change to them, cost nothing and need no Balance permission.
- **Allowed.** When `allow_live_money_movement` is true, `LiveCheck` makes no call and allows every change. The answer would change nothing, and the call would make the Balance permission a requirement for live configurations that have already opted in.

**The errors.** The summary and detail come from the engine, built from the type name and its noun, as the destroy refusal is (`internal/engine/refuse.go:59-71 (b324a4a)`).

Live account, switch false. Summary `Live-mode money movement is not allowed`:

```text
Stripe reports that the account this provider configuration uses is in live mode
(GET /v1/balance answered livemode: true). Creating or changing a payment intent can move
real money, so the provider does not do it in live mode unless the provider block sets
allow_live_money_movement = true.

To manage test data, configure this provider with a sandbox or test-mode key.
```

Check failed. Summary `Cannot tell whether the Stripe account is in live mode`:

```text
The provider creates or changes a payment intent only once Stripe has confirmed the
account's mode, because it can move real money. Reading GET /v1/balance failed:

<the Stripe error, as the client renders it, with its Request-Id>

A restricted key needs the Balance read permission for this check.
```

Both are resource-level errors. With `stripe_account` set, the first sentence names the connected account: "the connected account `acct_...` this provider configuration uses".

### D3. Where the guard runs

**At plan time, in `ModifyPlan`.**

- *Create* (prior state null, plan not null): the guard runs before the early return at `resource.go:123-125`.
- *Update, transition and replace* (both present): the guard runs last, after every other refusal and after the no-change rewrite at the end of `ModifyPlan`. It runs when `resp.Plan.Raw` differs from `req.State.Raw`. A plan the engine has made equal to the state makes no call. A plan another check has already failed makes no call. A replace plan of a resource that can be replaced (a draft invoice) is a change, so it is checked.
- *Destroy* returns before the guard, as today. Deleting a draft invoice moves no money, and every other money-moving resource refuses destroy anyway.

```go
if !req.State.Raw.IsNull() && req.Plan.Raw.IsNull() {
	// destroy refusal, unchanged
	return
}
// ...
if req.State.Raw.IsNull() || req.Plan.Raw.IsNull() {
	r.refuseLiveMoney(ctx, &resp.Diagnostics) // create
	return
}
// ... every existing check, then the no-change rewrite ...
if !resp.Plan.Raw.Equal(req.State.Raw) {
	r.refuseLiveMoney(ctx, &resp.Diagnostics)
}
```

`refuseLiveMoney` does nothing unless the declaration has `MovesMoney`.

**At apply, in `Create` and `Update`.** Each calls `refuseLiveMoney` before its first request and returns on an error, so nothing is sent. This catches:

- `-replace` and taint, which plan without a change the provider sees;
- a saved plan applied with another key;
- a plan made with `-refresh=false`.

`Delete` and `Read` are not checked.

**Not configured.** If a resource's `Configure` received no provider data (the provider was not configured), `refuseLiveMoney` refuses with the second error, detail "The provider is not configured." It never assumes test. Terraform configures the provider before it plans, so this is a backstop only.

**Reads.** `Read`, `ImportState` and every data source never call the check. A live object can be imported and refreshed, and a plan with no change to it succeeds, with the switch false.

### D4. Engine changes for the guard

```go
// Declaration gains:

// MovesMoney marks a resource whose create, update or lifecycle transition can move real
// money. In a live-mode account they fail at plan time, and again at apply, unless the
// provider sets `allow_live_money_movement` (adrs/1-pending/2026-10-22-money-moving-resources).
// Reads, imports and destroy are never checked.
MovesMoney bool
```

```go
// ProviderData is what the provider hands every resource and data source.
type ProviderData struct {
	Client Client
	Live   *LiveCheck
}

// LiveCheck answers, once per provider configuration, whether money may move: always when
// Allow is set, otherwise only when Stripe reports that the account is not in live mode.
type LiveCheck struct {
	allow  bool
	client Client

	mu      sync.Mutex
	checked bool
	live    bool
	account string // the Stripe-Account the client sends, for the error text
	err     error
}

func NewLiveCheck(client Client, allow bool, account string) *LiveCheck

// Refusal returns the error diagnostic that stops a change to typeName, or nil when the change
// may go ahead. The first call reads GET /v1/balance; later calls reuse its answer.
func (c *LiveCheck) Refusal(ctx context.Context, typeName string) diag.Diagnostic
```

- `internal/provider.Configure` builds `engine.ProviderData{Client: client, Live: engine.NewLiveCheck(client, allow, cfg.StripeAccount)}` and hands it as both `ResourceData` and `DataSourceData`. `Resource.Configure` and `DataSource.Configure` read `*engine.ProviderData` in place of `Client`. Data sources use only `Client`.
- The balance read decodes into the closed `wire` value type and reads `GetBool("livemode")`. No `any` is involved (architecture ADR D1, "No untyped values").
- `compile` refuses `MovesMoney` on a declaration with `NoDelete`, because there is no resource to guard.
- `(r *Resource) MoneyNote() string` returns the page callout (D10) for a `MovesMoney` resource and "" otherwise. `tools/docgen` reads it beside `DestroyNote()` (`tools/docgen/main.go:72-81`).
- Engine unit tests use a stub `Client` that counts balance reads.

### D5. Lifecycle transitions

The destroy-semantics ADR made irreversible actions attributes set through `Transition` (its D5). Money-moving objects need three things it did not: an action that only works from some states, a target reached by two actions (confirm, then capture), and an apply that tells the user when Stripe left the object somewhere else.

**`From` and chains.**

```go
// Transition gains:

// From lists the values of Field from which the action is sent. The action is skipped when
// the object holds any other value. Empty means any value but Value. Several transitions may
// share a Value: they run in declaration order, each from the value the one before left, so
// a payment intent's `succeeded` confirms it from `requires_confirmation` and then captures
// it from `requires_capture`.
From []string
```

`applyTransitions` (`resource.go:611-637`) already runs in declaration order and lays each response over the body. It gains the `From` test before each call. `Final` keeps its meaning. Every transition that shares a value declares the same `Final`, and `compile` refuses a declaration whose transitions of one value disagree on it.

**The apply ends where the plan said.** After the transitions, `Create` and `Update` compare each transition field's planned known value with the body's. When they differ, the apply fails with summary `Stripe left the <noun> at <value>`. The detail says what happened:

> Setting `status` to `succeeded` stopped at `requires_action`: no action moves a payment intent from `requires_action` to `succeeded`.

The state records the body as decoded, so it holds the real value, never the planned one. That avoids Terraform's "inconsistent result after apply" error. A first build has no waiting: the suite shows card flows settling in the response ("confirm", "capture the full amount"), and the fake has no asynchronous moves. A transient value worth waiting for, such as `processing`, joins later with suite proof, as `AdvanceAction.ReadyValue` waits for a test clock (`declaration.go:129-143`).

**Errors after the object exists.** A create that fails after Stripe has made the object leaves it in state, and Terraform marks it tainted (quoted above). On a resource whose destroy is refused, the next apply would try to destroy it and fail at the backstop. Such errors (the follow-up update, a transition, the check above) therefore end, on a refusing resource, with:

> The payment intent `pi_...` exists in Stripe and Terraform has marked it tainted. Because it cannot be destroyed, run `terraform untaint <address>` once the configuration is fixed, or remove it from the state.

This also applies to the existing refusing resources with transitions (quote, setup intent, schedule and the others in the destroy-semantics ADR's D5), which have the same exposure.

**Which values a lifecycle attribute offers.** A value is offered only where both hold:

1. the suite proves the action that reaches it;
2. the object stays at it unless someone acts on the object, or unless the object's own configuration asks Stripe to move it on.

A terminal value (`succeeded`, `canceled`, `void`, `expired`) always qualifies. An invoice's `open` qualifies, because it moves on only with `auto_advance` and automatic collection, which the configuration sets (D6.4). A payment intent's `requires_capture` does not: Stripe cancels an uncaptured authorization after its validity window whatever the configuration says. A configured value that never drifts by itself keeps plans quiet (D7).

**`RefuseWhen.Except`.**

```go
// RefuseWhen gains:

// Except refuses every value of Field but Value, such as an invoice in any status but
// `draft`.
Except bool
```

`refusal` (`refuse.go:25-36`) negates the comparison when `Except` is set and the state's value is known and not null.

### D6. The resources

Every resource below has `MovesMoney: true`. Its generator entry gains `CreatePath` and `UpdatePath` (`tools/schemagen/resources.go`), so create and update arguments come from the spec as for every resource (architecture ADR D1).

Three rules apply to all of them:

- **Unproven updates.** An update field the spec takes but the suite does not prove goes into `ReplaceOn`, as the setup intent does: "The other fields the spec takes on update are not proven, so they are treated as create-only" (`internal/resources/setup_intent/resource.go (b324a4a)`). On a refusing resource, a change to one then fails the plan with `Cannot replace`.
- **Omitted parameters.** Parameters that belong to an action the resource models through `status`, or that only a front end uses, are left out with `Omit`, as the setup intent leaves out `confirm`, `mandate_data` and `use_stripe_sdk` (`tools/schemagen/resources.go:61 (b324a4a)`).
- **Tests end with `acctest.Forget`** unless the resource deletes the object.

"Suite" cites subtests at `49125db`, "fake" cites `v0.13.0`, and "BD" is the suite's `docs/stripe-api-behaviour.md`.

#### D6.1 `stripe_payment_intent`

- **Create arguments.**
  - Proven: `amount`, `currency`, `automatic_payment_methods` ("create minimal"); `customer`, `description`, `capture_method`, `statement_descriptor_suffix`, `metadata` ("create with optional fields"); `payment_method` ("create with payment method attaches it to the intent").
  - The rest of the spec's create parameters ship as create arguments.
  - `Omit`: `confirm`, `confirmation_token`, `error_on_requires_action`, `mandate`, `mandate_data`, `off_session`, `payment_method_data`, `return_url`, `use_stripe_sdk`, `radar_options` and `hooks`, beside the data source's existing `payment_method_options` and `amount_details` (`tools/schemagen/resources.go:63`). Confirming is `status`.
- **Updates.**
  - Proven: `description`, `amount`, `customer`, `metadata` ("update"); `payment_method`, which moves the intent to `requires_confirmation` ("update payment method moves status").
  - `ReplaceOn`: `currency`, `capture_method`, `receipt_email`, `setup_future_usage`, `shipping`, `transfer_data`, `transfer_group`, `application_fee_amount`, `payment_method_configuration`, `statement_descriptor`, `statement_descriptor_suffix`, `excluded_payment_method_types`, `allowed_payment_method_types` and `customer_account`. Each is in the spec's update, but the suite does not prove any of them.
  - Stripe refuses `amount` once the intent has succeeded (`payment_intent_unexpected_state`: "amount after success", BD:87). The error is Stripe's, at apply.
- **`status`.**

  | Value | Transitions, in order | Final | Suite |
  |-|-|-|-|
  | `succeeded` | `/confirm` from `requires_confirmation`; `/capture` from `requires_capture` | `canceled` | "confirm", "confirm after attaching payment method", "capture the full amount", "already succeeded" |
  | `canceled` | `/cancel` from `requires_payment_method`, `requires_confirmation`, `requires_action`, `requires_capture` | `succeeded` | "cancel", "cancel without reason", "cancel an authorized intent releases the hold", "already succeeded" (cancel) |

  A manual-capture intent set to `succeeded` is confirmed and captured in one apply. Partial capture (`amount_to_capture`) and `cancellation_reason` are action parameters, not attributes, and are not offered.

  A declined card ("declined card": 402 `card_declined`, the intent stays `requires_payment_method`) fails the apply as D5 says. The user sets another `payment_method`, and the next apply updates and confirms.
- **Sensitive:** `client_secret`, as the data source has it.
- **Destroy:** refused. `RefuseReason`: "Stripe cannot delete a payment intent; set `status` to `canceled` to cancel one that has not succeeded."
- **Test inputs:** `pm_card_visa`, `pm_card_chargeDeclined`, and `automatic_payment_methods = { enabled = true, allow_redirects = "never" }` (suite `payment_intent_test.go:48-51`).
- **Fake:** serves create, retrieve, update, confirm, capture, cancel, list and search (`internal/resources/paymentintent/paymentintent.go:82-95`). Confirm takes only `payment_method`. No gap blocks this resource.

#### D6.2 `stripe_charge` (waits on S1 and F1)

- **The gap.** The suite never creates a charge with `POST /v1/charges` and never captures one; every charge comes from a payment intent (`charge_test.go`). The spec describes both operations as deprecated. The fake serves neither (`internal/resources/charge/charge.go:30-35`).
- **Create** (once S1 proves it): `amount`, `currency`, `source` (`tok_visa`), `customer`, `description`, `metadata`, `capture`, `receipt_email`, `statement_descriptor`, `statement_descriptor_suffix`, `shipping`, `transfer_group`. `Omit` the deprecated `card`, `destination` and `application_fee`.
- **Updates.**
  - Proven: `description`, `metadata` ("update").
  - `ReplaceOn`: `customer`, `fraud_details`, `receipt_email`, `shipping`, `transfer_group`.
  - Stripe refuses `amount` (`charge_invalid_parameter`: "immutable amount", BD:183).
- **Lifecycle:** `captured = true` through `/capture`, a bool transition (`declaration.go:107-127` allows one), once S1 proves capture.
- **Destroy:** refused. "Stripe cannot delete a charge; refund it with a `stripe_refund`."
- Built last, behind S1 and F1 (Q2).

#### D6.3 `stripe_refund` (also charge refunds)

- **One resource, created through `/v1/refunds`.** A refund made through `/v1/charges/{charge}/refunds` is the same object ("same object as /v1/refunds", BD:120). Two resources for one object would let two configurations manage it. `charge` and `payment_intent` are in an `ExactlyOneOf`. See Q3.
- **Create arguments.**
  - Proven: `payment_intent`, `amount`, `reason`, `metadata` ("create partial").
  - `amount` is server-defaulted: left out, Stripe refunds the rest ("create the rest").
  - `charge` on `/v1/refunds` is proven only as "unknown charge", an error. The charge path proves the rest (`charge_refund_test.go` "create partial", "create the rest"). S2 proves `charge` on `/v1/refunds`.
  - The spec's other create parameters ship.
- **Updates:** `metadata` only ("update metadata"); `amount` and `reason` are `parameter_unknown` ("immutable amount", "immutable reason").
- **Lifecycle:** none. `/cancel` is not proven to succeed: a card refund is `succeeded` at once and refuses it ("cancel", BD:89).
- **Destroy:** refused. "Stripe cannot delete or undo a refund."
- **Fake:** serves create with `amount`, `charge`, `metadata`, `payment_intent` and `reason`, plus retrieve, update and list (`internal/resources/refund/refund.go:74-78`). No gap blocks it.

#### D6.4 `stripe_invoice` (waits on S3 and F2)

- **Create arguments.**
  - Proven: `customer`, `collection_method`, `currency`, `days_until_due`, `auto_advance`, `pending_invoice_items_behavior`, `metadata` ("draft produced by a pending invoice item").
  - Pass `currency`: without it the account default applies (BD:168).
  - The spec's other create parameters ship.
- **Updates.** The suite has no test of `POST /v1/invoices/{id}`, and the fake does not serve it. The resource waits until S3 proves which fields update on a draft and on a finalized invoice. Proven fields then update in place, and the rest go into `ReplaceOn`.
- **`status`.**

  | Value | Transitions, in order | Final | Suite |
  |-|-|-|-|
  | `open` | `/finalize` from `draft` | `paid`, `void`, `uncollectible` | finalize as setup in "setup note" (`credit_note_test.go:36`), asserted by `requireObject` |
  | `paid` | `/finalize` from `draft`; `/pay` from `open` | `void` | `paidInvoice` (`invoice_payment_test.go:18-30`), which asserts `paid` |

  - Pay as proven sends `payment_method`, which is not a field of the invoice, and a transition sends only the object's own fields. S3 therefore proves `/pay` with no parameters on an invoice whose `default_payment_method` is set, before `paid` is offered.
  - `void`, `uncollectible` and `send` wait on S3, which proves each as its own subtest with its refusal from the wrong state. The suite only voids in a cleanup helper today (`invoice_test.go:79-82`).
  - A $0 invoice finalizes straight to `paid` (BD:119), so `open` on one fails the apply as D5 says.
- **Destroy:** `Delete` (the spec has `DELETE /v1/invoices/{invoice}`, documented for drafts), with `RefuseWhen{Field: "status", Value: "draft", Except: true}`. The reason is "Stripe deletes only a draft invoice; a finalized one stays in the account." Destroying a draft deletes it. Destroying or replacing any other invoice is refused at plan time, including a draft Stripe has finalized by itself, which the refreshed state shows. S3 proves the delete and its refusal.
- **Tests:** a test that keeps the invoice a draft ends with the harness's destroy; one that finalizes it ends with `Forget`.
- **Items.** `stripe_invoiceitem` joins an invoice through its create-only `invoice` (`internal/generated/invoiceitem/fields.go:19 (b324a4a)`). Terraform creates the invoice, and applies its `status`, before items that refer to it, so an invoice whose `status` is set in the same apply is finalized empty. The guide (D10) shows the order that works: pending items (without `invoice`) first, then the invoice with `pending_invoice_items_behavior = "include"` and `depends_on` on them, then `status`.
- **Fake gaps:** F2 (update, send, mark-uncollectible). Delete of a draft that holds items is `fake_unproven`, so a provider-made draft with items cannot be destroyed on the fake.

#### D6.5 `stripe_credit_note`

- **Create arguments.**
  - Proven: `invoice` (finalized; a draft is refused: "create errors" / "draft invoice", BD:173), `memo`, `metadata`, and `lines` of `custom_line_item` with `description`, `quantity` and `unit_amount` ("setup note").
  - The spec's other create parameters ship (`amount`, `credit_amount`, `refund_amount`, `refunds`, `out_of_band_amount`, `reason`, `email_type`, `effective_at`, `shipping_cost`). The fake implements only `amount`, `invoice`, `lines`, `memo`, `metadata` and `reason` (`creditnote.go:68-76`), so tests use those.
  - Whether a note refunds money depends on these amounts. That is why the whole resource is guarded, whatever a given note does.
- **Updates.** The spec takes `memo` and `metadata`. The suite proves neither, and the fake does not serve the update. Both go into `ReplaceOn`, so changing one fails the plan, until S4 and F3 land.
- **`status`:** `void` through `/void`, no `Final` (an issued note can only be voided). Suite: "a voided note keeps its lines" (`credit_note_line_item_test.go:105-109`), which calls `/void` and checks the object. S4 adds the assertion that `status` is `void`.
- **Destroy:** refused. "Stripe cannot delete a credit note; set `status` to `void` to void one." An invoice cannot be voided while an issued note remains (`credit_note_test.go:32-34`).
- **`_update` test:** sets `status = "void"`, the only change the resource can make until S4.

#### D6.6 `stripe_checkout_session`

- **Create arguments.**
  - Proven: `mode`, `success_url`, `line_items` ("create minimal"); `customer`, `client_reference_id`, `cancel_url`, `metadata` ("create with optional fields").
  - The spec's other create parameters ship. The fake implements only these seven (`checkoutsession.go:70-83`).
- **Updates.**
  - Proven: `metadata` ("update metadata").
  - `ReplaceOn`: `line_items`, `shipping_options`, `collected_information`.
- **`status`:** `expired` through `/expire`, `Final: complete`. Suite: "expire", "expire errors" / "already expired" (BD:91).
- **Sensitive:** `client_secret`, as the data source has it. `line_items` is in `Expand`, as on the data source.
- **Destroy:** refused. "Stripe cannot delete a checkout session; set `status` to `expired` to expire an open one."

#### D6.7 `stripe_payout` (waits on F4)

- **Create arguments.**
  - Proven: `amount`, `currency`, `description`, `metadata` ("create"). The account needs an external bank account in the currency, which only the Dashboard adds ("create without an external account", BD:121; the US sandbox has one, BD:187).
  - The spec's other create parameters ship.
- **Updates.**
  - Proven: `metadata` only ("update").
  - `description` is `parameter_unknown` ("description is not updatable", BD:187), and the spec's update agrees, so it is create-only.
- **Lifecycle:** none (Q5). `/reverse` makes a second payout, which is not the provider's to do (the owner on reversing transfers).
- **Destroy:** refused. "Stripe cannot delete a payout."
- **Fake gap F4:** every create is refused with "Sorry, you don't have any external accounts in that currency" (`payout.go:84-88`). Until the fake serves a platform external account, the tests skip before they create anything, with `tfacc.SkipUnlessFakeServes`.

#### D6.8 `stripe_topup`

- **Create arguments.**
  - Proven: `amount`, `currency`, `description`, `metadata` ("create").
  - A CA account is refused ("refused for a CA account", BD:122).
  - The spec's other create parameters ship.
- **Updates:** `description`, `metadata` ("update"), which is the spec's whole update.
- **`status`:** `canceled` through `/cancel`, from `pending`, `Final: succeeded, failed, reversed`. Suite: "cancel", "cancel errors" / "already canceled". The cancel subtest skips when the top-up is no longer pending. The fake keeps every top-up `pending`, so it proves the cancel on every run.
- **Destroy:** refused. "Stripe cannot delete a top-up; set `status` to `canceled` to cancel a pending one."

#### D6.9 `stripe_transfer`

- **Create arguments.**
  - Proven: `amount`, `currency`, `destination`, `description`, `metadata`, `transfer_group` ("create", "create with only the required fields").
  - `source_transaction` and `source_type` ship. The fake answers them `fake_not_implemented`, so tests do not set them.
  - Needs available balance (`balance_insufficient`: "more than the available balance") and a connected account with `transfers` active (`tfacc.TransfersAccount`).
- **Updates.**
  - Proven: `description`, `metadata` ("update").
  - `amount`, `destination` and `currency` are `parameter_unknown` ("immutable").
- **Lifecycle:** none. A reversal is a new object, not a state of the transfer.
- **Destroy:** refused. "Stripe cannot delete a transfer; a reversal is a new object, which destroy does not create."

#### D6.10 `stripe_application_fee_refund`

- **Shape.** `Parent: "fee"`, at `/v1/application_fees/{fee}/refunds`, with import ID `<fee>/<id>` (architecture ADR D7). The data source already uses this (`tools/schemagen/resources.go:218`).
- **Create arguments:** `amount` (server-defaulted: left out, it refunds the rest) and `metadata` ("create partial", "create the rest"). Errors: "more than remains" (BD:211). Needs a fee from `tfacc.DestinationCharge`.
- **Updates:** `metadata` ("update"); `amount` is `parameter_unknown` ("immutable amount").
- **Destroy:** refused. "Stripe cannot delete or undo an application fee refund."

#### D6.11 `stripe_transfer_reversal`

- **Shape.** `Parent: "transfer"`, at `/v1/transfers/{transfer}/reversals`, the same shape as the application fee refund.
- **Create arguments:** `amount` (server-defaulted) and `metadata` ("create partial", "create the rest").
- **Updates:** `metadata` ("update").
- `description` and `refund_application_fee` ship. The fake does not implement them, so tests do not set them.
- **Destroy:** refused.

### D7. Objects that change on their own

| Object | What Stripe changes by itself | Evidence |
|-|-|-|
| payment intent | `canceled` when an authorization expires; `processing` to `succeeded` for asynchronous methods | Stripe docs above; not seen in the suite, whose card confirms are synchronous |
| charge | `amount_refunded`, `refunded`, `disputed`; `balance_transaction` is null right after confirm | `refund_test.go` "create partial"; BD:88 |
| refund | `pending` to `succeeded` or `failed` for non-card methods | spec status description; card refunds succeed at once (BD:89) |
| invoice | finalized and charged when `auto_advance` is true; `amount_paid`, `amount_remaining` | Stripe docs above; BD:119 |
| checkout session | `expired` at `expires_at` (24 hours by default), `complete` when paid | spec; "expire" |
| payout | `pending` to `in_transit` to `paid` within seconds; `paid` to `failed` | BD:187; spec status description |
| top-up | `pending` to `succeeded` | "create" accepts either |
| transfer | `amount_reversed`, `reversed` through reversals | `transfer_reversal_test.go` "create partial" |

What the engine does with each kind of field:

- **Computed fields** (amounts, `refunded`, `balance_transaction`, an unconfigured `status`) are Computed without `UseStateForUnknown`, as the architecture ADR's D2 requires for "Fields that can change server-side". Refresh records Stripe's value. A plan that changes nothing configured is rewritten to the state (`resource.go:143-166`), so it shows no change and makes no balance call. A plan that does change something shows these as known after apply, which is honest.
- **A transition field left out of the configuration** is Optional and Computed, so Terraform keeps the state's value. A payout that went `paid`, or a session that expired, plans nothing.
- **A configured lifecycle value** is offered only where the object stays at it (D5). A payment intent set to `succeeded` stays `succeeded` whatever is refunded. An invoice set to `paid` stays `paid`. A session set to `expired` stays `expired`.

  Where the configuration lets Stripe move the object on, the next plan fails through `Final` rather than planning a change for ever. The only such case offered is an invoice set to `open` with `auto_advance` and automatic collection, which Stripe then pays. The error reads:

  > Stripe cannot move this invoice out of `paid`. Remove `status` from the configuration.

  That is `refuseLeavingFinal`'s message today (`resource.go:217-231`). Removing `status`, or setting it to `paid`, quiets the plan. The guide says to use `send_invoice` or `auto_advance = false` for invoices meant to stay open.
- **Read** never removes an object because of its status; only a 404 does (architecture ADR D2).

### D8. Tests

**Engine unit tests** (`internal/engine`, stub client):

- `LiveCheck`:
  - test mode allows;
  - live mode refuses with the first error;
  - an error, a missing `livemode` or a non-bool one refuses with the second;
  - allowed makes no call;
  - concurrent callers make one call;
  - the answer, error included, is reused.
- `ModifyPlan` on a `MovesMoney` resource:
  - create checks;
  - a plan equal to the state does not check;
  - a changed field checks;
  - a transition checks;
  - destroy does not check;
  - a plan refused by another check does not check;
  - a resource without `MovesMoney` never checks.
- `Create` and `Update` refuse before any request.
- `Read`, `ImportState` and data sources make no balance call.
- Transitions:
  - `From` skips and chains;
  - the end-state mismatch error and the recorded state;
  - the taint guidance on a refusing resource;
  - the `Final` agreement check in `compile`.
- `RefuseWhen.Except`.

**Provider unit tests** (`internal/provider`):

- the argument's schema and default;
- an unknown value fails `Configure`;
- `ProviderData` carries the switch;
- `TestMoneyMovingResources` lists the type names whose declaration has `MovesMoney` and compares them with the set in D6. A resource that moves money but forgot the flag, or a flag on the wrong resource, fails here.

**Acceptance tests** per resource: the six required tests, with `acctest.Forget` (destroy-semantics ADR, D4). They run on a fake account, whose balance is `livemode: false`, so the guard passes, and the request log shows the one `GET /v1/balance`.

**The guard end to end:** `TestAccPaymentIntent_liveMode`, against the fake only (`acctest.RequireFake`).

1. Seed the account's balance with `livemode: true` through the harness's `Seed` (`internal/acctest/admin.go:146-175 (b324a4a)`).
2. A `PlanOnly` create step with the switch unset expects `Live-mode money movement is not allowed`. A `tfacc.LogMark` check shows that no POST reached the fake.
3. A step with `allow_live_money_movement = true` creates the intent.
4. A step that changes `description` with the switch unset expects the same error at plan.
5. `Forget`.

**Missing permission** is a unit test only: the fake has no restricted keys.

### D9. The sandbox smoke run

- **No money-moving resource joins the smoke run.** Every one refuses destroy, or, for the invoice, refuses it once finalized. The destroy-semantics ADR's D4 admits a refusing resource only with a sweep that ends its objects, and excludes "An object nothing ends". Nothing ends a succeeded payment intent, a refund, a paid payout, a transfer or a fee refund. In a shared sandbox each run would add objects that stay for ever.
- **One smoke test that creates nothing:** `TestAccBalance_liveModeCheck`, in the `balance` package, which is already in the smoke bundle (`scripts/smoke-bundle/build.sh`, `wave1`).
  - It plans a `stripe_topup` create with the switch unset, in a `PlanOnly` step that expects a non-empty plan and no error.
  - Against the CI sandbox this proves that real Stripe answers `GET /v1/balance` with `livemode: false` for the CI key, the one fact the fake cannot prove.
  - It also runs against the fake.
- **The sweep is unchanged.** If Q8 later admits some of these resources, their sweep entries end what can be ended, as the destroy-semantics ADR allows the sweep to: expire open sessions, cancel open intents and pending top-ups, void open invoices and issued credit notes, delete drafts, and reverse the rest of a transfer, as `tfacc.Transfer` already does in smoke mode (`internal/acctest/tfacc/connect.go:87-104 (b324a4a)`).

### D10. Documentation

**Resource pages.** `renderResource` adds `MoneyNote()` as a `~>` callout straight after the lead, before the destroy callout (`tools/docgen/render.go:283-285`). It is one paragraph:

```markdown
~> A payment intent can move real money. In a live-mode Stripe account, creating or changing one, including setting its `status`, fails at plan time unless the provider sets `allow_live_money_movement = true`. Reading or importing one never does. The provider asks Stripe for the account's mode. See the Managing Test Data guide.
```

"including setting its `status`" appears only on resources with transitions. The resource's `Description` loses any sentence that says it is a data source only. Each `DataSourceDescription` keeps today's text.

**The provider page** (`docs-src/provider.md`) gains a section after Authentication:

```markdown
## Live mode and money movement

The resources that move money (`stripe_payment_intent`, `stripe_charge`, `stripe_refund`, `stripe_invoice`, `stripe_credit_note`, `stripe_checkout_session`, `stripe_payout`, `stripe_topup`, `stripe_transfer`, `stripe_transfer_reversal` and `stripe_application_fee_refund`) are meant for test data in sandboxes and test mode. Before one creates or changes anything, the provider reads the account's balance and checks Stripe's `livemode` flag. In live mode the change fails at plan time unless the provider block sets `allow_live_money_movement = true`. The key's prefix is never used. A restricted key needs the Balance read permission for this check. Reads, imports and data sources are never checked.
```

**The guide.** `docs-src/guides/test-data.md`, copied by `tools/docgen` to `docs/guides/test-data.md`, with front matter `page_title: "Managing Test Data"`. The documentation layout ADR's D3a already decided the location and the copy; `tools/docgen` gains the copy, and deletes a guide under `docs/guides/` that has no source. Sections:

1. **What the money-moving resources are for.** Seeding a sandbox or test-mode account with payments, refunds, invoices and transfers that an application's tests or demos read.
2. **The live-mode guard.** The switch, the balance check, the Balance permission, and that each provider configuration checks its own account.
3. **Moving objects through their states.** The `status` values each resource takes, with an example: a customer, a payment intent with `payment_method = "pm_card_visa"` and `status = "succeeded"`, then a `stripe_refund` of it. Also the invoice ordering of D6.4, and that a declined test card leaves the intent where Stripe put it.
4. **Objects that change on their own.** D7 in user terms, including invoices meant to stay `open`.
5. **Why destroy is refused, and how to clean up.** Stripe cannot delete these objects. Cancelling, voiding or expiring an object changes its state, so it is an attribute, set before the object is removed from the state with a `removed` block or `terraform state rm` (the destroy-semantics ADR's D3 text). A sandbox is cleaned up by deleting the sandbox in the Dashboard, not with `terraform destroy`.

**Changelog** (`CHANGELOG.md`, Unreleased), a new first bullet:

```markdown
- Resources for objects that move money, for managing test data: `stripe_payment_intent`, `stripe_refund`, `stripe_credit_note`, `stripe_checkout_session`, `stripe_topup`, `stripe_transfer`, `stripe_transfer_reversal` and `stripe_application_fee_refund`, each beside its data source. A new provider argument, `allow_live_money_movement` (default false), makes them fail at plan time in a live-mode account; the provider asks Stripe for the account's mode with `GET /v1/balance`, so a restricted key needs the Balance read permission to create or change one. Stripe cannot delete these objects, so destroying or replacing one fails at plan time; `status` cancels, captures, voids or expires them where Stripe allows. See the Managing Test Data guide.
```

The bullet names the resources the build actually ships. `stripe_charge`, `stripe_invoice` and `stripe_payout` are added to it as their follow-ups land.

**`CONTRIBUTING.md`**, "Adding a resource", gains:

> A resource whose create, update or transition can move money sets `MovesMoney` and joins the list in `TestMoneyMovingResources`. Its acceptance tests need nothing more: the fake's balance is in test mode.

**The architecture ADR.** In D3, the example gains `allow_live_money_movement = false` and the table gains the row of D1. The `api_key` note's "The provider does not refuse live keys: users own that choice." gains: "The resources that move money refuse live mode unless `allow_live_money_movement` is set (amended 2026-10-22 by `adrs/1-pending/2026-10-22-money-moving-resources/ADR.md`)."

**README.** Its resource overview names the money-moving resources and the switch in one sentence, pointing at the guide.

### D11. Fake and suite follow-ups

**Fake** (`fake-stripe-api`):

| ID | Gap at `v0.13.0` | Blocks |
|-|-|-|
| F1 | `POST /v1/charges` (with `source`) and `POST /v1/charges/{id}/capture` (`charge.go:30-35`) | `stripe_charge` |
| F2 | `POST /v1/invoices/{id}`, `/send`, `/mark_uncollectible` (`invoice.go:60-76`); delete of a draft that holds items is `fake_unproven`; pay takes only `payment_method` (`internal/resources/invoice/payment.go`), and `pay` with none, from `default_payment_method`, is needed | `stripe_invoice` |
| F3 | `POST /v1/credit_notes/{id}` (`memo`, `metadata`) (`creditnote.go:68-76`) | credit note updates |
| F4 | A platform external bank account, so `POST /v1/payouts` succeeds (`payout.go:84-88`); cancel of a pending payout is `fake_unproven` | `stripe_payout` tests |
| F5 | Checkout session update of `line_items`, `shipping_options`, `collected_information` (only `metadata`: `checkoutsession.go:70-83`) | moving those out of `ReplaceOn` |
| F6 | Transfer `source_transaction`; transfer reversal `description`, `refund_application_fee` (`fake_not_implemented`) | tests of those arguments only |
| F7 | Nothing needed if the admin seed accepts a balance with `livemode: true`, which the handler serves (`balance_test.go:55-77`). The guard's acceptance test confirms it. If the seed refuses, the fake must accept one | the guard's acceptance test |

**Suite** (`stripe-api-conformance`):

| ID | Proof needed | Unblocks |
|-|-|-|
| S1 | `POST /v1/charges` with `source=tok_visa` (and `capture=false`), and `/capture`; refusals of both | `stripe_charge` |
| S2 | `POST /v1/refunds` with `charge` | `stripe_refund.charge` |
| S3 | Invoice: `POST /v1/invoices/{id}` on a draft and on a finalized invoice (which fields, which refusals); `DELETE` of a draft and its refusal on a finalized one; `finalize`, `pay` (with no parameters, using `default_payment_method`), `void`, `mark_uncollectible` and `send` as subtests with their refusals from the wrong state | `stripe_invoice`, its `void`, `uncollectible` |
| S4 | Credit note `POST /v1/credit_notes/{id}` (`memo`, `metadata`); assert `status` `void` after `/void` | credit note updates |
| S5 | Checkout session update of `line_items` and `shipping_options` | as F5 |
| S6 | Payment intent update of the `ReplaceOn` fields in D6.1, as users ask | moving them to updatable |

## Resolved questions

Answered by the owner on 2026-10-22. Each question and recommendation as asked, with the answer:

1. **Transfer reversals.** The brief keeps them data source only "unless you argue otherwise". A reversal is the same shape and the same proof as an application fee refund, which is in scope: parent-scoped, `amount` and `metadata` on create, `metadata` on update ("create partial", "create the rest", "update"). The fake serves it, and refusing destroy needs nothing new. Leaving it out leaves the one gap in Connect money movement. Recommendation: add `stripe_transfer_reversal` (D6.11) as the 11th resource. **Answer:** yes, an 11th resource.
2. **Charges.** The suite proves neither direct create nor capture, Stripe's spec calls both deprecated, and the fake serves neither. Recommendation: keep it in scope but build it last, gated on S1 and F1. The alternative is to drop it and point users at `stripe_payment_intent`, which makes a charge. **Answer:** keep it, built last, behind S1 and F1.
3. **Refunds through the charge.** Recommendation: one `stripe_refund`, created through `/v1/refunds` with `charge` or `payment_intent`, plus S2, and no `stripe_charge_refund`. The alternative is a second resource on the charge path, which the suite proves today but which manages the same object. **Answer:** yes, one `stripe_refund` taking `charge` or `payment_intent`.
4. **Balance read permission.** Restricted keys need Balance: Read to create or change any money-moving object. Recommendation: accept it and document it. The alternative of reading the resource's own list fails closed on an empty account. **Answer:** acceptable; the provider page and the guide document it.
5. **Payout cancel.** The suite's cancel succeeds only before the payout settles, which takes seconds on the sandbox, and the fake marks it unproven. Recommendation: no `status` on payouts for now. The alternative offers `canceled` with `Final: paid, failed`, which would almost always fail. **Answer:** agreed, no `status` on payouts.
6. **Authorized-only payment intents.** `requires_capture` is useful test data, but Stripe cancels it by itself after the authorization window, which would make the next plan fail through `Final`. Recommendation: do not offer it. **Answer:** agreed.
7. **Invoices waiting on proof.** Recommendation: build `stripe_invoice` only after S3 and F2. The alternative ships it now with every update field in `ReplaceOn`, so even `metadata` would replace a draft and fail the plan on a finalized invoice. **Answer:** agreed, invoices are built after S3 and F2.
8. **Smoke.** Recommendation: as D9, only the zero-object check. The alternative admits payment intents and transfers with sweep entries and accepts that succeeded intents and reversed transfers accumulate in the CI sandbox. **Answer:** agreed, the smoke run gets only the check that creates nothing.
9. **Provisional technical choices**, listed so they can be overruled:
   - the switch has no environment variable;
   - when it is true no check is made;
   - a failed check is cached for the configuration;
   - the guard also applies to changes that move no money themselves (`metadata` on a live charge), as the brief says.

   **Answer:** every provisional choice accepted.

## Implementation Roadmap

**As built in stage A**, where D6 left a detail open or the build departed from it:

- A refund's `charge` is a read-only attribute (`ReadOnly` in the generator) and `payment_intent` is required (`Require`), until S2 proves `charge` on `/v1/refunds`. D6.3's `ExactlyOneOf` comes with S2.
- A payment intent's `hooks` is `ReadOnly` rather than omitted, because it is also a response field and the data source shows it. `payment_method` is `PreserveConfig`, because Stripe answers a test payment method such as `pm_card_visa` with a new one. `capture_method` and `confirmation_method` are server-defaulted.
- A checkout session's `collected_information`, which only the update takes, is `ReadOnly` rather than in `ReplaceOn`. `payment_method_options.pay_by_bank` is omitted, because the generator cannot model its shape. Seven fields Stripe fills in are server-defaulted. The data source's `line_items` became the list of items the resource takes, in place of an object whose `data` holds them. No release has shipped the old shape, so the changelog records no change.
- A top-up's `source` is `ReadOnly`, so the data source keeps the source object rather than the create's source ID.
- A transfer's `amount` is required (`Require`), as Stripe requires it (suite: "missing amount"). Its `source_type` is server-defaulted.
- An application fee refund's tests fund the platform's available balance, because the fake proves a refund only within that balance.
- `stripe_climate_order` sets `MovesMoney` as well, beyond D6's list: its create spends the account's available balance on a carbon removal order (owner, 2026-10-22). Its `TestAccClimateOrder_liveMode` proves the refusal against a seeded live balance.
- `LiveCheck` keeps Stripe's answer and Stripe's errors, but not a read that ended with its caller's context, and a change waiting for another's read gives up when its own context ends.
- The guard's acceptance test confirmed F7: the fake's admin seed accepts a balance with `livemode: true`.


### Step 1: The guard in the engine and provider

Add `MovesMoney`, `ProviderData`, `LiveCheck`, `refuseLiveMoney` in `ModifyPlan`, `Create` and `Update`, `MoneyNote`, the `compile` check, and the provider argument with its `Configure` wiring and the D1 description. No declaration sets `MovesMoney` yet. See D1 to D4.

Acceptance criteria: the engine and provider unit tests of D8 pass; `go test ./...` passes; no page changes on `go generate ./...` except the provider page's new argument.

Status: DONE (stage A)

Dependencies: (none)

### Step 2: Transitions and refusals

Add `Transition.From`, the end-state check, the taint guidance on refusing resources, the `Final` agreement check, and `RefuseWhen.Except`. See D5.

Acceptance criteria: the transition unit tests of D8 pass; existing resources' acceptance tests pass on both CLIs; their pages are unchanged.

Status: DONE (stage A), except `RefuseWhen.Except`, which waits for `stripe_invoice` (Step 5)

Dependencies: (none)

### Step 3: Docs machinery

Add the `MoneyNote` callout to `renderResource`, the guide copy from `docs-src/guides/` to `docs/guides/`, the provider page section and the guide of D10, with render tests under `tools/docgen/testdata`.

Acceptance criteria: `go test ./tools/docgen/...` passes; `scripts/check-docs.sh` passes; `docs/guides/test-data.md` exists with `page_title`.

Status: DONE (stage A)

Dependencies: Step 1: `MoneyNote`.

### Step 4: Resources the fake already serves

Build `stripe_payment_intent`, `stripe_refund` (with `payment_intent`; `charge` once S2 lands), `stripe_checkout_session`, `stripe_topup`, `stripe_transfer`, `stripe_application_fee_refund`, `stripe_credit_note` (with `memo` and `metadata` in `ReplaceOn`), and `stripe_transfer_reversal`. Include generator entries, declarations, examples, the six acceptance tests each, `TestAccPaymentIntent_liveMode`, `TestMoneyMovingResources` and the smoke test of D9. Regenerate `docs/`.

Acceptance criteria: the acceptance suite passes against the pinned fake with Terraform and with OpenTofu; each page has the money callout and, where destroy is refused, the destroy callout; `TestAccBalance_liveModeCheck` passes against the fake.

Status: DONE (stage A), except `stripe_credit_note`, which moves to Step 5, and the refund's `charge`, which waits for S2

Dependencies: Steps 1 to 3.

### Step 5: Resources waiting on follow-ups

Build `stripe_invoice` (after S3 and F2), with `RefuseWhen.Except`, `stripe_payout` (tests run after F4), `stripe_charge` (after S1 and F1), and `stripe_credit_note`, which stage A left out. Move credit note updates out of `ReplaceOn` after S4 and F3. Bump `.fake-version` to the fake release that serves each.

Acceptance criteria: as Step 4 for each resource; the invoice's draft tests destroy normally and its finalized tests end with `Forget`.

Status: PENDING

Dependencies: Step 4, and the follow-ups named for each resource.

### Step 6: Changelog, contributor docs and the architecture ADR

Apply the D10 changelog bullet (naming the resources shipped), the `CONTRIBUTING.md` sentence, the README sentence and the architecture ADR's D3 amendment.

Acceptance criteria: the changelog names every money-moving resource that ships; the architecture ADR's D3 lists `allow_live_money_movement`.

Status: DONE for the resources of stage A

Dependencies: Step 4 (and Step 5 for its resources).

## Architecture Documentation Updates

`architecture/` does not exist yet; the architecture ADR's Step 7 initialises it with `/init-adrs`. Until then the governing text is the architecture ADR, whose D3 is amended as D10 says. When `architecture/` is initialised, its description of provider configuration includes the `allow_live_money_movement` row of D1, and its description of resource mapping includes this paragraph:

```markdown
**Money movement.** A resource whose create, update or lifecycle transition can move money declares `MovesMoney`. Before it creates or changes an object, at plan time and again at apply, the engine asks Stripe for the account's mode with `GET /v1/balance`, once per provider configuration, and refuses the change in live mode unless the provider sets `allow_live_money_movement`. An error or an answer without `livemode` refuses it too. Reads, imports, data sources and destroy are never checked.
```

## Alternatives Considered

### 1. Infer the mode from the key prefix

Treat `sk_live_` and `rk_live_` as live and everything else as test.

**Pros:**
- No request, no permission.

**Cons:**
- The owner ruled it out: "never rely on parsing the key - always check against the api".
- `sk_org` keys share one prefix across sandboxes and live mode (Stripe's organization key docs).

**Reconsider if:** none expected.

### 2. A per-resource `allow_live` attribute

**Pros:**
- Finer control: a configuration could allow only transfers in live mode.

**Cons:**
- The owner chose one provider-level switch.
- A per-resource attribute is not a Stripe field (architecture ADR D7). A module author could also set it inside a module the user only calls, where a provider argument stays in the root configuration's hands.

**Reconsider if:** users ask to allow some money movement in live mode and not other.

### 3. Check at `Configure`

**Pros:**
- One place, before any resource runs.

**Cons:**
- Every plan of every configuration would make the call and need the Balance permission, including configurations with no money-moving resources and plans with no change to them.

**Reconsider if:** none expected.

### 4. The resource's own list or the object's own `livemode`

**Pros:**
- Uses a permission the key already has for the resource.

**Cons:**
- The list envelope has no `livemode`, and an empty account has no element, so a create on a fresh sandbox would fail closed.
- The object's own `livemode` covers updates only. It is a second rule, and the fake cannot produce a live object to test it.

**Reconsider if:** Stripe adds `livemode` to list envelopes, or a parameter-free endpoint that reports the mode without a permission.

### 5. Separate action attributes (`confirm = true`, `capture = true`)

**Pros:**
- Each action is explicit.

**Cons:**
- They are not Stripe fields (architecture ADR D7). They duplicate what `status` already says, and they conflict once Stripe moves the object. The destroy-semantics ADR set `status` transitions as the pattern.

**Reconsider if:** an action has no status it leads to.

### 6. Offer every status Stripe can be moved to

Such as `requires_capture` and `uncollectible`.

**Pros:**
- More kinds of test data.

**Cons:**
- A value Stripe leaves by itself makes the next plan fail through `Final` until the configuration is edited (D7), which is the perpetual noise the brief asks to avoid.

**Reconsider if:** users ask for one, with the documented consequence, or Stripe documents a way to hold an object there.
