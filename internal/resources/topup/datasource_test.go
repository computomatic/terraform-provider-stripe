package topup_test

import (
	"os"
	"regexp"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"

	"github.com/computomatic/terraform-provider-stripe/internal/acctest"
	"github.com/computomatic/terraform-provider-stripe/internal/acctest/tfacc"
	"github.com/computomatic/terraform-provider-stripe/internal/stripeapi"
	"github.com/computomatic/terraform-provider-stripe/internal/wire"
)

func TestMain(m *testing.M) { os.Exit(acctest.Main(m)) }

// Stripe refuses a top-up for a Canadian account (suite: "Top-ups are refused for CA
// accounts"), and the fake follows it, so the read runs only where a top-up can be made.
func TestAccTopup_read(t *testing.T) {
	acct := acctest.NewAccount(t)
	resp, err := tfacc.Client(acct).Do(t.Context(), stripeapi.Request{Method: "POST", Path: "/v1/topups", Params: wire.Object{
		{Name: "amount", Value: wire.Int(1000)},
		{Name: "currency", Value: wire.String(tfacc.AccountCurrency(t, acct))},
	}})
	if err != nil {
		t.Skipf("top-ups are refused on this account: %v", err)
	}
	id, _ := resp.Body.(wire.Object).GetString("id")
	resource.ParallelTest(t, resource.TestCase{
		ProtoV6ProviderFactories: tfacc.ProviderFactories,
		Steps: []resource.TestStep{{
			Config: acct.ProviderBlock() + `
data "stripe_topup" "test" {
  id = "` + id + `"
}
`,
			Check: resource.ComposeAggregateTestCheckFunc(
				resource.TestCheckResourceAttr("data.stripe_topup.test", "id", id),
				resource.TestCheckResourceAttr("data.stripe_topup.test", "amount", "1000"),
			),
		}},
	})
}

// An unknown top-up is a 404 `resource_missing` (suite: "retrieve unknown").
func TestAccTopup_notFound(t *testing.T) {
	acct := acctest.NewAccount(t)
	tfacc.SkipUnlessFakeAnswers(t, acct, "/v1/topups/tu_doesnotexist")
	resource.ParallelTest(t, resource.TestCase{
		ProtoV6ProviderFactories: tfacc.ProviderFactories,
		Steps: []resource.TestStep{{
			Config: acct.ProviderBlock() + `
data "stripe_topup" "test" {
  id = "tu_doesnotexist"
}
`,
			ExpectError: regexp.MustCompile("no stripe_topup found for id=tu_doesnotexist"),
		}},
	})
}
