package treasury_credit_reversal_test

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

const address = "data.stripe_treasury_credit_reversal.test"

// config reads the object with the given ID through a provider configuration that acts on the
// connected account.
func config(acct, conn acctest.Config, id string) string {
	return acct.ProviderBlock() + conn.ConnectedProviderBlock() + `
data "stripe_treasury_credit_reversal" "test" {
  provider = stripe.connected
  id       = "` + id + `"
}
`
}

// A credit reversal returns a received credit (suite: "seedTreasury").
func TestAccTreasuryCreditReversal_read(t *testing.T) {
	acct := acctest.NewAccount(t)
	conn := tfacc.IssuingAccount(t, acct)
	fa := tfacc.IssuingFinancialAccount(t, conn)
	credit, _ := tfacc.ReceivedCredit(t, conn, fa, 500)
	id := tfacc.Create(t, conn, "/v1/treasury/credit_reversals", wire.Object{{Name: "received_credit", Value: wire.String(credit)}})
	resource.ParallelTest(t, resource.TestCase{
		ProtoV6ProviderFactories: tfacc.ProviderFactories,
		Steps: []resource.TestStep{{
			Config: config(acct, conn, id),
			Check: resource.ComposeAggregateTestCheckFunc(
				resource.TestCheckResourceAttr(address, "id", id),
				resource.TestCheckResourceAttr(address, "financial_account", fa),
				resource.TestCheckResourceAttr(address, "received_credit", credit),
				resource.TestCheckResourceAttr(address, "amount", "500"),
				resource.TestCheckResourceAttr(address, "currency", "usd"),
				resource.TestCheckResourceAttrSet(address, "status"),
				resource.TestMatchResourceAttr(address, "transaction", regexp.MustCompile(`^trxn_`)),
			),
		}},
	})
}

// An unknown credit reversal is a 404 `resource_missing` (suite: "retrieve unknown").
func TestAccTreasuryCreditReversal_notFound(t *testing.T) {
	acct := acctest.NewAccount(t)
	conn := tfacc.IssuingAccount(t, acct)
	resource.ParallelTest(t, resource.TestCase{
		ProtoV6ProviderFactories: tfacc.ProviderFactories,
		Steps: []resource.TestStep{{
			Config:      config(acct, conn, "credrev_doesnotexist"),
			ExpectError: regexp.MustCompile("no stripe_treasury_credit_reversal found for id=credrev_doesnotexist"),
		}},
	})
}
