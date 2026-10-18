// Package climate_supplier declares the stripe_climate_supplier data source. There is no resource.
package climate_supplier

import (
	"github.com/hashicorp/terraform-plugin-framework/datasource"

	"github.com/computomatic/terraform-provider-stripe/internal/engine"
	"github.com/computomatic/terraform-provider-stripe/internal/generated/climate_supplier"
)

// Declaration is what the spec cannot say about this object. Stripe publishes the Climate catalogue, and an account only reads it (suite: "ClimateProductAndSupplier"), so only the data source exists.
var Declaration = engine.Declaration{
	TypeName:    "stripe_climate_supplier",
	IDPrefix:    "climsup_",
	Description: "A supplier of the carbon removal that Climate orders buy. Look one up by its `id`.",
	Paths:       engine.Paths{Retrieve: "/v1/climate/suppliers/{id}"},
	Delete:      engine.NoDelete,
}

// NewDataSource returns the data source constructor the provider registers. It looks the object up by `id` only: Stripe documents no field of it as unique.
func NewDataSource() (func() datasource.DataSource, error) {
	return engine.NewDataSource(Declaration, climate_supplier.Table)
}
