// Package charge declares the stripe_charge data source. There is no resource.
package charge

import (
	"github.com/hashicorp/terraform-plugin-framework/datasource"

	"github.com/computomatic/terraform-provider-stripe/internal/engine"
	"github.com/computomatic/terraform-provider-stripe/internal/generated/charge"
)

// Declaration is what the spec cannot say about this object. Charges come from payment intents, and capturing or refunding one is an action, so only the data source exists.
var Declaration = engine.Declaration{
	TypeName:    "stripe_charge",
	IDPrefix:    "ch_",
	Description: "A single attempt to move money from a customer. Look one up by its `id`.",
	Paths:       engine.Paths{Retrieve: "/v1/charges/{id}"},
	Delete:      engine.NoDelete,
}

// NewDataSource returns the data source constructor the provider registers. It looks the object up by `id` only: Stripe documents no field of it as unique.
func NewDataSource() (func() datasource.DataSource, error) {
	return engine.NewDataSource(Declaration, charge.Table)
}
