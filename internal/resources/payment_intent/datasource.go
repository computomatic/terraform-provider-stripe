// Package payment_intent declares the stripe_payment_intent data source. There is no resource.
package payment_intent

import (
	"github.com/hashicorp/terraform-plugin-framework/datasource"

	"github.com/computomatic/terraform-provider-stripe/internal/engine"
	"github.com/computomatic/terraform-provider-stripe/internal/generated/payment_intent"
)

// Declaration is what the spec cannot say about this object. Payment intents are made by the front end that collects the payment, and moving one through its states (confirm, capture, cancel) is an action, so only the data source exists.
var Declaration = engine.Declaration{
	TypeName:    "stripe_payment_intent",
	IDPrefix:    "pi_",
	Description: "A customer payment, such as the one behind an invoice. Look one up by its `id`. The intent's `amount_details`, `payment_method_options` and the nested objects of its last payment error are not available.",
	Paths:       engine.Paths{Retrieve: "/v1/payment_intents/{id}"},
	Delete:      engine.NoDelete,
	// The client secret lets a front end finish the payment.
	Sensitive: []string{"client_secret"},
}

// NewDataSource returns the data source constructor the provider registers. It looks the object up by `id` only: Stripe documents no field of it as unique.
func NewDataSource() (func() datasource.DataSource, error) {
	return engine.NewDataSource(Declaration, payment_intent.Table)
}
