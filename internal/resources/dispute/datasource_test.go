package dispute_test

import (
	"os"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"

	"github.com/computomatic/terraform-provider-stripe/internal/acctest"
	"github.com/computomatic/terraform-provider-stripe/internal/acctest/tfacc"
	"github.com/computomatic/terraform-provider-stripe/internal/stripeapi"
	"github.com/computomatic/terraform-provider-stripe/internal/wire"
)

func TestMain(m *testing.M) { os.Exit(acctest.Main(m)) }

// A charge made with the dispute test card is disputed at once (suite: "Disputes").
func TestAccDispute_read(t *testing.T) {
	acct := acctest.NewAccount(t)
	intent, charge := tfacc.SucceededPaymentIntent(t, acct, "pm_card_createDispute")
	list, err := tfacc.Client(acct).Do(t.Context(), stripeapi.Request{Method: "GET", Path: "/v1/disputes", Params: wire.Object{{Name: "payment_intent", Value: wire.String(intent)}}})
	if err != nil {
		t.Fatalf("listing disputes: %v", err)
	}
	data, _ := list.Body.(wire.Object).Get("data")
	first, _ := data.(wire.Array)
	if len(first) == 0 {
		t.Fatalf("no dispute for payment intent %s", intent)
	}
	dispute, _ := first[0].(wire.Object).GetString("id")
	resource.ParallelTest(t, resource.TestCase{
		ProtoV6ProviderFactories: tfacc.ProviderFactories,
		Steps: []resource.TestStep{{
			Config: acct.ProviderBlock() + `
data "stripe_dispute" "test" {
  id = "` + dispute + `"
}
`,
			Check: resource.ComposeAggregateTestCheckFunc(
				resource.TestCheckResourceAttr("data.stripe_dispute.test", "id", dispute),
				resource.TestCheckResourceAttr("data.stripe_dispute.test", "charge", charge),
				resource.TestCheckResourceAttr("data.stripe_dispute.test", "payment_intent", intent),
				resource.TestCheckResourceAttr("data.stripe_dispute.test", "status", "needs_response"),
				resource.TestCheckResourceAttr("data.stripe_dispute.test", "reason", "fraudulent"),
				resource.TestCheckResourceAttr("data.stripe_dispute.test", "amount", "2000"),
			),
		}},
	})
}
