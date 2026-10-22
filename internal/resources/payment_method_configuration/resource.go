// Package payment_method_configuration declares the stripe_payment_method_configuration resource and data source.
package payment_method_configuration

import (
	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/resource"

	"github.com/computomatic/terraform-provider-stripe/internal/engine"
	"github.com/computomatic/terraform-provider-stripe/internal/fields"
	"github.com/computomatic/terraform-provider-stripe/internal/generated/payment_method_configuration"
	"github.com/computomatic/terraform-provider-stripe/internal/wire"
)

// Declaration is what the spec cannot say about a payment method configuration. Stripe
// never deletes one, so destroy deactivates it (`active = false`) and drops it from state.
var Declaration = engine.Declaration{
	TypeName:              "stripe_payment_method_configuration",
	IDPrefix:              "pmc_",
	Description:           "Which payment methods are offered and how. `terraform destroy` deactivates the configuration, because Stripe cannot delete one; it stays in the account.",
	DataSourceDescription: "Which payment methods are offered and how.",
	Paths: engine.Paths{
		Create:   "/v1/payment_method_configurations",
		Retrieve: "/v1/payment_method_configurations/{id}",
		Update:   "/v1/payment_method_configurations/{id}",
		List:     "/v1/payment_method_configurations",
	},
	Delete:  engine.Archive,
	Archive: &engine.ArchiveAction{Field: "active", Value: wire.Bool(false)},
	// Stripe returns a preference for every payment method, whether or not the request set it.
	ServerDefaulted: paymentMethods(),
}

// paymentMethods lists the table's top-level objects, which are the payment methods.
func paymentMethods() []string {
	var names []string
	for _, f := range payment_method_configuration.Table {
		if f.Shape == fields.Object {
			names = append(names, f.Name)
		}
	}
	return names
}

// NewResource returns the resource constructor the provider registers.
func NewResource() (func() resource.Resource, error) {
	return engine.NewResource(Declaration, payment_method_configuration.Table)
}

// NewDataSource returns the data source constructor. No payment method configuration field is documented as unique, so the data source looks up by `id` only.
func NewDataSource() (func() datasource.DataSource, error) {
	return engine.NewDataSource(Declaration, payment_method_configuration.Table)
}
