# The provider's sandbox smoke run (L1)

## Executive Summary

The provider's wave 1 acceptance tests will run against the real CI Stripe sandbox on a schedule and before each release, to find behaviour the conformance suite never tested. The sandbox admits one user at a time and GitHub concurrency groups do not span repositories, so the run is a job in the suite's `e2e-stripe` workflow that executes a prebuilt, main-only test bundle the provider publishes to GHCR. This needs no change to the AWS role, no new AWS resource, no Actions secret and no owner-merged change; it needs one manual, one-time package-access grant by the owner once the image exists. The product owner has decided the open questions (see "Decisions").

## Status and ownership

Status: Accepted, 2026-10-17, by the product owner, who answered the seven open questions (recorded under "Decisions"). The ADR stays in `adrs/1-pending/` until implemented.

Evidence is current as of `terraform-provider-stripe` at `51fce7e` (main), `stripe-api-conformance` at `d9b694e` (main), `stripe-provider-infra` at `d3edba9` (main) and `fake-stripe-api` at `6d0050e` (main).

## Background Context

**Why the run exists.** The guarantees chain is: the suite is tested against Stripe, the fake against the suite, the provider against the fake. The weak link is behaviour the suite never tested, where the fake guesses and the provider inherits the guess. Running the provider's acceptance tests against real Stripe finds those gaps. Per user input (roadmap Phase 4, L1): "A provider acceptance run against the CI sandbox, on a schedule and before each release, not per change. It shares the suite's concurrency group, so it never overlaps a suite run in the same sandbox (one user per sandbox at a time, as the brief requires). Done when: the run exists and passes for wave 1." Failures feed back into the suite (L2), not into provider-only fixes.

**The sandbox and the key.** The project has two sandboxes, dev and CI, and will not take a third (per user input). The CI sandbox is account `acct_1RCSdUGf6IuPWnKh`. Its key is the Secrets Manager secret `StripeProvider/CiSandbox` in account 905418160304 (us-east-1), read at run time by the OIDC role `StripeProviderCiSandboxReader`. Nothing copies the key into a GitHub secret (per user input).

**The role's trust today** (`stripe-provider-infra` `ci-access.tf`, `d3edba9`): `sts:AssumeRoleWithWebIdentity` with `aud = sts.amazonaws.com` and `sub` equal to one of two immutable subjects, `repo:computomatic@115172828/fake-stripe-api@1199223229:ref:refs/heads/main` and `repo:computomatic@115172828/stripe-api-conformance@1412848540:ref:refs/heads/main`. The role's policy reads only that secret. `fake-stripe-api` is trusted but no workflow in it reads the secret today (`grep` for `secretsmanager` and the role name over its `.github/workflows/` at `6d0050e` finds nothing), so the suite is the only user.

**The provider's OIDC subject.** `gh api repos/computomatic/terraform-provider-stripe/actions/oidc/customization/sub` returns `{"use_default":true,"use_immutable_subject":false,"sub_claim_prefix":"repo:computomatic/terraform-provider-stripe"}`, and the repository ID is 963774367. The provider therefore emits name-based subjects (`repo:computomatic/terraform-provider-stripe:ref:refs/heads/main`), which the immutable-subject trust does not match. Trusting the provider directly (option 1) needs either `use_immutable_subject: true` on the repository, which is a repository setting in `computomatic/github-infra` (owner-merged), after which the subject is `repo:computomatic@115172828/terraform-provider-stripe@963774367:ref:refs/heads/main`; or a weaker name-based subject in the trust, which a renamed or re-created repository could take over. The recommended option does not touch this.

**Runners.** A job that assumes an AWS role must run on `ubuntu-latest`; the self-hosted runners forbid credentialed jobs (`computomatic/github-runners` README; revisiting it is github-runners #17). The provider's other jobs run on `aws-linux-2cpu` (`.github/workflows/ci.yml`, `51fce7e`).

**The suite's run** (`stripe-api-conformance` `.github/workflows/e2e-stripe.yml`, `d9b694e`). It runs on every push to `main` and on `workflow_dispatch` (input `test_filter`), under `concurrency: group: e2e-stripe, cancel-in-progress: false`, in one `e2e` job on `ubuntu-latest` with `timeout-minutes: 60` and `id-token: write`. The job assumes the role, reads the secret, masks the key, exports it, and runs `go test`. Its comment records the queue rule: "GitHub keeps only one pending run per group, so a burst of merges cancels the older pending runs and runs the latest pending one". A second job, `publish-results`, is in the same workflow.

**Clean state.** The suite's clean-state assertions (`TestCleanState`: empty product list, zero balance) call `requireCleanState`, which skips unless `TEST_CLEAN_STATE=1`; that flag "is never inferred from the base URL", and the README says "Real Stripe sandboxes carry history, so never set it against Stripe" (`clean_state_test.go:14-38`, `README.md:32`, `CLAUDE.md:28`, suite `d9b694e`). Junk in the CI sandbox therefore cannot trip a clean-state assertion; the suite already runs against an account with history. What junk can still break is uniqueness and quota: archived objects keep their unique keys (`docs/stripe-api-behaviour.md:19-22`: shipping rates, tax rates and promotion codes are archived, an archived feature still holds its `lookup_key`), and caps on live objects.

**What wave 1 leaves behind.** Wave 1 is product, price, coupon, customer, webhook_endpoint and billing_meter, plus the balance data source (`adrs/1-pending/2026-10-17-provider-architecture/ADR.md:32`). Destroy strategies: customer, coupon and webhook endpoint `engine.Delete`; product `engine.DeleteOrArchive` (archives when it has prices); price and billing meter `engine.Archive` (`internal/resources/{customer,coupon,webhook_endpoint,product,price,billing_meter}/resource.go`, `51fce7e`). So every price and every meter, and every product that had a price, stays in the sandbox for good. Tests named `TestAcc<Resource>_<case>` number about 6 to 13 per resource (`resource_test.go` in each directory).

**What the harness assumes today.** `internal/acctest` takes `STRIPE_FAKE_URL`, `STRIPE_FAKE_ADMIN_TOKEN` or `STRIPE_FAKE_SOURCE`; `NewAccount` creates a fresh account per test through the fake's admin API, and `Config.ProviderBlock()` renders the key and `base_url` into HCL (`internal/acctest/harness.go`, `51fce7e`). Tests use `resource.ParallelTest`. Some use fake-only aids: `acctest.RequestLog` (for example `TestAccProduct_destroyWithPrices` asserts the DELETE-then-archive sequence from the fake's request log) and `acctest.Seed` (data source tests for events, files and balance transactions). Some pick fixed IDs (`id = "prod_priced"` in `internal/resources/product/resource_test.go`), which are unique per fake account but not per real account. The provider already reads `STRIPE_API_KEY` when `api_key` is unset (`internal/provider/provider.go:231`).

**Cost.** Recurring cost needs the owner's approval. The run uses GitHub-hosted minutes on private repositories.

## Approach Overview

- The smoke run is a job, `provider-smoke`, in the suite's `e2e-stripe.yml`, in the same workflow run as the `e2e` job and therefore inside the `e2e-stripe` group. It never overlaps a suite run, a manual dispatch or itself, by the group's existing rule.
- It runs on a weekly schedule and on `workflow_dispatch` with a new input `provider_ref`. It does not run on pushes to `main`.
- The provider publishes a **smoke bundle** image to GHCR from its `main` branch: the compiled wave 1 acceptance test binaries and a pinned Terraform CLI. The suite job pulls the image with its own `GITHUB_TOKEN` and runs it with the key in its environment. No provider checkout, no deploy key, no PAT.
- The sandbox key flows exactly as for the suite: the suite repo's existing OIDC trust and reader role. `stripe-provider-infra` is unchanged.
- The provider's harness gains a **real-Stripe mode**: one shared account, run-unique names, fake-only tests skipped, serial by default, a sweep of leftovers before and after.
- "Before each release" is a step in the provider's release procedure: dispatch the suite workflow with the release commit's bundle tag and require a green run for that commit, re-dispatching if the run was displaced. Automating the gate is a later decision (it needs a cross-repo token).

## Approach Details

### The options

**Option 1: the provider's workflow runs the smoke tests and takes a lock both repos honour.**

- *Needs.* A lock store both can reach. AWS is the only shared store reachable by OIDC (a GitHub-side lock would need a token for the other repo's API; `GITHUB_TOKEN` is repository-scoped). `stripe-provider-infra` (owner-agent merge) would add an S3 lock object (S3 conditional writes) or a DynamoDB table, and extend the role's policy; the role's trust gains the provider's subject, which needs `use_immutable_subject: true` on the provider repo in `github-infra` (owner-merged, slow), or a name-based subject (weaker). The suite's workflow changes too, to acquire and release the lock, because the suite does not wait on anything today. Two repos, three owners of change.
- *Failure modes.* A stuck lock after a hard-killed job (a cancelled GitHub job gets SIGINT then SIGKILL; an `if: always()` release step may not run) needs a lease expiry and a steal rule, and the steal rule can put two users in the sandbox, which is the rule being protected. The lock is separate from the `e2e-stripe` group, so the suite's own queueing and the lock interleave: a suite run holds its group slot while it waits for the lock, or the other way round, and a manual dispatch can deadlock two runs each holding one half. Two mechanisms must be reasoned about together.
- *Release.* The provider's release workflow acquires the lock and runs. Works, once the above exists.
- *Cost.* Same minutes as the others, plus a few cents of AWS.

**Option 2: the suite's workflow runs the provider's smoke tests inside the `e2e-stripe` group.** Recommended, in the bundle form below.

- *Needs.* A new job in `stripe-api-conformance` (owner agent merges). From the provider, a source for the tests. A checkout of the private provider repo needs a read credential for it: a deploy key or fine-grained token stored as an Actions secret in the suite repo, which is a `github-infra` change (owner-merged) and a stored credential. The bundle form avoids it: the provider publishes an image to GHCR and the suite pulls it with `GITHUB_TOKEN`, as the provider's `acceptance` job already pulls the fake image (`ci.yml`, `packages: read`). That needs the package's Actions access to list the suite repo, a one-time package setting the owner makes by hand (Decision 3), the same setting the fake's package needed for the provider repo. No change to `stripe-provider-infra`; the suite's subject is already trusted.
- *Failure modes.* No lock, so none can stick. A cancelled run: the job is killed, in-flight Terraform state is lost and some objects are orphaned; the sweep at the start of the next run (inside the slot) removes them. A manual dispatch queues behind the running one. GitHub keeps one pending run per group, so a pending smoke run is replaced by a newer pending run of any kind, and a pending suite push run can be replaced by a smoke run; the newest `main` suite run is never lost because a later push replaces the smoke, not the reverse, but a smoke run may show as cancelled and must be re-dispatched (the release step checks for success, so it cannot pass by accident). The smoke job runs code from another repo while holding the key; see the trust rule below.
- *Release.* `gh workflow run e2e-stripe.yml -R computomatic/stripe-api-conformance -f provider_ref=<sha>` (the owner or owner agent uses their own credentials; nothing is stored) and waiting for the run.
- *Cost.* Minutes only.

**Option 3: a single scheduler dispatches both in sequence.** Rejected. The suite also runs on every push to `main`, outside any scheduler, so a scheduler cannot guarantee exclusivity unless it lives in the suite's group, which is option 2. A scheduler in another repo also needs a token that can dispatch workflows in both repos: a stored credential via `github-infra` (owner-merged). It adds a third workflow and solves nothing option 2 does not.

**Option 4 (found): a mutex inside the sandbox.** Both repos' test setup creates a Stripe object with a unique key (a Radar value list `alias`; a duplicate alias is a 400, `docs/stripe-api-behaviour.md:59`) and deletes it on exit. It needs no AWS or GitHub change, but it changes the suite's tests (it must acquire the mutex), puts coordination state in the account under test, and leaks a lock after a hard kill with no lease except a timestamp in the object. Not recommended; kept as the fallback if running provider code in the suite's job is refused.

### Recommended design (option 2, bundle form)

**Workflow change in `stripe-api-conformance` `.github/workflows/e2e-stripe.yml`.**

```yaml
on:
  push: { branches: [main] }
  schedule:
    - cron: "17 3 * * 0"          # Sunday 03:17 UTC, a quiet hour
  workflow_dispatch:
    inputs:
      test_filter: { ... unchanged ... }
      provider_ref:
        description: "Provider commit SHA whose smoke bundle to run. Set it to run the provider smoke instead of the suite."
        required: false
```

- The `e2e` job gets `if: github.event_name != 'schedule' && inputs.provider_ref == ''`: scheduled and provider-ref dispatches skip the suite (Decision 6: smoke only on the schedule).
- A new job `provider-smoke`, `if: github.event_name == 'schedule' || inputs.provider_ref != ''`, `runs-on: ubuntu-latest`, `timeout-minutes: 50`, `permissions: {contents: read, id-token: write, packages: read}`. Steps: configure AWS credentials and read the key exactly as `e2e` does (same pinned actions, same masking); log in to `ghcr.io` with `GITHUB_TOKEN`; resolve the tag (`sha-${provider_ref}` or `main` for a schedule); run the image with `-e STRIPE_API_KEY` taken from the environment (never on the command line) and `-e TF_SMOKE=1`; always upload the `go test -json` output as an artifact; write the same pass/fail table to the step summary.
- It sits in the `e2e-stripe` group because the group is workflow-wide. It does not have `publish-results` (the fidelity report joins suite runs, not smoke runs); `publish-results` gets `if` conditions that already exclude non-push events (`github.event_name == 'push'`), so it needs no change.
- Only images built from `main` are accepted: the provider publishes bundle tags only from a `push` to `main` or a tag, and the suite job refuses a `provider_ref` that is not an ancestor of the provider's `main` (checked with the GitHub compare API at job start using the job's own token; if the token cannot read the provider repo, the check is dropped and the publishing rule is the only control, which Decision 2 accepts).

**Workflow change in the provider: `.github/workflows/smoke-bundle.yml`.** On `push` to `main` and on `v*` tags: build `go test -c` binaries for the wave 1 packages (`internal/resources/{product,price,coupon,customer,webhook_endpoint,billing_meter,balance}`), a pinned Terraform (the `1.16.6` the CI jobs use), and a small `entrypoint.sh`; push `ghcr.io/computomatic/terraform-provider-stripe-smoke:sha-<sha>` and move `:main`. Runs on `aws-linux-2cpu` (no credentials besides `GITHUB_TOKEN` `packages: write`). The binaries need no repository files (Step 1), but the image must carry a `/tmp` directory and a CA bundle, run with a writable `/tmp` (`--tmpfs`), and have a shell for `entrypoint.sh`.

**Entrypoint.** `entrypoint.sh` runs, in order, serially per package: the sweep (below), `TF_ACC=1 ./<pkg>.test -test.run '^TestAcc' -test.parallel 1 -test.timeout 40m -test.v` for each package, and the sweep again with `if` semantics of "always". Exit status is the OR of the test runs. Terraform only; the OpenTofu pass adds time and the suite-gap finding it exists for does not depend on the CLI.

**Harness changes in `internal/acctest` (real-Stripe mode).**

- Mode switch: `TF_SMOKE=1` plus `STRIPE_API_KEY` (and optional `STRIPE_BASE_URL`, default `https://api.stripe.com`). The harness refuses to start unless the key starts with `sk_test_`. Smoke mode and fake mode are mutually exclusive: setting both is a fatal error.
- `NewAccount(t)` in smoke mode returns a `Config` with the shared account's base URL and an empty `Key`; `ProviderBlock()` then omits `api_key` so the provider reads `STRIPE_API_KEY` from the environment, and the key never appears in generated HCL, plugin-testing's working directories or logs. `AccountID` is the one account for every test. Nothing is created or deleted on the account itself.
- `Config.Name(base string) string` returns `tfsmoke-<run id>-<base>` in smoke mode (run id from `GITHUB_RUN_ID`, else a random 8 characters) and `base` against the fake, and `Config.ID(prefix, base)` does the same for caller-chosen IDs such as `prod_priced`. Every smoke-capable test passes its names and chosen IDs through them. Names carry the run id so concurrent tests, a re-run and leftovers never collide on unique keys (Stripe keeps IDs of archived objects).
- `acctest.RequireFake(t)` skips in smoke mode, with the reason. It guards `RequestLog`, `Seed` and `Config`-level fake-only assertions. The tests that cannot run against real Stripe: those that call `RequestLog` (`TestAccProduct_destroyWithPrices` and the file link and event destination tests), those that call `Seed` (the event, file and balance transaction data sources, whose objects Stripe creates on its own), `tfacc.SkipWhenFakeLacks` cases (the fake's gap markers do not exist on Stripe, so the helper becomes a pass-through there), and any test that depends on a fresh account (an empty list, a zero balance). The balance data source is kept only as a shape check, not a value check. A meta-test fails when a wave 1 test calls a fake-only function without `RequireFake`, so a new test cannot silently break the smoke run.
- Parallelism: `resource.ParallelTest` still calls `t.Parallel()`, and the entrypoint passes `-test.parallel 1`, so smoke tests run one at a time. Unique names make a higher value safe against collisions, but the sandbox has one rate limit and one set of object quotas, so raising it is a decision made from a measured run (Step 7), not a default.
- Tests that rely on the fake's behaviour where Stripe's is not suite-proven fail on the first smoke run. That is the point: each failure becomes a suite test (L2), then a fake fix, then, if needed, a provider fix.

**Selection and duration.** The selection is everything the harness can run in smoke mode in the wave 1 packages, `^TestAcc`; there is no second list to maintain. Duration is unmeasured. A rough bound: about 50 wave 1 tests, each three to five steps of terraform plan/apply/refresh, at one to three seconds per Stripe call and a second or two per Terraform process, is 15 to 30 minutes serial. The `go test` timeout is 40 minutes and the job limit 50, below the suite's 60, and Step 7 measures it and replaces both.

**Avoiding junk.**

- Every object is named by `Config.Name`, so everything the smoke run makes is recognisable: prefix `tfsmoke-`.
- Tests rely on `terraform destroy` (plugin-testing destroys at the end of every test, including failed ones where the process survives), so deletable objects (customers, coupons, webhook endpoints, products without prices) leave nothing.
- What stays by Stripe's rules: every price, every billing meter, every product that had a price. Per run that is on the order of tens of objects, none holding a unique key that a later suite run uses (the keys are in `tfsmoke-` space).
- The **sweep** (`TestSmokeSweep`, run by the entrypoint before and after the tests, always inside the group's slot, so it cannot touch the suite's objects): list customers, products, coupons, webhook endpoints and billing meters created in the last 14 days (the run is weekly, so a closing sweep that never ran is caught a week later, and a skipped week as well) (`created[gte]` where the list endpoint supports it, a full list for webhook endpoints, which are few) and delete or archive those whose name starts with `tfsmoke-`, leaving alone an inactive meter and the product-level archive of an inactive product (its prices are still listed and archived). It exists for two reasons: a cancelled or crashed run orphans objects, and Stripe caps live webhook endpoints per account, so leftover endpoints would fail later creates. The cap and the `created` filter per type are unrecorded; Step 2 checks them against the pinned spec and live probes in the suite.
- The suite's `TestCleanState` is unaffected (skips against Stripe). The suite's list-based tests already tolerate history, and `tfsmoke-` objects add to it. If the L2 triage finds a suite test that a leftover breaks, the fix is in the suite's test, not in the sweep.

**Key handling.** The key reaches the container through the process environment only; Docker is invoked with `-e STRIPE_API_KEY` (name only) so it is not on the command line. It is masked before export, as in the suite. Terraform logs stay off (`TF_LOG` unset). The harness never writes it to HCL or to test output; the smoke tests' `Check`s do not echo provider configuration.

**Trust rule.** The `provider-smoke` job runs provider code with the sandbox key. The bundle is built only from the provider's `main` or release tags, and a push to `main` is reviewed by the owner agent; the suite repo already runs only its own `main` code with the key. The extra step is that a provider change reaches the sandbox without a suite change. Accepted (Decision 2); the key is a test-mode key for a sandbox.

### Decisions

Decided by the product owner on 2026-10-17. They were open questions in the first draft.

1. **Cost: approved, capped at the weekly schedule plus manual dispatches.** There is no push trigger. Release runs and re-dispatches are manual dispatches. The run adds one `ubuntu-latest` job on private repositories: about 4 scheduled runs a month plus a handful of dispatches, so 8 to 10 runs at 25 to 40 minutes, roughly 4 to 7 hours a month. At GitHub's list price for 2-core Linux (about $0.006 to $0.008 a minute; the organisation's plan and included quota were not checked) that is about $1.50 to $3.50 a month before included minutes, probably nothing if the plan has headroom, and marginal next to the suite's push runs on the same runners. GHCR storage for one small image per `main` push is extra; Step 5 adds a retention rule. The duration figure is an estimate until Step 7 measures it.
2. **Provider code runs with the sandbox key: accepted.** Bundles are built only from the provider's `main` and release tags. The key is a test-mode key for a sandbox.
3. **GHCR access: a one-time manual step by the owner**, not a `github-infra` change. Once the image exists, the owner adds `stripe-api-conformance` to the package's Actions access (package settings, Manage Actions access), the same setting the fake's package needed for the provider repo. The owner raises it as an ask when the image exists (Step 6).
4. **Release gate: the documented procedure step for now.** Automating it is a later decision.
5. **Cadence: weekly, Sunday 03:17 UTC.** The release step must see a green run for the release's commit, and re-dispatches if its run was displaced or cancelled.
6. **Smoke only on the schedule.** The suite already runs on every push.
7. **Terraform only.** OpenTofu stays covered against the fake.

## Implementation Roadmap

The first three steps verify what the design assumed and can proceed in parallel. They settle the details Steps 4 to 7 build on. The duration of a run cannot be measured before a run exists, so Step 7 measures it; until then the timeouts (40 minutes for `go test`, 50 for the job) are provisional ceilings.

### Step 1: Verify the test binaries run without a checkout

Build the wave 1 test binaries with `go test -c`, copy them with a pinned Terraform into an otherwise empty container and run them against a running fake (`STRIPE_FAKE_URL`). Record any file, directory or environment they need at run time (testdata, working directory, `HOME`, plugin cache), and put it in the bundle design.

Acceptance criteria: in the empty container the wave 1 acceptance tests pass against the fake with no repository checkout present, and the list of runtime requirements is written down in the Step 5 notes; or the ADR is revised to ship those files in the image.

Status: DONE, 2026-10-18.

Findings. The seven wave 1 test binaries (`CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go test -c`, about 38 MB each) and Terraform 1.16.6 (`linux_amd64` from releases.hashicorp.com) were copied into a `FROM scratch` image and run against a fake started from `fake-stripe-api`'s source (`FAKE_STRIPE_ADMIN_TOKEN` set, listening on all interfaces, reached as `host.containers.internal`). With `-test.parallel 4` every wave 1 package passed; the only skips were the two `_replace` tests whose `SkipWhenFakeLacks` the fake still triggers (customer, webhook endpoint). The image held no repository file, no `HOME`, no plugin cache and no provider binary (the provider is served in process; `terraform init` needs no download). What the binaries need at run time:

- A writable `/tmp`. `scratch` has none, and plugin-testing fails with `failed to create temp dir: stat /tmp: no such file or directory`. A `/tmp` copied into the image arrived without world write, so a non-root user failed with `permission denied`; running as root worked, and so did `--tmpfs /tmp:rw,mode=1777` as the image's non-root user. The bundle ships a `/tmp` directory and the entrypoint's `docker run` passes `--tmpfs /tmp:rw,mode=1777`.
- `terraform` on `PATH` (plugin-testing looks it up; `TF_ACC_TERRAFORM_PATH` would also do).
- `TF_ACC=1`; without it every test skips cleanly. Without a fake (or smoke) setting, tests skip with the harness's message.
- `STRIPE_FAKE_URL` and `STRIPE_FAKE_ADMIN_TOKEN` against the fake. Against Stripe: `TF_SMOKE=1` and `STRIPE_API_KEY` (Step 3).
- Nothing else: no working directory, no testdata, no `HOME`, no repository files.
- Not exercised, because it needs real Stripe: TLS to `api.stripe.com`. Go on Linux reads root certificates from files such as `/etc/ssl/certs/ca-certificates.crt`, and `scratch` has none, so the bundle must ship a CA bundle there (Step 5 copies one from the build runner); the first smoke run proves it.

Bundle design revisions: ship a `/tmp` directory and a CA bundle in the image, and give the container a writable `/tmp` (`--tmpfs`). The image is otherwise the seven test binaries, `terraform` and `entrypoint.sh` (which needs a shell, so the base becomes a minimal image with `/bin/sh` rather than `scratch`, or the entrypoint becomes a small static program; Step 5 chooses).

Dependencies: (none)

### Step 2: Verify the sweep's list filters and the webhook endpoint cap

Check against the pinned spec (`spec/openapi.json`) which list endpoints for customers, products, coupons, webhook endpoints and billing meters accept `created[gte]`, and what pagination the rest need. Probe the cap on live webhook endpoints per account and the behaviour of the sweep's actions (delete, archive, deactivate) with live calls. The live probes are suite tests (L2) in `stripe-api-conformance`, run under the suite's rules (the dev sandbox, single-threaded from one machine) and recorded in its `docs/stripe-api-behaviour.md`; nothing is run locally against the CI sandbox.

Acceptance criteria: for each of the five types, `docs/stripe-api-behaviour.md` states whether `created[gte]` is accepted; the endpoint cap is recorded as a number or as "not enforced at N"; the sweep design in Step 4 is revised to match.

Status: PENDING

Dependencies: (none)

### Step 3: Real-Stripe mode in the harness

Implement the smoke mode in `internal/acctest` and `internal/acctest/tfacc` (mode switch, shared account, `Config.Name` and `Config.ID`, `RequireFake`, the `sk_test_` check, key-free `ProviderBlock`), convert the wave 1 tests to use `Config.Name`/`Config.ID` and guard fake-only tests, and add the meta-test for unguarded fake-only calls. Fake mode must behave exactly as before.

Acceptance criteria: `go test ./...` and the repository's CI pass unchanged against the fake; a harness unit test shows smoke mode refuses a non-`sk_test_` key, refuses fake and smoke together, and renders no key into HCL; the meta-test fails when a fake-only call lacks `RequireFake`.

Status: DONE, 2026-10-18. Built: `TF_SMOKE=1` mode with the `sk_test_` check and the fake exclusion in `internal/acctest` (checked in `Main`, so a bad setup exits non-zero before any test), the shared key-free smoke account, `Config.Name`, `Config.ID`, `Config.APIKey`, `RequireFake` and `Smoke()`, `SkipWhenFakeLacks` as a pass-through in smoke mode, `tfacc.Client` reading the key from the environment, unit tests in `smoke_test.go`, and the meta-test in `guard_test.go`. The wave 1 tests now route names and IDs through the new helpers; `TestAccProduct_destroyWithPrices` calls `RequireFake`, and `TestAccProduct_update` skips only its request-log check in smoke mode. Fake mode is unchanged. Where the code differed from the design: `RequestLog` and `Seed` also fail on the smoke account, a webhook endpoint (which has no name) gets a `tfsmoke-` description by default, and the meta-test accepts a read of `Config.Smoke` as a guard as well as `RequireFake`. `Config.Unique` and `Config.ID` also refuse, in smoke mode, a value a different test already asked for (archived objects keep their unique keys), and the run id carries `GITHUB_RUN_ATTEMPT` so a re-run does not reuse the first attempt's keys.

Dependencies: (none) beyond the findings of Step 1 on fixtures.

### Step 4: Sweep

Add `TestSmokeSweep` as designed in "Avoiding junk", adjusted to the Step 2 findings.

Acceptance criteria: against the fake, a unit test creates `tfsmoke-` and other objects and the sweep removes only the former; it is idempotent on a second run.

Status: DONE, 2026-10-18, except that the per-type list filters are still the spec's and stripe-go's word, not a live finding. Built: package `internal/smoke` (`sweep.go`), whose test binary the entrypoint runs with `-test.run '^TestSmokeSweep$'`. One table, `objectTypes`, holds each type's path, name field, `createdFilter` flag and action. It lists customers, products and coupons with `created[gte]` (14 days) and walks webhook endpoints and billing meters in full, reading every page before it changes anything. A name matches only with `strings.HasPrefix(name, "tfsmoke-")`: webhook endpoints are matched on `description`, coupons on `name`, meters on `display_name`. Customers, coupons and webhook endpoints are deleted; a product always has its prices listed and its active ones archived (an already archived product too, since destroy leaves an out-of-band price active), and each price must name the matched product or it is left alone and fails the sweep; the product is then archived unless already inactive, or deleted when it never had a price; meters are deactivated. Inactive meters are skipped, and an inactive product is left alone only after its prices are handled; a 404 counts as done, and any other error is collected with the object id and Stripe's message while the rest are tried. One line per object (type, id, name, action) goes to `t.Logf`. `TestSmokeSweep` takes its account from `acctest.NewAccount`, so it runs only against the smoke account or a fresh fake account. Tests: `TestSweepAgainstFake` (only the `tfsmoke-` objects of each type go, prices archived, lookalikes `x-tfsmoke-` and `TFSMOKE-` and other objects stay, a second run acts on nothing), and stub-server unit tests for the safety matrix, a mutation check on the prefix match (seven weaker matchers must each fail the safety test), 404 and error handling, the created filter per table entry, and the skips. The fake served every filter and action the sweep uses, so nothing is skipped with `SkipWhenFakeLacks`.

Still depends on Step 2: the `createdFilter` flags. If a live probe shows a type rejects `created[gte]` (or accepts it on meters and endpoints), flip the flag in `objectTypes`; nothing else changes. The webhook endpoint cap is not used by the sweep. Where the design did not fit: a price has no name, so the log line for a swept price shows its `nickname` (usually empty) and the id is what identifies it; and the harness `Smoke()` guard is enforced by `NewAccount` rather than inside the sweeper.

Dependencies: Step 2 (filters and cap), Step 3 (naming and mode).

### Step 5: Publish the smoke bundle

Add `.github/workflows/smoke-bundle.yml`, the image build (a `Dockerfile` or `scripts/smoke-bundle.sh`) and `entrypoint.sh` (sweep, tests per package serially, sweep again, exit status the OR of the runs). The workflow runs only on `push` to `main` and on `v*` tags, pins Terraform to the CI version, pushes `ghcr.io/computomatic/terraform-provider-stripe-smoke` with tags `main` and `sha-<sha>`, and removes old `sha-` tags by a retention rule.

Acceptance criteria: after a merge to `main` the image exists with both tags; `docker run` with `TF_SMOKE=1` and no key exits non-zero with the harness's "no key" message; a run with `TF_ACC` unset skips cleanly.

Status: PENDING

Dependencies: Steps 1, 3, 4.

### Step 6: Grant package access (manual, owner)

The owner adds `stripe-api-conformance` to the image package's Actions access, once. The owner agent raises the ask when Step 5 has published the image.

Acceptance criteria: a workflow in `stripe-api-conformance` can `docker pull` the image with its own `GITHUB_TOKEN`.

Status: PENDING

Dependencies: Step 5.

### Step 7: The suite job, first run and measurement

In `stripe-api-conformance`, add the schedule (`17 3 * * 0`), the `provider_ref` input, the `provider-smoke` job and the `e2e` job condition as described. Dispatch it with the wave 1 bundle, read the timings, replace the provisional timeouts, decide the parallelism from the observed rate-limit headroom, and triage failures into L2 suite tests.

Acceptance criteria: a dispatch made while a suite run is in progress queues behind it in the `e2e-stripe` group (observed once); a run on the CI sandbox passes for wave 1, and after the final sweep the sandbox holds no active, deletable `tfsmoke-` object. This is the roadmap's "Done when". The measured duration and the cost estimate are written back into Decision 1.

Status: PENDING

Dependencies: Steps 5 and 6.

### Step 8: Release procedure

Add the smoke step to `CONTRIBUTING.md`: dispatch `e2e-stripe.yml` in `computomatic/stripe-api-conformance` with `provider_ref` set to the release commit, wait for the run, require green for that commit, and re-dispatch if the run was displaced or cancelled. The user-facing README and `docs/` are untouched.

Acceptance criteria: CONTRIBUTING.md names the command, the pass condition and the re-dispatch rule.

Status: PENDING

Dependencies: Step 7.

## Architecture Documentation Updates

No `architecture/` directory exists in this repository, so there is nothing to update. Contributor material belongs in `CONTRIBUTING.md` (Step 6), per `CLAUDE.md`. If `/init-adrs` later creates `architecture/`, the testing section should describe the two harness modes and the smoke run.

## Alternatives Considered

### Option 1: provider workflow with a shared AWS lock

The provider's workflow runs the tests and serialises with the suite through an S3 or DynamoDB lock.

**Pros:**
- Each repo keeps its own workflow; the provider owns its run and can trigger it on its own release.
- No provider code runs in the suite's job.

**Cons:**
- Needs the suite to adopt the lock, a new AWS resource and role policy (`stripe-provider-infra`), and the provider's subject in the trust, which needs an owner-merged `github-infra` change to the repo's OIDC setting (or a weaker name-based subject).
- Two serialisation mechanisms (lock and `e2e-stripe` group) must be reasoned about together; a stuck or stolen lease breaks the one-user rule it exists to enforce.
- Recurring AWS cost, though small.

**Reconsider if:** the suite's workflow stops being the natural host (for example the sandbox is shared by more than two workflows), or running provider code in the suite's job is refused and option 4 is also refused.

### Option 2 with a provider checkout

Same as the recommendation, but the suite job checks out the provider repo with a read credential and runs `go test`.

**Pros:**
- No bundle image, no GHCR package grant, nothing to retain.

**Cons:**
- Needs a stored read credential (deploy key or token) in the suite's Actions secrets, an owner-merged `github-infra` change, and a long-lived secret.
- The suite job needs Go, Terraform and a build, adding minutes to every run.

**Reconsider if:** the package grant (Decision 3) cannot be arranged.

### Option 3: a single scheduler

One workflow in one repo dispatches the suite and the provider smoke in sequence.

**Pros:**
- One place to see the cadence.

**Cons:**
- Does not cover the suite's push-triggered runs, so exclusivity is not guaranteed unless the scheduler is inside the suite's group, which is option 2.
- Needs a stored cross-repo dispatch token (`github-infra`, owner-merged).

**Reconsider if:** the suite stops running on every push and runs only on a schedule.

### Option 4: a mutex object in the sandbox

Both test setups create and delete a uniquely keyed Stripe object as a lock.

**Pros:**
- No AWS or GitHub change; works from either repo.

**Cons:**
- The suite's tests must change to take it; coordination state lives in the account under test; no lease, so a hard kill leaves a stuck lock that needs a timestamp-based steal.
- The suite's own concurrency group still serialises suite runs separately.

**Reconsider if:** running provider code in the suite's job is refused.

### Smoke on every change, or against the dev sandbox

Run the smoke job in the provider's per-PR CI, or on the dev sandbox.

**Pros:**
- Earlier feedback.

**Cons:**
- Per-change runs would hold the CI sandbox constantly and break its one-user rule; the dev sandbox is reserved for single-threaded suite development from one machine. Credentialed jobs also cannot run on the provider's self-hosted runners.

**Reconsider if:** a third sandbox is ever approved.
