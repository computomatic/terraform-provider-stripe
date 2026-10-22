// Package quote_computed_upfront_line_item declares the stripe_quote_computed_upfront_line_item data source. There is no resource.
package quote_computed_upfront_line_item

import (
	"github.com/hashicorp/terraform-plugin-framework/datasource"

	"github.com/computomatic/terraform-provider-stripe/internal/engine"
	"github.com/computomatic/terraform-provider-stripe/internal/generated/quote_computed_upfront_line_item"
)

// Declaration is what the spec cannot say about this object. Stripe has no retrieve for a computed upfront line item, only the list under the quote, so the data source finds the item by `id` in that list. Stripe computes these from the quote.
var Declaration = engine.Declaration{
	TypeName:    "stripe_quote_computed_upfront_line_item",
	IDPrefix:    "li_",
	Description: "A line item of the amount a quote bills upfront, as Stripe computes it from the quote's line items. Look one up by the `quote` and the item's `id`.",
	Parent:      "quote",
	FindInList:  true,
	Paths:       engine.Paths{List: "/v1/quotes/{quote}/computed_upfront_line_items"},
	Delete:      engine.NoDelete,
}

// NewDataSource returns the data source constructor the provider registers. It needs the `quote` and the `id` of the item, which it finds in the quote's list.
func NewDataSource() (func() datasource.DataSource, error) {
	return engine.NewDataSource(Declaration, quote_computed_upfront_line_item.Table)
}
