package invoice_payment_test

import (
	"os"
	"regexp"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"

	"github.com/computomatic/terraform-provider-stripe/internal/acctest"
	"github.com/computomatic/terraform-provider-stripe/internal/acctest/tfacc"
	"github.com/computomatic/terraform-provider-stripe/internal/wire"
)

func TestMain(m *testing.M) { os.Exit(acctest.Main(m)) }

// Paying a finalized invoice with an attached test card makes one invoice payment (suite:
// "Invoice payments"). It is read by its id; the list pages differently on some accounts, so
// the data source never pages it.
func TestAccInvoicePayment_read(t *testing.T) {
	acct := acctest.NewAccount(t)
	// The fake confirms payments in US dollars only, as the suite pays its invoices.
	const currency = "usd"
	customer, invoice := tfacc.OpenInvoice(t, acct, currency)
	method := tfacc.Create(t, acct, "/v1/payment_methods/pm_card_visa/attach", wire.Object{{Name: "customer", Value: wire.String(customer)}})
	tfacc.Post(t, acct, "/v1/invoices/"+invoice+"/pay", wire.Object{{Name: "payment_method", Value: wire.String(method)}})
	id := tfacc.FirstListed(t, acct, "/v1/invoice_payments?invoice="+invoice)
	resource.ParallelTest(t, resource.TestCase{
		ProtoV6ProviderFactories: tfacc.ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: acct.ProviderBlock() + `
data "stripe_invoice_payment" "test" {
  id = "` + id + `"
}
`,
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("data.stripe_invoice_payment.test", "id", id),
					resource.TestCheckResourceAttr("data.stripe_invoice_payment.test", "invoice", invoice),
					resource.TestCheckResourceAttr("data.stripe_invoice_payment.test", "status", "paid"),
					resource.TestCheckResourceAttr("data.stripe_invoice_payment.test", "amount_paid", "1500"),
					resource.TestCheckResourceAttr("data.stripe_invoice_payment.test", "amount_requested", "1500"),
					resource.TestCheckResourceAttr("data.stripe_invoice_payment.test", "currency", currency),
					resource.TestCheckResourceAttr("data.stripe_invoice_payment.test", "is_default", "true"),
					resource.TestCheckResourceAttr("data.stripe_invoice_payment.test", "payment.type", "payment_intent"),
					resource.TestCheckResourceAttrSet("data.stripe_invoice_payment.test", "payment.payment_intent"),
					resource.TestCheckResourceAttrSet("data.stripe_invoice_payment.test", "status_transitions.paid_at"),
				),
			},
			{
				Config: acct.ProviderBlock() + `
data "stripe_invoice_payment" "test" {
  id = "inpay_doesnotexist"
}
`,
				ExpectError: regexp.MustCompile("no stripe_invoice_payment found for id=inpay_doesnotexist"),
			},
		},
	})
}
