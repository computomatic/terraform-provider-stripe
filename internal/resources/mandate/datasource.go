// Package mandate declares the stripe_mandate data source. There is no resource.
package mandate

import (
	"github.com/hashicorp/terraform-plugin-framework/datasource"

	"github.com/computomatic/terraform-provider-stripe/internal/engine"
	"github.com/computomatic/terraform-provider-stripe/internal/generated/mandate"
)

// Declaration is what the spec cannot say about this object. Mandates are made when a payment method is confirmed, so only the data source exists.
var Declaration = engine.Declaration{
	TypeName:    "stripe_mandate",
	IDPrefix:    "mandate_",
	Description: "A customer's authorization to charge a payment method. Look one up by its `id`.",
	Paths:       engine.Paths{Retrieve: "/v1/mandates/{id}"},
	Delete:      engine.NoDelete,
}

// NewDataSource returns the data source constructor the provider registers. It looks the object up by `id` only: Stripe documents no field of it as unique.
func NewDataSource() (func() datasource.DataSource, error) {
	return engine.NewDataSource(Declaration, mandate.Table)
}
