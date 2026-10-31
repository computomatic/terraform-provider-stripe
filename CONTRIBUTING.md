# Contributing

Thank you for your interest in the provider.

## Pull requests

We can't accept pull requests from outside the project at this time. A pull request opened from outside will be closed without review.

## Issues

The most useful contribution is an [issue](https://github.com/computomatic/terraform-provider-stripe/issues) with clear steps to reproduce your problem. Please include:

- the provider version;
- the Terraform or OpenTofu version;
- a minimal configuration that shows the problem, with no API keys or other secrets;
- the commands you ran;
- what you expected to happen and what happened instead, with the error output or the plan.

Feature requests are welcome as issues too, such as a missing resource, attribute or behaviour.

## For maintainers

The rest of this file is for the provider's maintainers.

The provider's design is in `adrs/`, starting with `adrs/1-pending/2026-10-17-provider-architecture/ADR.md`. It covers generation, resource mapping, configuration, the API version, the client, testing and naming.

### Build and test

```sh
go build ./...
go vet ./...
go test ./...
```

### Generated code

`spec/openapi.json` is the pinned Stripe OpenAPI spec (`spec/PIN.json` records its tag, commit and hash). `tools/schemagen` turns it into the field tables under `internal/generated` and into `internal/stripeapi/version.go`. After changing the spec, the pin, the generator or the list of resources in `tools/schemagen/resources.go`, regenerate and commit the result:

```sh
go generate ./internal/generated
```

`go test ./...` fails when a generated file is out of date. `tools/schemadiff old.json new.json` compares two `terraform providers schema -json` documents and marks breaking changes.

The generator also writes the coverage report, `internal/generated/coverage.json` and `coverage.md` (`adrs/1-pending/2026-10-29-full-attribute-coverage/ADR.md`, D1). It walks every table's create and update request and its object into leaves, keyed as the conformance suite's evidence record and the fake's `attributes.json` key them: object, side, `operationId` (request leaves), a dotted path in which an array or map of hashes adds no segment, and the union branch. A nested API object is one leaf, unless the table's request writes it as a hash; a list envelope is one leaf. Each leaf is `exposed` (the table holds it), `excluded` by a rule (X1 `expand` and `include`, X2 type markers no request writes, X3 lists returned only when expanded, X4 a recursion whose repeat has no `id`, X5 `confirm` where `status` confirms, X6 a leaf Stripe accepts and never returns) or by the register, or `awaiting_proof`. A leaf in a union branch is exposed only where the field at the union holds that branch. `coverage.md` gives the totals per table, then the excluded and awaiting leaves; the count awaiting proof is the work left. Generation fails on a leaf with no state, on a table field the walk does not reach, and on a Required request leaf, not exposed today, inside an object exposed today, because exposing it would break configurations.

Rule X6 leaves out a leaf of class `verified_accepted_only`, even one exposed today, with any hash it empties: a user could neither read such an argument back nor see it drift. `AcceptedOnly` in a table's entry keeps one, a dotted path to its reason, which says where its effect shows at another path or that the provider keeps it from configuration (`PreserveConfig`). The product owner approves each entry, and generation fails on an entry that keeps no such leaf, because none is listed or another rule or the register leaves it out. Generation also fails on a hash a table exposes without a member its request requires, which no configuration could send, until the member is exposed or the hash is in the register.

The register is `Exclude` in a table's entry in `tools/schemagen/resources.go`: a dotted path, whose leaves on both sides and in every branch it leaves out, to a reason that names its evidence (a suite test, a Stripe doc URL or a framework limit). The product owner approves each new entry in review. An entry may not remove a leaf exposed today. A table whose response repeats the parent ID that the engine adds as its declaration's `Parent` excludes that copy with the reason `the parent attribute holds it`, which a test checks against every declaration.

A table names, in `PagedLists`, the type that reads every element of each list its object requires, which Stripe returns a page at a time on every read (an invoice's `lines`, read by `stripe_invoice_line`); generation fails on a list with no entry.

The generator holds the shapes of the ADR's D3 (tested on `tools/schemagen/testdata/shapes_spec.json`):

- A union of several objects, in a response or a request, is one object with every variant's members, merged by name; a member two variants give different shapes fails generation, naming it, and needs a declaration override or a register entry.
- A parameter that takes a token or a hash keeps its string attribute, `<name>`, and gains `<name>_data` for the hash, sent under `<name>`; a configuration sets at most one, or exactly one where the create requires the parameter, and the resource reads Stripe's value into `<name>` only, which is then Computed: it keeps its state value while neither form changes, except inside a list element that can change in place, where it is unknown on a change. Generation fails when `<name>_data` is already a field. A declaration entry names the hash form, and anything below it, by `<name>_data`.
- A schema met again on its own path is held as the repeat's `id`, or left out where it has none.
- A request hash with no members is a nested attribute with none, configured as `{}`, which `/v2` sends. On `/v1`, where an empty value unsets a field, any object (not a map) whose configuration would encode to nothing, `{}` or every argument null, is refused at plan, a create judged by the arguments a create sends, and the encoder refuses one it would send empty, naming what the request sends, until the conformance suite proves what Stripe takes.
- A field the response gives another shape than the request takes the request's in the resource, which keeps the configured value, and the response's in the data source.
- A request value the response reports only through `<name>_provided` is Sensitive.
- An embedded copy of another object is read in full.
- A top-level `discounts` the resource takes in the request's shape, a list of hashes each with a `coupon`, `discount` or `promotion_code`, is expanded on every request the resource sends that returns the object, and each discount Stripe returns is mapped back by key, never by position: a `coupon` to a discount from that coupon with no promotion code, a `promotion_code` to the discount with it, a `discount` to the discount with that ID. An element no discount matches is dropped and a discount no element matches is added in the first form the element takes of `discount`, `promotion_code` and `coupon`, so both show as drift. The data source reads the IDs and never expands.

The hash form and the Sensitive mark are built only where the gate verifies a leaf, so that a table holds today what it held before; the other shapes arise only where a table could hold nothing before.

The evidence gate exposes a leaf once the pinned fake release's `attributes.json` lists it as `verified`, in spite of the table's `Keep`, `Omit`, `ReadOnly` and `NoResponse`; `verified_accepted_only` follows rule X6, and any other class leaves it awaiting proof. A `verified` leaf with the reason `refused_only`, which Stripe refused on every request the suite sent, is exposed too, and its field's `Refused` quotes the record's `refusals` with their account country and request, for the docs line. Generation fails on such a leaf with no refusal, on a placeholder in a refusal other than `<id:prefix>` and `<time>`, and on a class or a reason in `attributes.json` outside its closed set. The provider vendors three files with their pin, `spec/EVIDENCE_PIN.json` (the fake release, the suite's `suite_ref` and `run_id`, and the bytes and SHA-256 of each file):

- `spec/attributes.json`: the release asset of the fake release in `.fake-version`.
- `spec/evidence.json`: the suite's per-run evidence file `evidence/<suite_ref>/<run_id>.json` from its `results` branch, at the commit and run `attributes.json` names. The coverage report takes the proving tests and the default statements from it.
- `spec/create_probes.json`: a digest of the suite's `observations/` at that commit, which run to tens of megabytes: for each request leaf of a create, what Stripe held on a successful create that left it out, with the first probe that shows it. It holds only the values a default statement can be weighed against.

Each optional argument gets its default on the table's field (`Default`) and in the report (ADR D4). An argument only the update takes is `Default: not sent.`, with or without evidence. One a verified request leaf holds gets its default from the record, where that leaf is the create's when the create takes the argument: the value Stripe set on the minimal create (``Default (Stripe): `once`.``), `Default: none.` for null or absent, or the record's rule with the value observed where nothing else was set. Where the minimal create's variants give different values, there is no statement until the declaration words the rule in `Declaration.Defaults` (a dotted path to the rule's text, citing the suite test in a comment), which the engine accepts only for a rule or such a variant default. A leaf's `default.variants` holds literals alone, so the generator also reads `minimal_creates`: a variant that leaves the leaf out and that Stripe answers with anything else, such as a value it generates or copies, counts as a different value. The record's none says nothing of a hash sent in part or of a value another argument brings, so a declaration may word a default the record gives as none, citing the probe. Generation checks each statement against `spec/create_probes.json` and fails, naming the table, the argument and the probe, on a none or a literal a probe contradicts, on wording that leaves out a literal a probe holds, unless `ParentDefaults` names the field, and on wording a none no probe contradicts. A rule from the record is not checked: it is prose about another argument, such as `the currency of the session's line items`, so no value can be compared with it. A list's members are checked like any others, by their request path, though no line item member has a statement yet. The spec's "Defaults to" text is never a source: the report lists where it names another value than the evidence, for the suite to check, and, under "Defaults not yet recorded", names per table each verified optional argument with no statement ("verified, default not recorded") and counts the unverified ones. A statement a declaration words counts as recorded: the generator reads `Defaults` from the declaration's source under `internal/resources`, so each must be a map literal of string literals. The generator also marks on a table each embedded copy of an object one table reads (`Copy`) and each list Stripe returns a page at a time (`PagedBy`).

The generator also takes two facts per field from `spec/evidence.json` (ADR D2). A request field the gate exposes that the response returns, in its shape or another, is Optional and Computed (`ServerDefaulted`), so that removing it keeps Stripe's value, unless the record shows that Stripe leaves it null or absent on a minimal create, in every variant, and that an update unsets it as the engine sends the unset (`""` on `/v1`, null on `/v2`) to null or absent (E2); the fields exposed today keep today's rule until the ADR's Step 6. An optional argument an update takes is `Stable` when its response leaf is verified, the record proves a leaf of the table's update, and no `side_effects` or `filled_from` entry names its path; left unset, it keeps its state value in an update plan (E5). The reader decodes the record strictly: a member the format does not define, which the error locates in the record, or a value outside a closed set it reads, such as a hash's `update` in `families[].hashes`, fails generation. That covers a rule's observations, whose condition may name only the account's country and the variant, and whose matcher kind must be one the format defines; the plain create's literal is the rule's observed value.

`SendChangedMembers` follows the record's `families[].hashes`, not the family's summary `nested_hash_update`, which says nothing of a hash the record does not list: an entry, top-level or nested, is a hash listed as `siblings_kept` or a departure the product owner accepted (`acceptedDepartures`, `internal/resources/layout_test.go`). Below a `SendChangedMembers` object every hash is named there or in `SendWholeMembers`, which the engine checks when it compiles a declaration, so a hash the gate newly exposes fails the build until someone chooses, but for a hash with a Required member, sent whole, and a refused one. `SendWholeMembers` may not name a hash the record lists as kept, and a whole send names every member an update takes: null for one removed, the state's value for one the plan leaves unknown or `RefuseChange` refuses, and not an empty collection that stays empty. It may also name a top-level hash, where only the fake's suite-test declarations state that Stripe keeps left-out members, so that a removed member reaches Stripe as null, as on a payment link.

A hash Stripe cannot unset stays safe in place with `ReplaceOnRemoval`: an update changes it, and a plan that removes it, or a member named there, replaces the object instead of sending the unset (Stripe's `cannot be unset`, or a record with no unset). An empty collection holds nothing, so removing one changes nothing and sends no unset, and `[]` over a list that holds values counts as a removal. The engine refuses an entry that is Computed, whose removal keeps the value, and an object that cannot be replaced.

A verified update leaf says the suite proved something about the update, which is not always a change. Read its `refusals` and `enum_values`, and try the change on the fake, before moving a field out of `RefuseChange`: `You cannot change ... once it has been set`, or `fake_unproven` from the fake, means the suite only sent the value the object already held, so the field stays refused.

A `MatchData` list that is also `FixedKeys`, such as a payment link's `line_items`, takes data its update never sends, so changing the data replaces the object. On any `MatchData` list the plan fails on an element that sets both or neither of the key and the data. An element or either field not known at plan passes, because Terraform plans again at apply, when the configuration is known, and this then decides.

A table's `ParentDefaults` names a field whose recorded default is what the suite's parent object held, such as a refund's `amount`, rather than a value Stripe applies; it has no statement until the declaration words the rule. A table's `TokenForm` names the create parameter that takes a token in place of the hash of `CreateHash`, which leaves every member of the hash optional; the declaration's `CreateWrap` names it as `Token`, with the fields the hash needs in `Requires`. Validation refuses a create-only field beside the token, the plan of a create any field of the hash and a missing `Requires`, and the encoder both again for a token unknown at plan; an updatable field may stand beside the token once the object exists.

Moving to a new fake release replaces the three files and the pin in the same change as `.fake-version`. `tools/evidencepin` does all five with `gh`, so it needs read access to the fake's releases and to the suite's `results` branch and `observations/`; it refuses a release whose `attributes.json` names another release, an object the record creates with no observations, and otherwise what generation refuses of the first two files' headers (`tools/internal/evidencecheck`, which both tools call), so it never writes a pin that generation then rejects for them. `-verify` fetches them again and fails unless the vendored files and the pin match them:

```sh
go run ./tools/evidencepin -fake v0.26.0
go run ./tools/evidencepin -verify
go generate ./internal/generated
```

Generation refuses a file that differs from its pin, a pin with no digest of the probes or with one of another suite commit, either of the first two files when its `api_version` or `spec_sha256` differs from `spec/PIN.json`, an evidence record whose commit or run differs from the one `attributes.json` names or whose Stripe run did not pass, any of the three that names no suite commit or run, a key listed twice, a verified leaf the evidence record does not prove, a verified leaf the table cannot hold yet, and a verified leaf the walk does not have. Without a pin nothing is verified, and every leaf not exposed today awaits proof.

### Adding a resource

A resource is a short declaration in `internal/resources/<name>` that the generic engine in `internal/engine` turns into a resource and a data source. Each resource directory needs:

- its declaration, with a `Description` that says what `terraform destroy` does and anything else a user must know (a `Refuse` resource's `Description` leaves destroy out, because the engine writes its destroy callout from `RefuseReason`; a `RefuseWhen` resource's `Description` says what destroy does, and the engine's callout, from the `RefuseWhen` reason, says when destroy is refused), a `DataSourceDescription` that says only what the object is (a data source destroys and imports nothing), and an `ImportNote` (with `ImportNoteLevel` `engine.NoteWarning` for a consequence) when an imported object differs from a configured one. The engine adds to the note a sentence listing the arguments Stripe never returns, which an import cannot recover, except those the note names in backticks, and flags it as a warning when setting one plans a replacement;
- `examples/resources/stripe_<name>/resource.tf` and `import.sh`, whose last line is `terraform import stripe_<name>.<name> <id>` with a `<parent_id>/<id>` ID for a resource with a `Parent`, and `examples/data-sources/stripe_<name>/data-source.tf` when it has a data source;
- the six acceptance tests `TestAcc<Name>_{create,update,replace,import,delete,drift}`. Destroy deletes where Stripe has a `DELETE`, archives where Stripe has a reversible deactivation, and is refused otherwise (`engine.Refuse`). Every test of a resource whose destroy is refused ends with `acctest.Forget`, which removes the object from the state with a `removed` block, because the harness's closing destroy would fail; its `_delete` test proves the refusal at plan time and with `tfacc.Untouched` that nothing was sent, and its `_replace` test proves that a replacement is refused at plan time and a taint at apply. Such a test that may skip for a gap in the fake probes the fake before it creates anything, with `tfacc.SkipUnlessFakeServes`;
- an entry in `permissions` (`tools/docgen/permissions.go`) with the restricted-key permissions that Stripe's [API permissions reference](https://docs.stripe.com/keys/permissions-reference) gives the endpoints in its declaration, and in `Unverified` the endpoints the reference does not list. A new action endpoint, such as a `Transitions` path, needs its permission added too. The docs do not generate while a resource or data source has no entry;
- its regenerated pages under `docs/`.

A resource whose create, update or `status` change can move money sets `MovesMoney` in its declaration and joins the list in `TestMoneyMovingResources` (`internal/provider`). In a live-mode account its changes then fail unless the provider sets `allow_live_money_movement`; its acceptance tests need nothing more, because the fake's balance is in test mode.

A read-only object, such as the balance or the tax code catalogue, has a `datasource.go`, a data source example, a `permissions` entry and one `TestAcc<Name>_read` instead. `internal/resources` checks the declaration, the examples and the tests.

### Docs

The pages under `docs/` are generated by `tools/docgen` from the provider's schema, the examples under `examples/`, the provider page's prose in `docs-src/provider.md` and the guides in `docs-src/guides/`, one of which holds the generated restricted-key permissions table. Never edit them by hand. Regenerate them after any change to a schema, a description or an example, and commit the result with the change:

```sh
go generate ./...
```

Generation needs no Terraform or OpenTofu binary. `scripts/check-docs.sh` regenerates and fails when `docs/` changed. Run it before you push.

What a line says beyond its description comes from the engine (`AttributeDocs` of each resource and data source) and the provider (`provider.ArgumentDefaults`), per ADR `2026-10-29-full-attribute-coverage`:

- An argument whose field has `Refused` says so after its description.
- An optional argument's line ends with its default statement, and on a resource page, where the argument is also Computed, with the engine's sentence on removing it, from what the plan does with it: `Removing it from the configuration leaves the current value.` where the plan keeps the prior value (`UseStateForUnknown`, which E5 attaches to a `Stable` argument outside a list), `... lets Stripe set it when the list is next sent.` in a list element, whose prior value the plan cannot keep, and `... stops sending it, and the next apply records the value Stripe holds.` otherwise. Docgen fails, naming the page and the path, on a provider argument without a statement, and on an argument only an update takes without one. Evidence arrives object by object, so a verified argument whose default the pinned record does not give yet does not fail the docs; the coverage report names it. When the evidence of `v0.1.0` is pinned, `everyArgumentHasADefault` in `tools/docgen/split.go` turns true and every optional argument must have one, verified or not, but for a nested object with members, whose members state theirs.
- A list Stripe returns a page at a time says that it holds the first page and names the type that reads every element.
- An embedded copy's subsection is one line linking to the copied object's data source page, where that data source has every attribute the copy has (but `object`); otherwise it is written out in full.
- A resource or data source page over the budget of 250,000 bytes moves its largest top-level nested blocks, one at a time, to generated guide pages of their own (`docs/guides/<name>-<resource|data-source>-<attribute>.md`), linked from the block's line, until it fits. A hand-written guide may not take such a name.

### Acceptance tests

Tests that need a Stripe API run against a Stripe API fake, one fresh account per test, and are skipped when no fake is configured. The fake is not yet public. CI runs them on pushes to `main` and on pull requests from branches of this repository. Unit tests and the docs check run on every pull request. Set `STRIPE_FAKE_SOURCE` to the fake's source tree and the harness builds it from a temporary copy and starts it, or set `STRIPE_FAKE_URL` (and `STRIPE_FAKE_ADMIN_TOKEN` if it needs one) to use a running fake.

They use terraform-plugin-testing and need `TF_ACC=1`:

```sh
TF_ACC=1 STRIPE_FAKE_SOURCE=<fake source> go test ./internal/resources/...
```

The harness serves the provider in strict mode (`provider.NewStrict`). Users get a warning, and keep the planned value in state, where Stripe answers an apply with another value than planned (ADR `2026-10-29-full-attribute-coverage`, E4), and a warning, keeping the prior value, where Stripe returns a field in a shape the provider does not read (E6); in strict mode both fail the test, an unconfigured attribute's difference included, so a test fails where Stripe or the fake answers otherwise than the conformance suite recorded.

They run with the `terraform` on the path. To run them with OpenTofu, also set `TF_ACC_TERRAFORM_PATH=$(which tofu)`, `TF_ACC_PROVIDER_NAMESPACE=hashicorp` and `TF_ACC_PROVIDER_HOST=registry.opentofu.org`.

#### Real-Stripe mode

The same tests can run against a Stripe sandbox, which maintainers do for smoke runs. Set `TF_SMOKE=1` and `STRIPE_API_KEY` to an `sk_test_` key; do not set any `STRIPE_FAKE_*` variable, the harness refuses both together. Only the maintainers' sandbox run does this: never run it locally.

In this mode every test shares the one account, and the key stays in the environment, out of generated HCL. A test written for both modes:

- passes every display name through `acct.Name(base)`, every chosen ID through `acct.ID(prefix, base)` and every other value Stripe keeps unique (lookup keys, meter event names, Radar list aliases, promotion codes, domains, tax transaction references) through `acct.Unique(base)`. In smoke mode they gain a `tfsmoke-<run id>-` prefix and the latter two fail the test when a different test asks for the same value, because archived objects keep their keys, so give each test its own base. Against the fake they return the base unchanged;
- calls `acctest.RequireFake(t)` before `acctest.RequestLog` or `acctest.Seed`, or checks `acct.Smoke` before them, and assumes no fresh account (an empty list, a zero balance). `TestFakeOnlyCallsAreGuarded` fails a test file in any resource package, or a `tfacc` helper, that calls either without one of those before it in the same function;
- makes an object Stripe allows one of at a time, such as a live tax registration in a country, in a country or scope of its own, which no other test and no conformance suite test uses, and in smoke mode clears what an earlier run left there before and after the test. Where no such scope exists, it skips in smoke mode and says why. An object Stripe allows a few of, such as the three open financial accounts of a connected account, is ended after each test the same way. A cleanup that calls Stripe uses `context.Background()`, because the test's context is canceled before cleanups run.

Issuing and Treasury run on a connected account. Their tests get one from `tfacc.IssuingAccount`, which returns its configuration: `ConnectedProviderBlock` renders a second provider block, aliased `connected`, whose `stripe_account` is that account, and `tfacc.Client` on it sends the `Stripe-Account` header. Stripe verifies a new connected account for minutes, so in smoke mode the helper reuses one tagged `metadata[tfacc_role]=issuing` and never deletes it. The fake's request log does not hold the requests made through `Stripe-Account`, so these tests cannot assert on it. A personalization design's tests get their card logo from `tfacc.CardLogo`, which uploads one to the connected account (once, in smoke mode, as files cannot be deleted). Uploads go to the Files API host, which in smoke mode the provider block names as `files_base_url`; against the fake, `base_url` serves files too. `tfacc.ActiveCard` and `tfacc.CapturedTransaction` make the card activity that authorizations, transactions and disputes need.

Treasury money movement needs features of the financial account active and money in it. `tfacc.TreasuryFeatures` requests features and waits for them, `tfacc.FundedFinancialAccount` adds a received credit when the Issuing financial account holds too little, and `tfacc.DestinationFinancialAccount` returns a second financial account for outbound transfers, reused in smoke mode by its tag. Inbound transfers need a US bank account attached to the connected account itself, which `tfacc.SelfBankAccount` attaches with a setup intent (reused in smoke mode). Stripe refuses that setup intent to platforms it considers new, and the helper then skips the test with Stripe's message, before anything is created.

Transfers and application fees need a connected account that can receive money. `tfacc.TransfersAccount` returns one whose `transfers` capability is active, reused in smoke mode by its tag `metadata[tfacc_role]=transfers`. `tfacc.Transfer` funds the platform's available balance (`tfacc.FundAvailableBalance` charges the Stripe test card whose funds are available at once) and transfers to it, and in smoke mode reverses what remains of the transfer afterwards. `tfacc.DestinationCharge` makes a charge for the connected account with an application fee.

A payout needs a bank account of the platform's own in its currency, which Stripe adds only in the Dashboard. The payout tests seed a US bank account in `usd` into the fake account with `acctest.Seed`, after an `acct.Smoke` check, and fund the available balance with `tfacc.FundAvailableBalance`. Stripe draws a payout or transfer from the balance's card funds, so the helper counts those. In smoke mode it charges every test's full need on top of the shared balance, because tests that run at once would otherwise each find the same funds and spend them twice.

Stripe links a Financial Connections account only through its hosted flow. `tfacc.LinkedAccount` seeds one, held by a new customer, into the fake account, and skips the test in smoke mode. `tfacc.RefreshedOwnership` and `tfacc.SubscribedTransactions` start an ownership refresh or a subscription to transactions through the API and wait for the refresh to succeed, which the owner and transaction data sources need, because a data source never starts one.

Stripe makes some objects in the background, such as credit balance transactions, meter event summaries and active entitlements, and lists them a moment, or up to a minute, after their cause. A test waits for them with `tfacc.AwaitListed` before it reads one. The fake makes them at once. Stripe also changes some objects a while after it creates them, such as the `us_ssn` it adds to a `/v2` person's ID numbers a minute or two later, which the fake does after a few seconds; a test waits for such a change with `tfacc.AwaitRetrieved`.

#### When the smoke selection grows past wave 1

The smoke run executes the packages named in `scripts/smoke-bundle/build.sh`, today wave 1's. Every package already follows the rules above and `TestFakeOnlyCallsAreGuarded` checks them all, so a package added to that list needs no test change to follow them, though a test can still fail on the sandbox for a reason of the provider's own (see below). Its objects still need a way out of the sandbox: the sweep (`internal/smoke/sweep.go`) finds `tfsmoke-` objects of the types it lists, so a package whose objects it does not list joins with a sweep entry, and a resource whose destroy is refused joins only with one that ends its objects, as the destroy semantics ADR (D4) says.

#### When the sandbox and the fake disagree

The fake is checked against the project's conformance suite, and the suite against Stripe. So when a change passes against the fake but fails against the sandbox, the fix is never only in the provider:

- Stripe showed a behaviour the suite has no test for. The suite gets the test first, then the fake serves the behaviour, then the provider changes.
- The provider's test assumed something Stripe does not do. Fix the test, and make sure the suite pins what Stripe really does.

A sandbox problem, such as a rate limit or objects left by a cancelled run, is rerun, not coded around.

### CI

CI (`.github/workflows/ci.yml`) runs the checks as the `unit` job, the docs check (`scripts/check-docs.sh`) as the `docs` job, and the acceptance tests as the `acceptance` job, under Terraform and then OpenTofu, against the released fake image named by `.fake-version`. To move to a newer fake, re-pin with `tools/evidencepin` (see "Generated code"), which moves that line with the evidence, and regenerate. The `acceptance` job is skipped for pull requests from forks, because the fake image is private.

### Trying a build against the fake

Start the fake Stripe API with `-default-account-key sk_test_<anything>`, then point `base_url` at it:

```hcl
provider "stripe" {
  api_key  = "sk_test_<anything>"
  base_url = "http://127.0.0.1:12111"
}
```

To use a local build, `go install .` and add a `dev_overrides` entry for `registry.terraform.io/computomatic/stripe` in a CLI configuration file selected with `TF_CLI_CONFIG_FILE`.

### Clean room

Never copy source from `stripe/terraform-provider-stripe` or `lukasaron/terraform-provider-stripe`. Derive code from Stripe's public API docs and OpenAPI spec.

### Releasing

Releases are cut from `main`. Tag the commit `vX.Y.Z` and push the tag:

```sh
git tag vX.Y.Z
git push origin vX.Y.Z
```

The release workflow builds the provider for each supported platform, signs the checksums and publishes a GitHub release. The Terraform Registry picks the release up from there. Move the changelog's Unreleased entries under the new version before tagging.
