// Package refund declares the stripe_refund data source. There is no resource.
package refund

import (
	"github.com/hashicorp/terraform-plugin-framework/datasource"

	"github.com/computomatic/terraform-provider-stripe/internal/engine"
	"github.com/computomatic/terraform-provider-stripe/internal/generated/refund"
)

// Declaration is what the spec cannot say about this object. Refunds are issued against payments, and a refund cannot be deleted, so only the data source exists.
var Declaration = engine.Declaration{
	TypeName:    "stripe_refund",
	IDPrefix:    "re_",
	Description: "A refund of a charge or payment intent. Look one up by its `id`.",
	Paths:       engine.Paths{Retrieve: "/v1/refunds/{id}"},
	Delete:      engine.NoDelete,
}

// NewDataSource returns the data source constructor the provider registers. It looks the object up by `id` only: Stripe documents no field of it as unique.
func NewDataSource() (func() datasource.DataSource, error) {
	return engine.NewDataSource(Declaration, refund.Table)
}
