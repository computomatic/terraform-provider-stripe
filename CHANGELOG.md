## Unreleased

- Connect: the `stripe_account` resource and data source for connected accounts, the `stripe_account_external_account` resource and data source for their bank accounts, and the `stripe_account_capability` data source.
- New resources, each with a data source: `stripe_billing_alert`, `stripe_tax_id` and `stripe_customer_balance_transaction`. A tax ID is created through `/v1/tax_ids` with an `owner` of `self` or a customer. A customer balance transaction cannot be deleted, so destroying it only removes it from state and warns that it stays on the customer's balance.
- New data source: `stripe_customer_cash_balance_transaction`.
- New resource with a data source: `stripe_test_helpers_test_clock`. Setting a later `frozen_time` advances the clock in place and waits until it is ready; setting an earlier one fails at plan time. Destroying a clock deletes the customers on it and cancels their subscriptions.
- New data sources: `stripe_payout`, `stripe_topup`, `stripe_radar_early_fraud_warning`, `stripe_review` and `stripe_invoice_payment`, and `stripe_invoice_line`, `stripe_credit_note_line`, `stripe_quote_line_item` and `stripe_quote_computed_upfront_line_item`. Stripe has no retrieve for a line, so the line data sources find a line by its `id` among the lines of its parent, which they take as `invoice`, `credit_note` or `quote`.
- Issuing and Treasury, which Stripe runs on a platform's connected accounts: manage them through a provider configuration whose `stripe_account` is the connected account. New resources, each with a data source: `stripe_issuing_cardholder`, `stripe_issuing_card` (virtual cards) and `stripe_treasury_financial_account`. Destroying a cardholder deactivates it and destroying a card cancels it, because Stripe deletes neither; a canceled card cannot be activated again. Destroying a financial account leaves it open, because the provider does not close financial accounts yet, and a plan that changes its `features` fails.
- New data sources: `stripe_treasury_transaction`, `stripe_treasury_transaction_entry`, `stripe_treasury_received_credit`, `stripe_treasury_received_debit`, `stripe_treasury_credit_reversal`, `stripe_treasury_debit_reversal`, `stripe_treasury_inbound_transfer`, `stripe_treasury_outbound_transfer` and `stripe_treasury_outbound_payment`, and `stripe_financial_connections_account` and `stripe_financial_connections_session`.

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
