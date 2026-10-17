// Package product declares the stripe_product resource and data source.
package product

import (
	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/resource"

	"github.com/computomatic/terraform-provider-stripe/internal/engine"
	"github.com/computomatic/terraform-provider-stripe/internal/generated/product"
)

// Declaration is what the spec cannot say about a product. Stripe offers DELETE for
// products, and it fails once the product has a price, so a product that carries prices
// has to be removed from the account by hand.
var Declaration = engine.Declaration{
	TypeName:    "stripe_product",
	IDPrefix:    "prod_",
	Description: "A product Stripe sells: a good or a service that prices attach to. `terraform destroy` deletes the product, which Stripe refuses once a price exists for it.",
	Paths: engine.Paths{
		Create:   "/v1/products",
		Retrieve: "/v1/products/{id}",
		Update:   "/v1/products/{id}",
		Delete:   "/v1/products/{id}",
		List:     "/v1/products",
	},
	Delete: engine.Delete,
}

// NewResource returns the resource constructor the provider registers.
func NewResource() (func() resource.Resource, error) {
	return engine.NewResource(Declaration, product.Table)
}

// NewDataSource returns the data source constructor. No product field is documented as
// unique per account, so the data source looks up by `id` only.
func NewDataSource() (func() datasource.DataSource, error) {
	return engine.NewDataSource(Declaration, product.Table)
}
