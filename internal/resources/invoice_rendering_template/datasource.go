// Package invoice_rendering_template declares the stripe_invoice_rendering_template data source. There is no resource.
package invoice_rendering_template

import (
	"github.com/hashicorp/terraform-plugin-framework/datasource"

	"github.com/computomatic/terraform-provider-stripe/internal/engine"
	"github.com/computomatic/terraform-provider-stripe/internal/generated/invoice_rendering_template"
)

// Declaration is what the spec cannot say about this object. Templates are made in the Dashboard, and the API only reads, archives and unarchives them (suite: "Invoice rendering templates and verification reports are empty on the dev sandbox"), so only the data source exists.
var Declaration = engine.Declaration{
	TypeName:    "stripe_invoice_rendering_template",
	Description: "A template, made in the Stripe Dashboard, that sets how invoices look. Look one up by its `id`.",
	Paths:       engine.Paths{Retrieve: "/v1/invoice_rendering_templates/{id}", List: "/v1/invoice_rendering_templates"},
	Delete:      engine.NoDelete,
}

// NewDataSource returns the data source constructor the provider registers. It looks the object up by `id` only: Stripe documents no field of it as unique.
func NewDataSource() (func() datasource.DataSource, error) {
	return engine.NewDataSource(Declaration, invoice_rendering_template.Table)
}
