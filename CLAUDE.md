# terraform-provider-stripe

Placeholder spike from April 2025: resources `product`, `price`, `payment_link` and data sources `balance`, `product`, `price`, in Go with `terraform-plugin-framework`. It sits third in the build order, but the spike's code is not a base for the real provider. The real build restarts from scratch on the current plugin framework.

- The real provider build is gated on a go/no-go decision (G2), which needs a baseline scorecard of the official provider. Add no new resources until the owner agent says so.
- Clean room applies strictly here. Never copy the official or lukasaron providers' source, and do not read it while writing ours. Observe their schemas, behaviour and docs instead.
- Acceptance tests run against a Stripe sandbox only.

## Layout and checks

- `main.go`, `internal/provider/` (provider, one file per resource or data source, each with a `_test.go`), `docs/` (generated), `examples/`.
- Build: `go build ./...`. Vet: `go vet ./...`.
- Acceptance tests: `STRIPE_API_KEY=<sk_test_ key> ./test-provider.sh` (runs `go install .`, then `TF_ACC=1 go test ./internal/provider` with OpenTofu at `/usr/local/bin/tofu`).
- Docs: `./build-docs.sh` (tfplugindocs).
- The provider reads `STRIPE_API_KEY`; the project convention below names `TEST_API_BASE_URL` and `TEST_API_KEY`. The spike predates that and has not been migrated.

## Project conventions

This repo is one of eight in the Stripe Terraform provider project, owned by a durable agent. The aim is a world-class Terraform provider for Stripe, built in this order: a black-box Stripe API conformance suite (`stripe-api-conformance`), a fake Stripe API validated by it (`fake-stripe-api`), then the provider (`terraform-provider-stripe`), with a provider-agnostic scorecard (`terraform-provider-scorecard`) to prove it beats the official provider. Infrastructure lives only in `stripe-provider-infra`.

- Sessions started by the owner agent do not commit, push or open PRs. The owner agent reviews the diff and does that.
- Stripe is only ever called in a sandbox, with `sk_test_` keys, through `TEST_API_BASE_URL` and `TEST_API_KEY`. Never print, log or commit a key. Never run anything against the CI sandbox locally.
- Clean room: do not copy source from `stripe/terraform-provider-stripe` or `lukasaron/terraform-provider-stripe`. Observe their behaviour freely; derive our code from Stripe's public API docs and OpenAPI spec (`stripe/openapi`, `latest/spec3.json`).
- Do not add a license file or license headers unless the owner asks.
- Writing style: no em-dashes and no double hyphens used as dashes, in prose, comments, commit messages or PR text. A hook rejects them in new writes. Do not enumerate absences to prove a general claim.
- Commits and PR text carry no "Generated with Claude Code" or similar tags.
- When adding a dependency (package, GitHub Action, base image), check its latest stable version first (`gh api repos/{owner}/{repo}/releases/latest`) instead of relying on memory. Pin GitHub Actions to full commit SHAs with a comment naming the version.
- Keep the repo's roadmap, README and docs in step with the code in the same change.
