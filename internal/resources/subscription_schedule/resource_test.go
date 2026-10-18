package subscription_schedule_test

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

const address = "stripe_subscription_schedule.test"

const onePhase = `[{
    items    = [{ price = stripe_price.one.id }]
    duration = { interval = "month", interval_count = 1 }
  }]`

// scheduleConfig is a customer, two prices and a schedule invoiced by email. phases and
// extra land inside the schedule.
func scheduleConfig(c acctest.Config, customer string, phases string, extra string) string {
	return c.ProviderBlock() + fmt.Sprintf(`
resource "stripe_customer" "a" {}
resource "stripe_customer" "b" {}

resource "stripe_product" "test" {
  name = "Plan"
}

resource "stripe_price" "one" {
  product     = stripe_product.test.id
  currency    = "usd"
  unit_amount = 1000
  recurring = {
    interval = "month"
  }
}

resource "stripe_price" "two" {
  product     = stripe_product.test.id
  currency    = "usd"
  unit_amount = 2000
  recurring = {
    interval = "month"
  }
}

resource "stripe_subscription_schedule" "test" {
  customer     = stripe_customer.%s.id
  start_date   = "now"
  end_behavior = "cancel"
  default_settings = {
    collection_method = "send_invoice"
    invoice_settings = {
      days_until_due = 30
    }
  }
  phases = %s
%s}
`, customer, phases, extra)
}

func idOf(dst *string) resource.TestCheckFunc {
	return resource.TestCheckResourceAttrWith(address, "id", func(v string) error {
		*dst = v
		return nil
	})
}

func TestAccSubscriptionSchedule_create(t *testing.T) {
	acct := acctest.NewAccount(t)
	resource.ParallelTest(t, resource.TestCase{
		ProtoV6ProviderFactories: tfacc.ProviderFactories,
		Steps: []resource.TestStep{{
			Config: scheduleConfig(acct, "a", onePhase, "  metadata = { team = \"a\" }\n") + `
data "stripe_subscription_schedule" "by_id" {
  id = stripe_subscription_schedule.test.id
}
`,
			Check: resource.ComposeAggregateTestCheckFunc(
				resource.TestCheckResourceAttrSet(address, "id"),
				resource.TestCheckResourceAttrPair(address, "customer", "stripe_customer.a", "id"),
				resource.TestCheckResourceAttr(address, "status", "active"),
				resource.TestCheckResourceAttr(address, "end_behavior", "cancel"),
				resource.TestCheckResourceAttrSet(address, "subscription"),
				resource.TestCheckResourceAttr(address, "phases.#", "1"),
				resource.TestCheckResourceAttrPair(address, "phases.0.items.0.price", "stripe_price.one", "id"),
				resource.TestCheckResourceAttr(address, "phases.0.duration.interval", "month"),
				resource.TestCheckResourceAttrSet(address, "phases.0.start_date"),
				resource.TestCheckResourceAttr(address, "metadata.team", "a"),
				resource.TestCheckResourceAttrPair("data.stripe_subscription_schedule.by_id", "id", address, "id"),
				resource.TestCheckResourceAttrSet("data.stripe_subscription_schedule.by_id", "phases.0.end_date"),
			),
		}},
	})
}

func TestAccSubscriptionSchedule_update(t *testing.T) {
	acct := acctest.NewAccount(t)
	resource.ParallelTest(t, resource.TestCase{
		ProtoV6ProviderFactories: tfacc.ProviderFactories,
		Steps: []resource.TestStep{
			{Config: scheduleConfig(acct, "a", onePhase, "  metadata = { team = \"a\", tier = \"1\" }\n")},
			{
				Config: scheduleConfig(acct, "a", onePhase, "  metadata = { team = \"b\" }\n"),
				ConfigPlanChecks: resource.ConfigPlanChecks{PreApply: []plancheck.PlanCheck{
					plancheck.ExpectResourceAction(address, plancheck.ResourceActionUpdate),
				}},
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr(address, "metadata.%", "1"),
					resource.TestCheckResourceAttr(address, "metadata.team", "b"),
				),
			},
		},
	})
}

// An update replaces the phases and needs the current phase to keep its start_date (suite:
// "replace phases"), which the update sends back from the state.
func TestAccSubscriptionSchedule_updatePhases(t *testing.T) {
	acct := acctest.NewAccount(t)
	var start string
	twoPhases := `[
    {
      items    = [{ price = stripe_price.one.id }]
      duration = { interval = "month", interval_count = 1 }
    },
    {
      items    = [{ price = stripe_price.two.id }]
      duration = { interval = "month", interval_count = 1 }
    },
  ]`
	resource.ParallelTest(t, resource.TestCase{
		ProtoV6ProviderFactories: tfacc.ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: scheduleConfig(acct, "a", onePhase, ""),
				Check:  resource.TestCheckResourceAttrWith(address, "phases.0.start_date", func(v string) error { start = v; return nil }),
			},
			{
				Config: scheduleConfig(acct, "a", twoPhases, "  proration_behavior = \"none\"\n"),
				ConfigPlanChecks: resource.ConfigPlanChecks{PreApply: []plancheck.PlanCheck{
					plancheck.ExpectResourceAction(address, plancheck.ResourceActionUpdate),
				}},
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr(address, "phases.#", "2"),
					resource.TestCheckResourceAttrPair(address, "phases.1.items.0.price", "stripe_price.two", "id"),
					resource.TestCheckResourceAttrWith(address, "phases.0.start_date", func(v string) error {
						if v != start {
							return fmt.Errorf("the current phase started at %s, now %s", start, v)
						}
						return nil
					}),
				),
			},
		},
	})
}

// customer is accepted on create only (suite: "`customer`, `start_date` and
// `from_subscription` on update are `parameter_unknown`"), so changing it replaces the
// schedule, which cancels the old one.
func TestAccSubscriptionSchedule_replace(t *testing.T) {
	acct := acctest.NewAccount(t)
	var first string
	resource.ParallelTest(t, resource.TestCase{
		ProtoV6ProviderFactories: tfacc.ProviderFactories,
		Steps: []resource.TestStep{
			{Config: scheduleConfig(acct, "a", onePhase, ""), Check: idOf(&first)},
			{
				Config: scheduleConfig(acct, "b", onePhase, ""),
				ConfigPlanChecks: resource.ConfigPlanChecks{PreApply: []plancheck.PlanCheck{
					plancheck.ExpectResourceAction(address, plancheck.ResourceActionReplace),
				}},
				Check: resource.TestCheckResourceAttrWith(address, "id", func(v string) error {
					if v == first {
						return fmt.Errorf("the replacement kept the schedule ID %s", v)
					}
					return nil
				}),
			},
		},
	})
}

func TestAccSubscriptionSchedule_import(t *testing.T) {
	acct := acctest.NewAccount(t)
	cfg := scheduleConfig(acct, "a", onePhase, "  metadata = { team = \"a\" }\n")
	resource.ParallelTest(t, resource.TestCase{
		ProtoV6ProviderFactories: tfacc.ProviderFactories,
		Steps: []resource.TestStep{
			{Config: cfg},
			{
				ResourceName:      address,
				ImportState:       true,
				ImportStateVerify: true,
				// Stripe returns the start and end of each phase, not the `start_date` the
				// schedule was created with or the `duration` of a phase.
				ImportStateVerifyIgnore: []string{"start_date", "phases.0.duration"},
			},
		},
	})
}

// Stripe cannot delete a schedule, so destroy cancels it (suite: "Subscription schedules",
// `cancel`, which "cancels the subscription"); the schedule stays in the account.
func TestAccSubscriptionSchedule_delete(t *testing.T) {
	acct := acctest.NewAccount(t)
	var id, subscription string
	resource.ParallelTest(t, resource.TestCase{
		ProtoV6ProviderFactories: tfacc.ProviderFactories,
		CheckDestroy: func(*terraform.State) error {
			get := func(path string) (wire.Object, error) {
				resp, err := tfacc.Client(acct).Do(t.Context(), stripeapi.Request{Method: "GET", Path: path})
				if err != nil {
					return nil, err
				}
				return resp.Body.(wire.Object), nil
			}
			schedule, err := get("/v1/subscription_schedules/" + id)
			if err != nil {
				return fmt.Errorf("a cancelled schedule still retrieves: %w", err)
			}
			if status, _ := schedule.GetString("status"); status != "canceled" {
				return fmt.Errorf("schedule %s is %q after destroy, want canceled", id, status)
			}
			sub, err := get("/v1/subscriptions/" + subscription)
			if err != nil {
				return err
			}
			if status, _ := sub.GetString("status"); status != "canceled" {
				return fmt.Errorf("subscription %s is %q after destroy, want canceled", subscription, status)
			}
			return nil
		},
		Steps: []resource.TestStep{{
			Config: scheduleConfig(acct, "a", onePhase, ""),
			Check: resource.ComposeAggregateTestCheckFunc(
				idOf(&id),
				resource.TestCheckResourceAttrWith(address, "subscription", func(v string) error { subscription = v; return nil }),
			),
		}},
	})
}

// A schedule released out of band cannot be cancelled (suite: "Release and cancel of a
// schedule not in `not_started` or `active` are 400"), and has nothing left to cancel, so
// destroy leaves it as it is.
func TestAccSubscriptionSchedule_deleteAfterRelease(t *testing.T) {
	acct := acctest.NewAccount(t)
	var id string
	cfg := scheduleConfig(acct, "a", onePhase, "")
	resource.ParallelTest(t, resource.TestCase{
		ProtoV6ProviderFactories: tfacc.ProviderFactories,
		Steps: []resource.TestStep{
			{Config: cfg, Check: idOf(&id)},
			{
				PreConfig: func() { tfacc.Post(t, acct, "/v1/subscription_schedules/"+id+"/release", nil) },
				Config:    cfg,
				Check:     resource.TestCheckResourceAttr(address, "status", "released"),
			},
		},
	})
}

func TestAccSubscriptionSchedule_drift(t *testing.T) {
	acct := acctest.NewAccount(t)
	var id string
	cfg := scheduleConfig(acct, "a", onePhase, "  metadata = { team = \"a\" }\n")
	resource.ParallelTest(t, resource.TestCase{
		ProtoV6ProviderFactories: tfacc.ProviderFactories,
		Steps: []resource.TestStep{
			{Config: cfg, Check: idOf(&id)},
			{
				PreConfig: func() {
					tfacc.Post(t, acct, "/v1/subscription_schedules/"+id, wire.Object{
						{Name: "metadata", Value: wire.Object{{Name: "team", Value: wire.String("b")}}},
					})
				},
				Config: cfg,
				ConfigPlanChecks: resource.ConfigPlanChecks{PreApply: []plancheck.PlanCheck{
					plancheck.ExpectNonEmptyPlan(),
					plancheck.ExpectResourceAction(address, plancheck.ResourceActionUpdate),
				}},
				Check: resource.TestCheckResourceAttr(address, "metadata.team", "a"),
			},
		},
	})
}
