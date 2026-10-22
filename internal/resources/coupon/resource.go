// Package coupon declares the stripe_coupon resource and data source.
package coupon

import (
	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/resource"

	"github.com/computomatic/terraform-provider-stripe/internal/engine"
	"github.com/computomatic/terraform-provider-stripe/internal/generated/coupon"
)

// Declaration is what the spec cannot say about a coupon. Stripe offers DELETE, which stops
// new redemptions and leaves existing discounts in place.
var Declaration = engine.Declaration{
	TypeName:              "stripe_coupon",
	Description:           "A discount that can be applied to a customer or subscription. `terraform destroy` deletes the coupon, which stops new redemptions and leaves existing discounts in place.",
	DataSourceDescription: "A discount that can be applied to a customer or subscription.",
	Paths: engine.Paths{
		Create:   "/v1/coupons",
		Retrieve: "/v1/coupons/{id}",
		Update:   "/v1/coupons/{id}",
		Delete:   "/v1/coupons/{id}",
		List:     "/v1/coupons",
	},
	// Stripe returns these only when asked for.
	Expand: []string{"applies_to", "currency_options"},
	// Stripe returns the coupon's own currency among the options and refuses it as one.
	CurrencyOptions: "currency_options",
	Delete:          engine.Delete,
}

// NewResource returns the resource constructor the provider registers.
func NewResource() (func() resource.Resource, error) {
	return engine.NewResource(Declaration, coupon.Table)
}

// NewDataSource returns the data source constructor. No coupon field other than the ID is documented as unique, so the data source looks up by `id` only.
func NewDataSource() (func() datasource.DataSource, error) {
	return engine.NewDataSource(Declaration, coupon.Table)
}
