// Package tax_code declares the stripe_tax_code data source: a read-only catalogue of the
// tax codes Stripe knows. There is no resource.
package tax_code

import (
	"github.com/hashicorp/terraform-plugin-framework/datasource"

	"github.com/computomatic/terraform-provider-stripe/internal/engine"
	"github.com/computomatic/terraform-provider-stripe/internal/generated/tax_code"
)

// Declaration is what the spec cannot say about a tax code.
var Declaration = engine.Declaration{
	TypeName:    "stripe_tax_code",
	IDPrefix:    "txcd_",
	Description: "A Stripe tax code, which classifies what a product is for tax purposes. Look one up by its `id`, such as `txcd_10000000`.",
	Paths:       engine.Paths{Retrieve: "/v1/tax_codes/{id}"},
	Delete:      engine.NoDelete,
}

// NewDataSource returns the data source constructor the provider registers. It looks a tax code up by `id` only.
func NewDataSource() (func() datasource.DataSource, error) {
	return engine.NewDataSource(Declaration, tax_code.Table)
}
