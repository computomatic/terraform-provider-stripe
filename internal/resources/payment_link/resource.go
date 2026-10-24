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
	Description:           "A shareable link to a Stripe-hosted page that takes a payment. `terraform destroy` deactivates the link (`active = false`), because Stripe cannot delete one: the link stays in the account and stops taking payments. `active` is an ordinary argument, so a link can be deactivated and activated again. `line_items` are matched by `price`, never by position: a reorder changes nothing, and a changed `quantity` updates the item in place. Adding a price, dropping one or changing an item's other settings replaces the link, and so does a change to any argument other than `active`, `allow_promotion_codes`, `billing_address_collection`, `submit_type`, `metadata` and the quantity of a line item. A replaced link has a new `url`. A price may appear once. A line item names its `price`: `price_data`, which would create the price with the link, is not available, because an item without a price of its own cannot be told from another when Stripe returns them, so create the price with `stripe_price` first. A link takes at most 20 line items, and the provider reads at most the first 10 of them: a link with more is an error.",
	DataSourceDescription: "A shareable link to a Stripe-hosted page that takes a payment.",
	Paths: engine.Paths{
		Create:   "/v1/payment_links",
		Retrieve: "/v1/payment_links/{id}",
		Update:   "/v1/payment_links/{id}",
		List:     "/v1/payment_links",
	},
	// Stripe returns the line items only when asked for (suite: "create minimal").
	Expand: []string{"line_items"},
	// Stripe fills these in when the request leaves them out (suite: "create minimal").
	ServerDefaulted: []string{"invoice_creation", "managed_payments"},
	// Line items are matched by price. The suite proves only a quantity change on an item named
	// by its `id`; adding, removing or repricing an item is not proven, so it replaces the link.
	// Line items cannot exist outside a payment link, so the resource manages them all.
	MatchBy:   map[string]string{"line_items": "price"},
	FixedKeys: []string{"line_items"},
	// The suite proves updates of `active`, `allow_promotion_codes`, `billing_address_collection`,
	// `submit_type`, `metadata` and the quantity of a line item ("update", "update submit_type",
	// "update line item quantity"). The other fields the spec takes on update are validated or
	// refused (suite: "update on_behalf_of is validated" and the tests that follow it) but no
	// successful update is proven, so changing one replaces the link.
	ReplaceOn: []string{
		"after_completion", "application_fee_amount", "application_fee_percent", "automatic_tax",
		"consent_collection", "custom_fields", "custom_text", "customer_creation", "inactive_message",
		"invoice_creation", "line_items.adjustable_quantity", "name_collection", "on_behalf_of", "optional_items",
		"payment_intent_data", "payment_method_collection", "payment_method_options", "payment_method_types",
		"phone_number_collection", "restrictions", "shipping_address_collection", "shipping_options",
		"subscription_data", "tax_id_collection", "transfer_data",
	},
	Delete: engine.Archive,
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
