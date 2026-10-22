// Package entitlements_active_entitlement declares the stripe_entitlements_active_entitlement data source. There is no resource.
package entitlements_active_entitlement

import (
	"github.com/hashicorp/terraform-plugin-framework/datasource"

	"github.com/computomatic/terraform-provider-stripe/internal/engine"
	"github.com/computomatic/terraform-provider-stripe/internal/generated/entitlements_active_entitlement"
)

// Declaration is what the spec cannot say about this object. Stripe grants an entitlement for each feature of the products a customer subscribes to (suite: "Active entitlements"), so only the data source exists. The list filters by customer only, so `lookup_key` is not a lookup key.
var Declaration = engine.Declaration{
	TypeName:    "stripe_entitlements_active_entitlement",
	IDPrefix:    "ent_",
	Description: "A feature a customer is entitled to through its active subscriptions. Look one up by its `id`.",
	Paths:       engine.Paths{Retrieve: "/v1/entitlements/active_entitlements/{id}", List: "/v1/entitlements/active_entitlements"},
	Delete:      engine.NoDelete,
}

// NewDataSource returns the data source constructor the provider registers. It looks the object up by `id` only: the list endpoint filters by no unique field.
func NewDataSource() (func() datasource.DataSource, error) {
	return engine.NewDataSource(Declaration, entitlements_active_entitlement.Table)
}
