---
page_title: "Using Restricted Keys"
subcategory: ""
description: |-
  Run the provider with a Stripe restricted API key that has only the permissions its configuration needs, and find the permission each resource and data source needs.
---

# Using Restricted Keys

Stripe recommends restricted API keys (`rk_live_...` and `rk_test_...`) over secret keys. A secret key can do anything in the account, and Stripe advises against secret keys for new uses. A restricted key can do only what its permissions allow, so a leaked key does less harm. This guide explains how to set one up for the provider, and lists the permission each resource and data source needs.

## One key per workspace or pipeline

Create a separate restricted key for each workspace, pipeline or configuration that runs the provider, and give it only the permissions that configuration needs. Each permission in the Dashboard has the level **None**, **Read** or **Write**:

- A resource needs **Write**. Write includes Read, so the resource's refresh and import need nothing more.
- A data source needs **Read**.
- The resources that move money also need **Read** on Balance, for the live-mode check the Managing Test Data guide describes.

Build the key in a sandbox first. Grant the permissions the table below gives your configuration's resources and data sources, run a plan and an apply, then open the key's request logs from its menu on the [API keys](https://dashboard.stripe.com/apikeys) page to find any refused request. When a key lacks a permission, Stripe refuses the request with an error that says which permission to add, and the provider shows that error. Once the sandbox key works, create the live-mode key with the same permissions.

## Access policies on live-mode keys

Stripe recommends an [access policy](https://docs.stripe.com/keys#access-policies) on every live-mode key. Stripe blocks a request that the key's policy does not allow, and notifies you.

- An **IP addresses** policy allows only the listed IPv4 addresses or CIDR ranges. Use it when the provider runs from fixed addresses, such as self-hosted runners behind a NAT gateway.
- An **Advanced** policy allows requests by Autonomous System Number (ASN) and country, and can block common threat categories. Use it when the provider runs from a cloud provider's changing addresses, such as hosted CI runners.

## Expiry and rotation

[Rotate](https://docs.stripe.com/keys#rolling-keys) each key on a schedule, and at once when someone who could read it leaves or the key may have been exposed. When you rotate a key in the Dashboard, the old key can keep working for up to 7 days: put the new key in your secrets store, check that runs use it, and let the old one expire. Expire a key that no configuration uses any more.

Stripe may limit a key that has not been used to create a transfer or payout for 180 days, after which it cannot create either until you restore its access from the API keys page. A key whose configuration creates a `stripe_payout` or a `stripe_transfer` only now and then can run into this.

## Supplying the key

Keep the key in a secrets store, such as your CI system's secrets, HashiCorp Vault or a cloud provider's secrets manager, and give it to the provider in the `STRIPE_API_KEY` environment variable. Never write it in a configuration, a variable file or a pipeline definition. In GitHub Actions, for example:

```yaml
- run: terraform apply -auto-approve
  env:
    STRIPE_API_KEY: ${{ secrets.STRIPE_API_KEY }}
```

## Connected accounts

When the provider block sets `stripe_account`, every request is made on that connected account. The key then needs each permission at the level you allow it for connected accounts, which the Dashboard sets separately when you use Connect. The live-mode check reads the connected account's balance, so the money-moving resources need Balance read for connected accounts too.

Stripe marks some permissions (Direct) in the table: they cannot authorize a request made on a connected account, so a restricted key cannot use the types that need them with `stripe_account`. The connected-account resources, such as `stripe_account` and `stripe_account_person`, need the (Direct) `connected_account_read` or `connected_account_write`, so manage them from a provider block without `stripe_account`.

## Permissions by resource and data source

The names below are those of Stripe's [API permissions reference](https://docs.stripe.com/keys/permissions-reference), which gives the permission each API endpoint needs. A name ending in `_read` is the Read level of a permission and one ending in `_write` is its Write level. The Data source column is what the data source needs, and the Resource column is everything the resource needs.

Where Stripe's reference does not list an endpoint a type calls, the row says Unverified and names the endpoint. Test such a type in a sandbox and grant the permission Stripe's error names.

<!-- permissions-table -->
