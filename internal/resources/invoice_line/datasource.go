// Package invoice_line declares the stripe_invoice_line data source. There is no resource.
package invoice_line

import (
	"github.com/hashicorp/terraform-plugin-framework/datasource"

	"github.com/computomatic/terraform-provider-stripe/internal/engine"
	"github.com/computomatic/terraform-provider-stripe/internal/generated/invoice_line"
)

// Declaration is what the spec cannot say about this object. Stripe has no retrieve for a line, only the list under its invoice, so the data source finds the line by `id` in that list. Lines are made by the invoice.
var Declaration = engine.Declaration{
	TypeName:    "stripe_invoice_line",
	IDPrefix:    "il_",
	Description: "A line of an invoice. Look one up by the `invoice` and the line's `id`.",
	Parent:      "invoice",
	FindInList:  true,
	Paths:       engine.Paths{List: "/v1/invoices/{invoice}/lines"},
	Delete:      engine.NoDelete,
}

// NewDataSource returns the data source constructor the provider registers. It needs the `invoice` and the `id` of the line, which it finds in the invoice's list.
func NewDataSource() (func() datasource.DataSource, error) {
	return engine.NewDataSource(Declaration, invoice_line.Table)
}
