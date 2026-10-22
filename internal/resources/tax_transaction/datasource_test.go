package tax_transaction_test

import (
	"os"
	"regexp"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"

	"github.com/computomatic/terraform-provider-stripe/internal/acctest"
	"github.com/computomatic/terraform-provider-stripe/internal/acctest/tfacc"
)

func TestMain(m *testing.M) { os.Exit(acctest.Main(m)) }

const address = "data.stripe_tax_transaction.test"

func config(c acctest.Config, id string) string {
	return c.ProviderBlock() + `
data "stripe_tax_transaction" "test" {
  id = "` + id + `"
}
`
}

func TestAccTaxTransaction_read(t *testing.T) {
	acct := acctest.NewAccount(t)
	reference := acct.Unique("tfacc-tax-transaction-read")
	id := tfacc.TaxTransaction(t, acct, reference)
	resource.ParallelTest(t, resource.TestCase{
		ProtoV6ProviderFactories: tfacc.ProviderFactories,
		Steps: []resource.TestStep{{
			Config: config(acct, id),
			Check: resource.ComposeAggregateTestCheckFunc(
				resource.TestCheckResourceAttr(address, "id", id),
				resource.TestCheckResourceAttr(address, "reference", reference),
				resource.TestCheckResourceAttr(address, "type", "transaction"),
				resource.TestCheckResourceAttr(address, "currency", tfacc.AccountCurrency(t, acct)),
				resource.TestCheckResourceAttr(address, "customer_details.address.country", "US"),
				resource.TestCheckResourceAttr(address, "customer_details.address_source", "shipping"),
				resource.TestCheckNoResourceAttr(address, "reversal"),
				resource.TestCheckResourceAttrSet(address, "tax_date"),
			),
		}},
	})
}

// An unknown transaction is a 404 `resource_missing` (suite: "retrieve unknown", an altered
// real ID).
func TestAccTaxTransaction_notFound(t *testing.T) {
	acct := acctest.NewAccount(t)
	missing := tfacc.AlteredID(tfacc.TaxTransaction(t, acct, acct.Unique("tfacc-tax-transaction-missing")))
	resource.ParallelTest(t, resource.TestCase{
		ProtoV6ProviderFactories: tfacc.ProviderFactories,
		Steps: []resource.TestStep{{
			Config:      config(acct, missing),
			ExpectError: regexp.MustCompile(`no stripe_tax_transaction found for\s+id=` + missing),
		}},
	})
}
