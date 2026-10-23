---
page_title: "Managing Test Data"
subcategory: ""
description: |-
  Create payments, refunds, top-ups and transfers in a Stripe sandbox, and keep the provider from moving live money.
---

# Managing Test Data

Some Stripe objects move money: payment intents, refunds, checkout sessions, top-ups, transfers, transfer reversals, application fee refunds and Climate orders, which spend the account's balance. The provider manages them so you can seed a sandbox or a test-mode account with the payments your application's tests and demos read. This guide explains the safety switch that keeps them out of live mode, how to move them through their states, how they change on their own, and why `terraform destroy` does not remove them.

## The live-mode guard

Before a money-moving resource creates or changes an object, the provider asks Stripe whether the account is in live mode. It reads the account's balance (`GET /v1/balance`) once per provider configuration and checks its `livemode` flag. The key's prefix is never used, because some keys, such as organization keys, look the same in both modes.

- In a sandbox or test mode, the resources work as any other resource does.
- In live mode, creating or changing one of them, or setting its `status`, fails at plan time with `Live-mode money movement is not allowed`. The apply checks again before it sends anything.
- If Stripe cannot answer, the change fails too, with `Cannot tell whether the Stripe account is in live mode`. The provider never assumes test mode.

A restricted key needs the **Balance** read permission for the check, as well as the permissions of the resources it manages. Reading, refreshing and importing objects are never checked, and neither are data sources or other resources.

To move live money on purpose, set the switch in the provider block. No environment variable sets it, so it stays visible in the configuration:

```terraform
provider "stripe" {
  allow_live_money_movement = true
}
```

Each provider configuration checks its own account. A configuration with a sandbox provider and an aliased live one guards only the alias that does not set the switch.

## Creating test payments

A payment intent takes a `status` that confirms it. With Stripe's test card it succeeds in the same apply, and a refund can then refer to it:

```terraform
resource "stripe_customer" "test" {
  email = "test@example.com"
}

resource "stripe_payment_intent" "order" {
  amount         = 2000
  currency       = "usd"
  customer       = stripe_customer.test.id
  payment_method = "pm_card_visa"
  status         = "succeeded"

  automatic_payment_methods = {
    enabled         = true
    allow_redirects = "never"
  }
}

resource "stripe_refund" "partial" {
  payment_intent = stripe_payment_intent.order.id
  amount         = 500
}
```

Stripe turns a test payment method such as `pm_card_visa` into a new payment method, and the state keeps the value you configured. With `capture_method = "manual"`, setting `status` to `succeeded` confirms the intent and captures it in the same apply. A card that Stripe declines, or that needs a further step such as 3D Secure, leaves the intent where Stripe put it, and the apply fails with Stripe's error. Terraform then marks the intent tainted; because it cannot be destroyed, run `terraform untaint` once the configuration is fixed.

## Moving objects through their states

Stripe ends these objects by an action, not by deleting them. Where Stripe's API proves the action, it is a value of the resource's `status`:

| Resource | `status` values | What it does |
|-|-|-|
| `stripe_payment_intent` | `succeeded`, `canceled` | Confirms, and captures a manual intent; or cancels one that has not succeeded |
| `stripe_checkout_session` | `expired` | Expires an open session |
| `stripe_topup` | `canceled` | Cancels a top-up that is still pending |
| `stripe_climate_order` | `canceled` | Cancels an order that is not yet delivered, which refunds its subtotal but not its fees |

Leave `status` out to let the object stay where Stripe has it. A value Stripe cannot reach from the object's state fails at plan time: a succeeded payment intent cannot be canceled, and a completed session cannot be expired. Refunds, transfers, transfer reversals and application fee refunds have no `status` to set: a refund cannot be canceled once a card refund has succeeded, and a transfer is undone by a new object, a `stripe_transfer_reversal`.

## Objects that change on their own

Stripe moves some of these objects on without Terraform. A checkout session expires at its `expires_at`, 24 hours after creation unless set, and completes when a customer pays. A top-up moves from `pending` to `succeeded`. A refund of a non-card payment can move from `pending` to `succeeded` or `failed`. A transfer's `amount_reversed` grows with its reversals.

The provider reads those changes and plans nothing for them, as long as the configuration leaves `status` unset or sets it to the value the object stays at. The values a `status` takes are ones Stripe does not leave by itself, so a configuration that sets one keeps planning nothing once it is reached.

## Why destroy is refused

Stripe cannot delete a payment, a refund or a transfer, and the provider never cancels, refunds or reverses something on its own to make it go away. So `terraform destroy`, removing one of these resources from the configuration, and a change that would replace one, fail at plan time with `Cannot destroy`. Each resource page says which arguments cannot change.

To stop managing such an object, end it first if you want to (set `status` to `canceled` or `expired`, or create a `stripe_refund` or `stripe_transfer_reversal`), then remove it from the state. In Terraform 1.7 and later, and OpenTofu 1.10 and later, replace its resource block with a `removed` block and apply:

```terraform
removed {
  from = stripe_payment_intent.order

  lifecycle {
    destroy = false
  }
}
```

Or run `terraform state rm stripe_payment_intent.order` (`tofu state rm` in OpenTofu). The object stays in the Stripe account.

To clear out a sandbox's test data altogether, delete or reset the sandbox in the Stripe Dashboard rather than running `terraform destroy`.
