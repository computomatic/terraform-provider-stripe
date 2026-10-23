// Package application_fee_refund declares the stripe_application_fee_refund data source.
// There is no resource.
package application_fee_refund

import (
	"github.com/hashicorp/terraform-plugin-framework/datasource"

	"github.com/computomatic/terraform-provider-stripe/internal/engine"
	"github.com/computomatic/terraform-provider-stripe/internal/generated/application_fee_refund"
)

// Declaration is what the spec cannot say about an application fee refund: part or all of a
// fee the platform gives back to the connected account. It is addressed under its fee, and
// Stripe answers a refund of another fee as missing (suite: "Application fee refunds"). It is
// read-only for now.
var Declaration = engine.Declaration{
	TypeName:    "stripe_application_fee_refund",
	IDPrefix:    "fr_",
	Description: "A refund of an application fee, which gives some or all of the fee back to the connected account. Look one up by the `fee` and the refund's `id`.",
	Parent:      "fee",
	Paths: engine.Paths{
		Retrieve: "/v1/application_fees/{fee}/refunds/{id}",
		List:     "/v1/application_fees/{fee}/refunds",
	},
	Delete: engine.NoDelete,
}

// NewDataSource returns the data source constructor the provider registers. It needs the `fee` and the `id` of the refund. Stripe documents no refund field as unique.
func NewDataSource() (func() datasource.DataSource, error) {
	return engine.NewDataSource(Declaration, application_fee_refund.Table)
}
