// Package transfer declares the stripe_transfer data source. There is no resource.
package transfer

import (
	"github.com/hashicorp/terraform-plugin-framework/datasource"

	"github.com/computomatic/terraform-provider-stripe/internal/engine"
	"github.com/computomatic/terraform-provider-stripe/internal/generated/transfer"
)

// Declaration is what the spec cannot say about a transfer: money a platform moves from its
// available balance to a connected account (suite: "Transfers"). It is read-only for now.
// Stripe retrieves it by its ID, and its list filters by `destination`, `created` and
// `transfer_group`, none of which is unique. A transfer embeds the first page of its
// reversals.
var Declaration = engine.Declaration{
	TypeName:    "stripe_transfer",
	IDPrefix:    "tr_",
	Description: "Money moved from the platform's balance to a connected account. Look one up by its `id`. Read one of its reversals with the `stripe_transfer_reversal` data source.",
	Paths:       engine.Paths{Retrieve: "/v1/transfers/{id}", List: "/v1/transfers"},
	Delete:      engine.NoDelete,
}

// NewDataSource returns the data source constructor the provider registers. It looks a transfer up by `id` only: Stripe documents no field of it as unique.
func NewDataSource() (func() datasource.DataSource, error) {
	return engine.NewDataSource(Declaration, transfer.Table)
}
