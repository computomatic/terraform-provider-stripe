// Package invoiceitem declares the stripe_invoiceitem resource and data source.
package invoiceitem

import (
	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/resource"

	"github.com/computomatic/terraform-provider-stripe/internal/engine"
	"github.com/computomatic/terraform-provider-stripe/internal/generated/invoiceitem"
)

// Declaration is what the spec cannot say about an invoice item. Stripe deletes one with
// DELETE (suite: "Invoice items", `delete`), unless its invoice is finalized or void: that
// is a 400 without a code, which is shown as it is because a bare 400 does not say why.
var Declaration = engine.Declaration{
	TypeName:              "stripe_invoiceitem",
	IDPrefix:              "ii_",
	Description:           "A line to add to a customer's next invoice, or to a draft invoice. `terraform destroy` deletes the invoice item. Stripe refuses to delete or change one whose invoice is finalized or void; remove it from the state with `terraform state rm` then.",
	DataSourceDescription: "A line to add to a customer's next invoice, or to a draft invoice.",
	Paths: engine.Paths{
		Create:   "/v1/invoiceitems",
		Retrieve: "/v1/invoiceitems/{id}",
		Update:   "/v1/invoiceitems/{id}",
		Delete:   "/v1/invoiceitems/{id}",
		List:     "/v1/invoiceitems",
	},
	// Stripe reports the price an item was made from, or a one-off amount stood for, in
	// `pricing` (suite: "the object has `pricing`"), although the request may name none. A
	// pending item names the draft invoice that sweeps it up (suite: "draft produced by a
	// pending invoice item").
	ServerDefaulted: []string{"invoice", "pricing"},
	Delete:          engine.Delete,
}

// NewResource returns the resource constructor the provider registers.
func NewResource() (func() resource.Resource, error) {
	return engine.NewResource(Declaration, invoiceitem.Table)
}

// NewDataSource returns the data source constructor. Stripe documents no invoice item field as unique, so the data source looks up by `id` only.
func NewDataSource() (func() datasource.DataSource, error) {
	return engine.NewDataSource(Declaration, invoiceitem.Table)
}
