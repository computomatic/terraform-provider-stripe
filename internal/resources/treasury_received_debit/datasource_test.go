package treasury_received_debit_test

import (
	"os"
	"regexp"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"

	"github.com/computomatic/terraform-provider-stripe/internal/acctest"
	"github.com/computomatic/terraform-provider-stripe/internal/acctest/tfacc"
)

func TestMain(m *testing.M) { os.Exit(acctest.Main(m)) }

const address = "data.stripe_treasury_received_debit.test"

// config reads the object with the given ID through a provider configuration that acts on the
// connected account.
func config(acct, conn acctest.Config, id string) string {
	return acct.ProviderBlock() + conn.ConnectedProviderBlock() + `
data "stripe_treasury_received_debit" "test" {
  provider = stripe.connected
  id       = "` + id + `"
}
`
}

// Stripe's test helper makes a received debit, which a received credit funds first.
func TestAccTreasuryReceivedDebit_read(t *testing.T) {
	acct := acctest.NewAccount(t)
	conn := tfacc.IssuingAccount(t, acct)
	fa := tfacc.IssuingFinancialAccount(t, conn)
	tfacc.ReceivedCredit(t, conn, fa, 1000)
	id, transaction := tfacc.ReceivedDebit(t, conn, fa, 100)
	resource.ParallelTest(t, resource.TestCase{
		ProtoV6ProviderFactories: tfacc.ProviderFactories,
		Steps: []resource.TestStep{{
			Config: config(acct, conn, id),
			Check: resource.ComposeAggregateTestCheckFunc(
				resource.TestCheckResourceAttr(address, "id", id),
				resource.TestCheckResourceAttr(address, "financial_account", fa),
				resource.TestCheckResourceAttr(address, "amount", "100"),
				resource.TestCheckResourceAttr(address, "currency", "usd"),
				resource.TestCheckResourceAttr(address, "network", "ach"),
				resource.TestCheckResourceAttr(address, "transaction", transaction),
				resource.TestCheckResourceAttrSet(address, "status"),
			),
		}},
	})
}

// An unknown received debit is a 404 `resource_missing` (suite: "retrieve unknown").
func TestAccTreasuryReceivedDebit_notFound(t *testing.T) {
	acct := acctest.NewAccount(t)
	conn := tfacc.IssuingAccount(t, acct)
	resource.ParallelTest(t, resource.TestCase{
		ProtoV6ProviderFactories: tfacc.ProviderFactories,
		Steps: []resource.TestStep{{
			Config:      config(acct, conn, "rd_doesnotexist"),
			ExpectError: regexp.MustCompile("no stripe_treasury_received_debit found for id=rd_doesnotexist"),
		}},
	})
}
