package invoice_test

import (
	"os"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"

	"github.com/computomatic/terraform-provider-stripe/internal/acctest"
	"github.com/computomatic/terraform-provider-stripe/internal/acctest/tfacc"
)

func TestMain(m *testing.M) { os.Exit(acctest.Main(m)) }

// A subscription invoiced by email creates its invoice at once (suite: "Subscriptions"), so
// the test reads the invoice through the subscription's `latest_invoice`.
func TestAccInvoice_read(t *testing.T) {
	acct := acctest.NewAccount(t)
	resource.ParallelTest(t, resource.TestCase{
		ProtoV6ProviderFactories: tfacc.ProviderFactories,
		Steps: []resource.TestStep{{
			Config: acct.ProviderBlock() + `
resource "stripe_customer" "test" {}

resource "stripe_product" "test" {
  name = "` + acct.Name("Plan") + `"
}

resource "stripe_price" "test" {
  product     = stripe_product.test.id
  currency    = "usd"
  unit_amount = 1000
  recurring = {
    interval = "month"
  }
}

resource "stripe_subscription" "test" {
  customer          = stripe_customer.test.id
  collection_method = "send_invoice"
  days_until_due    = 30
  items             = [{ price = stripe_price.test.id, quantity = 2 }]
}

data "stripe_invoice" "test" {
  id = stripe_subscription.test.latest_invoice
}
`,
			Check: resource.ComposeAggregateTestCheckFunc(
				resource.TestCheckResourceAttrPair("data.stripe_invoice.test", "id", "stripe_subscription.test", "latest_invoice"),
				resource.TestCheckResourceAttrPair("data.stripe_invoice.test", "customer", "stripe_customer.test", "id"),
				resource.TestCheckResourceAttr("data.stripe_invoice.test", "status", "open"),
				resource.TestCheckResourceAttr("data.stripe_invoice.test", "collection_method", "send_invoice"),
				resource.TestCheckResourceAttr("data.stripe_invoice.test", "currency", "usd"),
				resource.TestCheckResourceAttr("data.stripe_invoice.test", "amount_due", "2000"),
				resource.TestCheckResourceAttr("data.stripe_invoice.test", "lines.data.#", "1"),
				resource.TestCheckResourceAttrPair("data.stripe_invoice.test", "parent.subscription_details.subscription", "stripe_subscription.test", "id"),
			),
		}},
	})
}
