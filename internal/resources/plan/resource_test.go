package plan_test

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

const address = "stripe_plan.test"

// planConfig is a product and a monthly plan on it. extra lands inside the plan block.
func planConfig(c acctest.Config, amount int, extra string) string {
	return c.ProviderBlock() + fmt.Sprintf(`
resource "stripe_product" "test" {
  name = "Gold plan"
}

resource "stripe_plan" "test" {
  product  = stripe_product.test.id
  currency = "usd"
  interval = "month"
  amount   = %d
%s}
`, amount, extra)
}

func idOf(dst *string) resource.TestCheckFunc {
	return resource.TestCheckResourceAttrWith(address, "id", func(v string) error {
		*dst = v
		return nil
	})
}

func TestAccPlan_create(t *testing.T) {
	acct := acctest.NewAccount(t)
	resource.ParallelTest(t, resource.TestCase{
		ProtoV6ProviderFactories: tfacc.ProviderFactories,
		ErrorCheck:               tfacc.SkipWhenFakeLacks(t, "expand["),
		Steps: []resource.TestStep{{
			Config: planConfig(acct, 2000, "  nickname = \"Monthly\"\n  trial_period_days = 7\n  metadata = { team = \"a\" }\n") + `
data "stripe_plan" "test" {
  id = stripe_plan.test.id
}
`,
			Check: resource.ComposeAggregateTestCheckFunc(
				resource.TestCheckResourceAttrSet(address, "id"),
				resource.TestCheckResourceAttrPair(address, "product", "stripe_product.test", "id"),
				resource.TestCheckResourceAttr(address, "amount", "2000"),
				resource.TestCheckResourceAttr(address, "amount_decimal", "2000"),
				resource.TestCheckResourceAttr(address, "currency", "usd"),
				resource.TestCheckResourceAttr(address, "interval", "month"),
				resource.TestCheckResourceAttr(address, "interval_count", "1"),
				resource.TestCheckResourceAttr(address, "active", "true"),
				resource.TestCheckResourceAttr(address, "billing_scheme", "per_unit"),
				resource.TestCheckResourceAttr(address, "usage_type", "licensed"),
				resource.TestCheckResourceAttr(address, "trial_period_days", "7"),
				resource.TestCheckResourceAttr(address, "metadata.team", "a"),
				resource.TestCheckResourceAttrPair("data.stripe_plan.test", "id", address, "id"),
				resource.TestCheckResourceAttr("data.stripe_plan.test", "nickname", "Monthly"),
				resource.TestCheckResourceAttr("data.stripe_plan.test", "amount", "2000"),
			),
		}},
	})
}

// A plan may be created with an ID of the caller's choosing.
func TestAccPlan_chosenID(t *testing.T) {
	acct := acctest.NewAccount(t)
	resource.ParallelTest(t, resource.TestCase{
		ProtoV6ProviderFactories: tfacc.ProviderFactories,
		ErrorCheck:               tfacc.SkipWhenFakeLacks(t, "expand["),
		Steps: []resource.TestStep{{
			Config: planConfig(acct, 2000, "  id = \"gold_monthly\"\n"),
			Check:  resource.TestCheckResourceAttr(address, "id", "gold_monthly"),
		}},
	})
}

func TestAccPlan_update(t *testing.T) {
	acct := acctest.NewAccount(t)
	resource.ParallelTest(t, resource.TestCase{
		ProtoV6ProviderFactories: tfacc.ProviderFactories,
		ErrorCheck:               tfacc.SkipWhenFakeLacks(t, "expand["),
		Steps: []resource.TestStep{
			{
				Config: planConfig(acct, 2000, "  nickname = \"Monthly\"\n  metadata = { team = \"a\", tier = \"1\" }\n"),
				Check:  resource.TestCheckResourceAttr(address, "nickname", "Monthly"),
			},
			{
				Config: planConfig(acct, 2000, "  nickname = \"Monthly v2\"\n  metadata = { team = \"b\" }\n  active = false\n  trial_period_days = 14\n"),
				ConfigPlanChecks: resource.ConfigPlanChecks{PreApply: []plancheck.PlanCheck{
					plancheck.ExpectResourceAction(address, plancheck.ResourceActionUpdate),
				}},
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr(address, "nickname", "Monthly v2"),
					resource.TestCheckResourceAttr(address, "active", "false"),
					resource.TestCheckResourceAttr(address, "trial_period_days", "14"),
					resource.TestCheckResourceAttr(address, "metadata.%", "1"),
					resource.TestCheckResourceAttr(address, "metadata.team", "b"),
				),
			},
		},
	})
}

// amount is accepted on create only, so changing it replaces the plan.
func TestAccPlan_replace(t *testing.T) {
	acct := acctest.NewAccount(t)
	var first string
	resource.ParallelTest(t, resource.TestCase{
		ProtoV6ProviderFactories: tfacc.ProviderFactories,
		ErrorCheck:               tfacc.SkipWhenFakeLacks(t, "expand["),
		Steps: []resource.TestStep{
			{Config: planConfig(acct, 2000, ""), Check: idOf(&first)},
			{
				Config: planConfig(acct, 2500, ""),
				ConfigPlanChecks: resource.ConfigPlanChecks{PreApply: []plancheck.PlanCheck{
					plancheck.ExpectResourceAction(address, plancheck.ResourceActionReplace),
				}},
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr(address, "amount", "2500"),
					resource.TestCheckResourceAttrWith(address, "id", func(v string) error {
						if v == first {
							return fmt.Errorf("the replacement kept the plan ID %s", v)
						}
						return nil
					}),
				),
			},
		},
	})
}

func TestAccPlan_import(t *testing.T) {
	acct := acctest.NewAccount(t)
	cfg := planConfig(acct, 2000, "  nickname = \"Monthly\"\n  metadata = { team = \"a\" }\n")
	resource.ParallelTest(t, resource.TestCase{
		ProtoV6ProviderFactories: tfacc.ProviderFactories,
		ErrorCheck:               tfacc.SkipWhenFakeLacks(t, "expand["),
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

// Stripe deletes a plan (suite: plan_test.go, "delete"), so destroy removes it.
func TestAccPlan_delete(t *testing.T) {
	acct := acctest.NewAccount(t)
	var id string
	resource.ParallelTest(t, resource.TestCase{
		ProtoV6ProviderFactories: tfacc.ProviderFactories,
		ErrorCheck:               tfacc.SkipWhenFakeLacks(t, "expand["),
		CheckDestroy: func(*terraform.State) error {
			_, err := tfacc.Client(acct).Do(t.Context(), stripeapi.Request{Method: "GET", Path: "/v1/plans/" + id})
			if !stripeapi.IsNotFound(err) {
				return fmt.Errorf("plan %s should be gone after destroy, got %v", id, err)
			}
			return nil
		},
		Steps: []resource.TestStep{{Config: planConfig(acct, 2000, ""), Check: idOf(&id)}},
	})
}

func TestAccPlan_drift(t *testing.T) {
	acct := acctest.NewAccount(t)
	var id string
	cfg := planConfig(acct, 2000, "  nickname = \"Monthly\"\n")
	resource.ParallelTest(t, resource.TestCase{
		ProtoV6ProviderFactories: tfacc.ProviderFactories,
		ErrorCheck:               tfacc.SkipWhenFakeLacks(t, "expand["),
		Steps: []resource.TestStep{
			{Config: cfg, Check: idOf(&id)},
			{
				PreConfig: func() {
					tfacc.Post(t, acct, "/v1/plans/"+id, wire.Object{
						{Name: "nickname", Value: wire.String("Renamed")},
					})
				},
				Config: cfg,
				ConfigPlanChecks: resource.ConfigPlanChecks{PreApply: []plancheck.PlanCheck{
					plancheck.ExpectNonEmptyPlan(),
					plancheck.ExpectResourceAction(address, plancheck.ResourceActionUpdate),
				}},
				Check: resource.TestCheckResourceAttr(address, "nickname", "Monthly"),
			},
		},
	})
}

// Tiers are returned only when expanded. Stripe returns the unbounded last tier's `up_to`
// as null, which must read back as the configured `inf`. Every plan test skips on the
// fake's fake_not_implemented reply for expand[tiers] until it serves it.
func TestAccPlan_tiers(t *testing.T) {
	acct := acctest.NewAccount(t)
	cfg := acct.ProviderBlock() + `
resource "stripe_product" "test" {
  name = "Metered plan"
}

resource "stripe_plan" "test" {
  product        = stripe_product.test.id
  currency       = "usd"
  interval       = "month"
  billing_scheme = "tiered"
  tiers_mode     = "graduated"
  tiers = [
    { up_to = "100", unit_amount = 1000 },
    { up_to = "inf", unit_amount = 500 },
  ]
}
`
	resource.ParallelTest(t, resource.TestCase{
		ProtoV6ProviderFactories: tfacc.ProviderFactories,
		ErrorCheck:               tfacc.SkipWhenFakeLacks(t, "expand["),
		Steps: []resource.TestStep{
			{
				Config: cfg,
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr(address, "tiers.#", "2"),
					resource.TestCheckResourceAttr(address, "tiers.0.up_to", "100"),
					resource.TestCheckResourceAttr(address, "tiers.1.up_to", "inf"),
				),
			},
			{Config: cfg, PlanOnly: true},
		},
	})
}
