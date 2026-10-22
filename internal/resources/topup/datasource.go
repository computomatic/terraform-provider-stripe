// Package topup declares the stripe_topup data source. There is no resource.
package topup

import (
	"github.com/hashicorp/terraform-plugin-framework/datasource"

	"github.com/computomatic/terraform-provider-stripe/internal/engine"
	"github.com/computomatic/terraform-provider-stripe/internal/generated/topup"
)

// Declaration is what the spec cannot say about this object. Stripe refuses top-ups for some countries, and the suite cannot create one on its account, so only the data source exists.
var Declaration = engine.Declaration{
	TypeName:    "stripe_topup",
	IDPrefix:    "tu_",
	Description: "A top-up of your Stripe balance from a bank account or source. Look one up by its `id`.",
	Paths:       engine.Paths{Retrieve: "/v1/topups/{id}", List: "/v1/topups"},
	Delete:      engine.NoDelete,
}

// NewDataSource returns the data source constructor the provider registers. It looks the object up by `id` only: Stripe documents no field of it as unique.
func NewDataSource() (func() datasource.DataSource, error) {
	return engine.NewDataSource(Declaration, topup.Table)
}
