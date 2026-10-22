// Package tax_transaction_line_item declares the stripe_tax_transaction_line_item data source. There is no resource.
package tax_transaction_line_item

import (
	"github.com/hashicorp/terraform-plugin-framework/datasource"

	"github.com/computomatic/terraform-provider-stripe/internal/engine"
	"github.com/computomatic/terraform-provider-stripe/internal/generated/tax_transaction_line_item"
)

// Declaration is what the spec cannot say about this object. Stripe has no retrieve for a line item, only the list under its transaction (suite: "Tax transactions from a calculation ignore an unknown line item cursor"), so the data source finds the item by `id` in that list. Line items are made by the transaction.
var Declaration = engine.Declaration{
	TypeName:    "stripe_tax_transaction_line_item",
	IDPrefix:    "tax_li_",
	Description: "A line item of a tax transaction. Look one up by the `transaction` and the item's `id`.",
	Parent:      "transaction",
	FindInList:  true,
	Paths:       engine.Paths{List: "/v1/tax/transactions/{transaction}/line_items"},
	Delete:      engine.NoDelete,
}

// NewDataSource returns the data source constructor the provider registers. It needs the `transaction` and the `id` of the line item, which it finds in the transaction's list.
func NewDataSource() (func() datasource.DataSource, error) {
	return engine.NewDataSource(Declaration, tax_transaction_line_item.Table)
}
