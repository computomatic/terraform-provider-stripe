// Package payout declares the stripe_payout resource and data source.
package payout

import (
	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/resource"

	"github.com/computomatic/terraform-provider-stripe/internal/engine"
	"github.com/computomatic/terraform-provider-stripe/internal/generated/payout"
)

// Declaration is what the spec cannot say about a payout (ADR
// 2026-10-22-money-moving-resources, D6.7). It moves money, so the live-mode guard applies. Only
// `metadata` updates (suite: "update"); `description` and `amount` are refused ("description is
// not updatable", "immutable amount"), as the spec's update says. There is no `status`: a payout
// can be canceled only while it is pending, which lasts seconds on Stripe (Q5). Stripe cannot
// delete a payout, so destroy is refused.
var Declaration = engine.Declaration{
	TypeName:              "stripe_payout",
	IDPrefix:              "po_",
	Description:           "A transfer of funds from your Stripe balance to a bank account or card. It goes to the account's default bank account in its currency unless `destination` names another, and needs that much available balance. Stripe moves a payout from `pending` through `in_transit` to `paid` by itself. Only `metadata` changes after creation.",
	DataSourceDescription: "A transfer of funds from your Stripe balance to a bank account or card. Look one up by its `id`.",
	Paths: engine.Paths{
		Create:   "/v1/payouts",
		Retrieve: "/v1/payouts/{id}",
		Update:   "/v1/payouts/{id}",
		List:     "/v1/payouts",
	},
	MovesMoney: true,
	// Stripe names the default bank account in the currency when the request leaves it out.
	ServerDefaulted: []string{"destination"},
	Delete:          engine.Refuse,
	RefuseReason:    "Stripe cannot delete a payout.",
}

// NewResource returns the resource constructor the provider registers.
func NewResource() (func() resource.Resource, error) {
	return engine.NewResource(Declaration, payout.Table)
}

// NewDataSource returns the data source constructor. It looks the object up by `id` only: Stripe documents no field of it as unique.
func NewDataSource() (func() datasource.DataSource, error) {
	return engine.NewDataSource(Declaration, payout.Table)
}
