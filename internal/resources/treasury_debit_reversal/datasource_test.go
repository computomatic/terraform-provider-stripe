package treasury_debit_reversal_test

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

const address = "data.stripe_treasury_debit_reversal.test"

// config reads the object with the given ID through a provider configuration that acts on the
// connected account.
func config(acct, conn acctest.Config, id string) string {
	return acct.ProviderBlock() + conn.ConnectedProviderBlock() + `
data "stripe_treasury_debit_reversal" "test" {
  provider = stripe.connected
  id       = "` + id + `"
}
`
}

// A debit reversal reverses a received debit (suite: "seedTreasury").
func TestAccTreasuryDebitReversal_read(t *testing.T) {
	acct := acctest.NewAccount(t)
	conn := tfacc.IssuingAccount(t, acct)
	fa := tfacc.IssuingFinancialAccount(t, conn)
	tfacc.ReceivedCredit(t, conn, fa, 1000)
	debit, _ := tfacc.ReceivedDebit(t, conn, fa, 100)
	id := tfacc.Create(t, conn, "/v1/treasury/debit_reversals", wire.Object{{Name: "received_debit", Value: wire.String(debit)}})
	resource.ParallelTest(t, resource.TestCase{
		ProtoV6ProviderFactories: tfacc.ProviderFactories,
		Steps: []resource.TestStep{{
			Config: config(acct, conn, id),
			Check: resource.ComposeAggregateTestCheckFunc(
				resource.TestCheckResourceAttr(address, "id", id),
				resource.TestCheckResourceAttr(address, "financial_account", fa),
				resource.TestCheckResourceAttr(address, "received_debit", debit),
				resource.TestCheckResourceAttr(address, "amount", "100"),
				resource.TestCheckResourceAttr(address, "currency", "usd"),
				resource.TestCheckResourceAttrSet(address, "status"),
			),
		}},
	})
}

// An unknown debit reversal is a 404 `resource_missing` (suite: "retrieve unknown").
func TestAccTreasuryDebitReversal_notFound(t *testing.T) {
	acct := acctest.NewAccount(t)
	conn := tfacc.IssuingAccount(t, acct)
	resource.ParallelTest(t, resource.TestCase{
		ProtoV6ProviderFactories: tfacc.ProviderFactories,
		Steps: []resource.TestStep{{
			Config:      config(acct, conn, "debrev_doesnotexist"),
			ExpectError: regexp.MustCompile("no stripe_treasury_debit_reversal found for id=debrev_doesnotexist"),
		}},
	})
}
