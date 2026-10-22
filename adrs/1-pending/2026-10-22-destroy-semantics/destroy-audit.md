# Destroy audit

Supporting document for `ADR.md` in this directory. One row per resource the provider manages (48), read at commit `6047d16`. Data-source-only objects have no destroy and are left out.

Sources:

- **Current destroy**: the `Delete`, `Archive`, `ArchiveOn`, `KeepWhen`, `Expire` and `RetainWarning` fields of `internal/resources/<name>/resource.go` (`6047d16`).
- **Spec**: `spec/openapi.json` (`6047d16`), the methods on the object's retrieve path and the action endpoints directly under it. `DELETE` means the retrieve path serves `DELETE`.
- **Suite**: `stripe-api-conformance` at `f2141be`, cited as `<file>: "<subtest>"`, and its `docs/stripe-api-behaviour.md`. "Reversible" means the suite proves the way back, or the spec has the matching reactivate endpoint and the suite proves it.
- **Create-only**: fields with `Create` and not `Update` in `internal/generated/<name>/fields.go` (`6047d16`), plus the declaration's `ReplaceOn`, `Parent` and `Attach`. These are what force a replacement today.

New destroy, as `ADR.md` defines it:

- **Delete**: `DELETE` on the object. Unchanged.
- **Delete or archive**: `DELETE`, and the archive when Stripe refuses it. Unchanged.
- **Archive**: a reversible deactivation.
- **Refuse**: destroy and replacement fail at plan time with the guidance to remove the object from state.
- **Archive, refuse when ...** / **Delete, refuse when ...**: the strategy applies, except to an object in the named state, whose destroy and replacement are refused.

Rows marked **(Q*n*)** follow the numbered question in the ADR's "Resolved questions", each answered as recommended.

## Summary

| New destroy | Count | Resources |
|-|-|-|
| Delete | 18 | account, apple_pay_domain, core_event_destination, coupon, customer, invoiceitem, plan, product_feature, radar_value_list, radar_value_list_item, subscription, subscription_item, tax_id, terminal_configuration, terminal_location, terminal_reader, test_helpers_test_clock, webhook_endpoint |
| Delete, refuse when default | 1 | account_external_account (Q5) |
| Delete or archive | 1 | product |
| Archive | 10 | billing_meter, issuing_cardholder, payment_link, payment_method_configuration, payment_method_domain, price, product_catalog_trial_offer, promotion_code, shipping_rate, tax_rate |
| Archive, refuse when in an end state | 3 | billing_portal_configuration (default), billing_alert (archived, Q3), issuing_card (canceled, Q4) |
| Refuse | 15 | billing_credit_grant, billing_feedback_option (Q2), climate_order, customer_balance_transaction, customer_payment_method (Q1), entitlements_feature (Q2), file_link, identity_verification_session, issuing_dispute, issuing_personalization_design, quote, setup_intent, subscription_schedule, tax_registration, treasury_financial_account |

Destroy changes for 19 resources: the 15 that now refuse (among them every resource with an `Ended` state: quote, setup_intent, subscription_schedule and billing_credit_grant), the 2 whose archive changes (billing_alert, issuing_card), and the 2 whose protected object is now refused instead of dropped from state with a warning (billing_portal_configuration, account_external_account). The other 29 keep their destroy exactly.

## Resources whose destroy changes

| Resource | Current destroy | Spec offers | New destroy | Action destroy stops sending | Attribute for that action | Create-only fields that become refusals |
|-|-|-|-|-|-|-|
| billing_alert | Archive: `POST /archive` | `activate`, `deactivate`, `archive`; no `DELETE`. The spec says of archive "This is non-reversible" | Archive: `POST /deactivate` (`status = inactive`), refuse when `status = archived` (Q3) | `archive`, which is irreversible (`billing_alert_test.go`: "archived alert cannot be reactivated") | `status` takes `active` and `inactive` today. `archived` would need a transition to `/archive`, proven (`billing_alert_test.go`: "archive") | Only when archived: `alert_type`, `title`, `usage_threshold` |
| billing_credit_grant | Archive: `POST /void` (`voided_at`), `Ended` when `expires_at` has passed | `void`, `expire`; no `DELETE`. Both irreversible (`billing_credit_grant_test.go`: "void again", "update once voided", "expire again") | Refuse | `void` | `expires_at` is updatable today and ends the grant (`billing_credit_grant_test.go`: "expires_at"). Void would need a new attribute shape, because `voided_at` is a timestamp and transitions set strings | `amount`, `applicability_config`, `category`, `customer`, `customer_account`, `effective_at`, `name`, `priority` |
| billing_feedback_option | Archive: `POST /deactivate` (`status = inactive`) | `deactivate` only: no `reactivate`, and the update takes only `description` | Refuse (Q2) | `deactivate`, irreversible (`billing_feedback_option_test.go`: "deactivate twice") | None today. A transition `status = "inactive"` to `/deactivate` is proven (`billing_feedback_option_test.go`: "deactivate") | None |
| billing_portal_configuration | Archive (`active = false`), `KeepWhen is_default`: a refused archive of the default warns and drops it from state | `active` on update; no `DELETE` | Archive, refuse when `is_default = true` | None | Not applicable | None |
| climate_order | Archive: `POST /cancel`, `KeepWhen status = delivered` | `cancel`; no `DELETE`. Irreversible (`climate_order_test.go`: "cancel again") | Refuse | `cancel`, which refunds `amount_subtotal` | None today. A transition `status = "canceled"` to `/cancel` is proven (`climate_order_test.go`: "cancel") | `amount`, `currency`, `metric_tons`, `product` |
| customer_balance_transaction | Retain: warns, drops from state | No `DELETE` (`customer_balance_transaction_test.go`: "delete is not served") | Refuse | None | None: Stripe has no way to end a balance transaction | `amount`, `currency`, `customer` (parent) |
| customer_payment_method | Archive: `POST /v1/payment_methods/{id}/detach` | `GET` on the customer path; detach is a `POST` on the payment method. Irreversible (`customer_payment_method_test.go`: "detached cannot be reattached") | Refuse (Q1) | `detach` | Not applicable: the resource is the attachment | `customer` (parent), `payment_method` (attached object) |
| entitlements_feature | Archive (`active = false`) | `active` on update; no `DELETE`. Irreversible: "A feature cannot be reactivated" (`docs/stripe-api-behaviour.md`, "Archived entitlement features are frozen") | Refuse (Q2) | The archive (`active = false`) | `active` is updatable today (`entitlements_feature_test.go`: "update archives") | `lookup_key` |
| file_link | Archive: `expires_at = now` | `expires_at` on update; no `DELETE`. Irreversible once passed (`docs/stripe-api-behaviour.md`, "An expired file link cannot be modified") | Refuse | `expires_at = now` | `expires_at` is updatable today (`file_link_test.go`: "update expires now") | `file` |
| identity_verification_session | Archive: `POST /cancel`, `KeepWhen status = verified` | `cancel`, `redact`; no `DELETE`. Cancel is final | Refuse | `cancel` | None today. A transition `status = "canceled"` to `/cancel` is proven (`identity_verification_session_test.go`: "cancel") | `client_reference_id`, `related_customer`, `related_customer_account`, `return_url`; `ReplaceOn` `options`, `provided_details` |
| issuing_card | Archive: `status = canceled` | `status` on update, which takes `active`, `inactive` and `canceled`; no `DELETE` (`issuing_card_test.go`: "delete is not a route") | Archive: `status = inactive`, refuse when `status = canceled` (Q4) | `status = canceled`, irreversible (`issuing_card_test.go`: "canceled card cannot be reactivated") | `status` is updatable today, and Stripe takes `canceled` on it (`issuing_card_test.go`: "cancel"). The generated enum listed only `active` and `inactive`, the create enum; the generator now adds the update's `canceled` | Only when canceled: `cardholder`, `currency`, `exp_month`, `exp_year`, `financial_account`, `lifecycle_controls`, `replacement_for`, `replacement_reason`, `type` |
| issuing_dispute | Retain | `submit`; no `DELETE`, no withdraw | Refuse | None | `status = "submitted"` exists | `transaction` |
| issuing_personalization_design | Retain | No `DELETE` (`issuing_personalization_design_test.go`: "delete is not a route"); `activate`, `deactivate`, `reject` are test helpers only | Refuse | None | None: only test helpers end a design, and live mode lacks them | None. `physical_bundle`, `card_logo`, `carrier_text` are `RefuseChange` today and stay refused |
| quote | Archive: `POST /cancel`, `Ended status = accepted` | `finalize`, `accept`, `cancel`; no `DELETE`. Cancel is final (`quote_test.go`: "already canceled") | Refuse | `cancel` | None today. Transitions to `/cancel`, `/finalize` and `/accept` are proven (`quote_test.go`: "cancel", "finalize", "accept") | `from_quote`, `subscription_data.billing_mode`, `test_clock` |
| setup_intent | Archive: `POST /cancel`, `Ended status = succeeded` | `cancel`, `confirm`, `verify_microdeposits`; no `DELETE`. Cancel is final | Refuse | `cancel` | None today. A transition `status = "canceled"` to `/cancel` is proven (`setup_intent_test.go`: "cancel") | `automatic_payment_methods`, `on_behalf_of`, `usage`; `ReplaceOn` `allowed_payment_method_types`, `attach_to_self`, `customer_account`, `excluded_payment_method_types`, `flow_directions`, `payment_method_configuration` |
| subscription_schedule | Archive: `POST /cancel`, `Ended status = released, completed` | `cancel`, `release`; no `DELETE`. Both final (`subscription_schedule_test.go`: "already cancelled", "cancel after release") | Refuse | `cancel`, which also cancels the subscription the schedule started | None today. Transitions `status = "canceled"` to `/cancel` and `"released"` to `/release` are proven (`subscription_schedule_test.go`: "cancel", "release") | `billing_mode`, `customer`, `customer_account`, `phases.currency`, `start_date` |
| tax_registration | Expire: starts a scheduled registration, sets `expires_at` a few seconds ahead, waits for `expired` | `expires_at` on update; no `DELETE` | Refuse | `active_from = now` and `expires_at` | `expires_at` and `active_from` are updatable today (`tax_registration_test.go`: "update expires at a few seconds ahead expires the registration", "update active from on a scheduled registration") | `country`, `country_options` |
| treasury_financial_account | Retain | `close`; no `DELETE` | Refuse | None | None. A transition `status = "closed"` to `/close` would need the suite to prove close, which it does not | `supported_currencies`. `features` is `RefuseChange` today and stays refused |
| account_external_account | Delete, `KeepWhen default_for_currency`: a refused delete of the default warns and drops it from state | `DELETE` | Delete, refuse when `default_for_currency = true` (Q5) | None | `default_for_currency` is updatable today: making another bank account the default releases this one | Only when default: `account` (parent), `account_number`, `country`, `currency`, `routing_number` |

## Resources whose destroy is unchanged

| Resource | Destroy today and after | Spec offers | Reversible archive proof |
|-|-|-|-|
| account | Delete | `DELETE`; `reject`, `unreject` | Not applicable |
| apple_pay_domain | Delete | `DELETE` | Not applicable |
| billing_meter | Archive: `POST /deactivate` | `deactivate`, `reactivate` | `billing_meter_test.go`: "deactivate and reactivate" |
| core_event_destination | Delete | `DELETE`; `enable`, `disable`, `ping` | Not applicable |
| coupon | Delete | `DELETE` | Not applicable |
| customer | Delete | `DELETE` | Not applicable |
| invoiceitem | Delete (a refused delete of an item on a finalized invoice is Stripe's error, shown as it is) | `DELETE` | Not applicable |
| issuing_cardholder | Archive: `status = inactive` | `status` on update | `issuing_cardholder_test.go`: "update status" (inactive, then active) |
| payment_link | Archive: `active = false` | `active` on update | `payment_link_test.go`: "deactivate and reactivate" |
| payment_method_configuration | Archive: `active = false` | `active` on update | `payment_method_configuration_test.go`: "update active toggles" |
| payment_method_domain | Archive: `enabled = false` | `enabled` on update | `payment_method_domain_test.go`: "update enabled toggles" |
| plan | Delete | `DELETE` | Not applicable |
| price | Archive: `active = false` | `active` on update | Not proven: `price_test.go` proves "archive with active false" only. The owner names prices as an archive |
| product | Delete or archive: `DELETE`, archive on 400 `invalid_request_error` | `DELETE`; `active` on update | Not proven: `product_test.go` proves the archive only. The owner names products as an archive |
| product_catalog_trial_offer | Archive: `active = false` | `active` on update | `product_catalog_trial_offer_test.go`: "update active toggles" |
| product_feature | Delete | `DELETE` | Not applicable |
| promotion_code | Archive: `active = false` | `active` on update | `promotion_code_test.go`: "update active toggles" |
| radar_value_list | Delete | `DELETE` | Not applicable |
| radar_value_list_item | Delete | `DELETE` | Not applicable |
| shipping_rate | Archive: `active = false` | `active` on update | `shipping_rate_test.go`: "update active toggles" |
| subscription | Delete (Stripe's `DELETE` cancels at once) | `DELETE`; `pause`, `resume`, `migrate` | Not applicable |
| subscription_item | Delete (the last item's refusal is Stripe's error, shown as it is) | `DELETE` | Not applicable |
| tax_id | Delete | `DELETE` | Not applicable |
| tax_rate | Archive: `active = false` | `active` on update | `tax_rate_test.go`: "update active toggles" |
| terminal_configuration | Delete | `DELETE` | Not applicable |
| terminal_location | Delete | `DELETE` | Not applicable |
| terminal_reader | Delete | `DELETE` | Not applicable |
| test_helpers_test_clock | Delete (deletes the clock's customers, cancels their subscriptions) | `DELETE`; `advance` | Not applicable |
| webhook_endpoint | Delete | `DELETE` | Not applicable |

## What a refused object leaves running

A refusing resource leaves the object as it is in the account when the user removes it from state. Where the object keeps acting, the user ends it first through an attribute, or in the Dashboard:

| Resource | Keeps doing after removal | Way to end it from Terraform first |
|-|-|-|
| subscription_schedule | Bills through the subscription it started | None until a `status` transition exists |
| file_link | Serves the file at its public URL | `expires_at` |
| tax_registration | Stripe keeps calculating tax for the country | `expires_at` |
| billing_credit_grant | Its credits keep applying to invoices | `expires_at` |
| quote | An open quote can still be accepted | None until a `status` transition exists |
| setup_intent | Can still be confirmed | None until a `status` transition exists |
| identity_verification_session | Can still be completed by the user | None until a `status` transition exists |
| climate_order | Proceeds to delivery | None until a `status` transition exists |
| entitlements_feature | Keeps granting the entitlement through products that carry it | `active = false` |
| customer_payment_method | Stays attached and chargeable | None (Q1) |
| treasury_financial_account | Stays open | None |
| issuing_personalization_design | Stays in review or active, and keeps its `lookup_key` | None |
| issuing_dispute | Stays as submitted or unsubmitted | None |
| customer_balance_transaction | Stays on the customer's balance (as today) | None |
| billing_feedback_option | Stays active | None until a `status` transition exists |
