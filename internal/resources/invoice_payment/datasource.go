// Package invoice_payment declares the stripe_invoice_payment data source. There is no resource.
package invoice_payment

import (
	"github.com/hashicorp/terraform-plugin-framework/datasource"

	"github.com/computomatic/terraform-provider-stripe/internal/engine"
	"github.com/computomatic/terraform-provider-stripe/internal/generated/invoice_payment"
)

// Declaration is what the spec cannot say about this object. Stripe makes an invoice payment when an invoice is paid, so only the data source exists.
var Declaration = engine.Declaration{
	TypeName:    "stripe_invoice_payment",
	IDPrefix:    "inpay_",
	Description: "A payment made against an invoice, with the payment intent that paid it. Look one up by its `id`.",
	Paths:       engine.Paths{Retrieve: "/v1/invoice_payments/{id}", List: "/v1/invoice_payments"},
	Delete:      engine.NoDelete,
}

// NewDataSource returns the data source constructor the provider registers. It looks the object up by `id` only: the list filters by `invoice`, but an invoice can have more than one payment.
func NewDataSource() (func() datasource.DataSource, error) {
	return engine.NewDataSource(Declaration, invoice_payment.Table)
}
