# Destroy follows the entity lifecycle

## Executive Summary

`terraform destroy` today ends objects Stripe cannot delete in whatever way Stripe allows: it cancels quotes, setup intents, schedules, Climate orders and verification sessions, voids credit grants, expires file links and tax registrations, detaches payment methods, or leaves the object and warns. This ADR amends D2 of the architecture ADR so that destroy is only a delete (an HTTP `DELETE` in the pinned spec) or an archive (a reversible deactivation), and anything else fails at plan time with instructions to remove the object from state with a `removed` block or `terraform state rm`. A change that would replace such an object fails at plan time too, and irreversible lifecycle actions become attributes the user sets where the suite proves them.

## Status

Status: Accepted (decided by the owner agent), 2026-10-22, with the eight open questions answered as recommended (see "Resolved questions"). The `create_before_destroy` risk under D2 is accepted as recorded. It is built (every step below is done) and stays in `adrs/1-pending/` with the repository's other accepted ADRs.

It amends D2 ("Delete versus archive") of `adrs/1-pending/2026-10-17-provider-architecture/ADR.md` and the acceptance resolution "Archive and expire" recorded in that ADR's Status. It replaces the rule that keeps the account's default billing portal configuration on destroy. Nothing in the `/v2` ADR (`adrs/1-pending/2026-10-17-v2-support/ADR.md`) changes: its one resource, `stripe_core_event_destination`, deletes.

Evidence is current as of 2026-10-22: the repository at `6047d16`, the conformance suite (`stripe-api-conformance`) at `f2141be`, terraform-plugin-framework v1.19.0, Terraform 1.15.6 and OpenTofu 1.12.3 run locally (CI runs Terraform 1.16.6 and OpenTofu 1.12.3, `.github/workflows/ci.yml:75,79` at `6047d16`).

The per-resource findings are in [`destroy-audit.md`](destroy-audit.md): 18 resources delete, 1 deletes or archives, 10 archive, 4 delete or archive except in a named state, and 15 refuse destroy.

## Background Context

### The owner's direction

Per the owner, 2026-10-22:

- "I think we should follow stripe api semantics. If reversing a transfer is actually a delete on the api, then that makes sense. If it's creating a second transfer, then that's not our place. If there's no delete supported, then we can error and reject the attempt. The only exception I can think of is "archive" where supported."
- "I think we need to focus on the entity (resource) lifecycle. Cancelling a payment or voiding an invoice don't make them go away, they mutate state. This are attributes, not delete actions. Make sure this same decision (and the above about deletes/undo) is applied consistently across the whole provider."

The owner's product decisions, from the brief for this ADR:

- A delete is an HTTP `DELETE` on the object in the pinned spec, whatever Stripe's docs call it. `DELETE /v1/subscriptions/{id}`, which cancels, is a delete.
- An archive is Stripe's reversible deactivation: an `active`-style flag, or a deactivate endpoint with a matching reactivate (products, prices, meters, tax rates, shipping rates, billing portal configurations and the like).
- Anything else refuses destroy: an irreversible end state reached by `POST` (expire, cancel, void, close, reject) is a state change. Destroy never creates an object and never sends a lifecycle action. The refusal is an error at plan time where the CLIs allow it, telling the user to remove the object from state with a `removed` block and `lifecycle { destroy = false }`, or `terraform state rm`.
- Replacement of such an object is refused at plan time too. A change to a create-only field of a resource whose destroy is refused is an error, as `RefuseChange` is today, unless the user removes the old object from state first.
- Lifecycle actions are attributes the user sets, as `stripe_issuing_dispute.status = "submitted"` already is, and only where the suite proves the action.
- This replaces the engine's `Retain` strategy, the `Expire` strategy, any archive `Ended` state that acts as an end of life, and the 2026-10-18 rule that keeps the default billing portal configuration on destroy.

### What the engine does today

- `DeleteKind` (`internal/engine/declaration.go:37-55` at `6047d16`) has six strategies: `Delete`, `Archive`, `DeleteOrArchive`, `Expire`, `Retain` and `NoDelete`.
- `ArchiveAction` (`declaration.go:70-89`) sets a field or calls an action endpoint. `Now` sends the literal `now` (file links, and the credit grant's `voided_at` after `/void`). `Ended` (`declaration.go:85-100`) lists other end states in which destroy leaves the object as it is: an accepted quote, a succeeded setup intent, a released or completed schedule, an expired credit grant.
- `KeepWhen` (`declaration.go:245-251`) turns a refused archive or delete of a protected object into a warning and drops it from state: the default portal configuration (`is_default`), the default bank account of a currency (`default_for_currency`), a delivered Climate order, a verified verification session.
- `Expire` (`declaration.go:116-130`, `resource.go:740-777`) starts a scheduled tax registration, sets `expires_at` a few seconds ahead and polls for `expired`.
- `Retain` (`resource.go:523-527`) sends nothing, warns with `RetainWarning` (`declaration.go:253-254`) and drops the object from state: customer balance transactions, Treasury financial accounts, Issuing personalization designs and disputes.
- `Delete` (`resource.go:482-549`) dispatches on the strategy at apply. Nothing is checked at plan time.
- `ModifyPlan` (`resource.go:107-155`) returns at once when the plan is null (`resource.go:111-113`), so a destroy plan passes through untouched.
- `RefuseChange` (`declaration.go:355-363`, `resource.go:249-287`) fails a plan that changes a listed field, unless the plan is a replace forced by a create-only or replace-on field, the parent or the attached object (`resource.go:230-242`). Its error suggests `terraform taint`.
- Create-only fields get `RequiresReplace` through `planMods` (`internal/engine/schema.go:49-61`), and their description ends "Changing it replaces the resource." (`schema.go:40-46`). `ReplaceIfSet` fields get `RequiresReplaceIf`.
- `Transitions` (`declaration.go:258-262`) make a response-only status field Optional and Computed, and a configured value calls the action endpoint that sets it. `stripe_issuing_dispute` (`status = "submitted"`) and `stripe_billing_alert` (`active`, `inactive`) use it.
- `tools/docgen` reads `ImportNote()` from each resource (`tools/docgen/main.go:67-81`) and renders the resource page as lead, Example Usage, Argument Reference, Attribute Reference, Timeouts, Import (`tools/docgen/render.go:280-305`).

### What the framework and the CLIs do

- The framework advertises `PlanDestroy` ("signals that the provider is ready for the PlanResourceChange RPC on resource destruction ... requires Terraform 1.3 or later", `terraform-plugin-framework@v1.19.0/internal/fwserver/server_capabilities.go:25-30`).
- It calls the resource's `ModifyPlan` on a destroy plan: "We do this regardless of whether the plan is null or not, because we want resources to be able to return diagnostics when planning to delete resources" (`internal/fwserver/server_planresourcechange.go:305-312`). After the attribute plan modifiers it returns early on any error (`server_planresourcechange.go:300-302`), so an error from an attribute plan modifier means `ModifyPlan` is not called.
- A probe provider (one resource whose `ModifyPlan` errors on a null plan and on a changed `RequiresReplace` field, and whose `Delete` errors; run with terraform-plugin-testing v1.16.0, then deleted) showed, on both Terraform 1.15.6 and OpenTofu 1.12.3:
  - removing the resource from the configuration fails in a `PlanOnly` step with the `ModifyPlan` error;
  - a `Destroy: true` step fails with the `ModifyPlan` error, not the `Delete` error, so `terraform destroy` is refused at plan;
  - a change to the `RequiresReplace` field fails in a `PlanOnly` step with the `ModifyPlan` error;
  - a tainted object is not seen at plan: the apply calls `Delete`, which fails. `-replace` behaves the same, since its first plan still carries the prior state (`resource.go:256` at `6047d16`);
  - a `removed { from = ...  lifecycle { destroy = false } }` step succeeds and leaves the state empty;
  - the harness's post-test destroy fails with the `ModifyPlan` error when the last step leaves the object in state.
- `removed` arrived in Terraform 1.7.0 ("`removed` block for refactoring modules", Terraform `CHANGELOG.md`, v1.7 branch). Terraform's `lifecycle` block in `removed` is required, and "Set `destroy` to `false` to remove the resource from state without destroying the actual resource" (https://developer.hashicorp.com/terraform/language/block/removed).
- OpenTofu's `removed` block in 1.7 to 1.9 forgets the object without a `lifecycle` block, and the 1.9 migration guide tells Terraform users to "Remove the `lifecycle` block" (https://opentofu.org/docs/v1.9/intro/migration/terraform-1.9/). OpenTofu 1.10: "The `removed` block now supports lifecycle rules and provisioners" (https://opentofu.org/docs/v1.10/intro/whats-new/). The form with `lifecycle { destroy = false }` therefore works on Terraform 1.7+ and OpenTofu 1.10+.

### Constraints

- Users already hold state for every resource released in 0.1.0 (`CHANGELOG.md`), so a resource's destroy changes for existing state the moment they upgrade. Nothing in state changes on upgrade.
- The sandbox smoke run executes only the wave 1 packages (`scripts/smoke-bundle/build.sh:15` at `6047d16`: billing_meter, coupon, customer, price, product, webhook_endpoint, balance), and its sweep removes their leftovers through the API (`internal/smoke/sweep.go:53-59`). None of them refuses destroy.

## Approach Overview

- **The rule (D1).** Destroy deletes where the spec has `DELETE`, archives where Stripe has a reversible deactivation, and is refused otherwise. It never creates an object and never sends a lifecycle action.
- **Two refusals (D2).** A new strategy `Refuse` refuses every object of a resource. A new field `RefuseWhen` refuses the objects of a deleting or archiving resource that are in a named state (the default portal configuration, a canceled card). `Retain`, `Expire`, `ExpireAction`, `Ended`, `ArchiveAction.Now`, `KeepWhen` and `RetainWarning` are deleted.
- **At plan time, with an apply backstop (D2).** `ModifyPlan` refuses a destroy plan. A replace plan is refused by the attribute plan modifier that would have forced it. `Delete` refuses again at apply, for `-replace`, taint and `replace_triggered_by`, which plan time cannot see.
- **Errors and pages (D3).** Every refusal says why Stripe cannot remove the object and shows the `removed` block and `terraform state rm`. The page of every refusing resource gets a `~>` callout and a "Removing from State" section with the same example.
- **Tests (D4).** Tests of a refusing resource end with a step, built by `acctest.Forget`, that removes it from state with a `removed` block. The `_delete` and `_replace` tests prove the plan-time refusal, the taint backstop and that nothing was sent to Stripe. The smoke sweep is unchanged.
- **Lifecycle attributes (D5).** Irreversible actions the suite proves become attributes through `Transitions` or the update, as answered in Q6.
- **Changelog and docs (D6).** A changelog entry names every resource whose destroy changes. CONTRIBUTING and the architecture ADR's D2 are amended.

## Approach Details

### D1. The rule

| Stripe offers | Destroy | Examples |
|-|-|-|
| `DELETE` on the object in the pinned spec | Delete | customer, coupon, subscription (which cancels), test clock |
| `DELETE`, refused for some objects that Stripe lets be archived | Delete, else archive with a warning | product with a price |
| No `DELETE`; a reversible deactivation: an `active`-style field the update takes back, or a deactivate endpoint with a matching reactivate | Archive | price, meter, portal configuration, cardholder |
| No `DELETE`, and only an irreversible end state reached by `POST`, or nothing | Refused | quote (cancel), credit grant (void), file link (expire), financial account (close), balance transaction (nothing) |

- An object of a deleting or archiving resource that Stripe refuses to delete or archive in a known state is refused in that state (`RefuseWhen`): the account's default portal configuration, the default bank account of a currency, a canceled card, an archived alert.
- Reversibility is judged by the suite where it has a proof, by the spec otherwise. The audit cites each. Price and product reactivation is not proven by the suite; both stay archives because the owner names them.
- A lifecycle action that ends an object (cancel, void, expire, close, submit) is a change of the object's state: where the suite proves it, the user sets it as an attribute (D5), then removes the object from state.

### D2. Engine

#### Declaration

```go
// DeleteKind selects what `terraform destroy` does (ADR D2, "Delete versus archive", as
// amended by adrs/1-pending/2026-10-22-destroy-semantics).
type DeleteKind int

const (
	// Delete calls DELETE. A 404 is success.
	Delete DeleteKind = iota
	// Archive retrieves the object, sends the declared reversible deactivation and removes it
	// from state.
	Archive
	// DeleteOrArchive calls DELETE and, when Stripe refuses in the way ArchiveOn names,
	// archives the object instead and warns.
	DeleteOrArchive
	// Refuse means Stripe can neither delete nor archive the object. Destroy and replacement
	// fail at plan time, and destroy again at apply, with RefuseReason and how to remove the
	// object from state.
	Refuse
	// NoDelete means Stripe offers no way to create the object here. There is no resource,
	// only a data source.
	NoDelete
)

// ArchiveAction is the request that archives an object. Field, Value and Path as today;
// Now and Ended are deleted.

// RefuseWhen names a state in which Stripe refuses to delete or archive an object of a
// deleting or archiving resource, such as the account's default portal configuration.
type RefuseWhen struct {
	// Field is a top-level attribute in state, such as `is_default`.
	Field string
	// Value is the refused value.
	Value wire.Value
	// Reason is the sentence that says why, such as "Stripe refuses to deactivate the
	// account's default portal configuration."
	Reason string
}
```

`Declaration` gains `RefuseReason string` (with `Refuse`: the sentence that says why Stripe cannot remove the object and, where an attribute ends it, which, such as "Stripe cannot delete a quote; set `status` to `canceled` to cancel one.") and `RefuseWhen *RefuseWhen` (with `Delete`, `Archive` or `DeleteOrArchive`). It loses `KeepWhen`, `Expire` and `RetainWarning`; `FieldIs` is deleted with `KeepWhen`.

`compile` (`internal/engine/model.go:109-128` at `6047d16`) checks:

- `Refuse` needs a `RefuseReason` and has no `Archive`, `ArchiveOn`, `Paths.Delete` or `RefuseWhen`.
- `RefuseWhen` needs `Delete`, `Archive` or `DeleteOrArchive`, a `Reason`, and a `Field` that is a top-level attribute the response fills.
- `Refuse` or `RefuseWhen` together with `CurrencyOptions` or `FixedKeys` is refused, because those replacements are forced in `ModifyPlan` (`currency.go:169`, `fixedkeys.go:18`) and no refusing resource has either. The engine is not extended for a resource that does not exist.

#### Which objects are refused

`(m *model) refusal(state tftypes.Value) (reason string, refused bool)`: for `Refuse`, always, with `RefuseReason`. For `RefuseWhen`, when the state's `Field` equals `Value`, with its `Reason`. The state is the refreshed prior state, so an object that reached the state out of band is refused at the next plan; with `-refresh=false` the apply backstop covers a stale state.

#### Destroy at plan time

`ModifyPlan` starts with:

```go
if !req.State.Raw.IsNull() && req.Plan.Raw.IsNull() {
	if reason, refused := r.m.refusal(req.State.Raw); refused {
		resp.Diagnostics.AddError("Cannot destroy "+r.m.decl.TypeName, r.m.destroyRefusal(reason))
	}
	return
}
```

This runs for `terraform destroy`, `terraform apply -destroy`, a resource removed from the configuration, a `removed` block with `destroy = true`, and a `count` or `for_each` that shrinks. Terraform stops the whole plan on the error, so a configuration that holds a refused object cannot be destroyed until the user removes that object from state. That is the intended outcome (per the owner: "we can error and reject the attempt").

#### Replacement at plan time

A replacement is a destroy, so it is refused where the destroy would be. The attribute plan modifier that forces the replacement does it, because it sees every attribute at every depth, including nested create-only fields such as `quote.subscription_data.billing_mode` and list elements such as `subscription_schedule.phases[*].currency`, which `replaces` (`resource.go:230-242`) does not:

- `planMods` (`schema.go:49-61`) takes the model. For a resource with `Refuse` or `RefuseWhen` it attaches one engine modifier, `replaceUnlessRefused`, in place of `RequiresReplace` and `RequiresReplaceIf`. It implements each typed modifier interface the engine uses.
- The modifier wraps the framework's (`RequiresReplace`, or `RequiresReplaceIf` with `changedByConfig`, `schema.go:65-70`) and acts only when that one asks for a replacement and `refusal(req.State.Raw)` refuses the object. It then fails the plan, with an attribute error `Cannot replace <type>` and the text of D3, and clears `RequiresReplace`, only for a real change: a planned value that is fully known (the wrapped modifier has seen that it differs), or, while the planned value still holds unknowns, a configured value that differs from the state's (`configChanges`: an unset value is no change, an unknown one is). The framework runs a nested object's modifiers before its children's, so a create-only object whose computed children are still unknown asks for a replacement that Terraform drops once the children keep their state (`UseStateForUnknown`); the quote's `from_quote.is_revision` does so on every plan after a change elsewhere. Otherwise the wrapped modifier's answer stands, as today.
- `Parent` and `Attach` attributes, and `ReplaceOn` fields, go through the same modifier, so `customer_payment_method.customer` and `setup_intent.flow_directions` are refused like create-only fields.
- The framework returns before `ModifyPlan` once a plan modifier has failed (`server_planresourcechange.go:300-302`), so `refuseChanges` never sees a refused replace.

`refuseChanges` keeps its rule: it skips a plan that `replaces`, because a replace sends the new value on create. On a refusing resource such a plan has already failed in the attribute modifier. Its error text changes for a refusing resource only: in place of "recreate the object deliberately with `terraform taint`" it says to remove the object from state and apply again to create a new one (D3), because taint would fail at apply.

The description of a create-only attribute on a `Refuse` resource ends "Changing it fails the plan, because this resource cannot be replaced." in place of "Changing it replaces the resource." (`describeResource`, `schema.go:40-46`). On a `RefuseWhen` resource it stays, and the page's callout names the exception.

#### Destroy at apply, the backstop

`Delete` for `Refuse` adds the same error as plan time and sends nothing. For `RefuseWhen` it checks the prior state first and, when it is refused, does the same. Otherwise it deletes or archives. If Stripe refuses that with a 400 `invalid_request_error`, `Delete` retrieves the object, and when the retrieve shows the refused state it reports Stripe's error followed by the guidance of D3. Any other failure is Stripe's error. This replaces `keepIfProtected` and `keptError` (`resource.go:625-647`): a protected object is never dropped from state with a warning.

The backstop catches `-replace`, `terraform taint` and `replace_triggered_by`, which plan as a replace without any change the provider sees. Under the default lifecycle the destroy fails first and nothing is created. Under `create_before_destroy` the new object is created and the old one stays in state as deposed, with the error; the page's callout says that replacing such a resource is not supported.

#### What is deleted

`Retain`, `Expire`, `ExpireAction`, `(r *Resource) expire` and `expired` (`resource.go:740-777`), `Ended`, `ArchiveAction.Now` and the `Past` branch of `holds`, `KeepWhen`, `FieldIs`, `keptError`, `keepIfProtected`, `RetainWarning`, and the `Retain` and `Expire` branches of `compile` and `Delete`. `pollInterval` stays for `Advance`.

#### Interactions

| Mechanism | After this ADR |
|-|-|
| `RefuseChange` | Unchanged rule. Its error drops the taint suggestion on refusing resources. A refused replace never reaches it |
| `DeleteOrArchive` | Unchanged. It may carry `RefuseWhen`; none does |
| `Transitions` | They carry the lifecycle attributes of D5. A transition into a refused state (an alert to `archived`) makes the next destroy refused. A transition declares in `Final` the values it cannot leave (an alert's `archived`; a schedule's `canceled`, `released` and `completed`; an accepted quote; a succeeded setup intent; the suite proves no such state for a Climate order or a verification session), and `ModifyPlan` fails a plan that moves the field from one of them to the transition's value, on the attribute, before Stripe would refuse the action. A resource page lists only the values the transitions set |
| `Ended` | Deleted. Each resource that used it refuses destroy |
| `Advance` | Unchanged. The test clock deletes |
| Read | Unchanged. A 404 still removes the object from state, so an object deleted out of band is never refused |

### D3. Errors and pages

The engine builds every text from the declaration; no resource writes its own.

**Destroy refused**, summary `Cannot destroy stripe_quote`:

````text
Stripe cannot delete a quote; set `status` to `canceled` to cancel one. The provider does
not destroy what Stripe cannot delete or archive, so the quote is still in the Stripe account
and in the state.

To stop managing it, replace its resource block with a removed block and apply (Terraform 1.7
or later, OpenTofu 1.10 or later):

removed {
  from = stripe_quote.<name>

  lifecycle {
    destroy = false
  }
}

or run `terraform state rm stripe_quote.<name>` (`tofu state rm` in OpenTofu).
````

The first sentence is `RefuseReason`, or the `RefuseWhen` reason. The noun ("quote") is the type name without `stripe_` and with spaces, as docgen derives it (`tools/docgen/render.go:83`). The provider does not know the resource's address, so the example says `<name>`; Terraform prints the address above the error ("with stripe_quote.main").

**Replacement refused**, an attribute error, summary `Cannot replace stripe_quote`:

````text
Changing `test_clock` needs a new quote, because Stripe cannot change it on an existing one,
and the existing quote cannot be destroyed. Stripe cannot delete a quote; set `status` to
`canceled` to cancel one.

Revert the change, add `lifecycle { ignore_changes = [test_clock] }` to leave it as it is, or
remove the existing quote from the state with a removed block or `terraform state rm` and apply
again to create a new one.
````

**The page.** The engine exposes `DestroyNote() string` (empty for a resource that never refuses), read by `tools/docgen` beside `ImportNote()` (`tools/docgen/main.go:67-81`). When it is set, `renderResource` (`tools/docgen/render.go:280-305`) adds:

- a `~>` callout after the lead paragraph, one paragraph as the Registry requires (documentation layout ADR, D1):

  ```markdown
  ~> Stripe cannot delete a quote; set `status` to `canceled` to cancel one. `terraform destroy`, and a change that would replace the quote, fail at plan time. To stop managing a quote, remove it from the state as [Removing from State](#removing-from-state) shows; it stays in the Stripe account.
  ```

  For `RefuseWhen` the middle sentence reads "`terraform destroy`, and a change that would replace it, fail at plan time for such a portal configuration."
- a `## Removing from State` section between Timeouts and Import:

  ````markdown
  ## Removing from State

  To stop managing a quote whose destroy is refused, remove it from the state without destroying it. In Terraform 1.7 and later, and OpenTofu 1.10 and later, replace the `resource` block with a [`removed` block](https://developer.hashicorp.com/terraform/language/block/removed) and apply:

  ```terraform
  removed {
    from = stripe_quote.example

    lifecycle {
      destroy = false
    }
  }
  ```

  Using `terraform state rm`:

  ```console
  % terraform state rm stripe_quote.example
  ```

  The quote stays in the Stripe account. Replacing one with `terraform apply -replace` or `terraform taint` fails in the same way, at apply.
  ````

The descriptions of the refusing resources lose their destroy sentences, which the callout now carries. The descriptions of archiving and deleting resources keep theirs.

### D4. Tests and the smoke run

**Harness** (`internal/acctest/forget.go`, `internal/acctest/tfacc/refused.go`):

- `acctest.Removed(addresses ...string) string` returns one `removed` block with `lifecycle { destroy = false }` per address.
- `acctest.Forget(t, steps, check, typesOrAddresses...)` appends the last step of a test of a refusing resource: the configuration of the last step that has one, without the resource blocks of the given types or at the given addresses (every one any step configures) and without every block that refers to one of them, found by `hclwrite` (already a dependency), plus `Removed` for each. A data source that reads a forgotten object is dropped and so destroyed as usual. The harness's post-test destroy would otherwise fail on the refusal (as the probe showed), so a test that forgets fails loudly, and the layout meta-test needs no new rule.
- `acctest.Without(t, config, typesOrAddresses...)` is a configuration without those blocks, for the `_delete` test's plan-time step.
- `tfacc.RefusedDestroy` and `tfacc.RefusedReplace` match the two errors. `tfacc.Holds` checks a field of the object through the API. `tfacc.LogMark` records the length of the fake's request log (newest first) and `tfacc.Untouched` checks the field and that no request after the mark changed the object or called one of its actions; the fake does not log requests made through `Stripe-Account`, so a connected account's object is checked by its field only.
- `tfacc.SkipUnlessFakeServes(t, c, method, path, params)` generalises `SkipUnlessFakeAnswers`. A test that skips on a gap in the fake after its create would leave the refused object in state and fail the post-test destroy, so such a test probes the fake before it creates anything (the financial account's update, a dispute's other reasons).

Against the fake the objects go with the account (`NewAccount`'s cleanup); against a sandbox see "Smoke" below.

**The required tests** (`internal/resources/layout_test.go:17` at `6047d16`) keep their names. For a refusing resource:

- `_delete`: create; remove the resource from the configuration in a `PlanOnly` step that expects `Cannot destroy`; a `Destroy: true` step expecting the same error, which proves `terraform destroy` is refused before apply; the `Forget` step, whose `tfacc.Untouched` check retrieves the object and finds it unchanged and, against the fake, finds no request to it after its create, which proves no lifecycle action was sent.
- `_replace`: where the resource has a create-only, replace-on, parent or attach field, a `PlanOnly` step changing one expects `Cannot replace`. Every refusing resource also has a step with `Taint` that expects `Cannot destroy` at apply, which proves the backstop. Then the `removed` step.
- `_create`, `_update`, `_import`, `_drift`, and any data source test that creates a refusing resource, end with the `Forget` step.

For a `RefuseWhen` resource, a test puts an object in the refused state (the first portal configuration of an account, the only bank account of a currency, a card set to `canceled`, an alert set to `archived` if Q3 adds it) and proves the plan-time refusal of destroy and of a replacement, and the `removed` step. Its other tests are unchanged.

**Engine unit tests** cover the `compile` checks, the destroy refusal in `ModifyPlan` for `Refuse` and for `RefuseWhen` in and out of the state, the replacement refusal for a top-level, a nested and a list-element create-only field and for a `ReplaceIfSet` field, the `RefuseChange` text on a refusing resource, and the `Delete` backstop.

**Smoke.** The smoke run's packages contain no refusing resource, so its sweep is unchanged. A refusing resource joins the smoke run only with a sweep entry (`internal/smoke/sweep.go:53-59`) that ends its `tfsmoke-` objects through the API, by cancel, void or expire as the object allows. The sweep is test hygiene, not the provider, and may send a lifecycle action. An object nothing ends (a customer balance transaction) stays out of the smoke run.

### D5. Lifecycle attributes

The audit lists, for each refusing resource, whether an attribute already ends the object and whether the suite proves an action that could. Existing attributes stay: `expires_at` on file links, tax registrations and credit grants, `active` on entitlement features, `status` on Issuing disputes and cards. All are added (Q6); each is a `Transition` on the response-only `status` and needs the suite proof the audit cites:

| Resource | Attribute value | Action | Suite proof |
|-|-|-|-|
| quote | `status = "canceled"` | `POST /v1/quotes/{id}/cancel` | `quote_test.go`: "cancel" |
| setup_intent | `status = "canceled"` | `POST /v1/setup_intents/{id}/cancel` | `setup_intent_test.go`: "cancel" |
| subscription_schedule | `status = "canceled"`, `"released"` | `/cancel`, `/release` | `subscription_schedule_test.go`: "cancel", "release" |
| climate_order | `status = "canceled"` | `/cancel` | `climate_order_test.go`: "cancel" |
| identity_verification_session | `status = "canceled"` | `/cancel` | `identity_verification_session_test.go`: "cancel" |
| billing_feedback_option | `status = "inactive"` | `/deactivate` | `billing_feedback_option_test.go`: "deactivate" |
| billing_alert | `status = "archived"` | `/archive` | `billing_alert_test.go`: "archive" |

Not proposed: a credit grant's void (`voided_at` is a timestamp, which a transition cannot set; `expires_at` already ends a grant), a financial account's close (not proven by the suite), and actions on personalization designs (test helpers only). The issuing card's `status` already takes `canceled` on update (`issuing_card_test.go`: "cancel"); its page listed only `active` and `inactive` because the generator took the first enum it met, the create's (`tools/schemagen/build.go:313-314` at `6047d16`). The generator now adds to an argument's enum the values its update takes, which lists `canceled` for the card and `auto` for a customer's `tax.validate_location`, and changes no other table.

### D6. Changelog and docs

**`CHANGELOG.md`, Unreleased**, a new first bullet:

```markdown
- Breaking: destroy follows Stripe's object lifecycle. `terraform destroy` deletes an object where Stripe has a `DELETE` and archives it where Stripe has a reversible deactivation; otherwise it fails at plan time, as does a change that would replace the object, and the error shows how to remove the object from the state with a `removed` block or `terraform state rm`. Destroy no longer cancels, voids, expires or detaches anything. This changes `stripe_billing_credit_grant`, `stripe_billing_feedback_option`, `stripe_climate_order`, `stripe_customer_payment_method`, `stripe_entitlements_feature`, `stripe_file_link`, `stripe_identity_verification_session`, `stripe_quote`, `stripe_setup_intent`, `stripe_subscription_schedule` and `stripe_tax_registration`, whose destroy used to end the object, and `stripe_billing_portal_configuration`, whose destroy of the account's default configuration used to leave it in the account with a warning and now fails. Objects in state are not changed by the upgrade.
```

The unreleased bullets that describe the old behaviour are rewritten in the same change:

- customer balance transaction: "A customer balance transaction cannot be deleted, so destroying or replacing one fails at plan time."
- Issuing and Treasury: "Destroying a cardholder or a card deactivates it (`status = inactive`), because Stripe deletes neither; a card whose `status` is `canceled` cannot be destroyed. Stripe cannot delete a financial account, so destroying or replacing one fails at plan time, and so does a plan that changes its `features`."
- the rest of Issuing: "Stripe deletes neither a design nor a dispute, so destroying or replacing one fails at plan time; a design keeps its `lookup_key`."
- billing alerts: "Destroying an alert deactivates it, and setting `status` to `archived` archives it."
- Connect: "Destroying the default bank account of a currency fails at plan time."

**`CONTRIBUTING.md`**, "Adding a resource": the declaration's `Description` says what destroy does unless the resource refuses destroy, whose callout the engine writes from `RefuseReason`; and "A resource that refuses destroy ends each acceptance test with a step built with `acctest.Removed`."

**The architecture ADR.** Its D2 "Delete versus archive" table and bullets are replaced with this text, and the Status resolution "Archive and expire" gains "(amended 2026-10-22 by `adrs/1-pending/2026-10-22-destroy-semantics/ADR.md`: destroy deletes or archives, and is refused otherwise)":

````markdown
**Delete versus archive.** Destroy follows the object's lifecycle in Stripe (amended by `adrs/1-pending/2026-10-22-destroy-semantics/ADR.md`). The declaration names one strategy:

|Strategy|When|Destroy does|
|-|-|-|
|`Delete`|The spec has `DELETE` on the object, whatever Stripe calls it (a subscription's `DELETE` cancels it)|Calls `DELETE`. A 404 is success, and so is a 200 with `deleted: true` (`docs/stripe-api-behaviour.md:67`)|
|`Archive`|No `DELETE`; a reversible deactivation: an `active`-style field the update takes back, or a deactivate endpoint with a matching reactivate (price, shipping rate, meter, portal configuration)|Retrieves, sends the declared update or action unless the object is already archived, then removes it from state|
|`DeleteOrArchive`|`DELETE`, which Stripe refuses for objects it lets be archived (a product with a price)|Calls `DELETE`, archives on the declared refusal and warns|
|`Refuse`|Neither: what ends the object (cancel, void, expire, close, detach) is irreversible, or nothing does|Fails at plan time, and at apply as a backstop, with how to remove the object from state with a `removed` block or `terraform state rm`. A change that would replace the object fails at plan time too|
|`NoDelete`|The object cannot be created here (files)|There is no resource; only a data source exists|

- `RefuseWhen` names a state in which Stripe refuses to delete or archive an object of a deleting or archiving resource (the account's default portal configuration, a canceled card). An object in that state is refused as under `Refuse`.
- Destroy never creates an object (a reversal, a refund) and never sends a lifecycle action. An irreversible action is a change of state: where the suite proves it, it is an attribute the user sets (`stripe_issuing_dispute.status = "submitted"`).
- `active` (or `enabled`) remains an ordinary Optional+Computed attribute. Read never removes an object from state because it is inactive; only a 404 does. An object archived out of band appears as drift on `active`, and `terraform import` works on archived objects.
- Unique keys on archived objects (a promotion code's `code`) stay taken. A create that collides surfaces Stripe's error unchanged. The provider never adopts an existing object silently. Whether a replace of such a resource can succeed is proven per resource by its replace acceptance test.
````

## Resolved questions

Answered by the owner on 2026-10-22, each as recommended. The question and recommendation as asked:

1. **`stripe_customer_payment_method`.** The resource is the attachment, and detach (`POST /v1/payment_methods/{id}/detach`) is the only inverse of its create. By the rule it is a `POST` lifecycle action and irreversible ("detached cannot be reattached"), so destroy is refused. The alternative is to name detach a delete of the attachment, the one exception to "an HTTP `DELETE`". Recommendation: refuse, as the rule says; but if you read "reversing ... is actually a delete" as covering the inverse of an attach, keep detach as a named exception.
2. **Irreversible "archives": `stripe_entitlements_feature` and `stripe_billing_feedback_option`.** Stripe calls the first an archive (`active = false`), but an archived feature is frozen and cannot be reactivated; the second has a deactivate endpoint and no reactivate. Neither is a reversible deactivation, so both refuse. Recommendation: refuse both. A feature can already be archived with `active = false` before it is removed; a feedback option gains `status = "inactive"` if Q6 adds it.
3. **`stripe_billing_alert`.** Destroy calls `/archive`, which the spec calls "non-reversible". The alert also has `/deactivate` and `/activate`, a reversible pair, so by the rule destroy deactivates, and an alert in `archived` is refused. Recommendation: deactivate, and add `status = "archived"` (Q6). The alternative is to refuse the alert altogether.
4. **`stripe_issuing_card`.** Destroy sets `status = canceled`, which is final. `status = inactive` is reversible (`issuing_card_test.go`: "update status"), so by the rule destroy archives to `inactive`, and a canceled card is refused. Recommendation: archive to `inactive`, and document that `status` takes `canceled`. The alternative is to refuse the card, which leaves an active card usable after it is removed from state.
5. **`stripe_account_external_account`.** Stripe refuses to delete the default bank account of a currency. Today destroy drops it from state with a warning, which also lets `terraform destroy` of a connected account and its bank account succeed (deleting the account deletes the bank account). Refusing it (consistent with the default portal configuration) makes that destroy fail until the user removes the default bank account from state. Recommendation: refuse, with a reason that says to make another bank account the default or to remove this one from state before destroying the account.
6. **Lifecycle attributes.** Which of the transitions in D5 to add, all proven by the suite: quote, setup intent, Climate order and verification session `canceled`; schedule `canceled` and `released`; feedback option `inactive`; alert `archived`. Recommendation: add all of them in this change. Without them a schedule removed from state keeps billing, and quotes, setup intents and verification sessions stay open, with no way to end them from Terraform.
7. **OpenTofu version.** The brief says the `removed` block with `lifecycle { destroy = false }` works from OpenTofu 1.7. OpenTofu added `lifecycle` to `removed` in 1.10; 1.7 to 1.9 forget the object with a bare `removed` block and reject `lifecycle`. Recommendation: the error and pages say "Terraform 1.7 or later, OpenTofu 1.10 or later" and always offer `state rm`, rather than showing two forms of the block.
8. **Price and product reactivation.** The suite proves that both archive, not that `active = true` restores them. Recommendation: keep both as archives as you named them, and ask the suite to prove the way back.

## Implementation Roadmap

### Step 1: Engine refusals

Add `Refuse`, `RefuseReason`, `RefuseWhen`, `refusal`, the destroy check in `ModifyPlan`, `replaceUnlessRefused` in `planMods`, the `Delete` backstop, the `compile` checks, the `RefuseChange` text and the attribute description, and `DestroyNote`, with the unit tests of D4. Nothing declares them yet. See D2 and D3.

Acceptance criteria: `go test ./internal/engine/...` passes with tests for each case D4 lists; no resource's behaviour or page changes.

Status: DONE

Dependencies: (none)

### Step 2: Harness and pages

Add `acctest.Removed` with a unit test. Teach `tools/docgen` the callout and the "Removing from State" section, with a render test under `tools/docgen/testdata`.

Acceptance criteria: `go test ./internal/acctest/... ./tools/docgen/...` passes; `go generate ./...` changes no page, because no resource refuses yet.

Status: DONE

Dependencies: Step 1: `DestroyNote`.

### Step 3: Resources

Move the declarations to the new destroy as the audit says: the refusing resources to `Refuse` with a `RefuseReason`; billing_portal_configuration and account_external_account to `RefuseWhen`; billing_alert and issuing_card to their new archive with `RefuseWhen`. Trim the refusing resources' descriptions. Rewrite their acceptance tests as D4 says. Regenerate `docs/`.

Acceptance criteria: the acceptance suite passes against the pinned fake with Terraform and with OpenTofu, as CI runs it; every refusing resource's `_delete` and `_replace` tests prove the plan-time refusal, the taint backstop and, against the fake, that no request reached the object; the page of every refusing resource has the callout and the section; `scripts/check-docs.sh` passes.

Status: DONE

Dependencies: Steps 1 and 2.

### Step 4: Remove the old strategies

Delete what D2 "What is deleted" lists.

Acceptance criteria: `go build ./...`, `go vet ./...` and `go test ./...` pass; `grep -rn "Retain\|ExpireAction\|KeepWhen\|Ended" internal/engine internal/resources` finds nothing.

Status: DONE

Dependencies: Step 3: no declaration uses them.

### Step 5: Lifecycle attributes

Add the transitions of D5 (Q6), each with an acceptance test that sets the value and checks the object's status through the harness, and document the card's `canceled`. Regenerate `docs/`.

Acceptance criteria: each added attribute's test passes on both CLIs; the refusing resource's callout names the attribute that ends it.

Status: DONE

Dependencies: Step 3. Independent of Step 4.

### Step 6: Changelog, contributor docs and the architecture ADR

Apply D6: the changelog entry and rewritten bullets, the `CONTRIBUTING.md` changes, and the amended D2 and Status in the architecture ADR. The ADR stays in `adrs/1-pending/`, as the owner keeps the repository's accepted ADRs.

Acceptance criteria: the changelog names every resource whose destroy changed, matching the audit; the architecture ADR's D2 contains no `Retain`, `Expire` or `KeepWhen`.

Status: DONE

Dependencies: Steps 3 to 5.

## Architecture Documentation Updates

`architecture/` does not exist yet; the architecture ADR's Step 7 initialises it. Until then the governing text is the architecture ADR itself, whose D2 is amended with the exact text in D6. When `architecture/` is initialised, its description of resource mapping takes that amended D2.

## Alternatives Considered

### 1. Keep ending objects on destroy

Destroy keeps cancelling, voiding, expiring and detaching, and `Retain` keeps warning.

**Pros:**
- `terraform destroy` succeeds for every configuration, and an object removed from the configuration stops acting.

**Cons:**
- It is what the owner rejected: a lifecycle action mutates the object, it does not remove it, and the provider decides on the user's behalf which action ends it.
- The same verb means delete, archive, cancel, void or nothing, depending on the resource.

**Reconsider if:** none expected.

### 2. Drop refused objects from state with a warning

Every object Stripe cannot remove is treated as `Retain` is today.

**Pros:**
- `terraform destroy` never fails.

**Cons:**
- A warning scrolls past; the user believes the object is gone while it keeps acting (a schedule billing, a file link serving its file). The owner asked for an error.

**Reconsider if:** users ask for an explicit opt-in, which could be a provider argument that changes only this behaviour.

### 3. Refuse only at apply

`Delete` fails, and the plan says the object will be destroyed.

**Pros:**
- One place in the engine.

**Cons:**
- The plan promises a destroy the apply cannot do, and an apply that fails half way has already destroyed the other objects of the plan. Plan time is possible on both CLIs, as the probe shows.

**Reconsider if:** a supported CLI stops calling the provider on destroy plans.

### 4. A per-resource attribute choosing what destroy does

Such as `on_destroy = "cancel"` or `"abandon"`.

**Pros:**
- Users choose, and destroy can succeed.

**Cons:**
- It is not a Stripe field, against D7 of the architecture ADR, and it puts a lifecycle action back into destroy.

**Reconsider if:** users show that ending an object and removing it from state in one step matters more than the rule.

### 5. Refuse replacement in `ModifyPlan`

Extend `replaces` (`resource.go:230-242`) to every create-only field and refuse there.

**Pros:**
- One place, beside `refuseChanges`.

**Cons:**
- `replaces` compares top-level attributes only; nested fields and list elements would need a second walk that repeats what the framework's plan modifiers already do at each path. The modifier sees exactly the changes that would replace.

**Reconsider if:** the framework gives `ModifyPlan` the plan modifiers' `RequiresReplace` paths.
