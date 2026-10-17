# Stripe provider for Terraform

Design: `adrs/1-pending/2026-10-17-provider-architecture/ADR.md`. The provider sends Stripe API version `2026-09-30.endive`.

## Build and test

```sh
go build ./...
go test ./...
```

Tests that need a Stripe API run against the fake, one fresh account per test, and are skipped when no fake is configured. Set `STRIPE_FAKE_SOURCE` to the fake's source tree and the harness builds it from a temporary copy and starts it, or set `STRIPE_FAKE_URL` (and `STRIPE_FAKE_ADMIN_TOKEN` if it needs one) to use a running fake.

## Try it against the fake

Start the fake Stripe API with `-default-account-key sk_test_<anything>`, then point `base_url` at it:

```hcl
provider "stripe" {
  api_key  = "sk_test_<anything>"
  base_url = "http://127.0.0.1:12111"
}
```

To use a local build, `go install .` and add a `dev_overrides` entry for `github.com/computomatic/stripe` in a CLI configuration file selected with `TF_CLI_CONFIG_FILE`.
