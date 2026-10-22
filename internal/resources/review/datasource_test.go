package review_test

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

// A review opens only when a Radar rule flags a payment, and the suite's account has no such
// rule (suite: "An elevated risk charge does not open a review"), so the test seeds one and
// runs on the fake only.
func TestAccReview_read(t *testing.T) {
	acctest.RequireFake(t)
	acct := acctest.NewAccount(t)
	id := acctest.Seed(t, acct, wire.Object{
		{Name: "object", Value: wire.String("review")},
		{Name: "livemode", Value: wire.Bool(false)},
		{Name: "open", Value: wire.Bool(true)},
		{Name: "opened_reason", Value: wire.String("rule")},
		{Name: "reason", Value: wire.String("rule")},
	})[0]
	resource.ParallelTest(t, resource.TestCase{
		ProtoV6ProviderFactories: tfacc.ProviderFactories,
		Steps: []resource.TestStep{{
			Config: acct.ProviderBlock() + `
data "stripe_review" "test" {
  id = "` + id + `"
}
`,
			Check: resource.ComposeAggregateTestCheckFunc(
				resource.TestCheckResourceAttr("data.stripe_review.test", "id", id),
				resource.TestCheckResourceAttr("data.stripe_review.test", "open", "true"),
				resource.TestCheckResourceAttr("data.stripe_review.test", "opened_reason", "rule"),
				resource.TestCheckResourceAttr("data.stripe_review.test", "reason", "rule"),
			),
		}},
	})
}

// An unknown review is a 404 `resource_missing` (suite: "retrieve unknown").
func TestAccReview_notFound(t *testing.T) {
	acct := acctest.NewAccount(t)
	tfacc.SkipUnlessFakeAnswers(t, acct, "/v1/reviews/prv_doesnotexist")
	resource.ParallelTest(t, resource.TestCase{
		ProtoV6ProviderFactories: tfacc.ProviderFactories,
		Steps: []resource.TestStep{{
			Config: acct.ProviderBlock() + `
data "stripe_review" "test" {
  id = "prv_doesnotexist"
}
`,
			ExpectError: regexp.MustCompile("no stripe_review found for id=prv_doesnotexist"),
		}},
	})
}
