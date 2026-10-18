// Package product_feature declares the stripe_product_feature resource and data source: an
// entitlement feature attached to a product.
package product_feature

import (
	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/resource"

	"github.com/computomatic/terraform-provider-stripe/internal/engine"
	"github.com/computomatic/terraform-provider-stripe/internal/generated/product_feature"
)

// Declaration is what the spec cannot say about a product feature. It is addressed under
// its product, so its import ID is `<product_id>/<product_feature_id>`. Stripe never updates
// one, so every argument forces replacement. Destroy detaches the feature (DELETE); the
// entitlement feature itself stays and can be attached again.
var Declaration = engine.Declaration{
	TypeName:    "stripe_product_feature",
	IDPrefix:    "prodft_",
	Description: "An entitlement feature attached to a product. `terraform destroy` detaches the feature from the product and leaves the `stripe_entitlements_feature` in place. Import with `<product_id>/<product_feature_id>`.",
	Parent:      "product",
	Paths: engine.Paths{
		Create:   "/v1/products/{product}/features",
		Retrieve: "/v1/products/{product}/features/{id}",
		Delete:   "/v1/products/{product}/features/{id}",
		List:     "/v1/products/{product}/features",
	},
	Delete: engine.Delete,
}

// NewResource returns the resource constructor the provider registers.
func NewResource() (func() resource.Resource, error) {
	return engine.NewResource(Declaration, product_feature.Table)
}

// NewDataSource returns the data source constructor. It needs the `product` and the `id` of the product feature. Stripe documents no product feature field as unique.
func NewDataSource() (func() datasource.DataSource, error) {
	return engine.NewDataSource(Declaration, product_feature.Table)
}
