// Package radar_early_fraud_warning declares the stripe_radar_early_fraud_warning data source. There is no resource.
package radar_early_fraud_warning

import (
	"github.com/hashicorp/terraform-plugin-framework/datasource"

	"github.com/computomatic/terraform-provider-stripe/internal/engine"
	"github.com/computomatic/terraform-provider-stripe/internal/generated/radar_early_fraud_warning"
)

// Declaration is what the spec cannot say about this object. Stripe raises a warning when a card issuer reports a charge as fraud, so only the data source exists.
var Declaration = engine.Declaration{
	TypeName:    "stripe_radar_early_fraud_warning",
	IDPrefix:    "issfr_",
	Description: "A warning from a card issuer that a charge may be fraudulent. Look one up by its `id`.",
	Paths:       engine.Paths{Retrieve: "/v1/radar/early_fraud_warnings/{id}", List: "/v1/radar/early_fraud_warnings"},
	Delete:      engine.NoDelete,
}

// NewDataSource returns the data source constructor the provider registers. It looks the object up by `id` only: the list filters by `charge` and `payment_intent`, but a charge can in principle have more than one warning.
func NewDataSource() (func() datasource.DataSource, error) {
	return engine.NewDataSource(Declaration, radar_early_fraud_warning.Table)
}
