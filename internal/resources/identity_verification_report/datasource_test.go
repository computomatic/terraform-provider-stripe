package identity_verification_report_test

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

const address = "data.stripe_identity_verification_report.test"

func config(c acctest.Config, id string) string {
	return c.ProviderBlock() + `
data "stripe_identity_verification_report" "test" {
  id = "` + id + `"
}
`
}

// A report needs a person to complete a session's hosted flow, so nothing in the API makes one (suite:
// "Invoice rendering templates and verification reports are empty on the dev sandbox"). The test
// reads the first report the account has and skips when there is none, as on a new account.
func TestAccIdentityVerificationReport_read(t *testing.T) {
	acct := acctest.NewAccount(t)
	resp, err := tfacc.Client(acct).Do(t.Context(), stripeapi.Request{Method: "GET", Path: "/v1/identity/verification_reports"})
	if err != nil {
		t.Fatalf("listing: %v", err)
	}
	data, _ := resp.Body.(wire.Object).Get("data")
	items, _ := data.(wire.Array)
	if len(items) == 0 {
		t.Skip("the account has no verification reports, and one needs a person to complete the hosted flow")
	}
	id, _ := items[0].(wire.Object).GetString("id")
	resource.ParallelTest(t, resource.TestCase{
		ProtoV6ProviderFactories: tfacc.ProviderFactories,
		Steps: []resource.TestStep{{
			Config: config(acct, id),
			Check: resource.ComposeAggregateTestCheckFunc(
				resource.TestCheckResourceAttr(address, "id", id),
				resource.TestCheckResourceAttrSet(address, "type"),
				resource.TestCheckResourceAttrSet(address, "verification_session"),
			),
		}},
	})
}

// An unknown report is a 404 `resource_missing` (suite: "retrieve unknown").
func TestAccIdentityVerificationReport_notFound(t *testing.T) {
	acct := acctest.NewAccount(t)
	resource.ParallelTest(t, resource.TestCase{
		ProtoV6ProviderFactories: tfacc.ProviderFactories,
		Steps: []resource.TestStep{{
			Config:      config(acct, "vr_doesnotexist"),
			ExpectError: regexp.MustCompile(`no stripe_identity_verification_report found for\s+id=vr_doesnotexist`),
		}},
	})
}
