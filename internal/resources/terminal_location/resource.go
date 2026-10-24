// Package terminal_location declares the stripe_terminal_location resource and data source.
package terminal_location

import (
	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/resource"

	"github.com/computomatic/terraform-provider-stripe/internal/engine"
	"github.com/computomatic/terraform-provider-stripe/internal/generated/terminal_location"
)

// Declaration is what the spec cannot say about a terminal location. Stripe deletes one with
// DELETE and answers a later GET with a deleted stub, so destroy deletes it. Stripe refuses
// to change a location's country after creation. It answers an omitted `address.line2` with
// `""` (suite: "create minimal") and refuses a US location without `address.state` (suite:
// "create in the US without address state").
var Declaration = engine.Declaration{
	TypeName:              "stripe_terminal_location",
	IDPrefix:              "tml_",
	Description:           "A place where you use Stripe Terminal readers. `terraform destroy` deletes the location. Changing the country of `address` creates a new location, because Stripe cannot change it. A location in the US needs `address.state`.",
	DataSourceDescription: "A place where you use Stripe Terminal readers.",
	Paths: engine.Paths{
		Create:   "/v1/terminal/locations",
		Retrieve: "/v1/terminal/locations/{id}",
		Update:   "/v1/terminal/locations/{id}",
		Delete:   "/v1/terminal/locations/{id}",
		List:     "/v1/terminal/locations",
	},
	Delete:          engine.Delete,
	ReplaceOn:       []string{"address.country"},
	ServerDefaulted: []string{"address.line2"},
}

// NewResource returns the resource constructor the provider registers.
func NewResource() (func() resource.Resource, error) {
	return engine.NewResource(Declaration, terminal_location.Table)
}

// NewDataSource returns the data source constructor. No location field is documented as unique, so the data source looks up by `id` only.
func NewDataSource() (func() datasource.DataSource, error) {
	return engine.NewDataSource(Declaration, terminal_location.Table)
}
