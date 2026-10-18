// Package country_spec declares the stripe_country_spec data source: a read-only catalogue
// of what Stripe supports in each country. There is no resource.
package country_spec

import (
	"github.com/hashicorp/terraform-plugin-framework/datasource"

	"github.com/computomatic/terraform-provider-stripe/internal/engine"
	"github.com/computomatic/terraform-provider-stripe/internal/generated/country_spec"
)

// Declaration is what the spec cannot say about a country spec. The table leaves out
// `supported_bank_account_currencies`, a map from a currency to a list of countries, which
// the engine does not model.
var Declaration = engine.Declaration{
	TypeName:    "stripe_country_spec",
	Description: "What Stripe supports in a country. Look one up by its `id`, the two-letter country code such as `US`. It omits `supported_bank_account_currencies`.",
	Paths:       engine.Paths{Retrieve: "/v1/country_specs/{id}"},
	Delete:      engine.NoDelete,
}

// NewDataSource returns the data source constructor the provider registers. It looks a country spec up by `id` only.
func NewDataSource() (func() datasource.DataSource, error) {
	return engine.NewDataSource(Declaration, country_spec.Table)
}
