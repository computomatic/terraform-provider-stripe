// Package checkout_session declares the stripe_checkout_session resource and data source.
package checkout_session

import (
	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/resource"

	"github.com/computomatic/terraform-provider-stripe/internal/engine"
	"github.com/computomatic/terraform-provider-stripe/internal/generated/checkout_session"
)

// Declaration is what the spec cannot say about a checkout session (ADR
// 2026-10-22-money-moving-resources, D6.6). It takes payments, so the live-mode guard applies.
// Stripe cannot delete one, so destroy is refused. `status` expires an open session (suite:
// "expire"); a completed one cannot be expired (suite: "already expired"). `metadata` and
// `shipping_options` are proven to update ("update metadata", "update shipping options"), and
// `collected_information` on a session of the `elements` UI mode, which is the only one that takes
// it (TestCheckoutSessionAttributes, base `elements`; "update errors" for the hosted page). What a
// customer does on the page happens only in a browser, so the record holds it for Stripe alone
// (`gap_stripe_only`): the resource reads `status`, `payment_status` and `customer_details` and
// sends none of them but `status = "expired"`.
var Declaration = engine.Declaration{
	TypeName:              "stripe_checkout_session",
	IDPrefix:              "cs_",
	Description:           "A customer's session on a Stripe-hosted checkout page. Set `status` to `expired` to expire an open session. Stripe expires a session by itself at `expires_at`, 24 hours after creation unless set, and completes it when the customer pays. The provider does not manage what a customer does on the page: `status`, `payment_status` and `customer_details` hold what Stripe returns at the last refresh, and a session completed or expired outside Terraform plans nothing unless the configuration sets `status`. Only `metadata`, `shipping_options` and, on a session whose `ui_mode` is `elements`, `collected_information` change after creation. The `client_secret` is sensitive.",
	DataSourceDescription: "A customer's session on a Stripe-hosted checkout page. The session's `line_items` are included.",
	Paths: engine.Paths{
		Create:   "/v1/checkout/sessions",
		Retrieve: "/v1/checkout/sessions/{id}",
		Update:   "/v1/checkout/sessions/{id}",
		List:     "/v1/checkout/sessions",
	},
	MovesMoney: true,
	// Stripe returns the line items and the optional items only when asked for (suite: "Checkout
	// sessions", and the recipe's `Expand`).
	Expand: []string{"line_items", "optional_items"},
	// The client secret lets a front end finish the checkout.
	Sensitive: []string{"client_secret"},
	// Stripe fills these in from the mode and the account when the request leaves them out
	// (suite: "create minimal"), makes the price of an item that sets `price_data` (variant
	// `inline_product`), answers a subscription with `saved_payment_method_options` (variant
	// `subscription`), and fills the rest of a hash the request sends in part (`nested_minimal/2`,
	// `nested_minimal/14`, `nested_minimal/20` and `enums/47`).
	ServerDefaulted: []string{
		"adaptive_pricing", "branding_settings", "consent_collection.promotions", "consent_collection.terms_of_service", "currency",
		"customer_creation", "invoice_creation", "line_items.price", "managed_payments", "optional_items.adjustable_quantity.maximum",
		"optional_items.adjustable_quantity.minimum", "payment_method_collection", "payment_method_options",
		"payment_method_options.acss_debit.verification_method", "payment_method_options.card",
		"payment_method_options.us_bank_account.verification_method", "phone_number_collection", "redirect_on_completion",
		"saved_payment_method_options", "saved_payment_method_options.allow_redisplay_filters",
		"saved_payment_method_options.payment_method_remove", "tax_id_collection", "ui_mode",
	},
	// Stripe fills these members of a hash the request sends in part (suite:
	// TestCheckoutSessionAttributes, probes `nested_minimal/2` and `enums/35`, `enums/47` and
	// `enums/51`), and makes a rate from an option's data (`enums/23`); the record's none is of a
	// create that leaves the hash out.
	Defaults: map[string]string{
		"after_expiration.recovery.allow_promotion_codes":            "`false` once `recovery` is set",
		"consent_collection.promotions":                              "`none` once `consent_collection` is set",
		"consent_collection.terms_of_service":                        "`none` once `consent_collection` is set",
		"name_collection.business.optional":                          "`false` once `name_collection.business` is set",
		"name_collection.individual.optional":                        "`false` once `name_collection.individual` is set",
		"optional_items.adjustable_quantity.maximum":                 "`99` once `adjustable_quantity` is set",
		"optional_items.adjustable_quantity.minimum":                 "`0` once `adjustable_quantity` is set",
		"payment_method_options.acss_debit.verification_method":      "`automatic` once `acss_debit` is set",
		"payment_method_options.boleto.expires_after_days":           "`3` once `boleto` is set",
		"payment_method_options.us_bank_account.verification_method": "`automatic` once `us_bank_account` is set",
		"shipping_options.shipping_rate":                             "none, or the rate Stripe makes from `shipping_rate_data`",
		"tax_id_collection.required":                                 "`never` once `tax_id_collection` is set",
	},
	// The spec's update takes them, but Stripe refuses any change of a hosted session's line
	// items (suite: "update errors" / "line items").
	ReplaceOn:    []string{"line_items"},
	Delete:       engine.Refuse,
	RefuseReason: "Stripe cannot delete a checkout session; set `status` to `expired` to expire an open one.",
	Transitions: []engine.Transition{
		{Field: "status", Value: "expired", Path: "/v1/checkout/sessions/{id}/expire", From: []string{"open"}, Final: []string{"complete"}},
	},
}

// NewResource returns the resource constructor the provider registers.
func NewResource() (func() resource.Resource, error) {
	return engine.NewResource(Declaration, checkout_session.Table)
}

// NewDataSource returns the data source constructor. It looks the object up by `id` only: Stripe documents no field of it as unique.
func NewDataSource() (func() datasource.DataSource, error) {
	return engine.NewDataSource(Declaration, checkout_session.Table)
}
