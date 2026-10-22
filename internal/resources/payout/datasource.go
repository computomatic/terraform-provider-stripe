// Package payout declares the stripe_payout data source. There is no resource.
package payout

import (
	"github.com/hashicorp/terraform-plugin-framework/datasource"

	"github.com/computomatic/terraform-provider-stripe/internal/engine"
	"github.com/computomatic/terraform-provider-stripe/internal/generated/payout"
)

// Declaration is what the spec cannot say about this object. Stripe pays out on a schedule and the account's bank accounts decide where, and the suite cannot create one on its account, so only the data source exists.
var Declaration = engine.Declaration{
	TypeName:    "stripe_payout",
	IDPrefix:    "po_",
	Description: "A transfer of funds from your Stripe balance to a bank account or card. Look one up by its `id`.",
	Paths:       engine.Paths{Retrieve: "/v1/payouts/{id}", List: "/v1/payouts"},
	Delete:      engine.NoDelete,
}

// NewDataSource returns the data source constructor the provider registers. It looks the object up by `id` only: Stripe documents no field of it as unique.
func NewDataSource() (func() datasource.DataSource, error) {
	return engine.NewDataSource(Declaration, payout.Table)
}
