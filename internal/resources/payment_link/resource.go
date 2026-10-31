// Package payment_link declares the stripe_payment_link resource and data source.
package payment_link

import (
	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/resource"

	"github.com/computomatic/terraform-provider-stripe/internal/engine"
	"github.com/computomatic/terraform-provider-stripe/internal/generated/payment_link"
	"github.com/computomatic/terraform-provider-stripe/internal/wire"
)

// Declaration is what the spec cannot say about a payment link. Stripe cannot delete one
// (suite: "Payment link errors", `DELETE` is an unrecognized URL), so destroy deactivates it
// (suite: "deactivate and reactivate") and drops it from state; the link stays in the account
// and its URL stops taking payments.
var Declaration = engine.Declaration{
	TypeName:              "stripe_payment_link",
	IDPrefix:              "plink_",
	Description:           "A shareable link to a Stripe-hosted page that takes a payment. `terraform destroy` deactivates the link (`active = false`), because Stripe cannot delete one: the link stays in the account and stops taking payments. `active` is an ordinary argument, so a link can be deactivated and activated again. `line_items` are matched by `price`, never by position: a reorder changes nothing, and a changed `quantity` updates the item in place. Each item names a `price`, or sets `price_data` to make one with the link; Stripe then fills in `price`, and the item is matched by its `price_data`. Adding a price, dropping one or changing an item's other settings replaces the link, and so does a change of `currency`, `managed_payments`, `optional_items` or a member Stripe takes only on creation, or removing a hash Stripe cannot unset. Other changes apply in place. A replaced link has a new `url`. A price may appear once. A link takes at most 20 line items, and the provider reads at most the first 10 of them: a link with more is an error.",
	DataSourceDescription: "A shareable link to a Stripe-hosted page that takes a payment.",
	ImportNote:            "Stripe returns the price of a line item, never the `price_data` that made it, so an imported link holds `price` alone. A configuration that keeps `price_data` plans a replacement: name the `price` instead.",
	Paths: engine.Paths{
		Create:   "/v1/payment_links",
		Retrieve: "/v1/payment_links/{id}",
		Update:   "/v1/payment_links/{id}",
		List:     "/v1/payment_links",
	},
	// Stripe returns the line items and the optional items only when asked for (suite: "create
	// minimal", and the recipe's `Expand`).
	Expand: []string{"line_items", "optional_items"},
	// Stripe fills these in when the request leaves them out (suite: "create minimal"), makes the
	// price of an item that sets `price_data` (variant `inline_product`), answers a link with a
	// transfer or a recurring price with the two data hashes (variants `destination` and
	// `subscription`), and fills the rest of a hash the request sends in part.
	ServerDefaulted: []string{
		"automatic_tax.liability", "consent_collection.promotions", "consent_collection.terms_of_service", "invoice_creation",
		"invoice_creation.invoice_data.issuer", "line_items.price", "managed_payments", "optional_items.adjustable_quantity.maximum",
		"optional_items.adjustable_quantity.minimum", "payment_intent_data", "subscription_data", "subscription_data.trial_settings",
	},
	// Stripe names the liability of a link whose automatic tax it turns on: the fake's update of
	// `automatic_tax.enabled`, from the recipe's `self` liability that sends no account.
	FilledFrom: map[string][]string{"automatic_tax.enabled": {"automatic_tax.liability"}},
	// Stripe fills these members of a hash the request sends in part (suite: TestPaymentLinkAttributes,
	// probes `nested_minimal/1` and `nested_minimal/11`); the record's none is of a create that leaves
	// the hash out.
	Defaults: map[string]string{
		"consent_collection.promotions":              "`none` once `consent_collection` is set",
		"consent_collection.terms_of_service":        "`none` once `consent_collection` is set",
		"name_collection.business.optional":          "`false` once `name_collection.business` is set",
		"name_collection.individual.optional":        "`false` once `name_collection.individual` is set",
		"optional_items.adjustable_quantity.maximum": "`99` once `adjustable_quantity` is set",
		"optional_items.adjustable_quantity.minimum": "`0` once `adjustable_quantity` is set",
	},
	// Line items are matched by price, or by `price_data` where Stripe makes the price. The suite
	// proves only a quantity change on an item named by its `id`; adding, removing or repricing
	// an item is not proven, so it replaces the link. Line items cannot exist outside a payment
	// link, so the resource manages them all.
	MatchBy:   map[string]string{"line_items": "price"},
	MatchData: map[string]string{"line_items": "price_data"},
	FixedKeys: []string{"line_items"},
	// `adjustable_quantity[minimum]` and `[maximum]` are verified on an update only by Stripe's
	// refusal (TestPaymentLinkAttributes/leaves/request/PostPaymentLinksPaymentLink/
	// line_items.adjustable_quantity.minimum and optional_items.adjustable_quantity.minimum), and
	// an update sends `optional_items` whole, so a list with bounds is not proven either.
	ReplaceOn: []string{"line_items.adjustable_quantity", "optional_items"},
	// Stripe answers an empty string for these with `cannot be unset`, or the record shows no
	// unset of them, so a removal replaces the link as it always has.
	ReplaceOnRemoval: []string{
		"consent_collection", "consent_collection.payment_method_reuse_agreement", "custom_text.after_submit", "custom_text.shipping_address",
		"custom_text.submit", "invoice_creation.invoice_data", "invoice_creation.invoice_data.account_tax_ids",
		"invoice_creation.invoice_data.custom_fields", "invoice_creation.invoice_data.rendering_options", "name_collection",
		"payment_method_options", "payment_method_types", "restrictions", "shipping_address_collection", "transfer_data",
	},
	// Stripe keeps the members an update of these leaves out (fake v0.30.0, `hashes` of
	// internal/resources/paymentlink, from the suite's update/5, update/9 and update/11) and
	// replaces their `metadata` whole, so a removed member has to be sent as null. The record's
	// `families[].hashes` does not list them, so they go whole and not in part.
	SendWholeMembers: []string{"consent_collection", "invoice_creation", "payment_intent_data", "subscription_data", "transfer_data"},
	Delete:           engine.Archive,
	Archive: &engine.ArchiveAction{
		Field: "active",
		Value: wire.Bool(false),
	},
}

// NewResource returns the resource constructor the provider registers.
func NewResource() (func() resource.Resource, error) {
	return engine.NewResource(Declaration, payment_link.Table)
}

// NewDataSource returns the data source constructor. Stripe documents no payment link field as unique, so the data source looks up by `id` only.
func NewDataSource() (func() datasource.DataSource, error) {
	return engine.NewDataSource(Declaration, payment_link.Table)
}
