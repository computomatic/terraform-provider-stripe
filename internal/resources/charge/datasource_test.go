package charge_test

import (
	"os"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"

	"github.com/computomatic/terraform-provider-stripe/internal/acctest"
	"github.com/computomatic/terraform-provider-stripe/internal/acctest/tfacc"
)

func TestMain(m *testing.M) { os.Exit(acctest.Main(m)) }

// A succeeded payment intent has a paid charge (suite: "Charges").
func TestAccCharge_read(t *testing.T) {
	acct := acctest.NewAccount(t)
	intent, charge := tfacc.SucceededPaymentIntent(t, acct, "pm_card_visa")
	resource.ParallelTest(t, resource.TestCase{
		ProtoV6ProviderFactories: tfacc.ProviderFactories,
		Steps: []resource.TestStep{{
			Config: acct.ProviderBlock() + `
data "stripe_charge" "test" {
  id = "` + charge + `"
}
`,
			Check: resource.ComposeAggregateTestCheckFunc(
				resource.TestCheckResourceAttr("data.stripe_charge.test", "id", charge),
				resource.TestCheckResourceAttr("data.stripe_charge.test", "payment_intent", intent),
				resource.TestCheckResourceAttr("data.stripe_charge.test", "status", "succeeded"),
				resource.TestCheckResourceAttr("data.stripe_charge.test", "paid", "true"),
				resource.TestCheckResourceAttr("data.stripe_charge.test", "amount", "2000"),
				resource.TestCheckResourceAttr("data.stripe_charge.test", "currency", "usd"),
			),
		}},
	})
}
