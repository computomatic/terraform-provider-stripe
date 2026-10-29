package main

// parentHolds is the register's reason for a table's own copy of the parent ID that the
// engine adds as the declaration's Parent attribute.
const parentHolds = "the parent attribute holds it"

// Resources lists the objects the generator writes tables for. A resource joins the list
// when its declaration is written.
var Resources = []Resource{
	{Package: "balance", ReadPath: "/v1/balance"},
	{Package: "billing_meter", CreatePath: "/v1/billing/meters", UpdatePath: "/v1/billing/meters/{id}"},
	{Package: "billing_portal_configuration", CreatePath: "/v1/billing_portal/configurations", UpdatePath: "/v1/billing_portal/configurations/{configuration}"},
	{Package: "country_spec", ReadPath: "/v1/country_specs/{country}", Omit: []string{"supported_bank_account_currencies"}},
	// The Omit entries: `include` is a request parameter, not an object field, and the signing
	// secret waits for the conformance suite and the fake to prove it (ADR 2026-10-17-v2-support).
	{Package: "event_destination_v2", CreatePath: "/v2/core/event_destinations", UpdatePath: "/v2/core/event_destinations/{id}", Omit: []string{"include", "webhook_endpoint.signing_secret"}},
	{Package: "coupon", CreatePath: "/v1/coupons", UpdatePath: "/v1/coupons/{coupon}"},
	{Package: "customer", CreatePath: "/v1/customers", UpdatePath: "/v1/customers/{customer}", Omit: []string{"sources", "subscriptions"}},
	{Package: "entitlements_feature", CreatePath: "/v1/entitlements/features", UpdatePath: "/v1/entitlements/features/{id}"},
	{Package: "plan", CreatePath: "/v1/plans", UpdatePath: "/v1/plans/{plan}"},
	{Package: "price", CreatePath: "/v1/prices", UpdatePath: "/v1/prices/{price}"},
	{Package: "product", CreatePath: "/v1/products", UpdatePath: "/v1/products/{id}"},
	{Package: "product_catalog_trial_offer", CreatePath: "/v1/product_catalog/trial_offers", UpdatePath: "/v1/product_catalog/trial_offers/{id}"},
	{Package: "product_feature", CreatePath: "/v1/products/{product}/features"},
	{Package: "promotion_code", CreatePath: "/v1/promotion_codes", UpdatePath: "/v1/promotion_codes/{promotion_code}"},
	// The spec leaves `type` optional, but Stripe refuses a create without it and has no
	// default (suite: "create without type").
	{Package: "shipping_rate", CreatePath: "/v1/shipping_rates", UpdatePath: "/v1/shipping_rates/{shipping_rate_token}", Require: []string{"type"}},
	{Package: "tax_code", ReadPath: "/v1/tax_codes/{id}"},
	{Package: "tax_rate", CreatePath: "/v1/tax_rates", UpdatePath: "/v1/tax_rates/{tax_rate}"},
	{Package: "webhook_endpoint", CreatePath: "/v1/webhook_endpoints", UpdatePath: "/v1/webhook_endpoints/{webhook_endpoint}"},
	{Package: "apple_pay_domain", CreatePath: "/v1/apple_pay/domains"},
	{Package: "balance_settings", ReadPath: "/v1/balance_settings", Omit: []string{"payments.payouts.automatic_transfer_rules_by_currency"}},
	{Package: "balance_transaction", ReadPath: "/v1/balance_transactions/{id}"},
	{Package: "billing_feedback_option", CreatePath: "/v1/billing/feedback_options", UpdatePath: "/v1/billing/feedback_options/{id}"},
	{Package: "event", ReadPath: "/v1/events/{id}", Omit: []string{"data"}},
	{Package: "file", CreatePath: "/v1/files", Omit: []string{"file", "file_link_data"},
		Exclude: map[string]string{"file": "Sent by the declaration as the upload of the local file `source` names: Terraform cannot send file content from HCL (framework limit; internal/resources/file)."}},
	{Package: "file_link", CreatePath: "/v1/file_links", UpdatePath: "/v1/file_links/{link}"},
	{Package: "payment_method_configuration", CreatePath: "/v1/payment_method_configurations", UpdatePath: "/v1/payment_method_configurations/{configuration}"},
	{Package: "payment_method_domain", CreatePath: "/v1/payment_method_domains", UpdatePath: "/v1/payment_method_domains/{payment_method_domain}"},
	{Package: "radar_value_list", CreatePath: "/v1/radar/value_lists", UpdatePath: "/v1/radar/value_lists/{value_list}", PagedLists: map[string]string{"list_items": "stripe_radar_value_list_item"}},
	{Package: "radar_value_list_item", CreatePath: "/v1/radar/value_list_items"},
	{Package: "reporting_report_type", ReadPath: "/v1/reporting/report_types/{report_type}"},
	{Package: "tax_registration", CreatePath: "/v1/tax/registrations", UpdatePath: "/v1/tax/registrations/{id}"},
	{Package: "tax_settings", ReadPath: "/v1/tax/settings", Omit: []string{"status_details.active"}},
	{Package: "terminal_configuration", CreatePath: "/v1/terminal/configurations", UpdatePath: "/v1/terminal/configurations/{configuration}"},
	{Package: "terminal_location", CreatePath: "/v1/terminal/locations", UpdatePath: "/v1/terminal/locations/{location}"},
	// The Omit entries of the billing objects: `discounts` is sent as objects and returned as
	// IDs, which no attribute holds both ways, so discounts wait for a decision on them.
	{Package: "billing_credit_grant", CreatePath: "/v1/billing/credit_grants", UpdatePath: "/v1/billing/credit_grants/{id}"},
	{Package: "credit_note", CreatePath: "/v1/credit_notes", UpdatePath: "/v1/credit_notes/{id}", PagedLists: map[string]string{"lines": "stripe_credit_note_line"}},
	// An invoice's payment error is a nested payment intent that repeats without end, and
	// three payment method options are empty objects in the response.
	{Package: "invoice", CreatePath: "/v1/invoices", UpdatePath: "/v1/invoices/{invoice}", PagedLists: map[string]string{"lines": "stripe_invoice_line"}, Omit: []string{"discounts", "last_finalization_error", "payment_settings.payment_method_options.blik", "payment_settings.payment_method_options.konbini", "payment_settings.payment_method_options.sepa_debit"}},
	{Package: "invoiceitem", CreatePath: "/v1/invoiceitems", UpdatePath: "/v1/invoiceitems/{invoiceitem}", Omit: []string{"discounts"}},
	{Package: "quote", CreatePath: "/v1/quotes", UpdatePath: "/v1/quotes/{quote}", Omit: []string{"discounts"}},
	{Package: "subscription", CreatePath: "/v1/subscriptions", UpdatePath: "/v1/subscriptions/{subscription_exposed_id}", PagedLists: map[string]string{"items": "stripe_subscription_item"}, Omit: []string{"discounts", "payment_settings.payment_method_options.konbini", "payment_settings.payment_method_options.sepa_debit", "items.discounts"}, CreateOnly: []string{"billing_cycle_anchor"}},
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
	{Package: "customer_payment_method", Exclude: map[string]string{"customer": parentHolds}, CreatePath: "/v1/payment_methods/{payment_method}/attach", UpdatePath: "/v1/payment_methods/{payment_method}", Omit: []string{"customer", "customer_account"}, ReadOnly: []string{"allow_redisplay", "card.exp_month", "card.networks", "us_bank_account"}},
	// Confirming is the resource's `status`, so the confirm-time parameters are left out, as
	// they are for setup intents, and `confirm` is excluded by rule X5. `hooks` is in the
	// response, so it stays readable but is not an argument, which the suite has not proven.
	{Package: "payment_intent", StatusConfirms: true, CreatePath: "/v1/payment_intents", UpdatePath: "/v1/payment_intents/{intent}", Omit: []string{
		"payment_method_options", "amount_details", "last_payment_error.payment_intent", "last_payment_error.setup_intent", "last_payment_error.source",
		"confirm", "confirmation_token", "error_on_requires_action", "mandate", "mandate_data", "off_session", "payment_method_data", "return_url",
		"use_stripe_sdk", "radar_options",
	}, ReadOnly: []string{"hooks"}},
	// A direct charge (ADR 2026-10-22-money-moving-resources, D6.2). The spec deprecates the
	// create in favour of payment intents, but Stripe serves it. Its deprecated `card` and
	// `destination` are left out; `application_fee` is also the response's fee ID, and
	// `radar_options` is set by a front end, as for payment intents, so both stay readable, as
	// does `fraud_details`, which only the update takes and the suite has not proven. Stripe
	// requires `amount` and `currency`, which the spec leaves optional (suite: "missing amount",
	// "missing currency").
	{Package: "charge", CreatePath: "/v1/charges", UpdatePath: "/v1/charges/{charge}", Omit: []string{"card", "destination"}, ReadOnly: []string{"application_fee", "fraud_details", "radar_options"}, Require: []string{"amount", "currency"}},
	{Package: "refund", CreatePath: "/v1/refunds", UpdatePath: "/v1/refunds/{refund}"},
	{Package: "dispute", ReadPath: "/v1/disputes/{dispute}"},
	// `collected_information` is set only by an update, which the suite has not proven, so it
	// stays readable.
	{Package: "checkout_session", CreatePath: "/v1/checkout/sessions", UpdatePath: "/v1/checkout/sessions/{session}", Omit: []string{"payment_method_options.pay_by_bank"}, ReadOnly: []string{"collected_information"}},
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
	// it requests, and the onboarding values a US Custom individual account is created with
	// ("verifiedIndividualForm", "issuingAccountForm", "company[name]" of the persons tests).
	// The rest of its create and update parameters, and the objects it only returns when
	// onboarding fills them in, are left out or read-only. `external_accounts` is the child
	// resource's. The response's `capabilities` is a hash of statuses, which a request does not
	// send, and its `individual` and `company` hold far more than the request, which the fake
	// does not show, so the three attributes are request-only and keep what the configuration
	// says. Their descriptions come from the response, and the individual's identity numbers,
	// which the person object does not return, from a person's create. The capability data
	// source reads the statuses.
	{Package: "account", CreatePath: "/v1/accounts", UpdatePath: "/v1/accounts/{account}",
		Keep: []string{"business_profile", "business_type", "capabilities", "charges_enabled", "company.name", "controller", "country", "created",
			"default_currency", "details_submitted", "email", "external_account", "future_requirements", "id",
			"individual.address.city", "individual.address.line1", "individual.address.postal_code", "individual.address.state",
			"individual.dob", "individual.email", "individual.first_name", "individual.id_number", "individual.last_name", "individual.phone",
			"individual.ssn_last_4", "individual.verification.document.front",
			"metadata", "payouts_enabled", "requirements", "settings", "tos_acceptance.date", "tos_acceptance.ip", "type"},
		ReadOnly: []string{"default_currency",
			"settings.bacs_debit_payments", "settings.branding", "settings.card_payments", "settings.invoices", "settings.payments", "settings.payouts",
			"settings.paypay_payments", "settings.sepa_debit_payments", "settings.card_issuing.tos_acceptance.user_agent", "settings.treasury.tos_acceptance.user_agent",
			"business_profile.minority_owned_business_designation", "business_profile.monthly_estimated_revenue", "business_profile.specified_commercial_transactions_act_url",
			"business_profile.support_address", "business_profile.support_email", "business_profile.support_phone", "business_profile.support_url"},
		NoResponse: []string{"capabilities", "company", "individual"},
		DocsFrom:   map[string]string{"individual": "/v1/accounts/{account}/persons"},
		Require:    []string{"country"}},
	// The external account is a bank account here (a card is a different object), created from
	// the hash the spec documents under `bank_account`. Its `account` is the parent ID. The
	// Omit entries are the card's update parameters and the documents, which need a file. The
	// account type is refused on a US account.
	{Package: "account_external_account", Exclude: map[string]string{"account": parentHolds}, CreatePath: "/v1/accounts/{account}/external_accounts", UpdatePath: "/v1/accounts/{account}/external_accounts/{id}",
		Response: "bank_account", CreateHash: "bank_account",
		Omit:     []string{"external_account", "account", "documents", "address_city", "address_country", "address_line1", "address_line2", "address_state", "address_zip", "exp_month", "exp_year", "name"},
		ReadOnly: []string{"account_type"}, Require: []string{"currency", "routing_number"}},
	{Package: "account_capability", Exclude: map[string]string{"account": parentHolds}, ReadPath: "/v1/accounts/{account}/capabilities/{capability}", Omit: []string{"account"}},
	// Wave 4 batch b Connect. Each table keeps what the conformance suite and the fake prove,
	// among many fields that need files, other countries or onboarding flows. A v1 person's
	// `account` and a v2 person's `account` are the parent the engine adds. The `authorizer`
	// and `legal_guardian` roles and a v1 person's verification documents are read-only: no
	// suite test sets them. A v2 account requests the one capability of each configuration
	// the suite requests; the balance capabilities Stripe adds to it, and whether a
	// configuration is applied, are read-only. Its country and entity type are create-only,
	// because the suite updates only the registered name of an identity. App installs are
	// read-only: no suite test can install an app.
	{Package: "account_person", Exclude: map[string]string{"account": parentHolds}, CreatePath: "/v1/accounts/{account}/persons", UpdatePath: "/v1/accounts/{account}/persons/{person}",
		Keep: []string{"address", "created", "dob", "email", "first_name", "future_requirements", "id", "id_number", "id_number_provided", "last_name", "metadata", "phone",
			"relationship", "requirements", "ssn_last_4", "ssn_last_4_provided", "verification"},
		ReadOnly: []string{"relationship.authorizer", "relationship.legal_guardian", "verification"}},
	{Package: "account_v2", CreatePath: "/v2/core/accounts", UpdatePath: "/v2/core/accounts/{id}",
		Keep: []string{"applied_configurations", "closed",
			"configuration.customer.applied", "configuration.customer.capabilities.automatic_indirect_tax",
			"configuration.merchant.applied", "configuration.merchant.capabilities.card_payments", "configuration.merchant.capabilities.stripe_balance.payouts",
			"configuration.recipient.applied", "configuration.recipient.capabilities.stripe_balance.payouts", "configuration.recipient.capabilities.stripe_balance.stripe_transfers",
			"contact_email", "created", "dashboard", "defaults.currency", "defaults.responsibilities", "display_name", "future_requirements", "id",
			"identity.business_details.registered_name", "identity.country", "identity.entity_type", "livemode", "metadata", "requirements"},
		ReadOnly: []string{"configuration.customer.applied", "configuration.merchant.applied", "configuration.recipient.applied",
			"configuration.merchant.capabilities.stripe_balance", "configuration.recipient.capabilities.stripe_balance.payouts"},
		CreateOnly: []string{"identity.country", "identity.entity_type"}},
	{Package: "account_person_v2", Exclude: map[string]string{"account": parentHolds}, CreatePath: "/v2/core/accounts/{account_id}/persons", UpdatePath: "/v2/core/accounts/{account_id}/persons/{id}",
		Keep: []string{"address.city", "address.country", "address.line1", "address.line2", "address.postal_code", "address.state", "created", "date_of_birth", "email",
			"given_name", "id", "id_numbers", "livemode", "metadata", "phone", "relationship", "surname", "updated"},
		ReadOnly: []string{"relationship.authorizer", "relationship.legal_guardian"}},
	{Package: "apps_install", ReadPath: "/v1/apps/installs/{id}"},
	// Wave 8 batch a. A billing alert is never updated: its status moves by action endpoints
	// the declaration names. A tax ID takes only `self` and `customer` owners, which the suite
	// proves; the other owners' arguments stay out of reach. The customer's balance
	// transactions are addressed under their customer, so the engine adds `customer` as the
	// parent and the response's copy is left out. Cash balance transactions are read-only.
	{Package: "billing_alert", CreatePath: "/v1/billing/alerts"},
	{Package: "tax_id", CreatePath: "/v1/tax_ids", ReadOnly: []string{"owner.account", "owner.customer_account"}},
	{Package: "customer_balance_transaction", Exclude: map[string]string{"customer": parentHolds}, CreatePath: "/v1/customers/{customer}/balance_transactions", UpdatePath: "/v1/customers/{customer}/balance_transactions/{transaction}", Omit: []string{"customer"}},
	{Package: "customer_cash_balance_transaction", Exclude: map[string]string{"customer": parentHolds}, ReadPath: "/v1/customers/{customer}/cash_balance_transactions/{transaction}", Omit: []string{"customer"}},
	// Wave 8 batch b. Test clocks advance by an action endpoint, so the table sees `frozen_time`
	// as create-only and the declaration moves it. Early fraud warnings and reviews are
	// read-only: the suite cannot create them on its account. An invoice payment is retrieved by
	// its own ID.
	{Package: "test_helpers_test_clock", CreatePath: "/v1/test_helpers/test_clocks", Omit: []string{"customer"}},
	{Package: "payout", CreatePath: "/v1/payouts", UpdatePath: "/v1/payouts/{payout}"},
	// The create's `source` is the ID of a legacy source, which the suite has not proven; the
	// response's is the source object, which stays readable.
	{Package: "topup", CreatePath: "/v1/topups", UpdatePath: "/v1/topups/{topup}", ReadOnly: []string{"source"}},
	{Package: "radar_early_fraud_warning", ReadPath: "/v1/radar/early_fraud_warnings/{early_fraud_warning}"},
	{Package: "review", ReadPath: "/v1/reviews/{review}"},
	{Package: "invoice_payment", ReadPath: "/v1/invoice_payments/{invoice_payment}"},
	// Wave 8 batch c. Stripe has no retrieve for a line, only the list under its invoice,
	// credit note or quote, so the table is the list element's. The invoice line's `invoice` is
	// the parent the engine adds.
	{Package: "invoice_line", Exclude: map[string]string{"invoice": parentHolds}, ListPath: "/v1/invoices/{invoice}/lines", Omit: []string{"invoice"}},
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
	{Package: "treasury_credit_reversal", CreatePath: "/v1/treasury/credit_reversals"},
	{Package: "treasury_debit_reversal", CreatePath: "/v1/treasury/debit_reversals"},
	{Package: "treasury_inbound_transfer", CreatePath: "/v1/treasury/inbound_transfers"},
	{Package: "treasury_outbound_transfer", CreatePath: "/v1/treasury/outbound_transfers"},
	{Package: "treasury_outbound_payment", CreatePath: "/v1/treasury/outbound_payments"},
	// The rest of Issuing. A design's `transfer_lookup_key` is an instruction, not a field of the
	// design, and nothing proves it; nothing proves setting a design's `preferences` either, so
	// they are read-only. A dispute's `treasury` names the received debit Stripe disputes in
	// place of a transaction, which nothing proves, and Stripe fills it in on a Treasury-backed
	// card, so it is read-only too, and the dispute needs its `transaction`. Stripe refuses a
	// dispute without `evidence` and evidence without its `reason` (suite: "Issuing disputes
	// (first run)"), although the spec leaves both optional. Authorizations, transactions,
	// physical bundles and tokens are read-only objects.
	{Package: "issuing_personalization_design", CreatePath: "/v1/issuing/personalization_designs", UpdatePath: "/v1/issuing/personalization_designs/{personalization_design}", Omit: []string{"transfer_lookup_key"}, ReadOnly: []string{"preferences"}},
	{Package: "issuing_dispute", CreatePath: "/v1/issuing/disputes", UpdatePath: "/v1/issuing/disputes/{dispute}", ReadOnly: []string{"treasury"}, Require: []string{"transaction", "evidence", "evidence.reason"}},
	{Package: "issuing_physical_bundle", ReadPath: "/v1/issuing/physical_bundles/{physical_bundle}"},
	{Package: "issuing_authorization", ReadPath: "/v1/issuing/authorizations/{authorization}"},
	{Package: "issuing_transaction", ReadPath: "/v1/issuing/transactions/{transaction}"},
	{Package: "issuing_token", ReadPath: "/v1/issuing/tokens/{token}"},
	{Package: "financial_connections_account", ReadPath: "/v1/financial_connections/accounts/{account}"},
	{Package: "financial_connections_session", ReadPath: "/v1/financial_connections/sessions/{session}", PagedLists: map[string]string{"accounts": "stripe_financial_connections_account"}},
	// Wave 8 batches e, f, h and i: read-only objects. A meter event summary, a payment intent's
	// line item and a tax transaction's line item have no retrieve, only a list under their
	// parent; the summary's `meter` is the parent the engine adds. A secret is read through
	// `find`. A tax transaction's `line_items` is returned only when expanded, which the fake
	// does not serve; the line item data source reads them.
	{Package: "billing_credit_balance_transaction", ReadPath: "/v1/billing/credit_balance_transactions/{id}"},
	{Package: "billing_meter_event_summary", Exclude: map[string]string{"meter": parentHolds}, ListPath: "/v1/billing/meters/{id}/event_summaries", Omit: []string{"meter"}},
	{Package: "entitlements_active_entitlement", ReadPath: "/v1/entitlements/active_entitlements/{id}"},
	{Package: "invoice_rendering_template", ReadPath: "/v1/invoice_rendering_templates/{template}"},
	{Package: "payment_intent_amount_details_line_item", ListPath: "/v1/payment_intents/{intent}/amount_details_line_items"},
	{Package: "tax_transaction", ReadPath: "/v1/tax/transactions/{transaction}", Omit: []string{"line_items"}},
	{Package: "tax_transaction_line_item", ListPath: "/v1/tax/transactions/{transaction}/line_items"},
	{Package: "tax_location", ReadPath: "/v1/tax/locations/{location}"},
	{Package: "reporting_report_run", ReadPath: "/v1/reporting/report_runs/{report_run}"},
	{Package: "event_v2", ReadPath: "/v2/core/events/{id}"},
	{Package: "apps_secret", ReadPath: "/v1/apps/secrets/find"},
	{Package: "identity_verification_report", ReadPath: "/v1/identity/verification_reports/{report}"},
	{Package: "payment_record", ReadPath: "/v1/payment_records/{id}"},
	{Package: "commerce_product_catalog_import_v2", ReadPath: "/v2/commerce/product_catalog/imports/{id}"},
	// Wave 8 batches d and k: Connect money movement and a customer's legacy cards and bank
	// accounts. A reversal is addressed under its transfer, a fee refund under its fee and a
	// card or bank account under its customer, so the engine adds each parent and the
	// response's copy is left out.
	// Stripe requires a transfer's `amount`, which the spec leaves optional (suite: "missing amount").
	{Package: "transfer", CreatePath: "/v1/transfers", UpdatePath: "/v1/transfers/{transfer}", PagedLists: map[string]string{"reversals": "stripe_transfer_reversal"}, Require: []string{"amount"}},
	{Package: "transfer_reversal", Exclude: map[string]string{"transfer": parentHolds}, CreatePath: "/v1/transfers/{id}/reversals", UpdatePath: "/v1/transfers/{transfer}/reversals/{id}", Omit: []string{"transfer"}},
	{Package: "application_fee", ReadPath: "/v1/application_fees/{id}", PagedLists: map[string]string{"refunds": "stripe_application_fee_refund"}},
	{Package: "application_fee_refund", Exclude: map[string]string{"fee": parentHolds}, CreatePath: "/v1/application_fees/{id}/refunds", UpdatePath: "/v1/application_fees/{fee}/refunds/{id}", Omit: []string{"fee"}},
	{Package: "customer_card", Exclude: map[string]string{"customer": parentHolds}, ReadPath: "/v1/customers/{customer}/cards/{id}", Omit: []string{"customer"}},
	{Package: "customer_bank_account", Exclude: map[string]string{"customer": parentHolds}, ReadPath: "/v1/customers/{customer}/bank_accounts/{id}", Omit: []string{"customer"}},
	// The line items of a checkout session and of a payment link have no retrieve, only the
	// list under their parent, so the table is the list element's.
	{Package: "checkout_session_line_item", ListPath: "/v1/checkout/sessions/{session}/line_items"},
	{Package: "payment_link_line_item", ListPath: "/v1/payment_links/{payment_link}/line_items"},
	// A Financial Connections account's owners have no retrieve, only the list under the
	// account, so the table is the list element's.
	{Package: "financial_connections_account_owner", ListPath: "/v1/financial_connections/accounts/{account}/owners"},
	{Package: "financial_connections_transaction", ReadPath: "/v1/financial_connections/transactions/{transaction}"},
}
