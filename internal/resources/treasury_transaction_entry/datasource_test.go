package treasury_transaction_entry_test

import (
	"os"
	"regexp"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"

	"github.com/computomatic/terraform-provider-stripe/internal/acctest"
	"github.com/computomatic/terraform-provider-stripe/internal/acctest/tfacc"
)

func TestMain(m *testing.M) { os.Exit(acctest.Main(m)) }

const address = "data.stripe_treasury_transaction_entry.test"

// config reads the object with the given ID through a provider configuration that acts on the
// connected account.
func config(acct, conn acctest.Config, id string) string {
	return acct.ProviderBlock() + conn.ConnectedProviderBlock() + `
data "stripe_treasury_transaction_entry" "test" {
  provider = stripe.connected
  id       = "` + id + `"
}
`
}

// A received credit's transaction has entries (suite: "its transaction").
func TestAccTreasuryTransactionEntry_read(t *testing.T) {
	acct := acctest.NewAccount(t)
	conn := tfacc.IssuingAccount(t, acct)
	fa := tfacc.IssuingFinancialAccount(t, conn)
	credit, transaction := tfacc.ReceivedCredit(t, conn, fa, 1000)
	id := tfacc.FirstListed(t, conn, "/v1/treasury/transaction_entries?financial_account="+fa+"&transaction="+transaction)
	resource.ParallelTest(t, resource.TestCase{
		ProtoV6ProviderFactories: tfacc.ProviderFactories,
		Steps: []resource.TestStep{{
			Config: config(acct, conn, id),
			Check: resource.ComposeAggregateTestCheckFunc(
				resource.TestCheckResourceAttr(address, "id", id),
				resource.TestCheckResourceAttr(address, "financial_account", fa),
				resource.TestCheckResourceAttr(address, "transaction", transaction),
				resource.TestCheckResourceAttr(address, "flow", credit),
				resource.TestCheckResourceAttr(address, "flow_type", "received_credit"),
				resource.TestCheckResourceAttr(address, "currency", "usd"),
				resource.TestCheckResourceAttrSet(address, "type"),
			),
		}},
	})
}

// An unknown transaction entry is a 404 `resource_missing` (suite: "retrieve unknown").
func TestAccTreasuryTransactionEntry_notFound(t *testing.T) {
	acct := acctest.NewAccount(t)
	conn := tfacc.IssuingAccount(t, acct)
	resource.ParallelTest(t, resource.TestCase{
		ProtoV6ProviderFactories: tfacc.ProviderFactories,
		Steps: []resource.TestStep{{
			Config:      config(acct, conn, "trxne_doesnotexist"),
			ExpectError: regexp.MustCompile("no stripe_treasury_transaction_entry found for id=trxne_doesnotexist"),
		}},
	})
}
