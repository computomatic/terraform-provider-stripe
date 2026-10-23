package balance_test

import (
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"

	"github.com/computomatic/terraform-provider-stripe/internal/acctest"
	"github.com/computomatic/terraform-provider-stripe/internal/acctest/tfacc"
)

// The live-mode guard asks Stripe for the account's mode before a money-moving resource plans
// a change. Planning a top-up with the switch unset succeeds only when the balance reports
// `livemode: false`. The step only plans, so it creates nothing: in the smoke run this proves
// the answer of the real sandbox, which the fake cannot (ADR 2026-10-22-money-moving-resources,
// D9).
func TestAccBalance_liveModeCheck(t *testing.T) {
	acct := acctest.NewAccount(t)
	resource.ParallelTest(t, resource.TestCase{
		ProtoV6ProviderFactories: tfacc.ProviderFactories,
		Steps: []resource.TestStep{{
			Config: acct.ProviderBlock() + `
resource "stripe_topup" "check" {
  amount   = 100
  currency = "usd"
}
`,
			PlanOnly:           true,
			ExpectNonEmptyPlan: true,
		}},
	})
}
