---
page_title: "Stripe Provider"
description: |-
  Manage the objects in a Stripe account with Terraform or OpenTofu.
---

# Stripe Provider

This is an independent, community-maintained provider. It is not affiliated with or endorsed by Stripe, Inc.

The Stripe provider manages the objects in a Stripe account: products, prices, customers, coupons, webhook endpoints, tax rates and more. Data sources read objects, and Stripe's read-only catalogues such as country specs and tax codes.

## Authentication

Use a Stripe restricted API key (`rk_test_...` or `rk_live_...`). A test-mode key manages your sandbox, and a live-mode key manages your live account.

- Create one restricted key for each workspace or pipeline, with only the permissions its configuration needs: Write for each resource it manages and Read for each data source it reads. The Using Restricted Keys guide lists the permission of every resource and data source.
- Attach an access policy to every live-mode key, so that Stripe accepts the key only from your IP addresses, or from your cloud provider's ASN and your countries.
- Rotate each key on a schedule and when someone who could read it leaves, and expire a key nothing uses any more.

Keep the key in a secrets store and give it to the provider in the `STRIPE_API_KEY` environment variable. The `api_key` argument also takes it, but a key written in a configuration ends up in version control, so prefer the environment variable and never commit a key.

A secret key (`sk_test_...` or `sk_live_...`) also works, but it can do anything in the account, and Stripe advises against secret keys for new uses.

To manage a connected account with the key of its platform, set `stripe_account` to the connected account's ID.

Organization keys (`sk_org_...`) need `stripe_context`, or the `STRIPE_CONTEXT` environment variable, set to the account the provider acts on. The provider sends the value unchanged as the `Stripe-Context` header of every request, the balance read of the live-mode check included. `stripe_context` and `stripe_account` both name that account, so a configuration that sets both fails. This project does not test organization keys: its tests set `stripe_context` with a platform's key, to one of its connected accounts or to the platform itself. `/v2` requests with `stripe_context` are not tested.

## Live mode and money movement

The resources that move money (`stripe_payment_intent`, `stripe_charge`, `stripe_refund`, `stripe_invoice`, `stripe_credit_note`, `stripe_checkout_session`, `stripe_payout`, `stripe_topup`, `stripe_transfer`, `stripe_transfer_reversal`, `stripe_application_fee_refund`, the Treasury `stripe_treasury_inbound_transfer`, `stripe_treasury_outbound_transfer`, `stripe_treasury_outbound_payment`, `stripe_treasury_credit_reversal` and `stripe_treasury_debit_reversal`, and `stripe_climate_order`, whose create spends the account's balance) are meant for test data in sandboxes and test mode. Before one creates or changes anything, the provider reads the account's balance and checks Stripe's `livemode` flag. In live mode the change fails at plan time unless the provider block sets `allow_live_money_movement = true`. The key's prefix is never used. A restricted key needs the Balance read permission for this check. Reads, imports and data sources are never checked. The Managing Test Data guide explains how to create, move on and stop managing these objects.

## Stripe API version

<!-- provider-description --> Attributes therefore mean what that version documents, and a change to your account's default API version does not change how the provider behaves. A later provider release may move to a newer API version and says so in its release notes.

## Defaults

An optional argument's line ends with its default once that default is verified against Stripe, and every provider argument's line, and every line of an argument only an update takes, ends with its own: `Default (Stripe):` is the value Stripe applies when the configuration leaves the argument out, or the rule by which Stripe picks one; `Default (provider):` is the value the provider applies; `Default: none.` means the argument stays unset; and `Default: not sent.` marks an argument only an update takes. An argument that Stripe fills in when the configuration leaves it out says what deleting it from the configuration does: it leaves the current value, it lets Stripe set the value when the list that holds it is next sent, or the provider stops sending it and records the value Stripe holds at the next apply. To change such a value, set the value you want instead.
