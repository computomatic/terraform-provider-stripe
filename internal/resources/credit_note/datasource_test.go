package credit_note_test

import (
	"os"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"

	"github.com/computomatic/terraform-provider-stripe/internal/acctest"
	"github.com/computomatic/terraform-provider-stripe/internal/acctest/tfacc"
	"github.com/computomatic/terraform-provider-stripe/internal/stripeapi"
	"github.com/computomatic/terraform-provider-stripe/internal/wire"
)

func TestMain(m *testing.M) { os.Exit(acctest.Main(m)) }

// create posts to Stripe behind the provider's back and returns the new object's ID.
func create(t *testing.T, acct acctest.Config, path string, params wire.Object) string {
	t.Helper()
	resp, err := tfacc.Client(acct).Do(t.Context(), stripeapi.Request{Method: "POST", Path: path, Params: params})
	if err != nil {
		t.Fatalf("POST %s: %v", path, err)
	}
	id, ok := resp.Body.(wire.Object).GetString("id")
	if !ok {
		t.Fatalf("POST %s: no id in %v", path, resp.Body)
	}
	return id
}

// A credit note is issued against an open invoice (suite: "Credit notes"): a custom line
// item on an invoice invoiced by email needs no card.
func TestAccCreditNote_read(t *testing.T) {
	acct := acctest.NewAccount(t)
	customer := create(t, acct, "/v1/customers", nil)
	invoice := create(t, acct, "/v1/invoices", wire.Object{
		{Name: "customer", Value: wire.String(customer)},
		{Name: "currency", Value: wire.String("usd")},
		{Name: "collection_method", Value: wire.String("send_invoice")},
		{Name: "days_until_due", Value: wire.Int(30)},
	})
	create(t, acct, "/v1/invoiceitems", wire.Object{
		{Name: "customer", Value: wire.String(customer)},
		{Name: "invoice", Value: wire.String(invoice)},
		{Name: "amount", Value: wire.Int(5000)},
		{Name: "currency", Value: wire.String("usd")},
	})
	tfacc.Post(t, acct, "/v1/invoices/"+invoice+"/finalize", nil)
	note := create(t, acct, "/v1/credit_notes", wire.Object{
		{Name: "invoice", Value: wire.String(invoice)},
		{Name: "reason", Value: wire.String("order_change")},
		{Name: "lines", Value: wire.Array{wire.Object{
			{Name: "type", Value: wire.String("custom_line_item")},
			{Name: "description", Value: wire.String("Goodwill")},
			{Name: "quantity", Value: wire.Int(1)},
			{Name: "unit_amount", Value: wire.Int(1500)},
		}}},
	})
	resource.ParallelTest(t, resource.TestCase{
		ProtoV6ProviderFactories: tfacc.ProviderFactories,
		Steps: []resource.TestStep{{
			Config: acct.ProviderBlock() + `
data "stripe_credit_note" "test" {
  id = "` + note + `"
}
`,
			Check: resource.ComposeAggregateTestCheckFunc(
				resource.TestCheckResourceAttr("data.stripe_credit_note.test", "id", note),
				resource.TestCheckResourceAttr("data.stripe_credit_note.test", "invoice", invoice),
				resource.TestCheckResourceAttr("data.stripe_credit_note.test", "customer", customer),
				resource.TestCheckResourceAttr("data.stripe_credit_note.test", "status", "issued"),
				resource.TestCheckResourceAttr("data.stripe_credit_note.test", "reason", "order_change"),
				resource.TestCheckResourceAttr("data.stripe_credit_note.test", "amount", "1500"),
			),
		}},
	})
}
