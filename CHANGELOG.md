## Unreleased

## 0.1.0 (2026-10-22)

The first release of the Stripe provider for Terraform and OpenTofu, published as `computomatic/stripe`.

- Every request is sent with Stripe API version `2026-09-30.endive`.
- Authenticate with a Stripe secret or restricted key, set in the `STRIPE_API_KEY` environment variable or the `api_key` argument. Use `stripe_account` to manage a connected account with its platform's key.
- 37 resources:
  - Products and pricing: `stripe_product`, `stripe_price`, `stripe_plan`, `stripe_product_feature`, `stripe_entitlements_feature`, `stripe_coupon`, `stripe_promotion_code`, `stripe_shipping_rate`, `stripe_product_catalog_trial_offer`.
  - Customers and payment methods: `stripe_customer`, `stripe_customer_payment_method`, `stripe_setup_intent`, `stripe_payment_method_configuration`, `stripe_payment_method_domain`, `stripe_apple_pay_domain`.
  - Billing and subscriptions: `stripe_subscription`, `stripe_subscription_item`, `stripe_subscription_schedule`, `stripe_invoiceitem`, `stripe_quote`, `stripe_billing_meter`, `stripe_billing_credit_grant`, `stripe_billing_feedback_option`, `stripe_billing_portal_configuration`.
  - Payments and checkout: `stripe_payment_link`.
  - Tax: `stripe_tax_rate`, `stripe_tax_registration`.
  - Radar: `stripe_radar_value_list`, `stripe_radar_value_list_item`.
  - Terminal: `stripe_terminal_configuration`, `stripe_terminal_location`, `stripe_terminal_reader`.
  - Climate and Identity: `stripe_climate_order`, `stripe_identity_verification_session`.
  - Webhooks and files: `stripe_webhook_endpoint`, `stripe_core_event_destination`, `stripe_file_link`.
- 56 data sources, which read most of the objects above and others that Stripe does not let you create through the provider, such as `stripe_charge`, `stripe_payment_intent`, `stripe_refund`, `stripe_dispute`, `stripe_invoice`, `stripe_credit_note`, `stripe_checkout_session`, `stripe_balance`, `stripe_balance_transaction`, `stripe_event`, `stripe_file`, `stripe_mandate`, `stripe_country_spec` and `stripe_tax_code`. The `docs/data-sources` pages list them all.
