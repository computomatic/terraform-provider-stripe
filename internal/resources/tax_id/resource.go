// Package tax_id declares the stripe_tax_id resource and data source.
package tax_id

import (
	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/resource"

	"github.com/computomatic/terraform-provider-stripe/internal/engine"
	"github.com/computomatic/terraform-provider-stripe/internal/generated/tax_id"
)

// Declaration is what the spec cannot say about a tax ID. Stripe serves one object through
// `/v1/tax_ids` and `/v1/customers/{customer}/tax_ids`; the suite proves `owner[type]` of
// `self` and `customer` on the first (suite: "create for a customer owner") and that a
// customer-owned ID retrieves there, so one resource covers both. Stripe never updates a tax
// ID (suite: "update is not served"), so every argument forces replacement. Destroy deletes.
var Declaration = engine.Declaration{
	TypeName:              "stripe_tax_id",
	IDPrefix:              "txi_",
	Description:           "A tax ID of your own account or of a customer, set by `owner`. Stripe does not update tax IDs, so every argument forces replacement. `terraform destroy` deletes the tax ID.",
	DataSourceDescription: "A tax ID of your own account or of a customer.",
	Paths: engine.Paths{
		Create:   "/v1/tax_ids",
		Retrieve: "/v1/tax_ids/{id}",
		Delete:   "/v1/tax_ids/{id}",
		List:     "/v1/tax_ids",
	},
	Delete:          engine.Delete,
	ServerDefaulted: []string{"owner"},
}

// NewResource returns the resource constructor the provider registers.
func NewResource() (func() resource.Resource, error) {
	return engine.NewResource(Declaration, tax_id.Table)
}

// NewDataSource returns the data source constructor. Stripe documents no tax ID field as unique, so the data source looks up by `id` only.
func NewDataSource() (func() datasource.DataSource, error) {
	return engine.NewDataSource(Declaration, tax_id.Table)
}
