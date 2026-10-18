// Package tax_settings declares the stripe_tax_settings data source: the account's Stripe Tax settings. There is no resource.
package tax_settings

import (
	"github.com/hashicorp/terraform-plugin-framework/datasource"

	"github.com/computomatic/terraform-provider-stripe/internal/engine"
	"github.com/computomatic/terraform-provider-stripe/internal/generated/tax_settings"
)

// Declaration is what the spec cannot say about the tax settings. They are a singleton
// Stripe serves from one path.
var Declaration = engine.Declaration{
	TypeName:    "stripe_tax_settings",
	Description: "The account's Stripe Tax settings: defaults, head office and status. It has no arguments.",
	Paths:       engine.Paths{Retrieve: "/v1/tax/settings"},
	Delete:      engine.NoDelete,
	Singleton:   true,
}

// NewDataSource returns the data source constructor the provider registers. It has no arguments.
func NewDataSource() (func() datasource.DataSource, error) {
	return engine.NewDataSource(Declaration, tax_settings.Table)
}
