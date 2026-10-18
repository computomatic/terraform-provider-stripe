// Package setup_intent declares the stripe_setup_intent resource and data source.
package setup_intent

import (
	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/resource"

	"github.com/computomatic/terraform-provider-stripe/internal/engine"
	"github.com/computomatic/terraform-provider-stripe/internal/generated/setup_intent"
	"github.com/computomatic/terraform-provider-stripe/internal/wire"
)

// Declaration is what the spec cannot say about a setup intent. Stripe never deletes one, so
// destroy cancels it (suite: "SetupIntent", `cancel`) and drops it from state. A canceled
// intent stays readable and cancelling twice is refused (`already canceled`), so destroy
// reads the status first. A succeeded intent cannot be cancelled (`already succeeded`,
// `setup_intent_unexpected_state`), has done its work and is left as it is. An intent in
// another state that Stripe refuses to cancel fails the destroy with Stripe's error.
var Declaration = engine.Declaration{
	TypeName:    "stripe_setup_intent",
	IDPrefix:    "seti_",
	Description: "Collects a customer's payment method for later use. `terraform destroy` cancels the setup intent, because Stripe cannot delete one; the canceled intent stays in the account. An intent that already succeeded cannot be canceled and is left as it is. Confirming an intent is not a resource: confirm it with its `client_secret` from your front end, or from your server with Stripe's API, and `status` reports what happened. The resource has no `confirm`, `mandate_data`, `payment_method_data` or `payment_method_options` argument. Setting `payment_method` moves the intent to `requires_confirmation`. `usage` and `on_behalf_of` cannot change after creation, and neither can the other arguments that only a create takes, so changing one replaces the intent. The `client_secret` is sensitive and stays in the state.",
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
	// update are not proven, so changing one replaces the intent.
	ReplaceOn: []string{
		"allowed_payment_method_types", "attach_to_self", "customer_account", "excluded_payment_method_types",
		"flow_directions", "payment_method_configuration",
	},
	Delete: engine.Archive,
	Archive: &engine.ArchiveAction{
		Field: "status",
		Value: wire.String("canceled"),
		Path:  "/v1/setup_intents/{id}/cancel",
		Ended: []engine.Ended{{Field: "status", Value: wire.String("succeeded")}},
	},
}

// NewResource returns the resource constructor the provider registers.
func NewResource() (func() resource.Resource, error) {
	return engine.NewResource(Declaration, setup_intent.Table)
}

// NewDataSource returns the data source constructor. Stripe documents no setup intent field as unique, so the data source looks up by `id` only.
func NewDataSource() (func() datasource.DataSource, error) {
	return engine.NewDataSource(Declaration, setup_intent.Table)
}
