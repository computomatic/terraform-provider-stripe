// Package tax_rate declares the stripe_tax_rate resource and data source.
package tax_rate

import (
	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/resource"

	"github.com/computomatic/terraform-provider-stripe/internal/engine"
	"github.com/computomatic/terraform-provider-stripe/internal/generated/tax_rate"
	"github.com/computomatic/terraform-provider-stripe/internal/wire"
)

// Declaration is what the spec cannot say about a tax rate. Stripe never deletes a tax
// rate (DELETE answers with an error), so destroy archives it (`active = false`) and drops
// it from state; the tax rate stays in the account.
var Declaration = engine.Declaration{
	TypeName:    "stripe_tax_rate",
	IDPrefix:    "txr_",
	Description: "A rate of tax that invoices and subscriptions apply. `terraform destroy` archives the tax rate, because Stripe cannot delete one.",
	Paths: engine.Paths{
		Create:   "/v1/tax_rates",
		Retrieve: "/v1/tax_rates/{id}",
		Update:   "/v1/tax_rates/{id}",
		List:     "/v1/tax_rates",
	},
	Delete:  engine.Archive,
	Archive: &engine.ArchiveAction{Field: "active", Value: wire.Bool(false)},
}

// NewResource returns the resource constructor the provider registers.
func NewResource() (func() resource.Resource, error) {
	return engine.NewResource(Declaration, tax_rate.Table)
}

// NewDataSource returns the data source constructor. No tax rate field is documented as unique, so the data source looks up by `id` only.
func NewDataSource() (func() datasource.DataSource, error) {
	return engine.NewDataSource(Declaration, tax_rate.Table)
}
