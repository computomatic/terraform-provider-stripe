# Stripe provider for Terraform and OpenTofu

This is an independent provider. It is not affiliated with or endorsed by Stripe, Inc. It is written and maintained by AI coding agents, and owned by Computomatic.

Manage the objects in your Stripe account as code: products, prices, coupons, customers, webhook endpoints, tax rates, billing meters and more. Each resource has a matching data source, and Stripe's read-only objects, such as the account balance and the tax code catalogue, are data sources.

## Installation

The provider is not published to a registry yet. Until it is, build it from source (Go 1.27 or later) and tell Terraform or OpenTofu to use the build with a `dev_overrides` entry in your CLI configuration.

```sh
git clone https://github.com/computomatic/terraform-provider-stripe
cd terraform-provider-stripe
GOBIN="$HOME/terraform-plugins" go install .
```

Terraform reads `~/.terraformrc` (or the file named by `TF_CLI_CONFIG_FILE`), and OpenTofu reads `~/.tofurc` (or the same variable). Each tool matches the provider by its full registry address, and the two addresses differ. For Terraform:

```hcl
provider_installation {
  dev_overrides {
    "registry.terraform.io/computomatic/stripe" = "/Users/you/terraform-plugins"
  }
  direct {}
}
```

For OpenTofu, use the same block in `~/.tofurc` with `registry.opentofu.org/computomatic/stripe` as the address.

With a `dev_overrides` entry, skip `terraform init` (or `tofu init`) for the provider; `plan` and `apply` use the build directly. The path is the directory that holds the `terraform-provider-stripe` binary. Both tools print a warning that development overrides are in effect, which is expected with a local build.

## Usage

```hcl
terraform {
  required_providers {
    stripe = {
      source = "computomatic/stripe"
    }
  }
}

provider "stripe" {}

resource "stripe_product" "gold" {
  name = "Gold plan"
}

resource "stripe_price" "monthly" {
  product     = stripe_product.gold.id
  currency    = "usd"
  unit_amount = 2000

  recurring = {
    interval = "month"
  }
}
```

## Authentication

The provider uses a Stripe secret key (`sk_test_...` or `sk_live_...`) or a restricted key (`rk_...`). Export it as `STRIPE_API_KEY` and leave `api_key` out of the configuration:

```sh
export STRIPE_API_KEY=sk_test_...
```

A test-mode key manages your sandbox and a live-mode key your live account. Never commit a key: a key written into a configuration ends up in version control.

## Stripe API version

The provider sends every request with Stripe API version `2026-09-30.endive`, whatever your account's default version is. Attributes mean what that version documents, and a change to your account's default version does not change the provider's behaviour.

## Documentation

`docs/` has a page for the provider and for every resource and data source, with the arguments, the attributes, an example and how to import.

Working on the provider itself? See [CONTRIBUTING.md](CONTRIBUTING.md).
