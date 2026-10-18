// Package balance_transaction declares the stripe_balance_transaction data source: a movement of funds on the account's balance. There is no resource.
package balance_transaction

import (
	"github.com/hashicorp/terraform-plugin-framework/datasource"

	"github.com/computomatic/terraform-provider-stripe/internal/engine"
	"github.com/computomatic/terraform-provider-stripe/internal/generated/balance_transaction"
)

// Declaration is what the spec cannot say about a balance transaction.
var Declaration = engine.Declaration{
	TypeName:    "stripe_balance_transaction",
	IDPrefix:    "txn_",
	Description: "A movement of funds on the account's balance. Look one up by its `id`.",
	Paths:       engine.Paths{Retrieve: "/v1/balance_transactions/{id}"},
	Delete:      engine.NoDelete,
}

// NewDataSource returns the data source constructor the provider registers. It looks a balance transaction up by `id` only.
func NewDataSource() (func() datasource.DataSource, error) {
	return engine.NewDataSource(Declaration, balance_transaction.Table)
}
