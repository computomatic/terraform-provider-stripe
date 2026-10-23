// Package checkout_session declares the stripe_checkout_session resource and data source.
package checkout_session

import (
	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/resource"

	"github.com/computomatic/terraform-provider-stripe/internal/engine"
	"github.com/computomatic/terraform-provider-stripe/internal/generated/checkout_session"
)

// Declaration is what the spec cannot say about a checkout session (ADR
// 2026-10-22-money-moving-resources, D6.6). It takes payments, so the live-mode guard applies.
// Stripe cannot delete one, so destroy is refused. `status` expires an open session (suite:
// "expire"); a completed one cannot be expired (suite: "already expired"). `metadata` and
// `shipping_options` are proven to update ("update metadata", "update shipping options").
var Declaration = engine.Declaration{
	TypeName:              "stripe_checkout_session",
	IDPrefix:              "cs_",
	Description:           "A customer's session on a Stripe-hosted checkout page. Set `status` to `expired` to expire an open session. Stripe expires a session by itself at `expires_at`, 24 hours after creation unless set, and completes it when the customer pays. Only `metadata` and `shipping_options` change after creation. The `client_secret` is sensitive.",
	DataSourceDescription: "A customer's session on a Stripe-hosted checkout page. The session's `line_items` are included.",
	Paths: engine.Paths{
		Create:   "/v1/checkout/sessions",
		Retrieve: "/v1/checkout/sessions/{id}",
		Update:   "/v1/checkout/sessions/{id}",
		List:     "/v1/checkout/sessions",
	},
	MovesMoney: true,
	// Stripe returns the line items only when asked for (suite: "Checkout sessions").
	Expand: []string{"line_items"},
	// The client secret lets a front end finish the checkout.
	Sensitive: []string{"client_secret"},
	// Stripe fills these in from the mode and the account when the request leaves them out
	// (suite: "create minimal").
	ServerDefaulted: []string{
		"currency", "customer_creation", "payment_method_collection", "phone_number_collection",
		"redirect_on_completion", "tax_id_collection", "ui_mode",
	},
	// The spec's update takes them, but Stripe refuses any change of a hosted session's line
	// items (suite: "update errors" / "line items").
	ReplaceOn:    []string{"line_items"},
	Delete:       engine.Refuse,
	RefuseReason: "Stripe cannot delete a checkout session; set `status` to `expired` to expire an open one.",
	Transitions: []engine.Transition{
		{Field: "status", Value: "expired", Path: "/v1/checkout/sessions/{id}/expire", From: []string{"open"}, Final: []string{"complete"}},
	},
}

// NewResource returns the resource constructor the provider registers.
func NewResource() (func() resource.Resource, error) {
	return engine.NewResource(Declaration, checkout_session.Table)
}

// NewDataSource returns the data source constructor. It looks the object up by `id` only: Stripe documents no field of it as unique.
func NewDataSource() (func() datasource.DataSource, error) {
	return engine.NewDataSource(Declaration, checkout_session.Table)
}
