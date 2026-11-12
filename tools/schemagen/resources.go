package main

import (
	"fmt"
	"maps"
)

// The reasons of LeftToStripe entries the record's rules share: what Stripe's refusal turns on,
// which the plan cannot know.
const (
	leftExpired          = "Stripe's refusal turns on the state of the coupon or promotion code it names, expired or inactive, which the plan cannot know (probes `isolated/*coupon_expired` and `isolated/*promotion_code_inactive`)."
	leftDiscountHeld     = "Stripe's refusal turns on which object holds the discount the request names, which the plan cannot know: it took a quote line's and a subscription item's own discount and refused one of the quote or subscription (probes `isolated/line_discount_held` and `isolated/item_discount_held` against `isolated/*discount_of_*`)."
	leftCouponBesideHeld = "Stripe refuses a coupon beside a discount in one element (`You may only specify one of these parameters: coupon, discount.`), but the record sends the discount as the one read from the object, so the plan cannot tell the refused request from one naming a discount on its own, on which Stripe's answer turns (probes `isolated/*coupon_beside_held`)."
	leftMissing          = "Stripe's refusal turns on whether the object the request names exists, which the plan cannot know (code `resource_missing`)."
	leftStartedLater     = "The probe's schedule was created to start a day after the create (`$gen` `now` with an offset), a time relative to when the probe ran, which a pattern cannot state and any value would widen to every start; the plan refuses the same requests to a schedule created with `start_date = \"now\"` (probes `isolated/phases_empty_started` and `isolated/phases_without_start_date_started`)."
	leftBusinessURL      = "Stripe's refusal turns on its own check of the URL, not on the request alone: it took `https://example.com/acme` on a create and refused it on an update, and took `https://stripe.com` on a create of an account with no recipient configuration (probes `isolated/url_*`)."
)

// parentHolds is the register's reason for a table's own copy of the parent ID that the
// engine adds as the declaration's Parent attribute.
const parentHolds = "the parent attribute holds it"

// cardOnly is the register's reason for the update parameters of a card, which the spec lists for
// every external account, on a table that is only ever a bank account.
const cardOnly = "A card's update parameter: Stripe refuses it on a bank account with `parameter_unknown` in every country (suite: TestAccountExternalAccountAttributes/leaves/request/PostAccountsAccountExternalAccountsId/%s), so as an argument of this table it could only ever fail the apply."

// mandateData is the register's reason for a confirm-time hash: the customer's acceptance of a
// mandate at the confirmation, which the intent's resource never sends.
const mandateData = "A confirm-time instruction: Stripe took the hash only in creates that also sent `confirm` true, which this resource never sends, and it describes the customer's acceptance at that confirmation (suite: TestSetupIntentAttributes/leaves/request/PostSetupIntents/mandate_data.customer_acceptance.type, every probe of the create that sent it)."

// confirmTime is the register's reason for an argument of an intent's create that Stripe took only
// beside `confirm`, which the resource never sends: an instruction for the confirmation, such as
// where to send the customer back to or how to authenticate, not a setting of the intent (the
// product owner's decision of 2026-11-07 leaves one-shot instructions out). test names the suite's
// leaf test.
func confirmTime(test string) string {
	return "A confirm-time instruction: Stripe took it only in creates that also sent `confirm` true, which this resource never sends, so it would apply to no request the provider makes (suite: " + test + ", every probe of the create that sent it)."
}

// priceKept is a price's Resource.AcceptedOnly register (the product owner's decision of
// 2026-11-04).
var priceKept = map[string]string{
	"custom_unit_amount.enabled":                  "Its effect shows in the `custom_unit_amount` Stripe returns, which holds the bounds only where it is true, and the provider keeps it from configuration.",
	"currency_options.custom_unit_amount.enabled": "Its effect shows in the `custom_unit_amount` Stripe returns, which holds the bounds only where it is true, and the provider keeps it from configuration.",
	"transfer_lookup_key":                         "Its effect shows in `lookup_key`, which the price takes from the price that held it, as a replacement price does, and the provider keeps it from configuration.",
}

// keptFor gives every path the same Resource.AcceptedOnly reason.
func keptFor(reason string, paths []string) map[string]string {
	out := make(map[string]string, len(paths))
	for _, p := range paths {
		out[p] = reason
	}
	return out
}

// discountsKept keeps the forms of a `discounts` element, which Stripe returns as discount IDs:
// the record's `discounts` shows each form returned as `discount_id` and expanded to the discount,
// which the engine maps back by key (D3), so the configured element is read back (the brief of
// the eighth pin, 2026-11-05).
var discountsKept = map[string]string{
	"discounts.coupon":         "Read back from the expanded discount's `source.coupon`, on a discount with no promotion code (record: `discounts`, relation `coupon`; the brief of the eighth pin, 2026-11-05).",
	"discounts.discount":       "Read back as the expanded discount's `id` (record: `discounts`, relation `existing_discount`; the brief of the eighth pin, 2026-11-05).",
	"discounts.promotion_code": "Read back from the expanded discount's `promotion_code` (record: `discounts`, relation `promotion_code`; the brief of the eighth pin, 2026-11-05).",
}

// lineDiscountsKept keeps the coupon and promotion code forms of a list element's `discounts`,
// such as a quote line's (its `discount` form is verified),
// which Stripe returns each whole beside the amount it takes off where the resource expands them:
// the record's `discounts` shows each form returned as `discount_object` at
// `line_items.data.discounts.discount`, which the engine maps back by key (D3) (the brief of the
// ninth pin, 2026-11-05).
func lineDiscountsKept(list string) map[string]string {
	return map[string]string{
		list + ".discounts.coupon":         "Read back from the `source.coupon` of the discount Stripe returns whole on the line, on a discount with no promotion code (record: `discounts`, relation `coupon`; the brief of the ninth pin, 2026-11-05).",
		list + ".discounts.promotion_code": "Read back from the `promotion_code` of the discount Stripe returns whole on the line (record: `discounts`, relation `promotion_code`; the brief of the ninth pin, 2026-11-05).",
	}
}

// requestBehaviour is the Resource.AcceptedOnly reason of an argument that says how Stripe
// handles the request it comes with, which Stripe never returns, so that a kept value cannot
// drift, and the provider keeps it from configuration (the product owner's decision of
// 2026-11-05, as for a customer's `tax.validate_location`).
const requestBehaviour = "It says how Stripe handles the request it comes with, as a customer's `tax.validate_location` does, so a kept value cannot drift, and the provider keeps it from configuration (the product owner's decision of 2026-11-05)."

// invoiceCreateDiscount is the register's reason for an invoice create's `discount` form.
const invoiceCreateDiscount = "It names a discount another object holds, such as a subscription's, and the record's `discounts` does not show Stripe returning it on the invoice, so the engine could not read it back as it reads the other forms (suite: TestInvoiceAttributes/leaves/request/PostInvoices/discounts.discount; the brief of the tenth pin, 2026-11-06)."

// invoiceKept is the invoice's register of accepted-only leaves: the forms of its `discounts`,
// and the arguments whose effect shows elsewhere on the invoice. Its create's `discount` form,
// which names a subscription's discount, is not kept: the record's `discounts` does not show it
// returned (the brief of the tenth pin, 2026-11-06).
var invoiceKept = map[string]string{
	"discounts.coupon":                       discountsKept["discounts.coupon"],
	"PostInvoicesInvoice discounts.discount": discountsKept["discounts.discount"],
	"discounts.promotion_code":               discountsKept["discounts.promotion_code"],
	"days_until_due":                         "Its effect shows in `due_date` (the product owner's decision of 2026-11-05).",
	"pending_invoice_items_behavior":         "Its effect shows in `lines` and the amounts, and it lets one apply make a draft with its items (the product owner's decision of 2026-11-05).",
	"subscription":                           "Its effect shows in `parent.subscription_details.subscription` (the product owner's decision of 2026-11-05).",
}

// quoteKept is the quote's register of accepted-only leaves: the forms of its `discounts` and of
// its lines', and the lines' tax rates.
func quoteKept() map[string]string {
	kept := map[string]string{
		"line_items.tax_rates": "Its effect shows on the quote's line items (`stripe_quote_line_item`) (the product owner's decision of 2026-11-05).",
	}
	maps.Copy(kept, discountsKept)
	maps.Copy(kept, lineDiscountsKept("line_items"))
	return kept
}

// subscriptionItemKept is the subscription item's register of accepted-only leaves: the arguments
// that say how Stripe handles the request, and the forms of its `discounts`, which it reads back
// as an invoice does (the brief of the tenth pin, 2026-11-06).
func subscriptionItemKept() map[string]string {
	kept := keptFor(requestBehaviour, []string{"off_session", "payment_behavior", "proration_behavior", "proration_date"})
	maps.Copy(kept, discountsKept)
	return kept
}

// tenthPinDecision cites the product owner's decision on the accepted-only leaves the tables
// exposed before the gate, which the tenth pin's record first classes accepted only.
const tenthPinDecision = " (the product owner's decision of 2026-11-06)"

// subscriptionRequestBehaviour is the reason of a subscription's or a schedule's argument that
// says how Stripe handles the request it comes with, as a subscription item's do.
const subscriptionRequestBehaviour = "It says how Stripe handles the request it comes with, as a subscription item's `proration_behavior` does, so a kept value cannot drift, and the provider keeps it from configuration" + tenthPinDecision + "."

// neededKept is the reason of an argument a configuration cannot work without, with what shows
// its effect.
func neededKept(without, effect string) string {
	return "A configuration cannot " + without + " without it; " + effect + tenthPinDecision + "."
}

// returnedTimestamp is the reason of the string branch of a time Stripe returns as a timestamp.
const returnedTimestamp = "Its effect shows in the timestamp Stripe returns" + tenthPinDecision + "."

// subscriptionUpdateDiscounts is the register's reason for the forms of a subscription update's
// `discounts` and its items', which would make the create-only lists updatable.
const subscriptionUpdateDiscounts = "Exposing the update's forms would make the list updatable, which the provider keeps create-only (D3): Stripe drops a discount that ends, and an update sending the list again, as every change of `items` sends each item's, would redeem the coupon or promotion code anew, an effect a user would not expect of a kept argument (the product owner's decision of 2026-11-07 leaves such leaves out; suite: TestSubscriptionAttributes/leaves/request/PostSubscriptionsSubscriptionExposedId/discounts.coupon)."

// subscriptionKept is the subscription's register of accepted-only leaves, each exposed before
// the gate.
func subscriptionKept() map[string]string {
	kept := keptFor(neededKept("add invoice items to the next invoice", "its effect shows on the invoice items Stripe adds to the subscription's next invoice (`stripe_invoiceitem`), and the provider keeps it from configuration"), []string{
		"add_invoice_items.discountable", "add_invoice_items.metadata", "add_invoice_items.period.end.timestamp",
		"add_invoice_items.period.end.type", "add_invoice_items.period.start.timestamp", "add_invoice_items.period.start.type",
		"add_invoice_items.price", "add_invoice_items.price_data.currency", "add_invoice_items.price_data.product",
		"add_invoice_items.price_data.tax_behavior", "add_invoice_items.price_data.unit_amount",
		"add_invoice_items.price_data.unit_amount_decimal", "add_invoice_items.quantity", "add_invoice_items.tax_rates",
	})
	maps.Copy(kept, keptFor("A configuration cannot discount an invoice item it adds to the next invoice without it; its effect shows on that invoice's line of the added item (suite: TestSubscriptionAttributes, claims `claim/coupon_applied` and `claim/promotion_code_applied`), and the provider keeps it from configuration"+tenthPinDecision+".", []string{
		"add_invoice_items.discounts.coupon", "add_invoice_items.discounts.promotion_code",
	}))
	maps.Copy(kept, keptFor(subscriptionRequestBehaviour, []string{
		"backdate_start_date", "items.clear_usage", "items.deleted", "off_session", "payment_behavior",
		"proration_behavior", "proration_date", "trial_from_plan", "trial_period_days",
	}))
	maps.Copy(kept, map[string]string{
		"cancel_at": returnedTimestamp,
		"PostSubscriptionsSubscriptionExposedId trial_end": returnedTimestamp,
	})
	return kept
}

// scheduleKept is the subscription schedule's register of accepted-only leaves, each exposed
// before the gate.
var scheduleKept = map[string]string{
	"phases.duration.interval":                            neededKept("give a phase without an `end_date` its length", "its effect shows in the phase's `end_date`"),
	"phases.duration.interval_count":                      neededKept("give a phase without an `end_date` its length", "its effect shows in the phase's `end_date`"),
	"phases.items.price":                                  neededKept("send a phase's item of a price in an update of the phases", "its effect shows in the phase item's `price`"),
	"PostSubscriptionSchedulesSchedule phases.start_date": returnedTimestamp,
	"proration_behavior":                                  subscriptionRequestBehaviour,
	"start_date":                                          neededKept("start a schedule on its own", "its effect shows in the first phase's `start_date`"),
}

// Resources lists the objects the generator writes tables for. A resource joins the list
// when its declaration is written.
var Resources = []Resource{
	{Package: "balance", ReadPath: "/v1/balance"},
	{Package: "billing_meter", CreatePath: "/v1/billing/meters", UpdatePath: "/v1/billing/meters/{id}"},
	{Package: "billing_portal_configuration", CreatePath: "/v1/billing_portal/configurations", UpdatePath: "/v1/billing_portal/configurations/{configuration}"},
	{Package: "country_spec", ReadPath: "/v1/country_specs/{country}", Omit: []string{"supported_bank_account_currencies"}},
	// The Omit entry: `include` is a request parameter, not an object field.
	{Package: "event_destination_v2", CreatePath: "/v2/core/event_destinations", UpdatePath: "/v2/core/event_destinations/{id}", Omit: []string{"include"},
		AcceptedOnly: map[string]string{"amazon_eventbridge.aws_region": "Its effect shows in `amazon_eventbridge.aws_event_source_arn`, which names the region."}},
	{Package: "coupon", CreatePath: "/v1/coupons", UpdatePath: "/v1/coupons/{coupon}"},
	{Package: "customer", AcceptedOnly: map[string]string{
		"source":                "The token becomes the customer's default source, read back at `default_source` (the product owner's decision of 2026-11-04).",
		"tax.validate_location": "It says how Stripe checks the location of the request it comes with, so a kept value cannot drift, and the provider keeps it from configuration (the product owner's decision of 2026-11-04).",
	}, CreatePath: "/v1/customers", UpdatePath: "/v1/customers/{customer}", Omit: []string{"sources", "subscriptions"}},
	{Package: "entitlements_feature", CreatePath: "/v1/entitlements/features", UpdatePath: "/v1/entitlements/features/{id}"},
	{Package: "plan", CreatePath: "/v1/plans", UpdatePath: "/v1/plans/{plan}"},
	{Package: "price", AcceptedOnly: priceKept, CreatePath: "/v1/prices", UpdatePath: "/v1/prices/{price}"},
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
	// IDs, which D3 maps back by key once the gate exposes them, as it does an invoice's and an
	// invoice item's (discountsKept).
	{Package: "billing_credit_grant", CreatePath: "/v1/billing/credit_grants", UpdatePath: "/v1/billing/credit_grants/{id}"},
	{Package: "credit_note", AcceptedOnly: map[string]string{
		"credit_amount": "Its effect shows in `amount` (the product owner's decision of 2026-11-05).",
		"refund_amount": "Its effect shows in `amount` and `refunds` (the product owner's decision of 2026-11-05).",
	}, CreatePath: "/v1/credit_notes", UpdatePath: "/v1/credit_notes/{id}", PagedLists: map[string]string{"lines": "stripe_credit_note_line"}},
	// An invoice's payment error is a nested payment intent that repeats without end, and
	// three payment method options are empty objects in the response.
	{Package: "invoice", LeftToStripe: map[string]string{
		"PostInvoices discounts.coupon":         leftExpired,
		"PostInvoices discounts.promotion_code": leftExpired,
		"PostInvoicesInvoice discounts.coupon":  leftCouponBesideHeld,
	}, AcceptedOnly: invoiceKept, Exclude: map[string]string{
		"PostInvoices discounts.discount": invoiceCreateDiscount,
	}, CreatePath: "/v1/invoices", UpdatePath: "/v1/invoices/{invoice}", PagedLists: map[string]string{"lines": "stripe_invoice_line"}, Omit: []string{"discounts", "last_finalization_error", "payment_settings.payment_method_options.blik", "payment_settings.payment_method_options.konbini", "payment_settings.payment_method_options.sepa_debit"}},
	{Package: "invoiceitem", LeftToStripe: map[string]string{
		"PostInvoiceitems discounts.coupon":            leftExpired,
		"PostInvoiceitems discounts.discount":          leftDiscountHeld,
		"PostInvoiceitems discounts.promotion_code":    leftExpired,
		"PostInvoiceitemsInvoiceitem discounts.coupon": leftCouponBesideHeld,
	}, Exclude: map[string]string{
		"PostInvoiceitemsInvoiceitem invoicing_rules": "Its one value, `\"\"`, removes the item's invoicing rules, which Stripe does not let a request set (the spec's description, pin `v2585`): an instruction for one update, not a setting a configuration could keep (suite: TestInvoiceItemAttributes/leaves/request/PostInvoiceitemsInvoiceitem/invoicing_rules).",
	}, AcceptedOnly: map[string]string{
		"pricing.price":            "Its effect shows in `pricing.price_details.price` (the product owner's decision of 2026-11-04).",
		"subscription":             "Its effect shows in `parent.subscription_details.subscription` (the product owner's decision of 2026-11-04).",
		"unit_amount_decimal":      "Its effect shows in `pricing.unit_amount_decimal` (the product owner's decision of 2026-11-04).",
		"discounts.coupon":         discountsKept["discounts.coupon"],
		"discounts.discount":       discountsKept["discounts.discount"],
		"discounts.promotion_code": discountsKept["discounts.promotion_code"],
	}, CreatePath: "/v1/invoiceitems", UpdatePath: "/v1/invoiceitems/{invoiceitem}", Omit: []string{"discounts"}},
	// A quote reads its discounts back as an invoice does, and its lines' from the discounts
	// each line holds whole.
	{Package: "quote", LeftToStripe: map[string]string{
		"PostQuotes discounts.coupon":                    leftExpired,
		"PostQuotes discounts.promotion_code":            leftExpired,
		"PostQuotes line_items.discounts.coupon":         leftExpired,
		"PostQuotes line_items.discounts.promotion_code": leftExpired,
		"PostQuotesQuote discounts.coupon":               leftCouponBesideHeld,
		"PostQuotesQuote line_items.discounts.coupon":    leftCouponBesideHeld,
		"PostQuotesQuote line_items.discounts.discount":  leftDiscountHeld,
	}, AcceptedOnly: quoteKept(), CreatePath: "/v1/quotes", UpdatePath: "/v1/quotes/{quote}", Omit: []string{"discounts"}},
	{Package: "subscription", LeftToStripe: map[string]string{
		"PostSubscriptions add_invoice_items.discounts.coupon":                                                               leftExpired,
		"PostSubscriptions add_invoice_items.discounts.promotion_code":                                                       leftExpired,
		"PostSubscriptions items.discounts.coupon":                                                                           leftExpired,
		"PostSubscriptions items.discounts.promotion_code":                                                                   leftExpired,
		"PostSubscriptionsSubscriptionExposedId add_invoice_items.discounts.discount":                                        leftDiscountHeld,
		"PostSubscriptionsSubscriptionExposedId payment_settings.payment_method_options.customer_balance.bank_transfer.type": "Stripe refused an EU bank transfer sent without `eu_bank_transfer` on a subscription created with a euro price (`price:eur`) and an EU transfer held, and the price's currency is another object's state, which the plan cannot know (probe `isolated/hash_customer_balance_bank_transfer`).",
	}, AcceptedOnly: subscriptionKept(), Exclude: map[string]string{
		"PostSubscriptionsSubscriptionExposedId discounts":            subscriptionUpdateDiscounts,
		"PostSubscriptionsSubscriptionExposedId items.discounts":      subscriptionUpdateDiscounts,
		"PostSubscriptionsSubscriptionExposedId billing_cycle_anchor": "The update's `now` resets the billing cycle anchor at the update that sends it, and `unchanged` keeps it: an instruction for that moment, not a setting, which kept in state would say nothing about the subscription; the create's timestamp, which the table holds, is the setting (suite: TestSubscriptionAttributes/leaves/request/PostSubscriptionsSubscriptionExposedId/billing_cycle_anchor.type).",
	}, CreatePath: "/v1/subscriptions", UpdatePath: "/v1/subscriptions/{subscription_exposed_id}", PagedLists: map[string]string{"items": "stripe_subscription_item"}, Omit: []string{"discounts", "payment_settings.payment_method_options.konbini", "payment_settings.payment_method_options.sepa_debit", "items.discounts"}, CreateOnly: []string{"billing_cycle_anchor"}},
	{Package: "subscription_item", LeftToStripe: map[string]string{
		"PostSubscriptionItems discounts.coupon":         leftExpired,
		"PostSubscriptionItems discounts.discount":       leftDiscountHeld,
		"PostSubscriptionItems discounts.promotion_code": leftExpired,
		"PostSubscriptionItemsItem discounts.coupon":     leftCouponBesideHeld,
	}, AcceptedOnly: subscriptionItemKept(),
		CreatePath: "/v1/subscription_items", UpdatePath: "/v1/subscription_items/{item}", Omit: []string{"discounts"}},
	{Package: "subscription_schedule", LeftToStripe: map[string]string{
		"PostSubscriptionSchedulesSchedule phases.items.price isolated/phases_empty":              leftStartedLater,
		"PostSubscriptionSchedulesSchedule phases.items.price isolated/phases_without_start_date": leftStartedLater,
	}, AcceptedOnly: scheduleKept, CreatePath: "/v1/subscription_schedules", UpdatePath: "/v1/subscription_schedules/{schedule}", Omit: []string{"from_subscription", "pause_schedules"}},
	// Wave 6. The setup intent leaves out what only confirming it uses, and its payment
	// method options, until the gate exposes them. The customer payment method is an
	// attachment, so the engine adds its `customer` as the parent and `customer_account` is
	// offered only as the gate exposes it. The Omit entries of the read-only objects are unions
	// and recursions the tables cannot hold. The ReadOnly entries are the payment method updates
	// the suite does not prove, on an object that cannot be replaced.
	// The Omit entry is the one `price_data` leaf the suite has not proven against Stripe.
	{Package: "payment_link", LeftToStripe: map[string]string{
		"PostPaymentLinks invoice_creation.invoice_data.account_tax_ids": leftMissing,
		"PostPaymentLinks payment_method_collection":                     "Stripe's refusal turns on whether the line items' prices recur, which a `price` names on another object: it refused `always` and `if_required` beside a one-time `price` and took `if_required` beside a recurring `price_data` (probes `isolated/collection_*`).",
		"PostPaymentLinks transfer_data.destination":                     leftMissing,
	}, CreatePath: "/v1/payment_links", UpdatePath: "/v1/payment_links/{payment_link}", Omit: []string{"line_items.price_data.product_data.tax_details"}},
	{Package: "setup_intent", Exclude: map[string]string{
		"mandate_data": mandateData,
		"PostSetupIntents payment_method_options.card.network":        confirmTime("TestSetupIntentAttributes/leaves/request/PostSetupIntents/payment_method_options.card.network"),
		"PostSetupIntents payment_method_options.card.three_d_secure": confirmTime("TestSetupIntentAttributes/leaves/request/PostSetupIntents/payment_method_options.card.three_d_secure.version"),
		"PostSetupIntents return_url":                                 confirmTime("TestSetupIntentAttributes/leaves/request/PostSetupIntents/return_url"),
		"PostSetupIntents use_stripe_sdk":                             confirmTime("TestSetupIntentAttributes/leaves/request/PostSetupIntents/use_stripe_sdk"),
	}, CreatePath: "/v1/setup_intents", UpdatePath: "/v1/setup_intents/{intent}", Omit: []string{"last_setup_error.payment_intent", "last_setup_error.setup_intent", "last_setup_error.source", "confirm", "mandate_data", "payment_method_data", "return_url", "use_stripe_sdk", "single_use", "payment_method_options", "confirmation_token"}},
	{Package: "customer_payment_method", Exclude: map[string]string{"customer": parentHolds}, CreatePath: "/v1/payment_methods/{payment_method}/attach", UpdatePath: "/v1/payment_methods/{payment_method}", Omit: []string{"customer", "customer_account"}, ReadOnly: []string{"allow_redisplay", "card.exp_month", "card.networks", "us_bank_account"}},
	// Confirming is the resource's `status`, so `confirm` is excluded by rule X5, and the Omit
	// entries leave out the confirm-time parameters until the gate exposes them.
	{Package: "payment_intent", LeftToStripe: map[string]string{
		"PostPaymentIntents statement_descriptor":       "Stripe's refusal turns on a `statement_descriptor_suffix` absent from the request, which a pattern of what a request sends cannot state: it took the refused requests once they sent a suffix too (probes `isolated/descriptor_*`), as the declaration's `Answers` words.",
		"PostPaymentIntentsIntent statement_descriptor": "Stripe's refusal turns on a `statement_descriptor_suffix` absent from the request and the intent, which a pattern of what a request sends cannot state: it took the refused update once it sent a suffix too or the intent held one (probes `isolated/descriptor_update_*`), as the declaration's `Answers` words.",
	}, StatusConfirms: true,
		Exclude: map[string]string{
			"mandate_data":                   mandateData,
			"PostPaymentIntents off_session": confirmTime("TestPaymentIntentAttributes/leaves/request/PostPaymentIntents/off_session"),
			"PostPaymentIntents payment_method_options.card.network": confirmTime("TestPaymentIntentAttributes/leaves/request/PostPaymentIntents/payment_method_options.card.network"),
			"PostPaymentIntents radar_options.referrer":              confirmTime("TestPaymentIntentAttributes/leaves/request/PostPaymentIntents/radar_options.referrer"),
			"PostPaymentIntents return_url":                          confirmTime("TestPaymentIntentAttributes/leaves/request/PostPaymentIntents/return_url"),
			"PostPaymentIntents use_stripe_sdk":                      confirmTime("TestPaymentIntentAttributes/leaves/request/PostPaymentIntents/use_stripe_sdk"),
			"payment_method_options.card.three_d_secure":             "Stripe never returns the hash, which its create takes only beside `confirm`, and its update refuses it unless `confirm` is true, with `cryptogram` and `transaction_id`, which it requires, unproven there, so that no configuration could send it (suite: TestPaymentIntentAttributes/leaves/request/PostPaymentIntentsIntent/payment_method_options.card.three_d_secure.version).",
			"payment_method_options.card.installments.plan":          "`type`, which a plan requires, is unproven on the create and verified on the update only by Stripe's refusal, so that no configuration could send the plan (suite: TestPaymentIntentAttributes/leaves/request/PostPaymentIntents/payment_method_options.card.installments.plan.count, refused without `fixed_count`, and PostPaymentIntentsIntent/payment_method_options.card.installments.plan.type, refused unless `confirm` is true).",
		}, CreatePath: "/v1/payment_intents", UpdatePath: "/v1/payment_intents/{intent}", Omit: []string{
			"payment_method_options", "amount_details", "last_payment_error.payment_intent", "last_payment_error.setup_intent", "last_payment_error.source",
			"confirm", "confirmation_token", "error_on_requires_action", "mandate", "mandate_data", "off_session", "payment_method_data", "return_url",
			"use_stripe_sdk", "radar_options",
		}, ReadOnly: []string{"hooks"}},
	// A direct charge (ADR 2026-10-22-money-moving-resources, D6.2). The spec deprecates the
	// create in favour of payment intents, but Stripe serves it. Its deprecated `card` and
	// `destination` are left out; the gate exposes `application_fee`, `fraud_details` and
	// `radar_options`, which `ReadOnly` keeps out of the baseline. Stripe requires `amount` and
	// `currency`, which the spec leaves optional (suite: "missing amount", "missing currency").
	{Package: "charge", AcceptedOnly: map[string]string{
		"capture": "Its effect shows in `captured`.",
		"source":  "The token becomes the charge's card, read back at `source` and `payment_method`; without it a charge needs a customer with a default source.",
	}, CreatePath: "/v1/charges", UpdatePath: "/v1/charges/{charge}", Omit: []string{"card", "destination"}, ReadOnly: []string{"application_fee", "fraud_details", "radar_options"}, Require: []string{"amount", "currency"}},
	{Package: "refund", CreatePath: "/v1/refunds", UpdatePath: "/v1/refunds/{refund}"},
	{Package: "dispute", ReadPath: "/v1/disputes/{dispute}"},
	{Package: "checkout_session", LeftToStripe: map[string]string{
		"PostCheckoutSessions branding_settings.icon.file":  leftMissing,
		"PostCheckoutSessions branding_settings.logo.file":  leftMissing,
		"PostCheckoutSessions payment_method_configuration": "Stripe's refusal turns on the payment methods the configuration it names turns on, which the plan cannot know (probes `isolated/configuration_bancontact` and `isolated/configuration_no_methods`).",
	}, CreatePath: "/v1/checkout/sessions", UpdatePath: "/v1/checkout/sessions/{session}", Omit: []string{"payment_method_options.pay_by_bank"}},
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
	// the hash the spec documents under `bank_account`, or from a token in `external_account`.
	// Its `account` is the parent ID. The Omit entries are the card's update parameters, which
	// the register keeps out, and the documents, which need a file. The account type is refused
	// on a US account; the suite leaves the countries that take one for later.
	{Package: "account_external_account", AcceptedOnly: map[string]string{
		"bank_account.account_number": "Its effect shows in `last4`, and the hash form needs it.",
		"external_account":            "Its effect shows in the bank details Stripe reads from the token.",
	}, Exclude: map[string]string{
		"account":         parentHolds,
		"address_city":    fmt.Sprintf(cardOnly, "address_city"),
		"address_country": fmt.Sprintf(cardOnly, "address_country"),
		"address_line1":   fmt.Sprintf(cardOnly, "address_line1"),
		"address_line2":   fmt.Sprintf(cardOnly, "address_line2"),
		"address_state":   fmt.Sprintf(cardOnly, "address_state"),
		"address_zip":     fmt.Sprintf(cardOnly, "address_zip"),
		"exp_month":       fmt.Sprintf(cardOnly, "exp_month"),
		"exp_year":        fmt.Sprintf(cardOnly, "exp_year"),
		"name":            fmt.Sprintf(cardOnly, "name"),
	}, CreatePath: "/v1/accounts/{account}/external_accounts", UpdatePath: "/v1/accounts/{account}/external_accounts/{id}",
		Response: "bank_account", CreateHash: "bank_account", TokenForm: "external_account",
		Omit:     []string{"external_account", "account", "documents", "address_city", "address_country", "address_line1", "address_line2", "address_state", "address_zip", "exp_month", "exp_year", "name"},
		ReadOnly: []string{"account_type"}},
	{Package: "account_capability", Exclude: map[string]string{"account": parentHolds}, ReadPath: "/v1/accounts/{account}/capabilities/{capability}", Omit: []string{"account"}},
	// Wave 4 batch b Connect. Each table keeps what the conformance suite and the fake prove,
	// among many fields that need files, other countries or onboarding flows. A v1 person's
	// `account` and a v2 person's `account` are the parent the engine adds. The `authorizer`
	// and `legal_guardian` roles and a v1 person's verification documents are read-only: no
	// suite test sets them. A v2 account requests the one capability of each configuration
	// the suite requests; the balance capabilities Stripe adds to it, and whether a
	// configuration is applied, are read-only until the record verifies them. App installs are
	// read-only: no suite test can install an app.
	{Package: "account_person", Exclude: map[string]string{"account": parentHolds}, CreatePath: "/v1/accounts/{account}/persons", UpdatePath: "/v1/accounts/{account}/persons/{person}",
		Keep: []string{"address", "created", "dob", "email", "first_name", "future_requirements", "id", "id_number", "id_number_provided", "last_name", "metadata", "phone",
			"relationship", "requirements", "ssn_last_4", "ssn_last_4_provided", "verification"},
		ReadOnly: []string{"relationship.authorizer", "relationship.legal_guardian", "verification"}},
	{Package: "account_v2", LeftToStripe: map[string]string{
		"PostV2CoreAccounts defaults.profile.business_url":                                                    leftBusinessURL,
		"PostV2CoreAccountsId configuration.recipient.capabilities.stripe_balance.stripe_transfers.requested": "Stripe's refusal turns on `dashboard` and the collectors of `defaults.responsibilities` being absent from the request, which a pattern of what a request sends cannot state: it took the same request beside them (probes `isolated/change_adds_recipient` and `isolated/change_adds_recipient_with_collectors`), as the declaration's `Answers` words.",
		"PostV2CoreAccountsId defaults.profile.business_url":                                                  leftBusinessURL,
	},
		CreatePath: "/v2/core/accounts", UpdatePath: "/v2/core/accounts/{id}",
		Keep: []string{"applied_configurations", "closed",
			"configuration.customer.applied", "configuration.customer.capabilities.automatic_indirect_tax",
			"configuration.merchant.applied", "configuration.merchant.capabilities.card_payments", "configuration.merchant.capabilities.stripe_balance.payouts",
			"configuration.recipient.applied", "configuration.recipient.capabilities.stripe_balance.payouts", "configuration.recipient.capabilities.stripe_balance.stripe_transfers",
			"contact_email", "created", "dashboard", "defaults.currency", "defaults.responsibilities", "display_name", "future_requirements", "id",
			"identity.business_details.registered_name", "identity.country", "identity.entity_type", "livemode", "metadata", "requirements"},
		ReadOnly: []string{"configuration.customer.applied", "configuration.merchant.applied", "configuration.recipient.applied",
			"configuration.merchant.capabilities.stripe_balance", "configuration.recipient.capabilities.stripe_balance.payouts"}},
	{Package: "account_person_v2", Exclude: map[string]string{"account": parentHolds}, CreatePath: "/v2/core/accounts/{account_id}/persons", UpdatePath: "/v2/core/accounts/{account_id}/persons/{id}",
		Keep: []string{"address.city", "address.country", "address.line1", "address.line2", "address.postal_code", "address.state", "created", "date_of_birth", "email",
			"given_name", "id", "id_numbers", "livemode", "metadata", "phone", "relationship", "surname", "updated"},
		ReadOnly: []string{"relationship.authorizer", "relationship.legal_guardian"}},
	{Package: "apps_install", ReadPath: "/v1/apps/installs/{id}"},
	// Wave 8 batch a. A billing alert is never updated: its status moves by action endpoints
	// the declaration names. A tax ID's `owner.account` and `owner.customer_account` were out of
	// reach until the record verified them, and the gate exposes them. The customer's balance
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
	{Package: "test_helpers_test_clock", Exclude: map[string]string{
		"customer": "With a `customer`, Stripe freezes the clock at its `created`, not at the configured `frozen_time`, which the table requires (the record's rule on `frozen_time`), so the argument would change what another argument does on every create, a surprise for a kept value (the product owner's decision of 2026-11-07 leaves such leaves out; suite: TestTestClockAttributes/leaves/request/PostTestHelpersTestClocks/customer).",
	}, CreatePath: "/v1/test_helpers/test_clocks", Omit: []string{"customer"}},
	{Package: "payout", CreatePath: "/v1/payouts", UpdatePath: "/v1/payouts/{payout}"},
	// The create's `source` is the ID of a legacy source, which the suite has not proven; the
	// response's is the source object, which stays readable.
	// Stripe returns a top-up's `source` as the whole Source, whose ID the table holds; no data
	// source reads a Source, so rule X7 cannot leave its members to one (decided by the product
	// owner, 2026-11-12).
	{Package: "topup", CreatePath: "/v1/topups", UpdatePath: "/v1/topups/{topup}", ReadOnly: []string{"source"},
		Exclude: map[string]string{"source.*": "The Source Stripe returns whole at `source`, whose ID the provider holds: Stripe has deprecated the Sources API (\"We've deprecated the Sources API and plan to remove support.\", https://docs.stripe.com/sources), and no data source reads a Source."}},
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
	// Wave 7 batch b. Issuing and Treasury run on a connected account. The card was virtual: the
	// Omit entries are the physical card's shipping, PIN, personalization design and second line,
	// of which the gate exposes what the record verifies, all but the PIN. Stripe refuses a card
	// without a cardholder (suite: "create errors"), although the spec leaves it optional.
	// A financial account's `features` is a hash of `requested` flags on the way in and of
	// feature statuses, returned only when expanded, on the way out, which the gate exposes as
	// the record verifies it, and `forwarding_settings`, which Stripe takes only on a closed
	// account, keeps out the bank account form the suite cannot reach. The Treasury flows and Financial
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
	// The rest of Issuing. A design's `transfer_lookup_key` and `preferences`, and a dispute's
	// `treasury`, were out of reach before the record verified them, and the gate exposes them as
	// it exposes any leaf a table leaves out (E2). A dispute's `treasury` names the received debit
	// Stripe disputes in place of a `transaction` (suite: TestIssuingDisputeAttributes, variant
	// `received_debit`, which sends no `transaction`), so neither is required, and the declaration
	// takes exactly one. Stripe refuses a dispute without `evidence` and evidence without its
	// `reason` (suite: "Issuing disputes (first run)"), although the spec leaves both optional.
	// Authorizations, transactions, physical bundles and tokens are read-only objects.
	{Package: "issuing_personalization_design", CreatePath: "/v1/issuing/personalization_designs", UpdatePath: "/v1/issuing/personalization_designs/{personalization_design}", Omit: []string{"transfer_lookup_key"}, ReadOnly: []string{"preferences"}},
	{Package: "issuing_dispute", CreatePath: "/v1/issuing/disputes", UpdatePath: "/v1/issuing/disputes/{dispute}", ReadOnly: []string{"treasury"}, Require: []string{"evidence", "evidence.reason"}},
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
