// Package invoice declares the stripe_invoice data source: a bill sent to a customer. There is no resource.
package invoice

import (
	"github.com/hashicorp/terraform-plugin-framework/datasource"

	"github.com/computomatic/terraform-provider-stripe/internal/engine"
	"github.com/computomatic/terraform-provider-stripe/internal/generated/invoice"
)

// Declaration is what the spec cannot say about an invoice. Invoices come from
// subscriptions, quotes and invoice items, and moving one through its states (finalize, pay,
// void) is an action, so only the data source exists. The invoice's last finalization error
// is not available.
var Declaration = engine.Declaration{
	TypeName:    "stripe_invoice",
	IDPrefix:    "in_",
	Description: "A bill sent to a customer, such as the `latest_invoice` of a subscription. Look one up by its `id`. The invoice's `last_finalization_error` is not available.",
	Paths:       engine.Paths{Retrieve: "/v1/invoices/{id}"},
	Delete:      engine.NoDelete,
	// The confirmation secret is the client secret of the invoice's payment.
	Sensitive: []string{"confirmation_secret.client_secret"},
}

// NewDataSource returns the data source constructor the provider registers. It looks an invoice up by `id` only: the list endpoint does not filter by the invoice `number`.
func NewDataSource() (func() datasource.DataSource, error) {
	return engine.NewDataSource(Declaration, invoice.Table)
}
