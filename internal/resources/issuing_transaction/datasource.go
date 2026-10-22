// Package issuing_transaction declares the stripe_issuing_transaction data source. There is no resource.
package issuing_transaction

import (
	"github.com/hashicorp/terraform-plugin-framework/datasource"

	"github.com/computomatic/terraform-provider-stripe/internal/engine"
	"github.com/computomatic/terraform-provider-stripe/internal/generated/issuing_transaction"
)

// Declaration is what the spec cannot say about an Issuing transaction: a capture or a refund
// on an Issuing card, which the card network makes, or Stripe's test helpers (suite: "Issuing
// transactions (first run)"). Captures are negative and refunds positive. Stripe retrieves it
// by its ID, and no field of it is unique.
var Declaration = engine.Declaration{
	TypeName:    "stripe_issuing_transaction",
	IDPrefix:    "ipi_",
	Description: "A capture or refund of money spent with an Issuing card: captures are negative amounts and refunds positive. It lives on a connected account, so read it through a provider configuration whose `stripe_account` is that account. Look one up by its `id`.",
	Paths:       engine.Paths{Retrieve: "/v1/issuing/transactions/{id}", List: "/v1/issuing/transactions"},
	Delete:      engine.NoDelete,
}

// NewDataSource returns the data source constructor the provider registers. It looks a transaction up by `id` only: Stripe documents no field of it as unique.
func NewDataSource() (func() datasource.DataSource, error) {
	return engine.NewDataSource(Declaration, issuing_transaction.Table)
}
