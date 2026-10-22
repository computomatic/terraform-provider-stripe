// Package treasury_received_credit declares the stripe_treasury_received_credit data source. There is no resource.
package treasury_received_credit

import (
	"github.com/hashicorp/terraform-plugin-framework/datasource"

	"github.com/computomatic/terraform-provider-stripe/internal/engine"
	"github.com/computomatic/terraform-provider-stripe/internal/generated/treasury_received_credit"
)

// Declaration is what the spec cannot say about this object. It is a money movement in a Treasury financial account on a connected account (suite: "Treasury reads on a US connected account"). Stripe retrieves it by its ID alone and lists it by financial account, and no field of it is unique, so the data source reads it by `id` and returns its `financial_account`. An external bank, or Stripe's test helper, creates it (suite: "seeded received credit").
var Declaration = engine.Declaration{
	TypeName:    "stripe_treasury_received_credit",
	IDPrefix:    "rc_",
	Description: "Money that arrived in a Treasury financial account from outside Stripe. It lives on a connected account, so read it through a provider configuration whose `stripe_account` is that account. Look one up by its `id`.",
	Paths:       engine.Paths{Retrieve: "/v1/treasury/received_credits/{id}", List: "/v1/treasury/received_credits"},
	Delete:      engine.NoDelete,
}

// NewDataSource returns the data source constructor the provider registers. It looks the object up by `id` only: Stripe documents no field of it as unique.
func NewDataSource() (func() datasource.DataSource, error) {
	return engine.NewDataSource(Declaration, treasury_received_credit.Table)
}
