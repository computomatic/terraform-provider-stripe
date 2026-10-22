// Package product declares the stripe_product resource and data source.
package product

import (
	"net/http"

	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/resource"

	"github.com/computomatic/terraform-provider-stripe/internal/engine"
	"github.com/computomatic/terraform-provider-stripe/internal/generated/product"
	"github.com/computomatic/terraform-provider-stripe/internal/wire"
)

// Declaration is what the spec cannot say about a product. Stripe offers DELETE for
// products and refuses it (400, `invalid_request_error`) once the product has a price. Destroy
// then archives the product (`active = false`) and warns.
var Declaration = engine.Declaration{
	TypeName:              "stripe_product",
	IDPrefix:              "prod_",
	Description:           "A product Stripe sells: a good or a service that prices attach to. `terraform destroy` deletes the product, or archives it with a warning when Stripe refuses because a price exists for it.",
	DataSourceDescription: "A product Stripe sells: a good or a service that prices attach to.",
	Paths: engine.Paths{
		Create:   "/v1/products",
		Retrieve: "/v1/products/{id}",
		Update:   "/v1/products/{id}",
		Delete:   "/v1/products/{id}",
		List:     "/v1/products",
	},
	Delete:    engine.DeleteOrArchive,
	Archive:   &engine.ArchiveAction{Field: "active", Value: wire.Bool(false)},
	ArchiveOn: &engine.Refusal{Status: http.StatusBadRequest, Type: "invalid_request_error"},
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
