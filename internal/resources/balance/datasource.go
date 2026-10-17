// Package balance declares the stripe_balance data source: the account's current balance.
// The balance is a singleton Stripe serves from one path, so there is no resource.
package balance

import (
	"github.com/hashicorp/terraform-plugin-framework/datasource"

	"github.com/computomatic/terraform-provider-stripe/internal/engine"
	"github.com/computomatic/terraform-provider-stripe/internal/generated/balance"
)

// Declaration is what the spec cannot say about the balance.
var Declaration = engine.Declaration{
	TypeName:    "stripe_balance",
	Description: "The account's current balance, by currency. It has no arguments.",
	Paths:       engine.Paths{Retrieve: "/v1/balance"},
	Delete:      engine.NoDelete,
	Singleton:   true,
}

// NewDataSource returns the data source constructor the provider registers.
func NewDataSource() (func() datasource.DataSource, error) {
	return engine.NewDataSource(Declaration, balance.Table)
}
