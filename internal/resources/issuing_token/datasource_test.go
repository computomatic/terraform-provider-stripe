package issuing_token_test

import (
	"os"
	"regexp"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"

	"github.com/computomatic/terraform-provider-stripe/internal/acctest"
	"github.com/computomatic/terraform-provider-stripe/internal/acctest/tfacc"
)

func TestMain(m *testing.M) { os.Exit(acctest.Main(m)) }

// A digital wallet makes a token when it provisions a card, and no test helper does, so a test
// can read no token: the read answers that the token is not found, a 404 `resource_missing` on
// `token` (suite: "Issuing tokens on a new test-mode card (first run)", "retrieve unknown").
func TestAccIssuingToken_read(t *testing.T) {
	acct := acctest.NewAccount(t)
	conn := tfacc.IssuingAccount(t, acct)
	resource.ParallelTest(t, resource.TestCase{
		ProtoV6ProviderFactories: tfacc.ProviderFactories,
		Steps: []resource.TestStep{{
			Config: acct.ProviderBlock() + conn.ConnectedProviderBlock() + `
data "stripe_issuing_token" "test" {
  provider = stripe.connected
  id       = "intok_doesnotexist"
}
`,
			ExpectError: regexp.MustCompile("no stripe_issuing_token found for id=intok_doesnotexist"),
		}},
	})
}
