package tax_transaction_line_item_test

import (
	"os"
	"regexp"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"

	"github.com/computomatic/terraform-provider-stripe/internal/acctest"
	"github.com/computomatic/terraform-provider-stripe/internal/acctest/tfacc"
)

func TestMain(m *testing.M) { os.Exit(acctest.Main(m)) }

const address = "data.stripe_tax_transaction_line_item.test"

func config(c acctest.Config, transaction, id string) string {
	return c.ProviderBlock() + `
data "stripe_tax_transaction_line_item" "test" {
  transaction = "` + transaction + `"
  id          = "` + id + `"
}
`
}

// Stripe has no retrieve for a line item, so the data source finds the item by its id among
// the transaction's line items, which list in the calculation's order (suite: "Tax
// transactions from a calculation ignore an unknown line item cursor").
func TestAccTaxTransactionLineItem_read(t *testing.T) {
	acct := acctest.NewAccount(t)
	transaction := tfacc.TaxTransaction(t, acct, acct.Unique("tfacc-tax-line-read"))
	id := tfacc.FirstListed(t, acct, "/v1/tax/transactions/"+transaction+"/line_items")
	resource.ParallelTest(t, resource.TestCase{
		ProtoV6ProviderFactories: tfacc.ProviderFactories,
		Steps: []resource.TestStep{{
			Config: config(acct, transaction, id),
			Check: resource.ComposeAggregateTestCheckFunc(
				resource.TestCheckResourceAttr(address, "id", id),
				resource.TestCheckResourceAttr(address, "transaction", transaction),
				resource.TestCheckResourceAttr(address, "reference", "tfacc-a"),
				resource.TestCheckResourceAttr(address, "amount", "1000"),
				resource.TestCheckResourceAttr(address, "amount_tax", "0"),
				resource.TestCheckResourceAttr(address, "quantity", "1"),
				resource.TestCheckResourceAttr(address, "tax_behavior", "exclusive"),
				resource.TestCheckResourceAttr(address, "tax_code", "txcd_99999999"),
				resource.TestCheckNoResourceAttr(address, "reversal"),
			),
		}},
	})
}

// A line item the transaction does not list is not found. Stripe answers an unknown cursor
// with the first page, and the provider pages only by the ids it was given, so it does not
// matter.
func TestAccTaxTransactionLineItem_notFound(t *testing.T) {
	acct := acctest.NewAccount(t)
	transaction := tfacc.TaxTransaction(t, acct, acct.Unique("tfacc-tax-line-missing"))
	resource.ParallelTest(t, resource.TestCase{
		ProtoV6ProviderFactories: tfacc.ProviderFactories,
		Steps: []resource.TestStep{{
			Config:      config(acct, transaction, "tax_li_doesnotexist"),
			ExpectError: regexp.MustCompile(`no stripe_tax_transaction_line_item found for\s+id=tax_li_doesnotexist`),
		}},
	})
}
