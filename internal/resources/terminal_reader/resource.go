// Package terminal_reader declares the stripe_terminal_reader resource and data source.
package terminal_reader

import (
	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/resource"

	"github.com/computomatic/terraform-provider-stripe/internal/engine"
	"github.com/computomatic/terraform-provider-stripe/internal/generated/terminal_reader"
)

// Declaration is what the spec cannot say about a terminal reader. Stripe deletes one with
// DELETE and answers a later GET with a deleted stub (suite: "TerminalReader", "delete"), so
// destroy deletes it. Stripe refuses to change a reader's location or registration code
// ("update errors", "immutable or unknown"), so both are create-only and changing one
// replaces the reader.
var Declaration = engine.Declaration{
	TypeName:    "stripe_terminal_reader",
	IDPrefix:    "tmr_",
	Description: "A Stripe Terminal card reader registered at a location. `terraform destroy` deletes the reader. Only `label` and `metadata` change in place: changing the `location` or the `registration_code` creates a new reader. Stripe never returns the `registration_code`, so an imported reader has none in state: a configuration that sets it plans a replacement unless it ignores changes to it with `lifecycle { ignore_changes = [registration_code] }`. In test mode the registration code `simulated-wpe` registers a simulated reader. The reader's `action` reports the most recent action it performed; the resource does not start actions, and the nested objects of an action's API error are not available.",
	Paths: engine.Paths{
		Create:   "/v1/terminal/readers",
		Retrieve: "/v1/terminal/readers/{id}",
		Update:   "/v1/terminal/readers/{id}",
		Delete:   "/v1/terminal/readers/{id}",
		List:     "/v1/terminal/readers",
	},
	Delete: engine.Delete,
}

// NewResource returns the resource constructor the provider registers.
func NewResource() (func() resource.Resource, error) {
	return engine.NewResource(Declaration, terminal_reader.Table)
}

// NewDataSource returns the data source constructor. The list endpoint filters by none of the reader's unique fields, so the data source looks up by `id` only.
func NewDataSource() (func() datasource.DataSource, error) {
	return engine.NewDataSource(Declaration, terminal_reader.Table)
}
