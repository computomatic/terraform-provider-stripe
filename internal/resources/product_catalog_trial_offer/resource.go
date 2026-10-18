// Package product_catalog_trial_offer declares the stripe_product_catalog_trial_offer
// resource and data source.
package product_catalog_trial_offer

import (
	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/resource"

	"github.com/computomatic/terraform-provider-stripe/internal/engine"
	"github.com/computomatic/terraform-provider-stripe/internal/generated/product_catalog_trial_offer"
	"github.com/computomatic/terraform-provider-stripe/internal/wire"
)

// Declaration is what the spec cannot say about a trial offer. Stripe cannot delete one
// (DELETE answers with an error), so destroy archives it (`active = false`) and drops it
// from state; the trial offer stays in the account.
var Declaration = engine.Declaration{
	TypeName:    "stripe_product_catalog_trial_offer",
	IDPrefix:    "to_",
	Description: "A trial period a price offers before it transitions to another price. `terraform destroy` archives the trial offer, because Stripe cannot delete one.",
	Paths: engine.Paths{
		Create:   "/v1/product_catalog/trial_offers",
		Retrieve: "/v1/product_catalog/trial_offers/{id}",
		Update:   "/v1/product_catalog/trial_offers/{id}",
		List:     "/v1/product_catalog/trial_offers",
	},
	Delete:  engine.Archive,
	Archive: &engine.ArchiveAction{Field: "active", Value: wire.Bool(false)},
}

// NewResource returns the resource constructor the provider registers.
func NewResource() (func() resource.Resource, error) {
	return engine.NewResource(Declaration, product_catalog_trial_offer.Table)
}

// NewDataSource returns the data source constructor. No trial offer field is documented as unique, so the data source looks up by `id` only.
func NewDataSource() (func() datasource.DataSource, error) {
	return engine.NewDataSource(Declaration, product_catalog_trial_offer.Table)
}
