---
page_title: "Stripe Provider"
description: |-
  Manage the objects in a Stripe account with Terraform or OpenTofu.
---

# Stripe Provider

This is an independent, community-maintained provider. It is not affiliated with or endorsed by Stripe, Inc.

The Stripe provider manages the objects in a Stripe account: products, prices, customers, coupons, webhook endpoints, tax rates and more. Data sources read objects, and Stripe's read-only catalogues such as country specs and tax codes.

## Authentication

The provider authenticates with a Stripe secret key (`sk_live_...` or `sk_test_...`) or a restricted key (`rk_...`). A test-mode key manages your sandbox, and a live-mode key manages your live account. A restricted key needs the permissions of the resources you manage.

Set the key in the `STRIPE_API_KEY` environment variable. The `api_key` argument also takes it, but a key written in a configuration ends up in version control, so prefer the environment variable and never commit a key.

To manage a connected account with the key of its platform, set `stripe_account` to the connected account's ID.

## Live mode and money movement

The resources that move money (`stripe_payment_intent`, `stripe_refund`, `stripe_checkout_session`, `stripe_topup`, `stripe_transfer`, `stripe_transfer_reversal` and `stripe_application_fee_refund`, and `stripe_climate_order`, whose create spends the account's balance) are meant for test data in sandboxes and test mode. Before one creates or changes anything, the provider reads the account's balance and checks Stripe's `livemode` flag. In live mode the change fails at plan time unless the provider block sets `allow_live_money_movement = true`. The key's prefix is never used. A restricted key needs the Balance read permission for this check. Reads, imports and data sources are never checked. The Managing Test Data guide explains how to create, move on and stop managing these objects.

## Stripe API version

<!-- provider-description --> Attributes therefore mean what that version documents, and a change to your account's default API version does not change how the provider behaves. A later provider release may move to a newer API version and says so in its release notes.
