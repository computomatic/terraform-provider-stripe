package radar_early_fraud_warning_test

import (
	"os"
	"regexp"
	"testing"
	"time"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"

	"github.com/computomatic/terraform-provider-stripe/internal/acctest"
	"github.com/computomatic/terraform-provider-stripe/internal/acctest/tfacc"
	"github.com/computomatic/terraform-provider-stripe/internal/stripeapi"
	"github.com/computomatic/terraform-provider-stripe/internal/wire"
)

func TestMain(m *testing.M) { os.Exit(acctest.Main(m)) }

// warningFor waits for the early fraud warning of a charge, which Stripe raises shortly after
// the charge (suite: "Early fraud warnings").
func warningFor(t *testing.T, c acctest.Config, charge string) string {
	t.Helper()
	deadline := time.Now().Add(time.Minute)
	for time.Now().Before(deadline) {
		resp, err := tfacc.Client(c).Do(t.Context(), stripeapi.Request{Method: "GET", Path: "/v1/radar/early_fraud_warnings?charge=" + charge})
		if err != nil {
			t.Fatal(err)
		}
		data, _ := resp.Body.(wire.Object).Get("data")
		if items, _ := data.(wire.Array); len(items) > 0 {
			id, _ := items[0].(wire.Object).GetString("id")
			return id
		}
		time.Sleep(time.Second)
	}
	t.Skipf("no early fraud warning appeared for charge %s", charge)
	return ""
}

// The issuer fraud test card gets a warning on its charge.
func TestAccRadarEarlyFraudWarning_read(t *testing.T) {
	acct := acctest.NewAccount(t)
	intent, charge := tfacc.SucceededPaymentIntent(t, acct, "pm_card_createIssuerFraudRecord")
	id := warningFor(t, acct, charge)
	resource.ParallelTest(t, resource.TestCase{
		ProtoV6ProviderFactories: tfacc.ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: acct.ProviderBlock() + `
data "stripe_radar_early_fraud_warning" "test" {
  id = "` + id + `"
}
`,
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("data.stripe_radar_early_fraud_warning.test", "id", id),
					resource.TestCheckResourceAttr("data.stripe_radar_early_fraud_warning.test", "charge", charge),
					resource.TestCheckResourceAttr("data.stripe_radar_early_fraud_warning.test", "payment_intent", intent),
					resource.TestCheckResourceAttrSet("data.stripe_radar_early_fraud_warning.test", "fraud_type"),
					resource.TestCheckResourceAttrSet("data.stripe_radar_early_fraud_warning.test", "actionable"),
				),
			},
			{
				Config: acct.ProviderBlock() + `
data "stripe_radar_early_fraud_warning" "test" {
  id = "issfr_doesnotexist"
}
`,
				ExpectError: regexp.MustCompile("no stripe_radar_early_fraud_warning found for id=issfr_doesnotexist"),
			},
		},
	})
}
