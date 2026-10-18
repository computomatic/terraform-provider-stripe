package refund_test

import (
	"os"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"

	"github.com/computomatic/terraform-provider-stripe/internal/acctest"
	"github.com/computomatic/terraform-provider-stripe/internal/acctest/tfacc"
	"github.com/computomatic/terraform-provider-stripe/internal/wire"
)

func TestMain(m *testing.M) { os.Exit(acctest.Main(m)) }

// A partial refund of a succeeded payment intent (suite: "Refunds", "create partial").
func TestAccRefund_read(t *testing.T) {
	acct := acctest.NewAccount(t)
	intent, charge := tfacc.SucceededPaymentIntent(t, acct, "pm_card_visa")
	refund := tfacc.Create(t, acct, "/v1/refunds", wire.Object{
		{Name: "payment_intent", Value: wire.String(intent)},
		{Name: "amount", Value: wire.Int(500)},
		{Name: "reason", Value: wire.String("requested_by_customer")},
	})
	resource.ParallelTest(t, resource.TestCase{
		ProtoV6ProviderFactories: tfacc.ProviderFactories,
		Steps: []resource.TestStep{{
			Config: acct.ProviderBlock() + `
data "stripe_refund" "test" {
  id = "` + refund + `"
}
`,
			Check: resource.ComposeAggregateTestCheckFunc(
				resource.TestCheckResourceAttr("data.stripe_refund.test", "id", refund),
				resource.TestCheckResourceAttr("data.stripe_refund.test", "amount", "500"),
				resource.TestCheckResourceAttr("data.stripe_refund.test", "currency", "usd"),
				resource.TestCheckResourceAttr("data.stripe_refund.test", "status", "succeeded"),
				resource.TestCheckResourceAttr("data.stripe_refund.test", "reason", "requested_by_customer"),
				resource.TestCheckResourceAttr("data.stripe_refund.test", "payment_intent", intent),
				resource.TestCheckResourceAttr("data.stripe_refund.test", "charge", charge),
			),
		}},
	})
}
