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
}
