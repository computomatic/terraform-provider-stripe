// Package transfer declares the stripe_transfer resource and data source.
package transfer

import (
	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/resource"

	"github.com/computomatic/terraform-provider-stripe/internal/engine"
	"github.com/computomatic/terraform-provider-stripe/internal/generated/transfer"
)

// Declaration is what the spec cannot say about a transfer: money a platform moves from its
// balance to a connected account (ADR 2026-10-22-money-moving-resources, D6.9). It moves money,
// so the live-mode guard applies. `description` and `metadata` update (suite: "update");
// `amount`, `destination` and `currency` cannot change (suite: "immutable"). Stripe cannot
// delete a transfer, and a reversal is a new object, so destroy is refused.
var Declaration = engine.Declaration{
	TypeName:              "stripe_transfer",
	IDPrefix:              "tr_",
	Description:           "Money moved from the platform's balance to a connected account. Only `description` and `metadata` change after creation. Reverse a transfer with `stripe_transfer_reversal`.",
	DataSourceDescription: "Money moved from the platform's balance to a connected account. Read one of its reversals with the `stripe_transfer_reversal` data source.",
	Paths: engine.Paths{
		Create:   "/v1/transfers",
		Retrieve: "/v1/transfers/{id}",
		Update:   "/v1/transfers/{id}",
		List:     "/v1/transfers",
	},
	MovesMoney: true,
	// Stripe names the balance the transfer came from when the request leaves it out.
	ServerDefaulted: []string{"source_type"},
	Delete:          engine.Refuse,
	RefuseReason:    "Stripe cannot delete a transfer; reverse it with a `stripe_transfer_reversal`, which destroy does not create.",
}

// NewResource returns the resource constructor the provider registers.
func NewResource() (func() resource.Resource, error) {
	return engine.NewResource(Declaration, transfer.Table)
}

// NewDataSource returns the data source constructor. It looks a transfer up by `id` only: Stripe documents no field of it as unique.
func NewDataSource() (func() datasource.DataSource, error) {
	return engine.NewDataSource(Declaration, transfer.Table)
}
