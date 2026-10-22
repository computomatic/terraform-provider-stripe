// Package entitlements_feature declares the stripe_entitlements_feature resource and data
// source.
package entitlements_feature

import (
	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/resource"

	"github.com/computomatic/terraform-provider-stripe/internal/engine"
	"github.com/computomatic/terraform-provider-stripe/internal/generated/entitlements_feature"
)

// Declaration is what the spec cannot say about an entitlement feature. Stripe never
// deletes one, and archiving one (`active = false`) is final: an archived feature is frozen,
// cannot be updated or reactivated, and its `lookup_key` stays taken. So destroy is refused.
var Declaration = engine.Declaration{
	TypeName:              "stripe_entitlements_feature",
	IDPrefix:              "feat_",
	Description:           "A feature a product can grant, identified by a unique `lookup_key`. Set `active` to `false` to archive the feature; an archived feature cannot be updated or reactivated, and its `lookup_key` cannot be used again.",
	DataSourceDescription: "A feature a product can grant, identified by a unique `lookup_key`.",
	Paths: engine.Paths{
		Create:   "/v1/entitlements/features",
		Retrieve: "/v1/entitlements/features/{id}",
		Update:   "/v1/entitlements/features/{id}",
		List:     "/v1/entitlements/features",
	},
	Delete:       engine.Refuse,
	RefuseReason: "Stripe cannot delete an entitlement feature; set `active` to `false` to archive one, which cannot be undone.",
	Lookups:      []engine.Lookup{{Field: "lookup_key"}},
}

// NewResource returns the resource constructor the provider registers.
func NewResource() (func() resource.Resource, error) {
	return engine.NewResource(Declaration, entitlements_feature.Table)
}

// NewDataSource returns the data source constructor. It looks a feature up by `id` or by `lookup_key`, which Stripe keeps unique per account.
func NewDataSource() (func() datasource.DataSource, error) {
	return engine.NewDataSource(Declaration, entitlements_feature.Table)
}
