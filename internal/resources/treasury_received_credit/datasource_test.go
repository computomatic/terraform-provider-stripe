package treasury_received_credit_test

import (
	"os"
	"regexp"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"

	"github.com/computomatic/terraform-provider-stripe/internal/acctest"
	"github.com/computomatic/terraform-provider-stripe/internal/acctest/tfacc"
)

func TestMain(m *testing.M) { os.Exit(acctest.Main(m)) }

const address = "data.stripe_treasury_received_credit.test"

// config reads the object with the given ID through a provider configuration that acts on the
// connected account.
func config(acct, conn acctest.Config, id string) string {
	return acct.ProviderBlock() + conn.ConnectedProviderBlock() + `
data "stripe_treasury_received_credit" "test" {
  provider = stripe.connected
  id       = "` + id + `"
}
`
}

// Stripe's test helper makes a received credit (suite: "seeded received credit").
func TestAccTreasuryReceivedCredit_read(t *testing.T) {
	acct := acctest.NewAccount(t)
	conn := tfacc.IssuingAccount(t, acct)
	fa := tfacc.IssuingFinancialAccount(t, conn)
	id, transaction := tfacc.ReceivedCredit(t, conn, fa, 1000)
	resource.ParallelTest(t, resource.TestCase{
		ProtoV6ProviderFactories: tfacc.ProviderFactories,
		Steps: []resource.TestStep{{
			Config: config(acct, conn, id),
			Check: resource.ComposeAggregateTestCheckFunc(
				resource.TestCheckResourceAttr(address, "id", id),
				resource.TestCheckResourceAttr(address, "financial_account", fa),
				resource.TestCheckResourceAttr(address, "amount", "1000"),
				resource.TestCheckResourceAttr(address, "currency", "usd"),
				resource.TestCheckResourceAttr(address, "network", "ach"),
				resource.TestCheckResourceAttr(address, "status", "succeeded"),
				resource.TestCheckResourceAttr(address, "transaction", transaction),
				resource.TestCheckResourceAttr(address, "initiating_payment_method_details.type", "us_bank_account"),
			),
		}},
	})
}

// An unknown received credit is a 404 `resource_missing` (suite: "retrieve unknown").
func TestAccTreasuryReceivedCredit_notFound(t *testing.T) {
	acct := acctest.NewAccount(t)
	conn := tfacc.IssuingAccount(t, acct)
	resource.ParallelTest(t, resource.TestCase{
		ProtoV6ProviderFactories: tfacc.ProviderFactories,
		Steps: []resource.TestStep{{
			Config:      config(acct, conn, "rc_doesnotexist"),
			ExpectError: regexp.MustCompile("no stripe_treasury_received_credit found for id=rc_doesnotexist"),
		}},
	})
}
