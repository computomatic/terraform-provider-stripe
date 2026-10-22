package invoice_line_test

import (
	"os"
	"regexp"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"

	"github.com/computomatic/terraform-provider-stripe/internal/acctest"
	"github.com/computomatic/terraform-provider-stripe/internal/acctest/tfacc"
)

func TestMain(m *testing.M) { os.Exit(acctest.Main(m)) }

// Stripe has no retrieve for an invoice line (suite: "Invoice lines"), so the data source
// finds the line by its id among the invoice's lines.
func TestAccInvoiceLine_read(t *testing.T) {
	acct := acctest.NewAccount(t)
	_, invoice := tfacc.OpenInvoice(t, acct, "")
	line := tfacc.FirstListed(t, acct, "/v1/invoices/"+invoice+"/lines")
	currency := tfacc.AccountCurrency(t, acct)
	resource.ParallelTest(t, resource.TestCase{
		ProtoV6ProviderFactories: tfacc.ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: acct.ProviderBlock() + `
data "stripe_invoice_line" "test" {
  invoice = "` + invoice + `"
  id      = "` + line + `"
}
`,
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("data.stripe_invoice_line.test", "id", line),
					resource.TestCheckResourceAttr("data.stripe_invoice_line.test", "invoice", invoice),
					resource.TestCheckResourceAttr("data.stripe_invoice_line.test", "amount", "1500"),
					resource.TestCheckResourceAttr("data.stripe_invoice_line.test", "currency", currency),
					resource.TestCheckResourceAttr("data.stripe_invoice_line.test", "description", "Setup"),
					resource.TestCheckResourceAttrSet("data.stripe_invoice_line.test", "parent.invoice_item_details.invoice_item"),
				),
			},
			{
				Config: acct.ProviderBlock() + `
data "stripe_invoice_line" "test" {
  invoice = "` + invoice + `"
  id      = "il_doesnotexist"
}
`,
				ExpectError: regexp.MustCompile("no stripe_invoice_line found for id=il_doesnotexist"),
			},
		},
	})
}
