package subscription_schedule_test

import (
	"fmt"
	"os"
	"regexp"
	"strconv"
	"testing"
	"time"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/plancheck"
	"github.com/hashicorp/terraform-plugin-testing/terraform"

	"github.com/computomatic/terraform-provider-stripe/internal/acctest"
	"github.com/computomatic/terraform-provider-stripe/internal/acctest/tfacc"
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
	return scheduleConfigFrom(c, customer, `  start_date   = "now"
`, phases, extra)
}

// scheduleConfigFrom is scheduleConfig with the start line in the caller's hands.
func scheduleConfigFrom(c acctest.Config, customer string, start string, phases string, extra string) string {
	return c.ProviderBlock() + fmt.Sprintf(`
resource "stripe_customer" "a" {
  email = "schedule-a@example.com"
}
resource "stripe_customer" "b" {
  email = "schedule-b@example.com"
}

resource "stripe_product" "test" {
  name = %q
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
%s  end_behavior = "cancel"
  default_settings = {
    collection_method = "send_invoice"
    invoice_settings = {
      days_until_due = 30
    }
  }
  phases = %s
%s}
`, c.Name("Plan"), customer, start, phases, extra)
}

func idOf(dst *string) resource.TestCheckFunc {
	return resource.TestCheckResourceAttrWith(address, "id", func(v string) error {
		*dst = v
		return nil
	})
}

// pathOf records the schedule's API path.
func pathOf(dst *string) resource.TestCheckFunc {
	return resource.TestCheckResourceAttrWith(address, "id", func(v string) error {
		*dst = "/v1/subscription_schedules/" + v
		return nil
	})
}

func TestAccSubscriptionSchedule_create(t *testing.T) {
	acct := acctest.NewAccount(t)
	resource.ParallelTest(t, resource.TestCase{
		ProtoV6ProviderFactories: tfacc.ProviderFactories,
		Steps: acctest.Forget(t, []resource.TestStep{{
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
		}}, nil, "stripe_subscription_schedule"),
	})
}

func TestAccSubscriptionSchedule_update(t *testing.T) {
	acct := acctest.NewAccount(t)
	resource.ParallelTest(t, resource.TestCase{
		ProtoV6ProviderFactories: tfacc.ProviderFactories,
		Steps: acctest.Forget(t, []resource.TestStep{
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
		}, nil, "stripe_subscription_schedule"),
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
		Steps: acctest.Forget(t, []resource.TestStep{
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
		}, nil, "stripe_subscription_schedule"),
	})
}

// `customer` is create-only, so changing it would replace the schedule, which Stripe cannot
// delete: the plan fails, and so does a taint, at apply.
func TestAccSubscriptionSchedule_replace(t *testing.T) {
	acct := acctest.NewAccount(t)
	cfg := scheduleConfig(acct, "a", onePhase, "")
	resource.ParallelTest(t, resource.TestCase{
		ProtoV6ProviderFactories: tfacc.ProviderFactories,
		Steps: acctest.Forget(t, []resource.TestStep{
			{Config: cfg},
			{Config: scheduleConfig(acct, "b", onePhase, ""), PlanOnly: true, ExpectError: tfacc.RefusedReplace},
			{Config: cfg, Taint: []string{address}, ExpectError: tfacc.RefusedDestroy},
		}, nil, "stripe_subscription_schedule"),
	})
}

func TestAccSubscriptionSchedule_import(t *testing.T) {
	acct := acctest.NewAccount(t)
	cfg := scheduleConfig(acct, "a", onePhase, "  metadata = { team = \"a\" }\n")
	resource.ParallelTest(t, resource.TestCase{
		ProtoV6ProviderFactories: tfacc.ProviderFactories,
		Steps: acctest.Forget(t, []resource.TestStep{
			{Config: cfg},
			{
				ResourceName:      address,
				ImportState:       true,
				ImportStateVerify: true,
				// Stripe returns the start and end of each phase, not the `start_date` the
				// schedule was created with or the `duration` of a phase.
				ImportStateVerifyIgnore: []string{"start_date", "phases.0.duration"},
			},
			// The block step checks the plan, so its configuration omits the two fields
			// Stripe does not return.
			{
				ResourceName:    address,
				ImportState:     true,
				ImportStateKind: resource.ImportBlockWithID,
				Config: scheduleConfigFrom(acct, "a", "", `[{
    items = [{ price = stripe_price.one.id }]
  }]`, "  metadata = { team = \"a\" }\n"),
			},
		}, nil, "stripe_subscription_schedule"),
	})
}

// Stripe cannot delete a schedule, so dropping it from the configuration, and `terraform
// destroy`, fail at plan time, and the schedule stays as it was.
func TestAccSubscriptionSchedule_delete(t *testing.T) {
	acct := acctest.NewAccount(t)
	var path string
	var mark int
	cfg := scheduleConfig(acct, "a", onePhase, "")
	resource.ParallelTest(t, resource.TestCase{
		ProtoV6ProviderFactories: tfacc.ProviderFactories,
		Steps: acctest.Forget(t, []resource.TestStep{
			{Config: cfg, Check: resource.ComposeTestCheckFunc(pathOf(&path), tfacc.LogMark(t, acct, &mark))},
			{Config: acctest.Without(t, cfg, "stripe_subscription_schedule"), PlanOnly: true, ExpectError: tfacc.RefusedDestroy},
			{Config: cfg, Destroy: true, ExpectError: tfacc.RefusedDestroy},
		}, tfacc.Untouched(t, acct, &path, &mark, "status", wire.String("active")), "stripe_subscription_schedule"),
	})
}

// Setting `status` to `canceled` cancels the schedule (suite: "cancel"), which also cancels the
// subscription it started ("cancels the subscription"), and cannot be undone.
func TestAccSubscriptionSchedule_cancel(t *testing.T) {
	acct := acctest.NewAccount(t)
	var path, subscription string
	resource.ParallelTest(t, resource.TestCase{
		ProtoV6ProviderFactories: tfacc.ProviderFactories,
		Steps: acctest.Forget(t, []resource.TestStep{
			{
				Config: scheduleConfig(acct, "a", onePhase, ""),
				Check: resource.ComposeTestCheckFunc(pathOf(&path),
					resource.TestCheckResourceAttrWith(address, "subscription", func(v string) error { subscription = "/v1/subscriptions/" + v; return nil })),
			},
			{
				Config: scheduleConfig(acct, "a", onePhase, "  status = \"canceled\"\n"),
				Check: resource.ComposeTestCheckFunc(
					resource.TestCheckResourceAttr(address, "status", "canceled"),
					tfacc.Holds(t, acct, &path, "status", wire.String("canceled")),
					tfacc.Holds(t, acct, &subscription, "status", wire.String("canceled")),
				),
			},
			// Canceling is final, so releasing the schedule afterwards fails at plan time.
			{
				Config:      scheduleConfig(acct, "a", onePhase, "  status = \"released\"\n"),
				PlanOnly:    true,
				ExpectError: regexp.MustCompile("Cannot change `status` from `canceled`"),
			},
		}, nil, "stripe_subscription_schedule"),
	})
}

// Setting `status` to `released` releases the subscription from the schedule (suite:
// "release"): the schedule ends and the subscription keeps billing on its own.
func TestAccSubscriptionSchedule_release(t *testing.T) {
	acct := acctest.NewAccount(t)
	var path, subscription string
	resource.ParallelTest(t, resource.TestCase{
		ProtoV6ProviderFactories: tfacc.ProviderFactories,
		Steps: acctest.Forget(t, []resource.TestStep{
			{
				Config: scheduleConfig(acct, "a", onePhase, ""),
				Check: resource.ComposeTestCheckFunc(pathOf(&path),
					resource.TestCheckResourceAttrWith(address, "subscription", func(v string) error { subscription = "/v1/subscriptions/" + v; return nil })),
			},
			{
				Config: scheduleConfig(acct, "a", onePhase, "  status = \"released\"\n"),
				Check: resource.ComposeTestCheckFunc(
					resource.TestCheckResourceAttr(address, "status", "released"),
					tfacc.Holds(t, acct, &path, "status", wire.String("released")),
					tfacc.Holds(t, acct, &subscription, "status", wire.String("active")),
				),
			},
		}, nil, "stripe_subscription_schedule"),
	})
}

func TestAccSubscriptionSchedule_drift(t *testing.T) {
	acct := acctest.NewAccount(t)
	var id string
	cfg := scheduleConfig(acct, "a", onePhase, "  metadata = { team = \"a\" }\n")
	resource.ParallelTest(t, resource.TestCase{
		ProtoV6ProviderFactories: tfacc.ProviderFactories,
		Steps: acctest.Forget(t, []resource.TestStep{
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
		}, nil, "stripe_subscription_schedule"),
	})
}

// Stripe fills arguments of a phase a create leaves out, which the plan then leaves to it and the
// next plan keeps: the end of a trial phase (suite: TestSubscriptionScheduleAttributes, probe
// `every_field/6`) and an added invoice item's `discountable` and `quantity` (`every_field/21`,
// with the suite's coupon of 10% once on its one-time free price). Each schedule sends what its
// probe sent, starting a day ahead; neither has a pause schedule, which reads back empty.
func TestAccSubscriptionSchedule_serverFilled(t *testing.T) {
	acct := acctest.NewAccount(t)
	start := strconv.FormatInt(time.Now().Add(24*time.Hour).Unix(), 10)
	config := acct.ProviderBlock() + fmt.Sprintf(`
resource "stripe_customer" "test" {
  email = "schedule-filled@example.com"
}

resource "stripe_product" "test" {
  name = %[1]q
}

resource "stripe_price" "free" {
  product     = stripe_product.test.id
  currency    = "usd"
  unit_amount = 0
  recurring = {
    interval = "month"
  }
}

resource "stripe_price" "one_time" {
  product     = stripe_product.test.id
  currency    = "usd"
  unit_amount = 0
}

resource "stripe_coupon" "test" {
  percent_off = 10
  duration    = "once"
}

resource "stripe_subscription_schedule" "trial" {
  customer   = stripe_customer.test.id
  start_date = %[2]q
  phases = [{
    items    = [{ price = stripe_price.free.id }]
    duration = { interval = "month" }
    trial    = true
  }]
}

resource "stripe_subscription_schedule" "invoice_item" {
  customer   = stripe_customer.test.id
  start_date = %[2]q
  phases = [{
    items             = [{ price = stripe_price.free.id }]
    duration          = { interval = "month" }
    add_invoice_items = [{ price = stripe_price.one_time.id, discounts = [{ coupon = stripe_coupon.test.id }] }]
  }]
}
`, acct.Name("Plan"), start)
	resource.ParallelTest(t, resource.TestCase{
		ProtoV6ProviderFactories: tfacc.ProviderFactories,
		Steps: acctest.Forget(t, []resource.TestStep{
			{
				Config: config,
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttrSet("stripe_subscription_schedule.trial", "phases.0.trial_end"),
					resource.TestCheckResourceAttr("stripe_subscription_schedule.trial", "pause_schedules.#", "0"),
					resource.TestCheckResourceAttr("stripe_subscription_schedule.invoice_item", "phases.0.add_invoice_items.0.discountable", "true"),
					resource.TestCheckResourceAttr("stripe_subscription_schedule.invoice_item", "phases.0.add_invoice_items.0.quantity", "1"),
				),
			},
			{Config: config, PlanOnly: true},
		}, nil, "stripe_subscription_schedule"),
	})
}

// Stripe replaces a phase's invoice item's `metadata` whole (the record's `map` of the update's
// `phases.add_invoice_items.metadata`), so an update that drops a key sends the map without it.
// As in the suite's `update/3`, whose keys and values these are, the schedule starts a day later
// and the update sends the first phase's `start_date` back as Stripe holds it, which Stripe needs
// to anchor the end dates to although the phase has not begun.
func TestAccSubscriptionSchedule_invoiceItemMetadataReplaced(t *testing.T) {
	acct := acctest.NewAccount(t)
	start := strconv.FormatInt(time.Now().Add(24*time.Hour).Unix(), 10)
	var path string
	config := func(metadata string) string {
		return acct.ProviderBlock() + fmt.Sprintf(`
resource "stripe_customer" "test" {
  email = "schedule-metadata@example.com"
}

resource "stripe_product" "test" {
  name = %[1]q
}

resource "stripe_price" "free" {
  product     = stripe_product.test.id
  currency    = "usd"
  unit_amount = 0
  recurring = {
    interval = "month"
  }
}

resource "stripe_price" "one_time" {
  product     = stripe_product.test.id
  currency    = "usd"
  unit_amount = 0
}

resource "stripe_subscription_schedule" "test" {
  customer   = stripe_customer.test.id
  start_date = %[2]q
  phases = [{
    items             = [{ price = stripe_price.free.id }]
    duration          = { interval = "month" }
    add_invoice_items = [{ price = stripe_price.one_time.id, metadata = %[3]s }]
  }]
}
`, acct.Name("Plan"), start, metadata)
	}
	resource.ParallelTest(t, resource.TestCase{
		ProtoV6ProviderFactories: tfacc.ProviderFactories,
		Steps: acctest.Forget(t, []resource.TestStep{
			{
				Config: config(`{ attrs_a = "three", attrs_b = "four" }`),
				Check: resource.ComposeAggregateTestCheckFunc(
					pathOf(&path),
					resource.TestCheckResourceAttr(address, "phases.0.add_invoice_items.0.metadata.%", "2"),
				),
			},
			{
				Config: config(`{ attrs_b = "four" }`),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr(address, "phases.0.add_invoice_items.0.metadata.%", "1"),
					resource.TestCheckResourceAttr(address, "phases.0.add_invoice_items.0.metadata.attrs_b", "four"),
					func(*terraform.State) error {
						if acct.Smoke {
							return nil // the request log is the fake's
						}
						for _, r := range acctest.RequestLog(t, acct) {
							if r.Method != "POST" || r.Path != path {
								continue
							}
							if got := firstPhaseStart(r.Params); got != start {
								return fmt.Errorf("the update sent the first phase's start as %q, want %s: %v", got, start, r.Params)
							}
							return nil
						}
						return fmt.Errorf("no update of %s in the request log", path)
					},
				),
			},
		}, nil, "stripe_subscription_schedule"),
	})
}

// firstPhaseStart is the `phases[0][start_date]` a logged request sent, or "".
func firstPhaseStart(params wire.Value) string {
	body, _ := params.(wire.Object)
	list, _ := body.Get("phases")
	phases, _ := list.(wire.Array)
	if len(phases) == 0 {
		return ""
	}
	first, _ := phases[0].(wire.Object)
	switch v, _ := first.Get("start_date"); v := v.(type) {
	case wire.String:
		return string(v)
	case wire.Int:
		return strconv.FormatInt(int64(v), 10)
	}
	return ""
}
