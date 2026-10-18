// Package promotion_code declares the stripe_promotion_code resource and data source.
package promotion_code

import (
	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/resource"

	"github.com/computomatic/terraform-provider-stripe/internal/engine"
	"github.com/computomatic/terraform-provider-stripe/internal/generated/promotion_code"
	"github.com/computomatic/terraform-provider-stripe/internal/wire"
)

// Declaration is what the spec cannot say about a promotion code. Stripe never deletes a
// promotion code (DELETE answers with an error), so destroy archives it (`active = false`)
// and drops it from state. An archived code keeps its `code`, so a new promotion code
// cannot reuse it.
var Declaration = engine.Declaration{
	TypeName:    "stripe_promotion_code",
	IDPrefix:    "promo_",
	Description: "A customer-facing code that redeems a coupon. `terraform destroy` archives the promotion code, because Stripe cannot delete one. An archived code keeps its `code`, so it cannot be used for another promotion code.",
	Paths: engine.Paths{
		Create:   "/v1/promotion_codes",
		Retrieve: "/v1/promotion_codes/{id}",
		Update:   "/v1/promotion_codes/{id}",
		List:     "/v1/promotion_codes",
	},
	Delete:  engine.Archive,
	Archive: &engine.ArchiveAction{Field: "active", Value: wire.Bool(false)},
	Lookups: []engine.Lookup{{Field: "code"}},
}

// NewResource returns the resource constructor the provider registers.
func NewResource() (func() resource.Resource, error) {
	return engine.NewResource(Declaration, promotion_code.Table)
}

// NewDataSource returns the data source constructor. It looks a promotion code up by `id` or by `code`. Stripe keeps `code` unique among active codes, so a code that was archived and reused can match more than once, which the lookup reports.
func NewDataSource() (func() datasource.DataSource, error) {
	return engine.NewDataSource(Declaration, promotion_code.Table)
}
