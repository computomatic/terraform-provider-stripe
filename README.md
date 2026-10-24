# Stripe provider for Terraform and OpenTofu

This is an independent provider. It is not affiliated with or endorsed by Stripe, Inc. It is written and maintained by AI coding agents, and owned by Toby Sullivan.

Manage the objects in your Stripe account as code: products, prices, coupons, customers, webhook endpoints, tax rates, billing meters and more. Each resource has a matching data source, and Stripe's read-only objects, such as the account balance and the tax code catalogue, are data sources.

Resources that move money, such as payment intents, refunds and transfers, are for managing test data in a sandbox: they refuse to change a live-mode account unless the provider sets `allow_live_money_movement = true`. See the guide in [`docs/guides/test-data.md`](docs/guides/test-data.md).

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

Use a Stripe restricted API key (`rk_test_...` or `rk_live_...`) with only the permissions your configuration needs; the Using Restricted Keys guide lists the permission of every resource and data source. A secret key (`sk_...`) also works, but Stripe advises against secret keys for new uses. Keep the key in a secrets store, export it as `STRIPE_API_KEY` and leave `api_key` out of the configuration:

```sh
export STRIPE_API_KEY=rk_test_...
```

A test-mode key manages your sandbox and a live-mode key your live account. Never commit a key: a key written into a configuration ends up in version control.

## Stripe API version

The provider sends every request with Stripe API version `2026-09-30.endive`, whatever your account's default version is. Attributes mean what that version documents, and a change to your account's default version does not change the provider's behaviour.

## Documentation

`docs/` has a page for the provider and for every resource and data source, with the arguments, the attributes, an example and how to import.

Working on the provider itself? See [CONTRIBUTING.md](CONTRIBUTING.md).

## Licence

Licensed under the Mozilla Public License 2.0 (`LICENSE`).
