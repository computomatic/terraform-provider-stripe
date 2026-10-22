package main

// Resources lists the objects the generator writes tables for. A resource joins the list
// when its declaration is written.
var Resources = []Resource{
	{Package: "balance", ReadPath: "/v1/balance"},
	{Package: "billing_meter", CreatePath: "/v1/billing/meters", UpdatePath: "/v1/billing/meters/{id}"},
	{Package: "billing_portal_configuration", CreatePath: "/v1/billing_portal/configurations", UpdatePath: "/v1/billing_portal/configurations/{configuration}"},
	{Package: "country_spec", ReadPath: "/v1/country_specs/{country}", Omit: []string{"supported_bank_account_currencies"}},
	// The Omit entries: `include` is a request parameter, not an object field, and the signing
	// secret waits for the conformance suite and the fake to prove it (ADR 2026-10-17-v2-support).
	{Package: "core_event_destination", CreatePath: "/v2/core/event_destinations", UpdatePath: "/v2/core/event_destinations/{id}", Omit: []string{"include", "webhook_endpoint.signing_secret"}},
	{Package: "coupon", CreatePath: "/v1/coupons", UpdatePath: "/v1/coupons/{coupon}"},
	{Package: "customer", CreatePath: "/v1/customers", UpdatePath: "/v1/customers/{customer}", Omit: []string{"sources", "subscriptions"}},
	{Package: "entitlements_feature", CreatePath: "/v1/entitlements/features", UpdatePath: "/v1/entitlements/features/{id}"},
	{Package: "plan", CreatePath: "/v1/plans", UpdatePath: "/v1/plans/{plan}"},
	{Package: "price", CreatePath: "/v1/prices", UpdatePath: "/v1/prices/{price}"},
	{Package: "product", CreatePath: "/v1/products", UpdatePath: "/v1/products/{id}"},
	{Package: "product_catalog_trial_offer", CreatePath: "/v1/product_catalog/trial_offers", UpdatePath: "/v1/product_catalog/trial_offers/{id}"},
	{Package: "product_feature", CreatePath: "/v1/products/{product}/features"},
	{Package: "promotion_code", CreatePath: "/v1/promotion_codes", UpdatePath: "/v1/promotion_codes/{promotion_code}"},
	{Package: "shipping_rate", CreatePath: "/v1/shipping_rates", UpdatePath: "/v1/shipping_rates/{shipping_rate_token}"},
	{Package: "tax_code", ReadPath: "/v1/tax_codes/{id}"},
	{Package: "tax_rate", CreatePath: "/v1/tax_rates", UpdatePath: "/v1/tax_rates/{tax_rate}"},
	{Package: "webhook_endpoint", CreatePath: "/v1/webhook_endpoints", UpdatePath: "/v1/webhook_endpoints/{webhook_endpoint}"},
	{Package: "apple_pay_domain", CreatePath: "/v1/apple_pay/domains"},
	{Package: "balance_settings", ReadPath: "/v1/balance_settings", Omit: []string{"payments.payouts.automatic_transfer_rules_by_currency"}},
	{Package: "balance_transaction", ReadPath: "/v1/balance_transactions/{id}"},
	{Package: "billing_feedback_option", CreatePath: "/v1/billing/feedback_options", UpdatePath: "/v1/billing/feedback_options/{id}"},
	{Package: "event", ReadPath: "/v1/events/{id}", Omit: []string{"data"}},
	{Package: "file", ReadPath: "/v1/files/{file}"},
	{Package: "file_link", CreatePath: "/v1/file_links", UpdatePath: "/v1/file_links/{link}"},
	{Package: "payment_method_configuration", CreatePath: "/v1/payment_method_configurations", UpdatePath: "/v1/payment_method_configurations/{configuration}"},
	{Package: "payment_method_domain", CreatePath: "/v1/payment_method_domains", UpdatePath: "/v1/payment_method_domains/{payment_method_domain}"},
	{Package: "radar_value_list", CreatePath: "/v1/radar/value_lists", UpdatePath: "/v1/radar/value_lists/{value_list}"},
	{Package: "radar_value_list_item", CreatePath: "/v1/radar/value_list_items"},
	{Package: "reporting_report_type", ReadPath: "/v1/reporting/report_types/{report_type}"},
	{Package: "tax_registration", CreatePath: "/v1/tax/registrations", UpdatePath: "/v1/tax/registrations/{id}"},
	{Package: "tax_settings", ReadPath: "/v1/tax/settings", Omit: []string{"status_details.active"}},
	{Package: "terminal_configuration", CreatePath: "/v1/terminal/configurations", UpdatePath: "/v1/terminal/configurations/{configuration}"},
	{Package: "terminal_location", CreatePath: "/v1/terminal/locations", UpdatePath: "/v1/terminal/locations/{location}"},
	// The Omit entries of the billing objects: `discounts` is sent as objects and returned as
	// IDs, which no attribute holds both ways, so discounts wait for a decision on them.
	{Package: "billing_credit_grant", CreatePath: "/v1/billing/credit_grants", UpdatePath: "/v1/billing/credit_grants/{id}"},
	{Package: "credit_note", ReadPath: "/v1/credit_notes/{id}"},
	// An invoice's payment error is a nested payment intent that repeats without end, and
	// three payment method options are empty objects in the response.
	{Package: "invoice", ReadPath: "/v1/invoices/{invoice}", Omit: []string{"last_finalization_error", "payment_settings.payment_method_options.blik", "payment_settings.payment_method_options.konbini", "payment_settings.payment_method_options.sepa_debit"}},
	{Package: "invoiceitem", CreatePath: "/v1/invoiceitems", UpdatePath: "/v1/invoiceitems/{invoiceitem}", Omit: []string{"discounts"}},
	{Package: "quote", CreatePath: "/v1/quotes", UpdatePath: "/v1/quotes/{quote}", Omit: []string{"discounts"}},
	{Package: "subscription", CreatePath: "/v1/subscriptions", UpdatePath: "/v1/subscriptions/{subscription_exposed_id}", Omit: []string{"discounts", "payment_settings.payment_method_options.konbini", "payment_settings.payment_method_options.sepa_debit", "items.discounts"}, CreateOnly: []string{"billing_cycle_anchor"}},
	{Package: "subscription_item", CreatePath: "/v1/subscription_items", UpdatePath: "/v1/subscription_items/{item}", Omit: []string{"discounts"}},
	{Package: "subscription_schedule", CreatePath: "/v1/subscription_schedules", UpdatePath: "/v1/subscription_schedules/{schedule}", Omit: []string{"from_subscription", "pause_schedules"}},
	// Wave 6. The setup intent leaves out what only confirming it uses, and its payment
	// method options, which the suite does not prove. The customer payment method is an
	// attachment, so the engine adds its `customer` as the parent and `customer_account` is
	// not offered. The Omit entries of the read-only objects are unions and recursions the
	// tables cannot hold. The ReadOnly entries are the payment method updates the suite does
	// not prove, on an object that cannot be replaced.
	{Package: "payment_link", CreatePath: "/v1/payment_links", UpdatePath: "/v1/payment_links/{payment_link}", Omit: []string{"line_items.price_data"}, Require: []string{"line_items.price"}},
	{Package: "setup_intent", CreatePath: "/v1/setup_intents", UpdatePath: "/v1/setup_intents/{intent}", Omit: []string{"last_setup_error.payment_intent", "last_setup_error.setup_intent", "last_setup_error.source", "confirm", "mandate_data", "payment_method_data", "return_url", "use_stripe_sdk", "single_use", "payment_method_options", "confirmation_token"}},
	{Package: "customer_payment_method", CreatePath: "/v1/payment_methods/{payment_method}/attach", UpdatePath: "/v1/payment_methods/{payment_method}", Omit: []string{"customer", "customer_account"}, ReadOnly: []string{"allow_redisplay", "card.exp_month", "card.networks", "us_bank_account"}},
	{Package: "payment_intent", ReadPath: "/v1/payment_intents/{intent}", Omit: []string{"payment_method_options", "amount_details", "last_payment_error.payment_intent", "last_payment_error.setup_intent", "last_payment_error.source"}},
	{Package: "charge", ReadPath: "/v1/charges/{charge}"},
	{Package: "refund", ReadPath: "/v1/refunds/{refund}"},
	{Package: "dispute", ReadPath: "/v1/disputes/{dispute}"},
	{Package: "checkout_session", ReadPath: "/v1/checkout/sessions/{session}"},
	{Package: "mandate", ReadPath: "/v1/mandates/{mandate}"},
	// Wave 7 batch a. The Climate catalogue is read-only. The identity session leaves out
	// verification flows and related persons, which the suite does not prove, so it takes a
	// `type`. Its redact action is a transition the resource leaves out.
	{Package: "terminal_reader", CreatePath: "/v1/terminal/readers", UpdatePath: "/v1/terminal/readers/{reader}", Omit: []string{"action.api_error.payment_intent", "action.api_error.setup_intent", "action.api_error.source"}},
	{Package: "climate_order", CreatePath: "/v1/climate/orders", UpdatePath: "/v1/climate/orders/{order}"},
	{Package: "identity_verification_session", CreatePath: "/v1/identity/verification_sessions", UpdatePath: "/v1/identity/verification_sessions/{session}", Omit: []string{"verification_flow", "related_person"}, Require: []string{"type"}},
	{Package: "climate_product", ReadPath: "/v1/climate/products/{product}"},
	{Package: "climate_supplier", ReadPath: "/v1/climate/suppliers/{supplier}"},
	// Wave 8a Connect. The account table keeps what the conformance suite and the fake prove: its
	// type or controller, country, email, business profile name, metadata and the capabilities
	// it requests. The rest of its create and update parameters, and the objects it only
	// returns when onboarding fills them in, are left out or read-only. `external_accounts` is
	// the child resource's. The response's `capabilities` is a hash of statuses, which a request
	// does not send, so the attribute is request-only and the capability data source reads the
	// statuses. The resource refuses a change to it.
	{Package: "account", CreatePath: "/v1/accounts", UpdatePath: "/v1/accounts/{account}",
		Omit:       []string{"account_token", "bank_account", "external_account", "documents", "company", "individual", "groups", "tos_acceptance", "external_accounts"},
		ReadOnly:   []string{"business_type", "default_currency", "settings", "business_profile.annual_revenue", "business_profile.estimated_worker_count", "business_profile.mcc", "business_profile.minority_owned_business_designation", "business_profile.monthly_estimated_revenue", "business_profile.product_description", "business_profile.specified_commercial_transactions_act_url", "business_profile.support_address", "business_profile.support_email", "business_profile.support_phone", "business_profile.support_url", "business_profile.url"},
		NoResponse: []string{"capabilities"},
		Require:    []string{"country"}},
	// The external account is a bank account here (a card is a different object), created from
	// the hash the spec documents under `bank_account`. Its `account` is the parent ID. The
	// Omit entries are the card's update parameters and the documents, which need a file. The
	// account type is refused on a US account.
	{Package: "account_external_account", CreatePath: "/v1/accounts/{account}/external_accounts", UpdatePath: "/v1/accounts/{account}/external_accounts/{id}",
		Response: "bank_account", CreateHash: "bank_account",
		Omit:     []string{"external_account", "account", "documents", "address_city", "address_country", "address_line1", "address_line2", "address_state", "address_zip", "exp_month", "exp_year", "name"},
		ReadOnly: []string{"account_type"}, Require: []string{"currency", "routing_number"}},
	{Package: "account_capability", ReadPath: "/v1/accounts/{account}/capabilities/{capability}", Omit: []string{"account"}},
	// Wave 8 batch a. A billing alert is never updated: its status moves by action endpoints
	// the declaration names. A tax ID takes only `self` and `customer` owners, which the suite
	// proves; the other owners' arguments stay out of reach. The customer's balance
	// transactions are addressed under their customer, so the engine adds `customer` as the
	// parent and the response's copy is left out. Cash balance transactions are read-only.
	{Package: "billing_alert", CreatePath: "/v1/billing/alerts"},
	{Package: "tax_id", CreatePath: "/v1/tax_ids", ReadOnly: []string{"owner.account", "owner.customer_account"}},
	{Package: "customer_balance_transaction", CreatePath: "/v1/customers/{customer}/balance_transactions", UpdatePath: "/v1/customers/{customer}/balance_transactions/{transaction}", Omit: []string{"customer"}},
	{Package: "customer_cash_balance_transaction", ReadPath: "/v1/customers/{customer}/cash_balance_transactions/{transaction}", Omit: []string{"customer"}},
	// Wave 8 batch b. Test clocks advance by an action endpoint, so the table sees `frozen_time`
	// as create-only and the declaration moves it. Payouts, top-ups, early fraud warnings and
	// reviews are read-only: the suite cannot create them on its account. An invoice payment is
	// retrieved by its own ID.
	{Package: "test_helpers_test_clock", CreatePath: "/v1/test_helpers/test_clocks", Omit: []string{"customer"}},
	{Package: "payout", ReadPath: "/v1/payouts/{payout}"},
	{Package: "topup", ReadPath: "/v1/topups/{topup}"},
	{Package: "radar_early_fraud_warning", ReadPath: "/v1/radar/early_fraud_warnings/{early_fraud_warning}"},
	{Package: "review", ReadPath: "/v1/reviews/{review}"},
	{Package: "invoice_payment", ReadPath: "/v1/invoice_payments/{invoice_payment}"},
	// Wave 8 batch c. Stripe has no retrieve for a line, only the list under its invoice,
	// credit note or quote, so the table is the list element's. The invoice line's `invoice` is
	// the parent the engine adds.
	{Package: "invoice_line", ListPath: "/v1/invoices/{invoice}/lines", Omit: []string{"invoice"}},
	{Package: "credit_note_line", ListPath: "/v1/credit_notes/{credit_note}/lines"},
	{Package: "quote_line_item", ListPath: "/v1/quotes/{quote}/line_items"},
	{Package: "quote_computed_upfront_line_item", ListPath: "/v1/quotes/{quote}/computed_upfront_line_items"},
	// Wave 7 batch b. Issuing and Treasury run on a connected account. The card is virtual: the
	// Omit entries are the physical card's shipping, PIN, personalization design and second line.
	// Stripe refuses a card without a cardholder (suite: "create errors"), although the spec
	// leaves it optional.
	// A financial account's `features` is a hash of `requested` flags on the way in and of
	// feature statuses, returned only when expanded, on the way out; the top-level
	// `active_features`, `pending_features` and `restricted_features` read the statuses, so the
	// attribute is request-only, as an account's `capabilities` is. `forwarding_settings` belongs
	// to closing the account, which the suite does not prove. The Treasury flows and Financial
	// Connections objects are read-only. A transaction's `entries` is returned only when
	// expanded, which neither the suite nor the fake proves yet; the transaction entry data
	// source reads them.
	{Package: "issuing_cardholder", CreatePath: "/v1/issuing/cardholders", UpdatePath: "/v1/issuing/cardholders/{cardholder}"},
	{Package: "issuing_card", CreatePath: "/v1/issuing/cards", UpdatePath: "/v1/issuing/cards/{card}", Omit: []string{"shipping", "pin", "personalization_design", "second_line"}, Require: []string{"cardholder"}},
	{Package: "treasury_financial_account", CreatePath: "/v1/treasury/financial_accounts", UpdatePath: "/v1/treasury/financial_accounts/{financial_account}", Omit: []string{"forwarding_settings"}, NoResponse: []string{"features"}},
	{Package: "treasury_transaction", ReadPath: "/v1/treasury/transactions/{id}", Omit: []string{"entries"}},
	{Package: "treasury_transaction_entry", ReadPath: "/v1/treasury/transaction_entries/{id}"},
	{Package: "treasury_received_credit", ReadPath: "/v1/treasury/received_credits/{id}"},
	{Package: "treasury_received_debit", ReadPath: "/v1/treasury/received_debits/{id}"},
	{Package: "treasury_credit_reversal", ReadPath: "/v1/treasury/credit_reversals/{credit_reversal}"},
	{Package: "treasury_debit_reversal", ReadPath: "/v1/treasury/debit_reversals/{debit_reversal}"},
	{Package: "treasury_inbound_transfer", ReadPath: "/v1/treasury/inbound_transfers/{id}"},
	{Package: "treasury_outbound_transfer", ReadPath: "/v1/treasury/outbound_transfers/{outbound_transfer}"},
	{Package: "treasury_outbound_payment", ReadPath: "/v1/treasury/outbound_payments/{id}"},
	{Package: "financial_connections_account", ReadPath: "/v1/financial_connections/accounts/{account}"},
	{Package: "financial_connections_session", ReadPath: "/v1/financial_connections/sessions/{session}"},
	// Wave 8 batches e, f, h and i: read-only objects. A meter event summary, a payment intent's
	// line item and a tax transaction's line item have no retrieve, only a list under their
	// parent; the summary's `meter` is the parent the engine adds. A secret is read through
	// `find`. A tax transaction's `line_items` is returned only when expanded, which the fake
	// does not serve; the line item data source reads them.
	{Package: "billing_credit_balance_transaction", ReadPath: "/v1/billing/credit_balance_transactions/{id}"},
	{Package: "billing_meter_event_summary", ListPath: "/v1/billing/meters/{id}/event_summaries", Omit: []string{"meter"}},
	{Package: "entitlements_active_entitlement", ReadPath: "/v1/entitlements/active_entitlements/{id}"},
	{Package: "invoice_rendering_template", ReadPath: "/v1/invoice_rendering_templates/{template}"},
	{Package: "payment_intent_amount_details_line_item", ListPath: "/v1/payment_intents/{intent}/amount_details_line_items"},
	{Package: "tax_transaction", ReadPath: "/v1/tax/transactions/{transaction}", Omit: []string{"line_items"}},
	{Package: "tax_transaction_line_item", ListPath: "/v1/tax/transactions/{transaction}/line_items"},
	{Package: "tax_location", ReadPath: "/v1/tax/locations/{location}"},
	{Package: "reporting_report_run", ReadPath: "/v1/reporting/report_runs/{report_run}"},
	{Package: "core_event", ReadPath: "/v2/core/events/{id}"},
	{Package: "apps_secret", ReadPath: "/v1/apps/secrets/find"},
	{Package: "identity_verification_report", ReadPath: "/v1/identity/verification_reports/{report}"},
	{Package: "payment_record", ReadPath: "/v1/payment_records/{id}"},
	{Package: "commerce_product_catalog_import", ReadPath: "/v2/commerce/product_catalog/imports/{id}"},
}
