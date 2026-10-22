package account_capability_test

import (
	"fmt"
	"os"
	"regexp"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"

	"github.com/computomatic/terraform-provider-stripe/internal/acctest"
	"github.com/computomatic/terraform-provider-stripe/internal/acctest/tfacc"
)

func TestMain(m *testing.M) { os.Exit(acctest.Main(m)) }

func config(c acctest.Config, extra string) string {
	return c.ProviderBlock() + fmt.Sprintf(`
resource "stripe_account" "test" {
  type    = "custom"
  country = "US"
  capabilities = {
    card_payments = { requested = true }
    transfers     = { requested = true }
  }
}

%s`, extra)
}

// Stripe answers for every capability the account could request, requested or not (suite:
// "TestAccountCapability", "list" and "retrieve"). A requested one is `inactive` until the
// account is onboarded; the others are `unrequested`.
func TestAccAccountCapability_read(t *testing.T) {
	acct := acctest.NewAccount(t)
	resource.ParallelTest(t, resource.TestCase{
		ProtoV6ProviderFactories: tfacc.ProviderFactories,
		Steps: []resource.TestStep{{
			Config: config(acct, `
data "stripe_account_capability" "requested" {
  account = stripe_account.test.id
  id      = "card_payments"
}

data "stripe_account_capability" "not_requested" {
  account = stripe_account.test.id
  id      = "us_bank_account_ach_payments"
}
`),
			Check: resource.ComposeAggregateTestCheckFunc(
				resource.TestCheckResourceAttrPair("data.stripe_account_capability.requested", "account", "stripe_account.test", "id"),
				resource.TestCheckResourceAttr("data.stripe_account_capability.requested", "id", "card_payments"),
				resource.TestCheckResourceAttr("data.stripe_account_capability.requested", "requested", "true"),
				resource.TestCheckResourceAttr("data.stripe_account_capability.requested", "status", "inactive"),
				resource.TestCheckResourceAttrSet("data.stripe_account_capability.requested", "requested_at"),
				resource.TestCheckResourceAttr("data.stripe_account_capability.not_requested", "requested", "false"),
				resource.TestCheckResourceAttr("data.stripe_account_capability.not_requested", "status", "unrequested"),
			),
		}},
	})
}

// A capability Stripe does not know is a 404 (suite: "retrieve unrequested capability"), and
// an account that does not exist a 403 `account_invalid`; both are not found.
func TestAccAccountCapability_readMissing(t *testing.T) {
	acct := acctest.NewAccount(t)
	resource.ParallelTest(t, resource.TestCase{
		ProtoV6ProviderFactories: tfacc.ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: config(acct, `
data "stripe_account_capability" "bogus" {
  account = stripe_account.test.id
  id      = "bogus_payments"
}
`),
				ExpectError: regexp.MustCompile(`no stripe_account_capability found for id=bogus_payments`),
			},
			{
				Config: acct.ProviderBlock() + `
data "stripe_account_capability" "missing" {
  account = "acct_1DoesNotExist0000"
  id      = "card_payments"
}
`,
				ExpectError: regexp.MustCompile(`no stripe_account_capability found for id=card_payments`),
			},
		},
	})
}
