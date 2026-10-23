// Package transfer_reversal declares the stripe_transfer_reversal data source. There is no
// resource.
package transfer_reversal

import (
	"github.com/hashicorp/terraform-plugin-framework/datasource"

	"github.com/computomatic/terraform-provider-stripe/internal/engine"
	"github.com/computomatic/terraform-provider-stripe/internal/generated/transfer_reversal"
)

// Declaration is what the spec cannot say about a transfer reversal: money moved back from a
// connected account to the platform. It is addressed under its transfer, and Stripe answers a
// reversal of another transfer as missing (suite: "Transfer reversals"). It is read-only for
// now.
var Declaration = engine.Declaration{
	TypeName:    "stripe_transfer_reversal",
	IDPrefix:    "trr_",
	Description: "A reversal of a transfer, which moves some or all of the transfer's amount back from the connected account to the platform. Look one up by the `transfer` and the reversal's `id`.",
	Parent:      "transfer",
	Paths: engine.Paths{
		Retrieve: "/v1/transfers/{transfer}/reversals/{id}",
		List:     "/v1/transfers/{transfer}/reversals",
	},
	Delete: engine.NoDelete,
}

// NewDataSource returns the data source constructor the provider registers. It needs the `transfer` and the `id` of the reversal. Stripe documents no reversal field as unique.
func NewDataSource() (func() datasource.DataSource, error) {
	return engine.NewDataSource(Declaration, transfer_reversal.Table)
}
