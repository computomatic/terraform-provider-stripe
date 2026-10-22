package treasury_transaction_test

import (
	"os"
	"regexp"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"

	"github.com/computomatic/terraform-provider-stripe/internal/acctest"
	"github.com/computomatic/terraform-provider-stripe/internal/acctest/tfacc"
)

func TestMain(m *testing.M) { os.Exit(acctest.Main(m)) }

const address = "data.stripe_treasury_transaction.test"

// config reads the object with the given ID through a provider configuration that acts on the
// connected account.
func config(acct, conn acctest.Config, id string) string {
	return acct.ProviderBlock() + conn.ConnectedProviderBlock() + `
data "stripe_treasury_transaction" "test" {
  provider = stripe.connected
  id       = "` + id + `"
}
`
}

// A received credit makes a transaction whose flow is the credit (suite: "its transaction").
func TestAccTreasuryTransaction_read(t *testing.T) {
	acct := acctest.NewAccount(t)
	conn := tfacc.IssuingAccount(t, acct)
	fa := tfacc.IssuingFinancialAccount(t, conn)
	credit, id := tfacc.ReceivedCredit(t, conn, fa, 1000)
	resource.ParallelTest(t, resource.TestCase{
		ProtoV6ProviderFactories: tfacc.ProviderFactories,
		Steps: []resource.TestStep{{
			Config: config(acct, conn, id),
			Check: resource.ComposeAggregateTestCheckFunc(
				resource.TestCheckResourceAttr(address, "id", id),
				resource.TestCheckResourceAttr(address, "financial_account", fa),
				resource.TestCheckResourceAttr(address, "amount", "1000"),
				resource.TestCheckResourceAttr(address, "currency", "usd"),
				resource.TestCheckResourceAttr(address, "flow_type", "received_credit"),
				resource.TestCheckResourceAttr(address, "flow", credit),
				resource.TestCheckResourceAttrSet(address, "status"),
				resource.TestCheckResourceAttrSet(address, "balance_impact.cash"),
			),
		}},
	})
}

// An unknown transaction is a 404 `resource_missing` (suite: "retrieve unknown").
func TestAccTreasuryTransaction_notFound(t *testing.T) {
	acct := acctest.NewAccount(t)
	conn := tfacc.IssuingAccount(t, acct)
	resource.ParallelTest(t, resource.TestCase{
		ProtoV6ProviderFactories: tfacc.ProviderFactories,
		Steps: []resource.TestStep{{
			Config:      config(acct, conn, "trxn_doesnotexist"),
			ExpectError: regexp.MustCompile("no stripe_treasury_transaction found for id=trxn_doesnotexist"),
		}},
	})
}
