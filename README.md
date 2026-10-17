# Stripe provider for Terraform

Design: `adrs/1-pending/2026-10-17-provider-architecture/ADR.md`. The provider sends Stripe API version `2026-09-30.endive`.

## Build and test

```sh
go build ./...
go test ./...
```

## Generated code

`spec/openapi.json` is the pinned Stripe OpenAPI spec (`spec/PIN.json` records its tag, commit and hash). `tools/schemagen` turns it into the field tables under `internal/generated` and into `internal/stripeapi/version.go`. After changing the spec, the pin, the generator or the list of resources in `tools/schemagen/resources.go`, regenerate and commit the result:

```sh
go generate ./internal/generated
```

`go test ./...` fails when a generated file is out of date. `tools/schemadiff old.json new.json` compares two `terraform providers schema -json` documents and marks breaking changes.

A resource is a short declaration in `internal/resources/<name>` that the generic engine in `internal/engine` turns into a resource and a data source. Each resource directory needs its declaration, a docs example under `examples/` and the six acceptance tests `TestAcc<Name>_{create,update,replace,import,delete,drift}`; `internal/resources` checks that. A read-only singleton such as the balance has a `datasource.go`, a data source example and one `TestAcc<Name>_read` instead.

Tests that need a Stripe API run against the fake, one fresh account per test, and are skipped when no fake is configured. Set `STRIPE_FAKE_SOURCE` to the fake's source tree and the harness builds it from a temporary copy and starts it, or set `STRIPE_FAKE_URL` (and `STRIPE_FAKE_ADMIN_TOKEN` if it needs one) to use a running fake.

Acceptance tests use terraform-plugin-testing and need `TF_ACC=1`: `TF_ACC=1 STRIPE_FAKE_SOURCE=<fake source> go test ./internal/resources/...`. They run with the `terraform` on the path. To run them with OpenTofu, also set `TF_ACC_TERRAFORM_PATH=$(which tofu)`, `TF_ACC_PROVIDER_NAMESPACE=hashicorp` and `TF_ACC_PROVIDER_HOST=registry.opentofu.org`.

## Try it against the fake

Start the fake Stripe API with `-default-account-key sk_test_<anything>`, then point `base_url` at it:

```hcl
provider "stripe" {
  api_key  = "sk_test_<anything>"
  base_url = "http://127.0.0.1:12111"
}
```

To use a local build, `go install .` and add a `dev_overrides` entry for `github.com/computomatic/stripe` in a CLI configuration file selected with `TF_CLI_CONFIG_FILE`.
