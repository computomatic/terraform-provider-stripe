// Package setup_intent declares the stripe_setup_intent resource and data source.
package setup_intent

import (
	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/resource"

	"github.com/computomatic/terraform-provider-stripe/internal/engine"
	"github.com/computomatic/terraform-provider-stripe/internal/generated/setup_intent"
)

// Declaration is what the spec cannot say about a setup intent. Stripe never deletes one, and
// cancelling one is final, so destroy is refused. Setting `status` to `canceled` cancels it
// (suite: "SetupIntent", `cancel`); a canceled intent stays readable, and a succeeded one
// cannot be cancelled (`already succeeded`, `setup_intent_unexpected_state`).
var Declaration = engine.Declaration{
	TypeName:              "stripe_setup_intent",
	IDPrefix:              "seti_",
	Description:           "Collects a customer's payment method for later use. Set `status` to `canceled` to cancel the intent; one that already succeeded cannot be canceled. Confirming an intent is not a resource: confirm it with its `client_secret` from your front end, or from your server with Stripe's API, and `status` reports what happened. The resource has no `confirm`, `mandate_data`, `payment_method_data` or `payment_method_options` argument. Setting `payment_method` moves the intent to `requires_confirmation`. `usage` and `on_behalf_of` cannot change after creation, and neither can the other arguments that only a create takes, so changing one fails the plan. The `client_secret` is sensitive and stays in the state.",
	DataSourceDescription: "Collects a customer's payment method for later use.",
	Paths: engine.Paths{
		Create:   "/v1/setup_intents",
		Retrieve: "/v1/setup_intents/{id}",
		Update:   "/v1/setup_intents/{id}",
		List:     "/v1/setup_intents",
	},
	// Stripe returns the client secret to the server that created the intent.
	OnceReturned: []string{"client_secret"},
	// Stripe reports `attach_to_self` as false when the request leaves it out.
	ServerDefaulted: []string{"attach_to_self"},
	// The suite proves updates of `description`, `customer`, `payment_method` and `metadata`
	// ("update", "update payment method moves status"). The other fields the spec takes on
	// update are not proven, so they are treated as create-only.
	ReplaceOn: []string{
		"allowed_payment_method_types", "attach_to_self", "customer_account", "excluded_payment_method_types",
		"flow_directions", "payment_method_configuration",
	},
	Delete:       engine.Refuse,
	RefuseReason: "Stripe cannot delete a setup intent; set `status` to `canceled` to cancel one.",
	// A succeeded intent cannot be cancelled (suite: "cancel errors", "already succeeded").
	Transitions: []engine.Transition{{Field: "status", Value: "canceled", Path: "/v1/setup_intents/{id}/cancel", Final: []string{"succeeded"}}},
}

// NewResource returns the resource constructor the provider registers.
func NewResource() (func() resource.Resource, error) {
	return engine.NewResource(Declaration, setup_intent.Table)
}

// NewDataSource returns the data source constructor. Stripe documents no setup intent field as unique, so the data source looks up by `id` only.
func NewDataSource() (func() datasource.DataSource, error) {
	return engine.NewDataSource(Declaration, setup_intent.Table)
}
