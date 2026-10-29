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

When the provider block sets `stripe_account`, or `stripe_context` to a connected account's ID, every request is made on that connected account. The key then needs each permission at the level you allow it for connected accounts, which the Dashboard sets separately when you use Connect. The live-mode check reads the connected account's balance, so the money-moving resources need Balance read for connected accounts too.

Stripe marks some permissions (Direct) in the table: they cannot authorize a request made on a connected account, so a restricted key cannot use the types that need them on a connected account. The connected-account resources, such as `stripe_account` and `stripe_account_person`, need the (Direct) `connected_account_read` or `connected_account_write`, so manage them from a provider block that acts as the platform: one without `stripe_account`, whose `stripe_context`, if set, names the platform.

## Permissions by resource and data source

The names below are those of Stripe's [API permissions reference](https://docs.stripe.com/keys/permissions-reference), which gives the permission each API endpoint needs. A name ending in `_read` is the Read level of a permission and one ending in `_write` is its Write level. The Data source column is what the data source needs, and the Resource column is everything the resource needs.

Where Stripe's reference does not list an endpoint a type calls, the row says Unverified and names the endpoint. Test such a type in a sandbox and grant the permission Stripe's error names.

| Type | Data source | Resource | Notes |
| --- | --- | --- | --- |
| `stripe_account` | `connected_account_read` (Direct) | `connected_account_write` (Direct) | Unverified: the reference does not list `POST /v1/accounts`. |
| `stripe_account_capability` | `connected_account_read` (Direct) | No resource |  |
| `stripe_account_external_account` | `connected_account_read` (Direct) | `connected_account_write` (Direct) |  |
| `stripe_account_person` | `connected_account_read` (Direct) | `connected_account_write` (Direct) |  |
| `stripe_account_person_v2` | `v2_account_read` | `v2_account_read`, `v2_account_storer_write` |  |
| `stripe_account_v2` | `v2_account_read` | `v2_account_read`, `v2_account_storer_write` |  |
| `stripe_apple_pay_domain` | Unverified | Unverified | Unverified: the reference does not list `POST /v1/apple_pay/domains`, `GET /v1/apple_pay/domains/{id}`, `DELETE /v1/apple_pay/domains/{id}` and `GET /v1/apple_pay/domains`. |
| `stripe_application_fee` | `application_fee_read` (Direct) | No resource |  |
| `stripe_application_fee_refund` | `application_fee_read` (Direct) | `application_fee_write` (Direct), `balance_read` | `balance_read` is for the live-mode check. |
| `stripe_apps_install` | `app_install_read` | No resource |  |
| `stripe_apps_secret` | `secret_read` | No resource |  |
| `stripe_balance` | `balance_read` | No resource |  |
| `stripe_balance_settings` | `v2_account_storer_read` | No resource |  |
| `stripe_balance_transaction` | `balance_read` | No resource |  |
| `stripe_billing_alert` | `billing_alert_read` | `billing_alert_write` |  |
| `stripe_billing_credit_balance_transaction` | `billing_credit_balance_read` | No resource |  |
| `stripe_billing_credit_grant` | `billing_credit_grant_read` | `billing_credit_grant_write` |  |
| `stripe_billing_feedback_option` | `subscription_read` | `subscription_write` |  |
| `stripe_billing_meter` | `billing_meter_read` | `billing_meter_write` |  |
| `stripe_billing_meter_event_summary` | `billing_meter_event_read` | No resource |  |
| `stripe_billing_portal_configuration` | `customer_portal_read` | `customer_portal_write` |  |
| `stripe_charge` | `charge_read` | `charge_write`, `balance_read` | `balance_read` is for the live-mode check. Unverified: the reference does not list `POST /v1/charges`. |
| `stripe_checkout_session` | `checkout_session_read` | `checkout_session_write`, `balance_read` | `balance_read` is for the live-mode check. |
| `stripe_checkout_session_line_item` | `checkout_session_read` | No resource |  |
| `stripe_climate_order` | `climate_order_read` | `climate_order_write`, `balance_read` | `balance_read` is for the live-mode check. |
| `stripe_climate_product` | `climate_order_read` | No resource |  |
| `stripe_climate_supplier` | `climate_order_read` | No resource |  |
| `stripe_commerce_product_catalog_import_v2` | `product_catalog_import_read` | No resource |  |
| `stripe_country_spec` | Unverified | No resource | Unverified: the reference does not list `GET /v1/country_specs/{id}`. |
| `stripe_coupon` | `coupon_read` | `coupon_write` |  |
| `stripe_credit_note` | `credit_note_read` | `credit_note_write`, `balance_read` | `balance_read` is for the live-mode check. |
| `stripe_credit_note_line` | `credit_note_read` | No resource |  |
| `stripe_customer` | `customer_read` | `customer_write` | Unverified: the reference does not list `POST /v1/customers/{id}`. |
| `stripe_customer_balance_transaction` | `customer_read` | `customer_write` |  |
| `stripe_customer_bank_account` | Unverified | No resource | Unverified: the reference does not list `GET /v1/customers/{customer}/bank_accounts/{id}` and `GET /v1/customers/{customer}/bank_accounts`. |
| `stripe_customer_card` | Unverified | No resource | Unverified: the reference does not list `GET /v1/customers/{customer}/cards/{id}` and `GET /v1/customers/{customer}/cards`. |
| `stripe_customer_cash_balance_transaction` | `customer_read` | No resource |  |
| `stripe_customer_payment_method` | `payment_method_read` | `payment_method_write` |  |
| `stripe_dispute` | `dispute_read` | No resource |  |
| `stripe_entitlements_active_entitlement` | `entitlement_read` | No resource |  |
| `stripe_entitlements_feature` | `feature_read` | `feature_write` |  |
| `stripe_event` | `event_read` | No resource |  |
| `stripe_event_destination_v2` | `webhook_read` | `webhook_write` |  |
| `stripe_event_v2` | `event_read` | No resource | Unverified: the reference does not list `GET /v2/core/events/{id}`. |
| `stripe_file` | `file_read` | `file_write` |  |
| `stripe_file_link` | `file_read` | `file_write` |  |
| `stripe_financial_connections_account` | `connections_api_accounts_list_read`, `connections_api_accounts_retrieve_read` | No resource |  |
| `stripe_financial_connections_account_owner` | `connections_api_accounts_ownership_read`, `connections_api_accounts_retrieve_read` | No resource | Also reads the account the owners belong to. |
| `stripe_financial_connections_session` | `connections_api_link_account_sessions_read` | No resource | Unverified: the reference does not list `GET /v1/financial_connections/sessions`. |
| `stripe_financial_connections_transaction` | `connections_api_transactions_read` | No resource |  |
| `stripe_identity_verification_report` | `identity_product_read` | No resource |  |
| `stripe_identity_verification_session` | `identity_product_read` | `identity_product_write` |  |
| `stripe_invoice` | `invoice_read` | `invoice_write`, `balance_read` | `balance_read` is for the live-mode check. |
| `stripe_invoice_line` | `invoice_read` | No resource |  |
| `stripe_invoice_payment` | `invoice_read` | No resource |  |
| `stripe_invoice_rendering_template` | `invoice_rendering_template_read` | No resource |  |
| `stripe_invoiceitem` | `invoice_read` | `invoice_write` |  |
| `stripe_issuing_authorization` | `issuing_authorization_read` | No resource |  |
| `stripe_issuing_card` | `issuing_card_read` | `issuing_card_write` | Unverified: the reference does not list `GET /v1/issuing/cards/{id}`. |
| `stripe_issuing_cardholder` | `issuing_cardholder_read` | `issuing_cardholder_write` |  |
| `stripe_issuing_dispute` | `issuing_dispute_read` | Unverified | Unverified: the reference does not list `POST /v1/issuing/disputes`, `POST /v1/issuing/disputes/{id}` and `POST /v1/issuing/disputes/{id}/submit`. |
| `stripe_issuing_personalization_design` | `issuing_card_read` | `issuing_card_write` |  |
| `stripe_issuing_physical_bundle` | `issuing_card_read` | No resource |  |
| `stripe_issuing_token` | `issuing_token_read` | No resource | Unverified: the reference does not list `GET /v1/issuing/tokens/{id}`. |
| `stripe_issuing_transaction` | `issuing_transaction_read` | No resource |  |
| `stripe_mandate` | `mandate_read` | No resource |  |
| `stripe_payment_intent` | `payment_intent_read` | `payment_intent_write`, `balance_read` | `balance_read` is for the live-mode check. Unverified: the reference does not list `POST /v1/payment_intents`, `GET /v1/payment_intents/{id}`, `POST /v1/payment_intents/{id}`, `POST /v1/payment_intents/{id}/confirm` and `POST /v1/payment_intents/{id}/cancel`. |
| `stripe_payment_intent_amount_details_line_item` | `payment_intent_read` | No resource |  |
| `stripe_payment_link` | `payment_links_read` | `payment_links_write` |  |
| `stripe_payment_link_line_item` | `payment_links_read` | No resource |  |
| `stripe_payment_method_configuration` | Unverified | `payment_method_configurations_write` | Unverified: the reference does not list `GET /v1/payment_method_configurations/{id}` and `GET /v1/payment_method_configurations`. |
| `stripe_payment_method_domain` | `payment_method_domain_read` | `payment_method_domain_write` |  |
| `stripe_payment_record` | `payment_records_read` | No resource |  |
| `stripe_payout` | `payout_read` | `payout_write`, `balance_read` | `balance_read` is for the live-mode check. |
| `stripe_plan` | `plan_read` | `plan_write` |  |
| `stripe_price` | `plan_read` | `plan_write` |  |
| `stripe_product` | `product_read` | `product_write` |  |
| `stripe_product_catalog_trial_offer` | Unverified | `subscription_write` | Unverified: the reference does not list `POST /v1/product_catalog/trial_offers`, `GET /v1/product_catalog/trial_offers/{id}` and `GET /v1/product_catalog/trial_offers`. |
| `stripe_product_feature` | `product_read` | `product_write` |  |
| `stripe_promotion_code` | `promotion_code_read` | `promotion_code_write` |  |
| `stripe_quote` | `quote_read` | `quote_write` |  |
| `stripe_quote_computed_upfront_line_item` | `quote_read` | No resource |  |
| `stripe_quote_line_item` | `quote_read` | No resource |  |
| `stripe_radar_early_fraud_warning` | `charge_read` | No resource |  |
| `stripe_radar_value_list` | `radar_value_list_read` | `radar_value_list_write` |  |
| `stripe_radar_value_list_item` | `radar_value_list_read` | `radar_value_list_write` |  |
| `stripe_refund` | `charge_read` | `charge_write`, `balance_read` | `balance_read` is for the live-mode check. Unverified: the reference does not list `POST /v1/refunds`. |
| `stripe_reporting_report_run` | `report_runs_and_report_types_read` | No resource |  |
| `stripe_reporting_report_type` | `report_runs_and_report_types_read` | No resource |  |
| `stripe_review` | `review_read` | No resource |  |
| `stripe_setup_intent` | `setup_intent_read` | `setup_intent_write` | Unverified: the reference does not list `POST /v1/setup_intents`, `GET /v1/setup_intents/{id}` and `POST /v1/setup_intents/{id}/cancel`. |
| `stripe_shipping_rate` | `shipping_rate_read` | `shipping_rate_write` |  |
| `stripe_subscription` | `subscription_read` | `subscription_write` |  |
| `stripe_subscription_item` | `subscription_read` | `subscription_write` |  |
| `stripe_subscription_schedule` | `subscription_read` | `subscription_write` |  |
| `stripe_tax_code` | `quote_read` | No resource |  |
| `stripe_tax_id` | `tax_id_read` | `tax_id_write` |  |
| `stripe_tax_location` | `tax_locations_read` | No resource |  |
| `stripe_tax_rate` | `tax_rate_read` | `tax_rate_write` |  |
| `stripe_tax_registration` | `tax_settings_read` | `tax_settings_write` |  |
| `stripe_tax_settings` | `tax_settings_read` | No resource |  |
| `stripe_tax_transaction` | `tax_calculations_and_transactions_read` | No resource |  |
| `stripe_tax_transaction_line_item` | `tax_calculations_and_transactions_read` | No resource |  |
| `stripe_terminal_configuration` | `terminal_configuration_read` | `terminal_configuration_write` |  |
| `stripe_terminal_location` | `terminal_location_read` | `terminal_location_write` |  |
| `stripe_terminal_reader` | `terminal_reader_read` | `terminal_reader_write` |  |
| `stripe_test_helpers_test_clock` | `billing_clock_read` | `billing_clock_write` |  |
| `stripe_topup` | `top_up_read` (Direct) | `top_up_write` (Direct), `balance_read` | `balance_read` is for the live-mode check. |
| `stripe_transfer` | Unverified | `transfer_write` (Direct), `balance_read` | `balance_read` is for the live-mode check. Unverified: the reference does not list `GET /v1/transfers/{id}`, `POST /v1/transfers/{id}` and `GET /v1/transfers`. |
| `stripe_transfer_reversal` | `transfer_read` (Direct) | `transfer_write` (Direct), `balance_read` | `balance_read` is for the live-mode check. Unverified: the reference does not list `POST /v1/transfers/{transfer}/reversals`. |
| `stripe_treasury_credit_reversal` | `treasury_credit_reversal_read` | `treasury_credit_reversal_write`, `balance_read` | `balance_read` is for the live-mode check. |
| `stripe_treasury_debit_reversal` | `treasury_debit_reversal_read` | `treasury_debit_reversal_write`, `balance_read` | `balance_read` is for the live-mode check. |
| `stripe_treasury_financial_account` | `financial_account_read_treasury` | `financial_account_read_treasury`, `treasury_financial_account_write` |  |
| `stripe_treasury_inbound_transfer` | `treasury_inbound_transfer_read` | `treasury_inbound_transfer_write`, `balance_read` | `balance_read` is for the live-mode check. |
| `stripe_treasury_outbound_payment` | `treasury_outbound_payment_read` | `treasury_outbound_payment_write`, `balance_read` | `balance_read` is for the live-mode check. |
| `stripe_treasury_outbound_transfer` | `treasury_outbound_transfer_read` | `treasury_outbound_transfer_write`, `balance_read` | `balance_read` is for the live-mode check. |
| `stripe_treasury_received_credit` | `treasury_received_credit_read` | No resource |  |
| `stripe_treasury_received_debit` | `treasury_received_debit_read` | No resource |  |
| `stripe_treasury_transaction` | `treasury_transaction_read` | No resource |  |
| `stripe_treasury_transaction_entry` | `treasury_transaction_read` | No resource |  |
| `stripe_webhook_endpoint` | `webhook_read` | `webhook_write` |  |
