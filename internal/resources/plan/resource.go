// Package plan declares the stripe_plan resource and data source.
package plan

import (
	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/resource"

	"github.com/computomatic/terraform-provider-stripe/internal/engine"
	"github.com/computomatic/terraform-provider-stripe/internal/generated/plan"
)

// Declaration is what the spec cannot say about a plan. A plan is the legacy form of a
// price: Stripe offers DELETE for it, which removes the plan and leaves existing
// subscriptions on it.
var Declaration = engine.Declaration{
	TypeName:              "stripe_plan",
	Description:           "A legacy recurring price. Prefer `stripe_price` for new work. `terraform destroy` deletes the plan, which stops new subscriptions to it and leaves existing ones in place.",
	DataSourceDescription: "A legacy recurring price. Prefer `stripe_price` for new work.",
	Paths: engine.Paths{
		Create:   "/v1/plans",
		Retrieve: "/v1/plans/{id}",
		Update:   "/v1/plans/{id}",
		Delete:   "/v1/plans/{id}",
		List:     "/v1/plans",
	},
	// Stripe returns tiers only when asked for.
	Expand: []string{"tiers"},
	// The last tier of a tiered plan is unbounded: Stripe returns its `up_to` as null, and
	// the configuration says `inf`.
	NullAs: map[string]string{"tiers.up_to": "inf"},
	// Stripe derives each amount from the other, in tiers too.
	ServerDefaulted: []string{
		"amount", "amount_decimal",
		"tiers.unit_amount", "tiers.unit_amount_decimal", "tiers.flat_amount", "tiers.flat_amount_decimal",
	},
	Delete: engine.Delete,
}

// NewResource returns the resource constructor the provider registers.
func NewResource() (func() resource.Resource, error) {
	return engine.NewResource(Declaration, plan.Table)
}

// NewDataSource returns the data source constructor. No plan field other than the ID is documented as unique, so the data source looks up by `id` only.
func NewDataSource() (func() datasource.DataSource, error) {
	return engine.NewDataSource(Declaration, plan.Table)
}
