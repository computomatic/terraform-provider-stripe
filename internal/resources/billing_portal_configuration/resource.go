// Package billing_portal_configuration declares the stripe_billing_portal_configuration
// resource and data source.
package billing_portal_configuration

import (
	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/resource"

	"github.com/computomatic/terraform-provider-stripe/internal/engine"
	"github.com/computomatic/terraform-provider-stripe/internal/generated/billing_portal_configuration"
	"github.com/computomatic/terraform-provider-stripe/internal/wire"
)

// Declaration is what the spec cannot say about a portal configuration. Stripe never
// deletes one, so destroy deactivates it (`active = false`) and drops it from state. The
// first configuration an account creates becomes its default, and Stripe refuses to
// deactivate a default: when the archive fails and the configuration is the default
// (`is_default`), destroy warns, leaves it active and drops it from state. Any other
// failure fails the destroy.
var Declaration = engine.Declaration{
	TypeName:              "stripe_billing_portal_configuration",
	IDPrefix:              "bpc_",
	Description:           "How the customer portal looks and what customers can do in it. `terraform destroy` deactivates the configuration, because Stripe cannot delete one. Stripe refuses to deactivate the account's default configuration (the first one created), so destroy leaves that one active with a warning.",
	DataSourceDescription: "How the customer portal looks and what customers can do in it.",
	Paths: engine.Paths{
		Create:   "/v1/billing_portal/configurations",
		Retrieve: "/v1/billing_portal/configurations/{id}",
		Update:   "/v1/billing_portal/configurations/{id}",
		List:     "/v1/billing_portal/configurations",
	},
	Delete:   engine.Archive,
	Archive:  &engine.ArchiveAction{Field: "active", Value: wire.Bool(false)},
	KeepWhen: &engine.FieldIs{Field: "is_default", Value: wire.Bool(true)},
}

// NewResource returns the resource constructor the provider registers.
func NewResource() (func() resource.Resource, error) {
	return engine.NewResource(Declaration, billing_portal_configuration.Table)
}

// NewDataSource returns the data source constructor. No portal configuration field is documented as unique, so the data source looks up by `id` only.
func NewDataSource() (func() datasource.DataSource, error) {
	return engine.NewDataSource(Declaration, billing_portal_configuration.Table)
}
