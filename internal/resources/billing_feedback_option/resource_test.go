package billing_feedback_option_test

import (
	"fmt"
	"os"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/plancheck"

	"github.com/computomatic/terraform-provider-stripe/internal/acctest"
	"github.com/computomatic/terraform-provider-stripe/internal/acctest/tfacc"
	"github.com/computomatic/terraform-provider-stripe/internal/wire"
)

func TestMain(m *testing.M) { os.Exit(acctest.Main(m)) }

const address = "stripe_billing_feedback_option.test"

// optionConfig is a feedback option. extra lands inside the resource block.
func optionConfig(c acctest.Config, description string, extra string) string {
	return c.ProviderBlock() + fmt.Sprintf(`
resource "stripe_billing_feedback_option" "test" {
  description = %q
%s}
`, description, extra)
}

func idOf(dst *string) resource.TestCheckFunc {
	return resource.TestCheckResourceAttrWith(address, "id", func(v string) error {
		*dst = v
		return nil
	})
}

// pathOf records the option's API path.
func pathOf(dst *string) resource.TestCheckFunc {
	return resource.TestCheckResourceAttrWith(address, "id", func(v string) error {
		*dst = "/v1/billing/feedback_options/" + v
		return nil
	})
}

func TestAccBillingFeedbackOption_create(t *testing.T) {
	acct := acctest.NewAccount(t)
	resource.ParallelTest(t, resource.TestCase{
		ProtoV6ProviderFactories: tfacc.ProviderFactories,
		Steps: acctest.Forget(t, []resource.TestStep{{
			Config: optionConfig(acct, "Too expensive", "") + `
data "stripe_billing_feedback_option" "test" {
  id = stripe_billing_feedback_option.test.id
}
`,
			Check: resource.ComposeAggregateTestCheckFunc(
				resource.TestCheckResourceAttrSet(address, "id"),
				resource.TestCheckResourceAttr(address, "description", "Too expensive"),
				resource.TestCheckResourceAttr(address, "status", "active"),
				resource.TestCheckNoResourceAttr(address, "status_transitions.deactivated_at"),
				resource.TestCheckResourceAttrPair("data.stripe_billing_feedback_option.test", "id", address, "id"),
				resource.TestCheckResourceAttr("data.stripe_billing_feedback_option.test", "description", "Too expensive"),
			),
		}}, nil, "stripe_billing_feedback_option"),
	})
}

func TestAccBillingFeedbackOption_update(t *testing.T) {
	acct := acctest.NewAccount(t)
	var first string
	resource.ParallelTest(t, resource.TestCase{
		ProtoV6ProviderFactories: tfacc.ProviderFactories,
		Steps: acctest.Forget(t, []resource.TestStep{
			{Config: optionConfig(acct, "Too expensive", ""), Check: idOf(&first)},
			{
				Config: optionConfig(acct, "Missing features", ""),
				ConfigPlanChecks: resource.ConfigPlanChecks{PreApply: []plancheck.PlanCheck{
					plancheck.ExpectResourceAction(address, plancheck.ResourceActionUpdate),
				}},
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr(address, "description", "Missing features"),
					resource.TestCheckResourceAttrWith(address, "id", func(v string) error {
						if v != first {
							return fmt.Errorf("the in-place update changed the ID from %s to %s", first, v)
						}
						return nil
					}),
				),
			},
		}, nil, "stripe_billing_feedback_option"),
	})
}

// An option has no field that forces replacement, but `replace_triggered_by` and a taint plan
// one, which the provider cannot see at plan time; Stripe cannot delete an option, so the apply
// fails before anything is created.
func TestAccBillingFeedbackOption_replace(t *testing.T) {
	acct := acctest.NewAccount(t)
	config := func(trigger string) string {
		return optionConfig(acct, "Too expensive", `
  lifecycle {
    replace_triggered_by = [terraform_data.trigger]
  }
`) + fmt.Sprintf(`
resource "terraform_data" "trigger" {
  input = %q
}
`, trigger)
	}
	resource.ParallelTest(t, resource.TestCase{
		ProtoV6ProviderFactories: tfacc.ProviderFactories,
		Steps: acctest.Forget(t, []resource.TestStep{
			{Config: config("one")},
			{Config: config("two"), ExpectError: tfacc.RefusedDestroy},
			{Config: config("one"), Taint: []string{address}, ExpectError: tfacc.RefusedDestroy},
		}, nil, "stripe_billing_feedback_option"),
	})
}

func TestAccBillingFeedbackOption_import(t *testing.T) {
	acct := acctest.NewAccount(t)
	cfg := optionConfig(acct, "Too expensive", "")
	resource.ParallelTest(t, resource.TestCase{
		ProtoV6ProviderFactories: tfacc.ProviderFactories,
		Steps: acctest.Forget(t, []resource.TestStep{
			{Config: cfg},
			{
				ResourceName:      address,
				ImportState:       true,
				ImportStateVerify: true,
			},
			{
				ResourceName:    address,
				ImportState:     true,
				ImportStateKind: resource.ImportBlockWithID,
				Config:          cfg,
			},
		}, nil, "stripe_billing_feedback_option"),
	})
}

// Stripe cannot delete a feedback option, and deactivating one is final, so dropping it from
// the configuration, and `terraform destroy`, fail at plan time, and the option stays active.
func TestAccBillingFeedbackOption_delete(t *testing.T) {
	acct := acctest.NewAccount(t)
	var path string
	var mark int
	cfg := optionConfig(acct, "Too expensive", "")
	resource.ParallelTest(t, resource.TestCase{
		ProtoV6ProviderFactories: tfacc.ProviderFactories,
		Steps: acctest.Forget(t, []resource.TestStep{
			{Config: cfg, Check: resource.ComposeTestCheckFunc(pathOf(&path), tfacc.LogMark(t, acct, &mark))},
			{Config: acctest.Without(t, cfg, "stripe_billing_feedback_option"), PlanOnly: true, ExpectError: tfacc.RefusedDestroy},
			{Config: cfg, Destroy: true, ExpectError: tfacc.RefusedDestroy},
		}, tfacc.Untouched(t, acct, &path, &mark, "status", wire.String("active")), "stripe_billing_feedback_option"),
	})
}

// Setting `status` to `inactive` deactivates the option through its action endpoint (suite:
// "deactivate"), after which it reads back inactive.
func TestAccBillingFeedbackOption_deactivate(t *testing.T) {
	acct := acctest.NewAccount(t)
	var path string
	resource.ParallelTest(t, resource.TestCase{
		ProtoV6ProviderFactories: tfacc.ProviderFactories,
		Steps: acctest.Forget(t, []resource.TestStep{
			{Config: optionConfig(acct, "Too expensive", ""), Check: pathOf(&path)},
			{
				Config: optionConfig(acct, "Too expensive", "  status = \"inactive\"\n"),
				Check: resource.ComposeTestCheckFunc(
					resource.TestCheckResourceAttr(address, "status", "inactive"),
					tfacc.Holds(t, acct, &path, "status", wire.String("inactive")),
				),
			},
		}, nil, "stripe_billing_feedback_option"),
	})
}

func TestAccBillingFeedbackOption_drift(t *testing.T) {
	acct := acctest.NewAccount(t)
	var id string
	cfg := optionConfig(acct, "Too expensive", "")
	resource.ParallelTest(t, resource.TestCase{
		ProtoV6ProviderFactories: tfacc.ProviderFactories,
		Steps: acctest.Forget(t, []resource.TestStep{
			{Config: cfg, Check: idOf(&id)},
			{
				PreConfig: func() {
					tfacc.Post(t, acct, "/v1/billing/feedback_options/"+id, wire.Object{
						{Name: "description", Value: wire.String("Renamed")},
					})
				},
				Config: cfg,
				ConfigPlanChecks: resource.ConfigPlanChecks{PreApply: []plancheck.PlanCheck{
					plancheck.ExpectNonEmptyPlan(),
					plancheck.ExpectResourceAction(address, plancheck.ResourceActionUpdate),
				}},
				Check: resource.TestCheckResourceAttr(address, "description", "Too expensive"),
			},
		}, nil, "stripe_billing_feedback_option"),
	})
}
