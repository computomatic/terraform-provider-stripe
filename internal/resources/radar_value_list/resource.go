// Package radar_value_list declares the stripe_radar_value_list resource and data source.
package radar_value_list

import (
	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/resource"

	"github.com/computomatic/terraform-provider-stripe/internal/engine"
	"github.com/computomatic/terraform-provider-stripe/internal/generated/radar_value_list"
)

// Declaration is what the spec cannot say about a value list. Stripe deletes one with DELETE,
// which deletes its items too, so destroy deletes it. The `alias` is unique in the account.
var Declaration = engine.Declaration{
	TypeName:              "stripe_radar_value_list",
	IDPrefix:              "rsl_",
	Description:           "A list of values that Radar rules refer to by `alias`. `terraform destroy` deletes the list and every item in it.",
	DataSourceDescription: "A list of values that Radar rules refer to by `alias`.",
	Paths: engine.Paths{
		Create:   "/v1/radar/value_lists",
		Retrieve: "/v1/radar/value_lists/{id}",
		Update:   "/v1/radar/value_lists/{id}",
		Delete:   "/v1/radar/value_lists/{id}",
		List:     "/v1/radar/value_lists",
	},
	Delete:  engine.Delete,
	Lookups: []engine.Lookup{{Field: "alias"}},
}

// NewResource returns the resource constructor the provider registers.
func NewResource() (func() resource.Resource, error) {
	return engine.NewResource(Declaration, radar_value_list.Table)
}

// NewDataSource returns the data source constructor. It finds a list by `id` or by its unique `alias`.
func NewDataSource() (func() datasource.DataSource, error) {
	return engine.NewDataSource(Declaration, radar_value_list.Table)
}
