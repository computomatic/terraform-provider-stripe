// Package checkout_session_line_item declares the stripe_checkout_session_line_item data
// source. There is no resource.
package checkout_session_line_item

import (
	"github.com/hashicorp/terraform-plugin-framework/datasource"

	"github.com/computomatic/terraform-provider-stripe/internal/engine"
	"github.com/computomatic/terraform-provider-stripe/internal/generated/checkout_session_line_item"
)

// Declaration is what the spec cannot say about this object. Stripe has no retrieve for a checkout session's line item, only the list under the session (suite: "Checkout sessions"), so the data source finds the item by `id` in that list. Line items are set when the session is created. The parent is named `checkout_session`, as Stripe names a session elsewhere, although the path calls it `session`.
var Declaration = engine.Declaration{
	TypeName:    "stripe_checkout_session_line_item",
	IDPrefix:    "li_",
	Description: "A line item of a Checkout Session. Look one up by the `checkout_session` and the item's `id`.",
	Parent:      "checkout_session",
	FindInList:  true,
	Paths:       engine.Paths{List: "/v1/checkout/sessions/{checkout_session}/line_items"},
	Delete:      engine.NoDelete,
}

// NewDataSource returns the data source constructor the provider registers. It needs the `checkout_session` and the `id` of the item, which it finds in the session's list. Stripe documents no item field as unique, and the list filters by none.
func NewDataSource() (func() datasource.DataSource, error) {
	return engine.NewDataSource(Declaration, checkout_session_line_item.Table)
}
