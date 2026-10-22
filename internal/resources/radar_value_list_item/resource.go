// Package radar_value_list_item declares the stripe_radar_value_list_item resource and data source.
package radar_value_list_item

import (
	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/resource"

	"github.com/computomatic/terraform-provider-stripe/internal/engine"
	"github.com/computomatic/terraform-provider-stripe/internal/generated/radar_value_list_item"
)

// Declaration is what the spec cannot say about a value list item. Stripe never updates one,
// so every argument forces replacement. Destroy deletes the item.
var Declaration = engine.Declaration{
	TypeName:              "stripe_radar_value_list_item",
	IDPrefix:              "rsli_",
	Description:           "A value in a Radar value list. `terraform destroy` deletes the item.",
	DataSourceDescription: "A value in a Radar value list.",
	Paths: engine.Paths{
		Create:   "/v1/radar/value_list_items",
		Retrieve: "/v1/radar/value_list_items/{id}",
		Delete:   "/v1/radar/value_list_items/{id}",
		List:     "/v1/radar/value_list_items",
	},
	Delete: engine.Delete,
}

// NewResource returns the resource constructor the provider registers.
func NewResource() (func() resource.Resource, error) {
	return engine.NewResource(Declaration, radar_value_list_item.Table)
}

// NewDataSource returns the data source constructor. An item is unique only within its list, so the data source looks up by `id` only.
func NewDataSource() (func() datasource.DataSource, error) {
	return engine.NewDataSource(Declaration, radar_value_list_item.Table)
}
