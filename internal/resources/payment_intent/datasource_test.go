package payment_intent_test

import (
	"os"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"

	"github.com/computomatic/terraform-provider-stripe/internal/acctest"
	"github.com/computomatic/terraform-provider-stripe/internal/acctest/tfacc"
)

func TestMain(m *testing.M) { os.Exit(acctest.Main(m)) }

// A payment intent confirmed with a test card succeeds and has a charge (suite: "Payment
// intents"). Intents are made by the code that takes the payment, so the test makes one
// behind the provider's back.
func TestAccPaymentIntent_read(t *testing.T) {
	acct := acctest.NewAccount(t)
	intent, charge := tfacc.SucceededPaymentIntent(t, acct, "pm_card_visa")
	resource.ParallelTest(t, resource.TestCase{
		ProtoV6ProviderFactories: tfacc.ProviderFactories,
		Steps: []resource.TestStep{{
			Config: acct.ProviderBlock() + `
data "stripe_payment_intent" "test" {
  id = "` + intent + `"
}
`,
			Check: resource.ComposeAggregateTestCheckFunc(
				resource.TestCheckResourceAttr("data.stripe_payment_intent.test", "id", intent),
				resource.TestCheckResourceAttr("data.stripe_payment_intent.test", "status", "succeeded"),
				resource.TestCheckResourceAttr("data.stripe_payment_intent.test", "amount", "2000"),
				resource.TestCheckResourceAttr("data.stripe_payment_intent.test", "currency", "usd"),
				resource.TestCheckResourceAttr("data.stripe_payment_intent.test", "latest_charge", charge),
			),
		}},
	})
}
