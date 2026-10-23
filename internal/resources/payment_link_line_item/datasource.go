// Package payment_link_line_item declares the stripe_payment_link_line_item data source. There
// is no resource.
package payment_link_line_item

import (
	"github.com/hashicorp/terraform-plugin-framework/datasource"

	"github.com/computomatic/terraform-provider-stripe/internal/engine"
	"github.com/computomatic/terraform-provider-stripe/internal/generated/payment_link_line_item"
)

// Declaration is what the spec cannot say about this object. Stripe has no retrieve for a payment link's line item, only the list under the link (suite: "Payment links"), so the data source finds the item by `id` in that list. Line items are managed on the `stripe_payment_link`.
var Declaration = engine.Declaration{
	TypeName:    "stripe_payment_link_line_item",
	IDPrefix:    "li_",
	Description: "A line item of a payment link. Look one up by the `payment_link` and the item's `id`.",
	Parent:      "payment_link",
	FindInList:  true,
	Paths:       engine.Paths{List: "/v1/payment_links/{payment_link}/line_items"},
	Delete:      engine.NoDelete,
}

// NewDataSource returns the data source constructor the provider registers. It needs the `payment_link` and the `id` of the item, which it finds in the link's list. Stripe documents no item field as unique, and the list filters by none.
func NewDataSource() (func() datasource.DataSource, error) {
	return engine.NewDataSource(Declaration, payment_link_line_item.Table)
}
