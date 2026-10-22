// Package payment_intent_amount_details_line_item declares the stripe_payment_intent_amount_details_line_item data source. There is no resource.
package payment_intent_amount_details_line_item

import (
	"github.com/hashicorp/terraform-plugin-framework/datasource"

	"github.com/computomatic/terraform-provider-stripe/internal/engine"
	"github.com/computomatic/terraform-provider-stripe/internal/generated/payment_intent_amount_details_line_item"
)

// Declaration is what the spec cannot say about this object. Stripe has no retrieve for a line item, only the list under its payment intent (suite: "Payment intent line items"), so the data source finds the item by `id` in that list. Line items are made by the payment intent.
var Declaration = engine.Declaration{
	TypeName:    "stripe_payment_intent_amount_details_line_item",
	IDPrefix:    "uli_",
	Description: "A line item of a payment intent's amount details. Look one up by the `payment_intent` and the item's `id`.",
	Parent:      "payment_intent",
	FindInList:  true,
	Paths:       engine.Paths{List: "/v1/payment_intents/{payment_intent}/amount_details_line_items"},
	Delete:      engine.NoDelete,
}

// NewDataSource returns the data source constructor the provider registers. It needs the `payment_intent` and the `id` of the line item, which it finds in the intent's list.
func NewDataSource() (func() datasource.DataSource, error) {
	return engine.NewDataSource(Declaration, payment_intent_amount_details_line_item.Table)
}
