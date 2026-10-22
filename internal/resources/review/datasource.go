// Package review declares the stripe_review data source. There is no resource.
package review

import (
	"github.com/hashicorp/terraform-plugin-framework/datasource"

	"github.com/computomatic/terraform-provider-stripe/internal/engine"
	"github.com/computomatic/terraform-provider-stripe/internal/generated/review"
)

// Declaration is what the spec cannot say about this object. Stripe opens a review when a Radar rule flags a payment, so only the data source exists.
var Declaration = engine.Declaration{
	TypeName:    "stripe_review",
	IDPrefix:    "prv_",
	Description: "A Radar review of a payment that a rule flagged. Look one up by its `id`.",
	Paths:       engine.Paths{Retrieve: "/v1/reviews/{id}", List: "/v1/reviews"},
	Delete:      engine.NoDelete,
}

// NewDataSource returns the data source constructor the provider registers. It looks the object up by `id` only: Stripe documents no field of it as unique.
func NewDataSource() (func() datasource.DataSource, error) {
	return engine.NewDataSource(Declaration, review.Table)
}
