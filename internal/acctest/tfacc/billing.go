package tfacc

import (
	"testing"

	"github.com/computomatic/terraform-provider-stripe/internal/acctest"
	"github.com/computomatic/terraform-provider-stripe/internal/stripeapi"
	"github.com/computomatic/terraform-provider-stripe/internal/wire"
)

// AccountCurrency returns the account's default currency. The fake's account settles in the
// currency of the dev sandbox it copies (`cad`), so a test that needs one reads it instead of
// naming one.
func AccountCurrency(t *testing.T, c acctest.Config) string {
	t.Helper()
	resp, err := Client(c).Do(t.Context(), stripeapi.Request{Method: "GET", Path: "/v1/account"})
	if err != nil {
		t.Fatalf("reading the account: %v", err)
	}
	currency, _ := resp.Body.(wire.Object).GetString("default_currency")
	if currency == "" {
		t.Fatalf("the account has no default currency: %v", resp.Body)
	}
	return currency
}

// OpenInvoice creates a customer with one invoice item of 1500 and a finalized `send_invoice`
// invoice for it, which needs no payment method (suite: "Invoices"). An empty currency is the
// account's. It returns the customer's ID and the invoice's.
func OpenInvoice(t *testing.T, c acctest.Config, currency string) (customer, invoice string) {
	t.Helper()
	if currency == "" {
		currency = AccountCurrency(t, c)
	}
	customer = Create(t, c, "/v1/customers", nil)
	Create(t, c, "/v1/invoiceitems", wire.Object{
		{Name: "customer", Value: wire.String(customer)},
		{Name: "amount", Value: wire.Int(1500)},
		{Name: "currency", Value: wire.String(currency)},
		{Name: "description", Value: wire.String("Setup")},
	})
	invoice = Create(t, c, "/v1/invoices", wire.Object{
		{Name: "customer", Value: wire.String(customer)},
		{Name: "currency", Value: wire.String(currency)},
		{Name: "collection_method", Value: wire.String("send_invoice")},
		{Name: "days_until_due", Value: wire.Int(30)},
		{Name: "auto_advance", Value: wire.Bool(false)},
		{Name: "pending_invoice_items_behavior", Value: wire.String("include")},
	})
	Post(t, c, "/v1/invoices/"+invoice+"/finalize", nil)
	return customer, invoice
}

// FirstListed returns the ID of the first object a list endpoint returns.
func FirstListed(t *testing.T, c acctest.Config, path string) string {
	t.Helper()
	resp, err := Client(c).Do(t.Context(), stripeapi.Request{Method: "GET", Path: path})
	if err != nil {
		t.Fatalf("GET %s: %v", path, err)
	}
	data, _ := resp.Body.(wire.Object).Get("data")
	items, _ := data.(wire.Array)
	if len(items) == 0 {
		t.Fatalf("GET %s lists nothing", path)
	}
	id, _ := items[0].(wire.Object).GetString("id")
	return id
}
