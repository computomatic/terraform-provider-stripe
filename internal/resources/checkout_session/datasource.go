// Package checkout_session declares the stripe_checkout_session data source. There is no resource.
package checkout_session

import (
	"github.com/hashicorp/terraform-plugin-framework/datasource"

	"github.com/computomatic/terraform-provider-stripe/internal/engine"
	"github.com/computomatic/terraform-provider-stripe/internal/generated/checkout_session"
)

// Declaration is what the spec cannot say about this object. Sessions are made by the code that starts a checkout, and expiring one is an action, so only the data source exists.
var Declaration = engine.Declaration{
	TypeName:    "stripe_checkout_session",
	IDPrefix:    "cs_",
	Description: "A customer's session on a Stripe-hosted checkout page. Look one up by its `id`. The session's `line_items` are included.",
	Paths:       engine.Paths{Retrieve: "/v1/checkout/sessions/{id}"},
	// Stripe returns the line items only when asked for (suite: "Checkout sessions").
	Expand: []string{"line_items"},
	// The client secret lets a front end finish the checkout.
	Sensitive: []string{"client_secret"},
	Delete:    engine.NoDelete,
}

// NewDataSource returns the data source constructor the provider registers. It looks the object up by `id` only: Stripe documents no field of it as unique.
func NewDataSource() (func() datasource.DataSource, error) {
	return engine.NewDataSource(Declaration, checkout_session.Table)
}
