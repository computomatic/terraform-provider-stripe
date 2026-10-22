// Package tax_transaction declares the stripe_tax_transaction data source. There is no resource.
package tax_transaction

import (
	"github.com/hashicorp/terraform-plugin-framework/datasource"

	"github.com/computomatic/terraform-provider-stripe/internal/engine"
	"github.com/computomatic/terraform-provider-stripe/internal/generated/tax_transaction"
)

// Declaration is what the spec cannot say about this object. A transaction is recorded from a tax calculation, which is a request a checkout makes rather than an object to manage, and it cannot be deleted (suite: "Tax transactions from a calculation ignore an unknown line item cursor"), so only the data source exists. Stripe has no list of transactions, so `reference` cannot be a lookup key.
var Declaration = engine.Declaration{
	TypeName:    "stripe_tax_transaction",
	IDPrefix:    "tax_",
	Description: "A record of tax collected on a sale, or of a reversal of one, which Stripe Tax reports. Look one up by its `id`. Read its line items with the `stripe_tax_transaction_line_item` data source.",
	Paths:       engine.Paths{Retrieve: "/v1/tax/transactions/{id}"},
	Delete:      engine.NoDelete,
}

// NewDataSource returns the data source constructor the provider registers. It looks the object up by `id` only: Stripe has no list to filter by `reference`.
func NewDataSource() (func() datasource.DataSource, error) {
	return engine.NewDataSource(Declaration, tax_transaction.Table)
}
