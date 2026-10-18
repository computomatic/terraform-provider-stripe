// Package balance_settings declares the stripe_balance_settings data source: the account's balance settings. There is no resource.
package balance_settings

import (
	"github.com/hashicorp/terraform-plugin-framework/datasource"

	"github.com/computomatic/terraform-provider-stripe/internal/engine"
	"github.com/computomatic/terraform-provider-stripe/internal/generated/balance_settings"
)

// Declaration is what the spec cannot say about the balance settings. They are a singleton
// Stripe serves from one path. `payments.payouts.automatic_transfer_rules_by_currency` is
// not modelled.
var Declaration = engine.Declaration{
	TypeName:    "stripe_balance_settings",
	Description: "The account's balance settings: payout schedule, statement descriptor and settlement timing. It has no arguments.",
	Paths:       engine.Paths{Retrieve: "/v1/balance_settings"},
	Delete:      engine.NoDelete,
	Singleton:   true,
}

// NewDataSource returns the data source constructor the provider registers. It has no arguments.
func NewDataSource() (func() datasource.DataSource, error) {
	return engine.NewDataSource(Declaration, balance_settings.Table)
}
