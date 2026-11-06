package subscription_test

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
	"github.com/computomatic/terraform-provider-stripe/internal/stripeapi"
	"github.com/computomatic/terraform-provider-stripe/internal/wire"
)

func TestMain(m *testing.M) { os.Exit(acctest.Main(m)) }

const address = "stripe_subscription.test"

// subscriptionConfig is a customer, two prices and a subscription invoiced by email, so no
// payment method is needed. items and extra land inside the subscription.
func subscriptionConfig(c acctest.Config, items string, extra string) string {
	return subscriptionDue(c, 30, items, extra)
}

func subscriptionDue(c acctest.Config, days int, items string, extra string) string {
	return c.ProviderBlock() + fmt.Sprintf(`
resource "stripe_customer" "test" {
  email = "subscription@example.com"
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

resource "stripe_subscription" "test" {
  customer          = stripe_customer.test.id
  collection_method = "send_invoice"
  days_until_due    = %d
  items             = %s
%s}
`, c.Name("Plan"), days, items, extra)
}

const oneItem = `[{ price = stripe_price.one.id }]`

func idOf(dst *string) resource.TestCheckFunc {
	return resource.TestCheckResourceAttrWith(address, "id", func(v string) error {
		*dst = v
		return nil
	})
}

func TestAccSubscription_create(t *testing.T) {
	acct := acctest.NewAccount(t)
	resource.ParallelTest(t, resource.TestCase{
		ProtoV6ProviderFactories: tfacc.ProviderFactories,
		Steps: []resource.TestStep{{
			Config: subscriptionConfig(acct, `[{ price = stripe_price.one.id, quantity = 3 }]`, "  description = \"Team\"\n  metadata = { team = \"a\" }\n") + `
data "stripe_subscription" "by_id" {
  id = stripe_subscription.test.id
}

data "stripe_invoice" "latest" {
  id = stripe_subscription.test.latest_invoice
}
`,
			Check: resource.ComposeAggregateTestCheckFunc(
				resource.TestCheckResourceAttrSet(address, "id"),
				resource.TestCheckResourceAttrPair(address, "customer", "stripe_customer.test", "id"),
				resource.TestCheckResourceAttr(address, "status", "active"),
				resource.TestCheckResourceAttr(address, "collection_method", "send_invoice"),
				resource.TestCheckResourceAttr(address, "days_until_due", "30"),
				resource.TestCheckResourceAttr(address, "description", "Team"),
				resource.TestCheckResourceAttr(address, "cancel_at_period_end", "false"),
				resource.TestCheckResourceAttr(address, "items.#", "1"),
				resource.TestCheckResourceAttrPair(address, "items.0.price", "stripe_price.one", "id"),
				resource.TestCheckResourceAttr(address, "items.0.quantity", "3"),
				resource.TestCheckResourceAttrSet(address, "items.0.id"),
				resource.TestCheckResourceAttr(address, "metadata.team", "a"),
				resource.TestCheckResourceAttrPair("data.stripe_subscription.by_id", "id", address, "id"),
				resource.TestCheckResourceAttr("data.stripe_subscription.by_id", "items.0.quantity", "3"),
				resource.TestCheckResourceAttrPair("data.stripe_invoice.latest", "parent.subscription_details.subscription", address, "id"),
			),
		}},
	})
}

func TestAccSubscription_update(t *testing.T) {
	acct := acctest.NewAccount(t)
	resource.ParallelTest(t, resource.TestCase{
		ProtoV6ProviderFactories: tfacc.ProviderFactories,
		Steps: []resource.TestStep{
			{Config: subscriptionConfig(acct, oneItem, "  metadata = { team = \"a\", tier = \"1\" }\n")},
			{
				Config: subscriptionDue(acct, 14, oneItem, "  cancel_at_period_end = true\n  description = \"Updated\"\n  metadata = { team = \"b\" }\n"),
				ConfigPlanChecks: resource.ConfigPlanChecks{PreApply: []plancheck.PlanCheck{
					plancheck.ExpectResourceAction(address, plancheck.ResourceActionUpdate),
				}},
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr(address, "days_until_due", "14"),
					resource.TestCheckResourceAttr(address, "cancel_at_period_end", "true"),
					// Stripe records why, although the configuration gives no details (suite:
					// update "cancel_at_period_end").
					resource.TestCheckResourceAttr(address, "cancellation_details.reason", "cancellation_requested"),
					resource.TestCheckNoResourceAttr(address, "cancellation_details.feedback_option"),
					resource.TestCheckResourceAttr(address, "description", "Updated"),
					resource.TestCheckResourceAttr(address, "metadata.%", "1"),
					resource.TestCheckResourceAttr(address, "metadata.team", "b"),
				),
			},
			{
				Config: subscriptionDue(acct, 14, oneItem, "  cancel_at_period_end = false\n  description = \"Updated\"\n  metadata = { team = \"b\" }\n"),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr(address, "cancel_at_period_end", "false"),
					resource.TestCheckNoResourceAttr(address, "cancellation_details.reason"),
				),
			},
		},
	})
}

// Items are matched by price. A new price adds an item and a dropped one deletes it (the
// fake answers both with proration_behavior = "none"), and an item whose price stays keeps
// its ID and changes quantity in place (suite: "swap item price" names an item by ID).
func TestAccSubscription_updateItems(t *testing.T) {
	acct := acctest.NewAccount(t)
	var item string
	resource.ParallelTest(t, resource.TestCase{
		ProtoV6ProviderFactories: tfacc.ProviderFactories,
		ErrorCheck:               tfacc.SkipWhenFakeLacks(t, "items"),
		Steps: []resource.TestStep{
			{
				Config: subscriptionConfig(acct, oneItem, ""),
				Check:  resource.TestCheckResourceAttrWith(address, "items.0.id", func(v string) error { item = v; return nil }),
			},
			{
				Config: subscriptionConfig(acct, `[{ price = stripe_price.one.id, quantity = 2 }]`, "  proration_behavior = \"none\"\n"),
				ConfigPlanChecks: resource.ConfigPlanChecks{PreApply: []plancheck.PlanCheck{
					plancheck.ExpectResourceAction(address, plancheck.ResourceActionUpdate),
				}},
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr(address, "items.#", "1"),
					resource.TestCheckResourceAttr(address, "items.0.quantity", "2"),
					resource.TestCheckResourceAttrWith(address, "items.0.id", func(v string) error {
						if v != item {
							return fmt.Errorf("the item ID changed from %s to %s", item, v)
						}
						return nil
					}),
				),
			},
			{
				// A swap adds the new price and deletes the old one.
				Config: subscriptionConfig(acct, `[{ price = stripe_price.two.id }]`, "  proration_behavior = \"none\"\n"),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr(address, "items.#", "1"),
					resource.TestCheckResourceAttrPair(address, "items.0.price", "stripe_price.two", "id"),
				),
			},
		},
	})
}

const threePrices = `
resource "stripe_price" "three" {
  product     = stripe_product.test.id
  currency    = "usd"
  unit_amount = 3000
  recurring = {
    interval = "month"
  }
}
`

// Removing the middle item of three deletes that item and leaves the others as they are.
func TestAccSubscription_removeTheMiddleItem(t *testing.T) {
	acct := acctest.NewAccount(t)
	ids := map[string]string{}
	remember := func(i int) resource.TestCheckFunc {
		return resource.TestCheckResourceAttrWith(address, fmt.Sprintf("items.%d.id", i), func(v string) error { ids[fmt.Sprint(i)] = v; return nil })
	}
	three := `[{ price = stripe_price.one.id }, { price = stripe_price.two.id }, { price = stripe_price.three.id }]`
	two := `[{ price = stripe_price.one.id }, { price = stripe_price.three.id }]`
	resource.ParallelTest(t, resource.TestCase{
		ProtoV6ProviderFactories: tfacc.ProviderFactories,
		ErrorCheck:               tfacc.SkipWhenFakeLacks(t, "items"),
		Steps: []resource.TestStep{
			{
				Config: subscriptionConfig(acct, three, "") + threePrices,
				Check:  resource.ComposeAggregateTestCheckFunc(remember(0), remember(2)),
			},
			{
				Config: subscriptionConfig(acct, two, "  proration_behavior = \"none\"\n") + threePrices,
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr(address, "items.#", "2"),
					resource.TestCheckResourceAttrPair(address, "items.0.price", "stripe_price.one", "id"),
					resource.TestCheckResourceAttrPair(address, "items.1.price", "stripe_price.three", "id"),
					resource.TestCheckResourceAttrWith(address, "items.0.id", func(v string) error {
						if v != ids["0"] {
							return fmt.Errorf("the first item changed from %s to %s", ids["0"], v)
						}
						return nil
					}),
					resource.TestCheckResourceAttrWith(address, "items.1.id", func(v string) error {
						if v != ids["2"] {
							return fmt.Errorf("the last item changed from %s to %s", ids["2"], v)
						}
						return nil
					}),
				),
			},
		},
	})
}

// A reorder is not a change: the apply sends Stripe no update to the subscription.
func TestAccSubscription_reorderItems(t *testing.T) {
	acct := acctest.NewAccount(t)
	var id string
	ab := `[{ price = stripe_price.one.id }, { price = stripe_price.two.id }]`
	ba := `[{ price = stripe_price.two.id }, { price = stripe_price.one.id }]`
	resource.ParallelTest(t, resource.TestCase{
		ProtoV6ProviderFactories: tfacc.ProviderFactories,
		Steps: []resource.TestStep{
			{Config: subscriptionConfig(acct, ab, ""), Check: idOf(&id)},
			{
				Config: subscriptionConfig(acct, ba, ""),
				Check: func(*terraform.State) error {
					if acct.Smoke {
						return nil // the request log is the fake's; the plan-only step below still runs
					}
					for _, r := range acctest.RequestLog(t, acct) {
						if r.Method == "POST" && r.Path == "/v1/subscriptions/"+id {
							return fmt.Errorf("a reorder sent an update: %v", r.Params)
						}
					}
					return nil
				},
			},
			{Config: subscriptionConfig(acct, ba, ""), PlanOnly: true},
		},
	})
}

// A value not known at plan may yet resolve to an item that names its price one way, so the plan
// passes and the plan Terraform makes at apply decides: `terraform_data`'s `id` is known only then.
func TestAccSubscription_itemKnownAtApply(t *testing.T) {
	const (
		data    = `price_data = { currency = "usd", product = stripe_product.test.id, recurring = { interval = "month" }, unit_amount = 1000 }`
		toNull  = `terraform_data.late.id != "" ? null : stripe_price.one.id`
		toPrice = `terraform_data.late.id != "" ? stripe_price.one.id : null`
	)
	for name, tc := range map[string]struct {
		items, input string
		refused      string
	}{
		"an unknown price beside data": {items: `[{ price = ` + toNull + `, ` + data + ` }]`, input: `"late"`},
		"an unknown element":           {items: `[terraform_data.late.output]`, input: `{ price = stripe_price.one.id }`},
		"both, known at apply":         {items: `[{ price = ` + toPrice + `, ` + data + ` }]`, input: `"late"`, refused: "sets exactly one"},
		"neither, known at apply":      {items: `[{ price = ` + toNull + ` }]`, input: `"late"`, refused: "Empty object"},
	} {
		t.Run(name, func(t *testing.T) {
			acct := acctest.NewAccount(t)
			cfg := subscriptionConfig(acct, tc.items, "") + fmt.Sprintf(`
resource "terraform_data" "late" {
  input = %s
}
`, tc.input)
			steps := []resource.TestStep{{Config: cfg}}
			if tc.refused != "" {
				steps[0].ExpectError = regexp.MustCompile(tc.refused)
			} else {
				steps = append(steps, resource.TestStep{Config: cfg, PlanOnly: true})
			}
			resource.ParallelTest(t, resource.TestCase{ProtoV6ProviderFactories: tfacc.ProviderFactories, Steps: steps})
		})
	}
}

// An item that sets price_data has no price in the plan: adding one on update keeps the item
// Stripe creates in the state, and changing its data replaces the item.
func TestAccSubscription_priceDataItems(t *testing.T) {
	acct := acctest.NewAccount(t)
	data := func(amount, quantity int) string {
		return fmt.Sprintf(`{ quantity = %d, price_data = { currency = "usd", product = stripe_product.test.id, recurring = { interval = "month" }, unit_amount = %d } }`, quantity, amount)
	}
	withData := func(amount, quantity int) string {
		return `[{ price = stripe_price.one.id }, ` + data(amount, quantity) + `]`
	}
	var first string
	resource.ParallelTest(t, resource.TestCase{
		ProtoV6ProviderFactories: tfacc.ProviderFactories,
		Steps: []resource.TestStep{
			{Config: subscriptionConfig(acct, oneItem, "")},
			{
				Config: subscriptionConfig(acct, withData(1500, 1), "  proration_behavior = \"none\"\n"),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr(address, "items.#", "2"),
					resource.TestCheckResourceAttrPair(address, "items.0.price", "stripe_price.one", "id"),
					resource.TestCheckResourceAttrWith(address, "items.1.id", func(v string) error { first = v; return nil }),
				),
			},
			{Config: subscriptionConfig(acct, withData(1500, 1), "  proration_behavior = \"none\"\n"), PlanOnly: true},
			{
				// Only the quantity changes: the item is updated in place, not replaced.
				Config: subscriptionConfig(acct, withData(1500, 3), "  proration_behavior = \"none\"\n"),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr(address, "items.1.quantity", "3"),
					resource.TestCheckResourceAttrWith(address, "items.1.id", func(v string) error {
						if v != first {
							return fmt.Errorf("a quantity change must keep the item, but its ID went from %s to %s", first, v)
						}
						return nil
					}),
				),
			},
			{
				Config: subscriptionConfig(acct, withData(1800, 3), "  proration_behavior = \"none\"\n"),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr(address, "items.#", "2"),
					resource.TestCheckResourceAttrWith(address, "items.1.id", func(v string) error {
						if v == first {
							return fmt.Errorf("changed price_data must replace the item, but it kept ID %s", v)
						}
						return nil
					}),
				),
			},
		},
	})
}

// A price changed from outside stays on the same element and shows as drift; the next apply
// moves the subscription back to the configured price.
func TestAccSubscription_priceChangedFromOutside(t *testing.T) {
	acct := acctest.NewAccount(t)
	var item, two string
	cfg := subscriptionConfig(acct, oneItem, "  proration_behavior = \"none\"\n")
	resource.ParallelTest(t, resource.TestCase{
		ProtoV6ProviderFactories: tfacc.ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: subscriptionConfig(acct, oneItem, ""),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttrWith(address, "items.0.id", func(v string) error { item = v; return nil }),
					resource.TestCheckResourceAttrWith("stripe_price.two", "id", func(v string) error { two = v; return nil }),
				),
			},
			{
				PreConfig: func() {
					tfacc.Post(t, acct, "/v1/subscription_items/"+item, wire.Object{
						{Name: "price", Value: wire.String(two)}, {Name: "proration_behavior", Value: wire.String("none")},
					})
				},
				Config: cfg,
				ConfigPlanChecks: resource.ConfigPlanChecks{PreApply: []plancheck.PlanCheck{
					plancheck.ExpectNonEmptyPlan(),
				}},
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr(address, "items.#", "1"),
					resource.TestCheckResourceAttrPair(address, "items.0.price", "stripe_price.one", "id"),
				),
			},
		},
	})
}

// The same price twice fails the plan.
func TestAccSubscription_duplicatePriceIsRefused(t *testing.T) {
	acct := acctest.NewAccount(t)
	resource.ParallelTest(t, resource.TestCase{
		ProtoV6ProviderFactories: tfacc.ProviderFactories,
		Steps: []resource.TestStep{{
			Config:      subscriptionConfig(acct, `[{ price = stripe_price.one.id }, { price = stripe_price.one.id, quantity = 2 }]`, ""),
			ExpectError: regexp.MustCompile(`Duplicate price`),
		}},
	})
}

// An item that stripe_subscription_item added is not part of the subscription's `items`:
// updating the subscription neither shows nor deletes it, with no ignore_changes.
func TestAccSubscription_leavesItemsAddedFromOutside(t *testing.T) {
	acct := acctest.NewAccount(t)
	cfg := func(metadata string) string {
		return subscriptionConfig(acct, oneItem, "  metadata = { team = \""+metadata+"\" }\n") + `
resource "stripe_subscription_item" "extra" {
  subscription       = stripe_subscription.test.id
  price              = stripe_price.two.id
  proration_behavior = "none"
}
`
	}
	resource.ParallelTest(t, resource.TestCase{
		ProtoV6ProviderFactories: tfacc.ProviderFactories,
		Steps: []resource.TestStep{
			{Config: cfg("a"), Check: resource.TestCheckResourceAttr(address, "items.#", "1")},
			{Config: cfg("a"), PlanOnly: true},
			{
				Config: cfg("b"),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr(address, "items.#", "1"),
					resource.TestCheckResourceAttr(address, "metadata.team", "b"),
					resource.TestCheckResourceAttrSet("stripe_subscription_item.extra", "id"),
				),
			},
			{Config: cfg("b"), PlanOnly: true},
		},
	})
}

// customer is accepted on create only (suite: "`customer`, `currency` and `billing_mode`
// on update are `parameter_unknown`"), so changing it replaces the subscription, which
// cancels the old one.
func TestAccSubscription_replace(t *testing.T) {
	acct := acctest.NewAccount(t)
	var first string
	cfg := func(customer string) string {
		return acct.ProviderBlock() + fmt.Sprintf(`
resource "stripe_customer" "a" {
  email = "subscription-a@example.com"
}
resource "stripe_customer" "b" {
  email = "subscription-b@example.com"
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

resource "stripe_subscription" "test" {
  customer          = stripe_customer.%s.id
  collection_method = "send_invoice"
  days_until_due    = 30
  items             = [{ price = stripe_price.one.id }]
}
`, acct.Name("Plan"), customer)
	}
	resource.ParallelTest(t, resource.TestCase{
		ProtoV6ProviderFactories: tfacc.ProviderFactories,
		Steps: []resource.TestStep{
			{Config: cfg("a"), Check: idOf(&first)},
			{
				Config: cfg("b"),
				ConfigPlanChecks: resource.ConfigPlanChecks{PreApply: []plancheck.PlanCheck{
					plancheck.ExpectResourceAction(address, plancheck.ResourceActionReplace),
				}},
				Check: resource.TestCheckResourceAttrWith(address, "id", func(v string) error {
					if v == first {
						return fmt.Errorf("the replacement kept the subscription ID %s", v)
					}
					return nil
				}),
			},
		},
	})
}

func TestAccSubscription_import(t *testing.T) {
	acct := acctest.NewAccount(t)
	cfg := subscriptionConfig(acct, `[{ price = stripe_price.one.id, quantity = 2 }]`, "  description = \"Team\"\n  metadata = { team = \"a\" }\n")
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

// Stripe's DELETE cancels a subscription (suite: "Subscriptions", `cancel`, "delete"), and a
// cancelled subscription still retrieves with the status `canceled`.
func TestAccSubscription_delete(t *testing.T) {
	acct := acctest.NewAccount(t)
	var id string
	resource.ParallelTest(t, resource.TestCase{
		ProtoV6ProviderFactories: tfacc.ProviderFactories,
		CheckDestroy: func(*terraform.State) error {
			resp, err := tfacc.Client(acct).Do(t.Context(), stripeapi.Request{Method: "GET", Path: "/v1/subscriptions/" + id})
			if err != nil {
				return fmt.Errorf("a cancelled subscription still retrieves: %w", err)
			}
			if status, _ := resp.Body.(wire.Object).GetString("status"); status != "canceled" {
				return fmt.Errorf("subscription %s is %q after destroy, want canceled", id, status)
			}
			return nil
		},
		Steps: []resource.TestStep{{Config: subscriptionConfig(acct, oneItem, ""), Check: idOf(&id)}},
	})
}

func TestAccSubscription_drift(t *testing.T) {
	acct := acctest.NewAccount(t)
	var id string
	cfg := subscriptionConfig(acct, oneItem, "  description = \"Team\"\n")
	resource.ParallelTest(t, resource.TestCase{
		ProtoV6ProviderFactories: tfacc.ProviderFactories,
		Steps: []resource.TestStep{
			{Config: cfg, Check: idOf(&id)},
			{
				PreConfig: func() {
					tfacc.Post(t, acct, "/v1/subscriptions/"+id, wire.Object{{Name: "description", Value: wire.String("changed")}})
				},
				Config: cfg,
				ConfigPlanChecks: resource.ConfigPlanChecks{PreApply: []plancheck.PlanCheck{
					plancheck.ExpectNonEmptyPlan(),
					plancheck.ExpectResourceAction(address, plancheck.ResourceActionUpdate),
				}},
				Check: resource.TestCheckResourceAttr(address, "description", "Team"),
			},
		},
	})
}

// freeConfig is a customer and the suite's subscription price, a free monthly one in `usd`,
// with the subscriptions in body, each created as a probe of TestSubscriptionAttributes
// created its own.
func freeConfig(c acctest.Config, body string) string {
	return c.ProviderBlock() + fmt.Sprintf(`
resource "stripe_customer" "test" {
  email = "subscription-free@example.com"
}

resource "stripe_product" "test" {
  name = %q
}

resource "stripe_price" "free" {
  product     = stripe_product.test.id
  currency    = "usd"
  unit_amount = 0
  recurring = {
    interval = "month"
  }
}
%s`, c.Name("Plan"), body)
}

// Stripe fills arguments a create leaves out, which the plan then leaves to it and the next
// plan keeps: the end of a trial `trial_period_days` asks for (suite: TestSubscriptionAttributes,
// probe `minimal/trial`), `save_default_payment_method` beside `payment_method_types`
// (`enums/100`), and the flexible billing mode's `proration_discounts` and a billing schedule's
// duration count (`enums/127`). Each subscription sends what its probe sent.
func TestAccSubscription_serverFilled(t *testing.T) {
	acct := acctest.NewAccount(t)
	config := freeConfig(acct, `
resource "stripe_subscription" "trial" {
  customer          = stripe_customer.test.id
  items             = [{ price = stripe_price.free.id }]
  trial_period_days = 7
}

resource "stripe_subscription" "link" {
  customer         = stripe_customer.test.id
  items            = [{ price = stripe_price.free.id }]
  payment_settings = { payment_method_types = ["link"] }
}

resource "stripe_subscription" "flexible" {
  customer          = stripe_customer.test.id
  items             = [{ price = stripe_price.free.id }]
  billing_mode      = { type = "flexible" }
  billing_schedules = [{ bill_until = { type = "duration", duration = { interval = "month" } } }]
}
`)
	resource.ParallelTest(t, resource.TestCase{
		ProtoV6ProviderFactories: tfacc.ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: config,
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttrSet("stripe_subscription.trial", "trial_end"),
					resource.TestCheckResourceAttr("stripe_subscription.link", "payment_settings.save_default_payment_method", "off"),
					resource.TestCheckResourceAttr("stripe_subscription.flexible", "billing_mode.flexible.proration_discounts", "included"),
					resource.TestCheckResourceAttr("stripe_subscription.flexible", "billing_schedules.0.bill_until.duration.interval_count", "1"),
				),
			},
			{Config: config, PlanOnly: true},
		},
	})
}

// Stripe refuses a discount the subscription does not hold, the one the suite could send on
// create, at the top level and on an item (suite: TestSubscriptionAttributes, probes
// `every_field/17` and `every_field/23`, `discounts.discount` with `di_attrs00000000`), and the
// apply says so with Stripe's message.
func TestAccSubscription_discountRefused(t *testing.T) {
	acct := acctest.NewAccount(t)
	resource.ParallelTest(t, resource.TestCase{
		ProtoV6ProviderFactories: tfacc.ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: freeConfig(acct, `
resource "stripe_subscription" "test" {
  customer  = stripe_customer.test.id
  items     = [{ price = stripe_price.free.id }]
  discounts = [{ discount = "di_attrs00000000" }]
}
`),
				ExpectError: regexp.MustCompile(`No such discount`),
			},
			{
				Config: freeConfig(acct, `
resource "stripe_subscription" "test" {
  customer = stripe_customer.test.id
  items    = [{ price = stripe_price.free.id, discounts = [{ discount = "di_attrs00000000" }] }]
}
`),
				ExpectError: regexp.MustCompile(`No such discount`),
			},
		},
	})
}

// Ending a trial with `trial_end = "now"` keeps the configured value in state, though Stripe
// answers with the time the trial ended (suite: TestSubscriptionAttributes, probe `update/24`: a
// subscription of the free price created with its trial ending a week ahead, then updated with
// `"now"`; the record proves the string on no create).
func TestAccSubscription_endTrialNow(t *testing.T) {
	acct := acctest.NewAccount(t)
	weekAhead := strconv.FormatInt(time.Now().Add(7*24*time.Hour).Unix(), 10)
	config := func(trialEnd string) string {
		return freeConfig(acct, fmt.Sprintf(`
resource "stripe_subscription" "test" {
  customer  = stripe_customer.test.id
  items     = [{ price = stripe_price.free.id }]
  trial_end = %q
}
`, trialEnd))
	}
	resource.ParallelTest(t, resource.TestCase{
		ProtoV6ProviderFactories: tfacc.ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: config(weekAhead),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr(address, "trial_end", weekAhead),
					resource.TestCheckResourceAttr(address, "status", "trialing"),
				),
			},
			{
				Config: config("now"),
				ConfigPlanChecks: resource.ConfigPlanChecks{PreApply: []plancheck.PlanCheck{
					plancheck.ExpectResourceAction(address, plancheck.ResourceActionUpdate),
				}},
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr(address, "trial_end", "now"),
					resource.TestCheckResourceAttr(address, "status", "active"),
				),
			},
			{Config: config("now"), PlanOnly: true},
		},
	})
}

// `cancel_at` takes the strings `max_period_end`, `min_period_end` and `max_billed_until`, which
// Stripe answers with the time it picks; the configured string stays in state (suite:
// TestSubscriptionAttributes, on subscriptions of the free price: the creates `enums/11`,
// `enums/29` and `every_field/14`; the updates `enums/128` and `enums/145` of subscriptions
// created without `cancel_at`, and `update/12` of one created to cancel in 20 days).
func TestAccSubscription_cancelAtStrings(t *testing.T) {
	acct := acctest.NewAccount(t)
	inTwentyDays := strconv.FormatInt(time.Now().Add(20*24*time.Hour).Unix(), 10)
	sub := func(name, cancelAt string) string {
		line := ""
		if cancelAt != "" {
			line = fmt.Sprintf("  cancel_at = %q\n", cancelAt)
		}
		return fmt.Sprintf(`
resource "stripe_subscription" %q {
  customer = stripe_customer.test.id
  items    = [{ price = stripe_price.free.id }]
%s}
`, name, line)
	}
	created := sub("max_period_end", "max_period_end") + sub("min_period_end", "min_period_end") + sub("max_billed_until", "max_billed_until")
	config := func(a, b, c string) string {
		return freeConfig(acct, created+sub("a", a)+sub("b", b)+sub("c", c))
	}
	updated := config("max_period_end", "min_period_end", "max_billed_until")
	resource.ParallelTest(t, resource.TestCase{
		ProtoV6ProviderFactories: tfacc.ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: config("", "", inTwentyDays),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("stripe_subscription.max_period_end", "cancel_at", "max_period_end"),
					resource.TestCheckResourceAttr("stripe_subscription.min_period_end", "cancel_at", "min_period_end"),
					resource.TestCheckResourceAttr("stripe_subscription.max_billed_until", "cancel_at", "max_billed_until"),
					resource.TestCheckNoResourceAttr("stripe_subscription.a", "cancel_at"),
					resource.TestCheckResourceAttr("stripe_subscription.c", "cancel_at", inTwentyDays),
				),
			},
			{
				Config: updated,
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("stripe_subscription.a", "cancel_at", "max_period_end"),
					resource.TestCheckResourceAttr("stripe_subscription.b", "cancel_at", "min_period_end"),
					resource.TestCheckResourceAttr("stripe_subscription.c", "cancel_at", "max_billed_until"),
				),
			},
			{Config: updated, PlanOnly: true},
		},
	})
}

// Stripe merges an item's `metadata` key by key (the record's `map` of the update's
// `items.metadata`), so an update that drops a key sends it as `""`, which removes it; sent
// whole, the key would stay and fail the apply. The keys are the suite's (`update/1`, `unset/1`).
func TestAccSubscription_itemMetadataKeyRemoved(t *testing.T) {
	acct := acctest.NewAccount(t)
	resource.ParallelTest(t, resource.TestCase{
		ProtoV6ProviderFactories: tfacc.ProviderFactories,
		ErrorCheck:               tfacc.SkipWhenFakeLacks(t, "items"),
		Steps: []resource.TestStep{
			{
				Config: subscriptionConfig(acct, `[{ price = stripe_price.one.id, metadata = { attrs_a = "one", attrs_b = "two" } }]`, ""),
				Check:  resource.TestCheckResourceAttr(address, "items.0.metadata.%", "2"),
			},
			{
				Config: subscriptionConfig(acct, `[{ price = stripe_price.one.id, metadata = { attrs_b = "two" } }]`, ""),
				ConfigPlanChecks: resource.ConfigPlanChecks{PreApply: []plancheck.PlanCheck{
					plancheck.ExpectResourceAction(address, plancheck.ResourceActionUpdate),
				}},
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr(address, "items.0.metadata.%", "1"),
					resource.TestCheckResourceAttr(address, "items.0.metadata.attrs_b", "two"),
				),
			},
		},
	})
}

// Stripe returns a create's `permissions` and an update's `account_subcategories` and `prefetch` of
// a US bank account's Financial Connections sorted (the record's `normalised` form `sorted`), so
// the configured order stays in state. The values are the suite's (probe `update/37`, its create
// and its update).
func TestAccSubscription_bankAccountListsKeepTheirOrder(t *testing.T) {
	acct := acctest.NewAccount(t)
	bank := func(subcategories, permissions, prefetch, verification string) string {
		return fmt.Sprintf(`  payment_settings = {
    payment_method_options = {
      us_bank_account = {
        financial_connections = {
          filters     = { account_subcategories = %s }
          permissions = %s
          prefetch    = %s
        }
        verification_method = %q
      }
    }
  }
`, subcategories, permissions, prefetch, verification)
	}
	updated := subscriptionConfig(acct, oneItem, bank(`["savings", "checking"]`, `["balances", "payment_method"]`, `["ownership", "balances"]`, "instant"))
	resource.ParallelTest(t, resource.TestCase{
		ProtoV6ProviderFactories: tfacc.ProviderFactories,
		ErrorCheck:               tfacc.SkipWhenFakeLacks(t, "payment_settings"),
		Steps: []resource.TestStep{
			{Config: subscriptionConfig(acct, oneItem, bank(`["checking", "savings"]`, `["payment_method", "balances"]`, `["balances", "ownership"]`, "automatic"))},
			{
				Config: updated,
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr(address, "payment_settings.payment_method_options.us_bank_account.financial_connections.prefetch.0", "ownership"),
					resource.TestCheckResourceAttr(address, "payment_settings.payment_method_options.us_bank_account.financial_connections.filters.account_subcategories.0", "savings"),
				),
			},
			{Config: updated, PlanOnly: true},
		},
	})
}
