// Package dispute declares the stripe_dispute data source. There is no resource.
package dispute

import (
	"github.com/hashicorp/terraform-plugin-framework/datasource"

	"github.com/computomatic/terraform-provider-stripe/internal/engine"
	"github.com/computomatic/terraform-provider-stripe/internal/generated/dispute"
)

// Declaration is what the spec cannot say about this object. Disputes are opened by the cardholder's bank, so only the data source exists.
var Declaration = engine.Declaration{
	TypeName:    "stripe_dispute",
	IDPrefix:    "du_",
	Description: "A customer's challenge of a charge with their bank. Look one up by its `id`.",
	Paths:       engine.Paths{Retrieve: "/v1/disputes/{id}"},
	Delete:      engine.NoDelete,
}

// NewDataSource returns the data source constructor the provider registers. It looks the object up by `id` only: Stripe documents no field of it as unique.
func NewDataSource() (func() datasource.DataSource, error) {
	return engine.NewDataSource(Declaration, dispute.Table)
}
