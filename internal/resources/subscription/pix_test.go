package subscription_test

import (
	"fmt"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/plancheck"

	"github.com/computomatic/terraform-provider-stripe/internal/acctest"
	"github.com/computomatic/terraform-provider-stripe/internal/acctest/tfacc"
)

// pixConfig is the suite's probe `update/35` (TestSubscriptionAttributes): a subscription invoiced
// to a customer of its own, on a monthly price in Brazilian real, with Pix's options, which holds
// options when not nil.
func pixConfig(c acctest.Config, options string) string {
	return c.ProviderBlock() + fmt.Sprintf(`
resource "stripe_customer" "test" {
  email = "subscription@example.com"
}

resource "stripe_product" "test" {
  name = %q
}

resource "stripe_price" "brl" {
  product     = stripe_product.test.id
  currency    = "brl"
  unit_amount = 1000
  recurring   = { interval = "month" }
}

resource "stripe_subscription" "test" {
  customer          = stripe_customer.test.id
  collection_method = "send_invoice"
  days_until_due    = 30
  items             = [{ price = stripe_price.brl.id }]
  payment_settings  = { payment_method_options = { pix = %s } }
}
`, c.Name("Pix plan"), options)
}

// Pix's options change in place, sent whole as the suite's probe `update/35` sends them, and
// Stripe keeps a member the configuration removes (probe `siblings/35`), which the plan keeps too.
func TestAccSubscription_pixOptions(t *testing.T) {
	acct := acctest.NewAccount(t)
	const pix = "payment_settings.payment_method_options.pix."
	created := `{
    expires_after_seconds = 3600
    mandate_options       = { amount = 1000, amount_includes_iof = "always", end_date = "2035-02-15", payment_schedule = "halfyearly" }
  }`
	updated := `{
    expires_after_seconds = 7200
    mandate_options       = { amount = 7, amount_includes_iof = "never", end_date = "2035-03-15", payment_schedule = "monthly" }
  }`
	removed := `{
    expires_after_seconds = 7200
    mandate_options       = { amount = 7, amount_includes_iof = "never", payment_schedule = "monthly" }
  }`
	resource.ParallelTest(t, resource.TestCase{
		ProtoV6ProviderFactories: tfacc.ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config:           pixConfig(acct, created),
				Check:            resource.TestCheckResourceAttr(address, pix+"mandate_options.payment_schedule", "halfyearly"),
				ConfigPlanChecks: resource.ConfigPlanChecks{PostApplyPostRefresh: []plancheck.PlanCheck{plancheck.ExpectEmptyPlan()}},
			},
			{
				Config: pixConfig(acct, updated),
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply:             []plancheck.PlanCheck{plancheck.ExpectResourceAction(address, plancheck.ResourceActionUpdate)},
					PostApplyPostRefresh: []plancheck.PlanCheck{plancheck.ExpectEmptyPlan()},
				},
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr(address, pix+"expires_after_seconds", "7200"),
					resource.TestCheckResourceAttr(address, pix+"mandate_options.end_date", "2035-03-15"),
					resource.TestCheckResourceAttr(address, pix+"mandate_options.amount_includes_iof", "never"),
				),
			},
			{
				Config:           pixConfig(acct, removed),
				ConfigPlanChecks: resource.ConfigPlanChecks{PreApply: []plancheck.PlanCheck{plancheck.ExpectEmptyPlan()}},
				Check:            resource.TestCheckResourceAttr(address, pix+"mandate_options.end_date", "2035-03-15"),
			},
		},
	})
}
