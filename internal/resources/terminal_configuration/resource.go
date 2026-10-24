// Package terminal_configuration declares the stripe_terminal_configuration resource and data source.
package terminal_configuration

import (
	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/resource"

	"github.com/computomatic/terraform-provider-stripe/internal/engine"
	"github.com/computomatic/terraform-provider-stripe/internal/generated/terminal_configuration"
)

// Declaration is what the spec cannot say about a terminal configuration. Stripe deletes one
// with DELETE and answers a later GET with a deleted stub, so destroy deletes it. It answers
// an omitted `name` with `""` (suite: "create with no parameters").
var Declaration = engine.Declaration{
	TypeName:              "stripe_terminal_configuration",
	IDPrefix:              "tmc_",
	Description:           "How Stripe Terminal readers behave: tipping, splash screens, reboot window and Wi-Fi. `terraform destroy` deletes the configuration.",
	DataSourceDescription: "How Stripe Terminal readers behave: tipping, splash screens, reboot window and Wi-Fi.",
	Paths: engine.Paths{
		Create:   "/v1/terminal/configurations",
		Retrieve: "/v1/terminal/configurations/{id}",
		Update:   "/v1/terminal/configurations/{id}",
		Delete:   "/v1/terminal/configurations/{id}",
		List:     "/v1/terminal/configurations",
	},
	Delete:          engine.Delete,
	ServerDefaulted: []string{"name"},
	Sensitive: []string{
		"wifi.enterprise_eap_peap.password",
		"wifi.enterprise_eap_tls.private_key_file_password",
		"wifi.personal_psk.password",
	},
}

// NewResource returns the resource constructor the provider registers.
func NewResource() (func() resource.Resource, error) {
	return engine.NewResource(Declaration, terminal_configuration.Table)
}

// NewDataSource returns the data source constructor. No configuration field is documented as unique, so the data source looks up by `id` only.
func NewDataSource() (func() datasource.DataSource, error) {
	return engine.NewDataSource(Declaration, terminal_configuration.Table)
}
