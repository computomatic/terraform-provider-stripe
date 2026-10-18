package billing_feedback_option_test

import (
	"fmt"
	"os"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/plancheck"
	"github.com/hashicorp/terraform-plugin-testing/terraform"

	"github.com/computomatic/terraform-provider-stripe/internal/acctest"
	"github.com/computomatic/terraform-provider-stripe/internal/acctest/tfacc"
	"github.com/computomatic/terraform-provider-stripe/internal/stripeapi"
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

func TestAccBillingFeedbackOption_create(t *testing.T) {
	acct := acctest.NewAccount(t)
	resource.ParallelTest(t, resource.TestCase{
		ProtoV6ProviderFactories: tfacc.ProviderFactories,
		Steps: []resource.TestStep{{
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
		}},
	})
}

func TestAccBillingFeedbackOption_update(t *testing.T) {
	acct := acctest.NewAccount(t)
	var first string
	resource.ParallelTest(t, resource.TestCase{
		ProtoV6ProviderFactories: tfacc.ProviderFactories,
		Steps: []resource.TestStep{
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
		},
	})
}

// Stripe accepts every create field on update, so no argument forces a replacement. A
// replacement forced from outside (a changed trigger) deactivates the option and creates
// another.
func TestAccBillingFeedbackOption_replace(t *testing.T) {
	acct := acctest.NewAccount(t)
	var first string
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
		Steps: []resource.TestStep{
			{Config: config("one"), Check: idOf(&first)},
			{
				Config: config("two"),
				ConfigPlanChecks: resource.ConfigPlanChecks{PreApply: []plancheck.PlanCheck{
					plancheck.ExpectResourceAction(address, plancheck.ResourceActionDestroyBeforeCreate),
				}},
				Check: resource.TestCheckResourceAttrWith(address, "id", func(v string) error {
					if v == first {
						return fmt.Errorf("the replacement kept the option ID %s", v)
					}
					return nil
				}),
			},
		},
	})
}

func TestAccBillingFeedbackOption_import(t *testing.T) {
	acct := acctest.NewAccount(t)
	cfg := optionConfig(acct, "Too expensive", "")
	resource.ParallelTest(t, resource.TestCase{
		ProtoV6ProviderFactories: tfacc.ProviderFactories,
		Steps: []resource.TestStep{
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
		},
	})
}

// Stripe cannot delete a feedback option, so destroy deactivates it through the deactivate
// endpoint (suite: billing_feedback_option_test.go, "deactivate"). A second deactivate is
// an error ("deactivate twice"), so an option deactivated out of band is left as it is.
func TestAccBillingFeedbackOption_delete(t *testing.T) {
	acct := acctest.NewAccount(t)
	var id string
	resource.ParallelTest(t, resource.TestCase{
		ProtoV6ProviderFactories: tfacc.ProviderFactories,
		CheckDestroy: func(*terraform.State) error {
			resp, err := tfacc.Client(acct).Do(t.Context(), stripeapi.Request{Method: "GET", Path: "/v1/billing/feedback_options/" + id})
			if err != nil {
				return fmt.Errorf("a deactivated feedback option still retrieves: %w", err)
			}
			if status, _ := resp.Body.(wire.Object).GetString("status"); status != "inactive" {
				return fmt.Errorf("feedback option %s should be inactive after destroy, got %q", id, status)
			}
			return nil
		},
		Steps: []resource.TestStep{{Config: optionConfig(acct, "Too expensive", ""), Check: idOf(&id)}},
	})
}

// An option deactivated out of band shows as drift only in the computed status, which
// carries no configuration, and destroy must not deactivate it a second time.
func TestAccBillingFeedbackOption_deleteDeactivated(t *testing.T) {
	acct := acctest.NewAccount(t)
	var id string
	resource.ParallelTest(t, resource.TestCase{
		ProtoV6ProviderFactories: tfacc.ProviderFactories,
		Steps: []resource.TestStep{
			{Config: optionConfig(acct, "Too expensive", ""), Check: idOf(&id)},
			{
				PreConfig: func() {
					tfacc.Post(t, acct, "/v1/billing/feedback_options/"+id+"/deactivate", nil)
				},
				Config: optionConfig(acct, "Too expensive", ""),
				Check:  resource.TestCheckResourceAttr(address, "status", "inactive"),
			},
		},
	})
}

func TestAccBillingFeedbackOption_drift(t *testing.T) {
	acct := acctest.NewAccount(t)
	var id string
	cfg := optionConfig(acct, "Too expensive", "")
	resource.ParallelTest(t, resource.TestCase{
		ProtoV6ProviderFactories: tfacc.ProviderFactories,
		Steps: []resource.TestStep{
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
		},
	})
}
