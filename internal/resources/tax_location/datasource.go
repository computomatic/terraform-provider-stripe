// Package tax_location declares the stripe_tax_location data source. There is no resource.
package tax_location

import (
	"github.com/hashicorp/terraform-plugin-framework/datasource"

	"github.com/computomatic/terraform-provider-stripe/internal/engine"
	"github.com/computomatic/terraform-provider-stripe/internal/generated/tax_location"
)

// Declaration is what the spec cannot say about this object. The provider reads tax locations only. Stripe cannot delete one (suite: "Tax locations are served").
var Declaration = engine.Declaration{
	TypeName:    "stripe_tax_location",
	IDPrefix:    "taxloc_",
	Description: "A place where a performance or event happens, which Stripe Tax uses to tax tickets to it. Look one up by its `id`.",
	Paths:       engine.Paths{Retrieve: "/v1/tax/locations/{id}", List: "/v1/tax/locations"},
	Delete:      engine.NoDelete,
}

// NewDataSource returns the data source constructor the provider registers. It looks the object up by `id` only: Stripe documents no field of it as unique.
func NewDataSource() (func() datasource.DataSource, error) {
	return engine.NewDataSource(Declaration, tax_location.Table)
}
