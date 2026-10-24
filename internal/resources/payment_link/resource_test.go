package payment_link_test

import (
	"fmt"
	"os"
	"regexp"
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

const address = "stripe_payment_link.test"

// linkConfig is a product with a 2000 cent and a 1000 cent price and a payment link. items
// and extra land inside the link.
func linkConfig(c acctest.Config, items string, extra string) string {
	return c.ProviderBlock() + fmt.Sprintf(`
resource "stripe_product" "test" {
  name = %q
}

resource "stripe_price" "a" {
  product     = stripe_product.test.id
  currency    = "usd"
  unit_amount = 2000
}

resource "stripe_price" "b" {
  product     = stripe_product.test.id
  currency    = "usd"
  unit_amount = 1000
}

resource "stripe_payment_link" "test" {
  line_items = %s
%s}
`, c.Name("Course"), items, extra)
}

const oneItem = `[{ price = stripe_price.a.id, quantity = 2 }]`

func idOf(dst *string) resource.TestCheckFunc {
	return resource.TestCheckResourceAttrWith(address, "id", func(v string) error {
		*dst = v
		return nil
	})
}

// updatesOf counts the updates the provider has sent to the payment link. The count is the
// fake's, so in smoke mode it is always 0 and the plan-only step after a reorder is the check.
func updatesOf(t *testing.T, acct acctest.Config, id string) int {
	t.Helper()
	if acct.Smoke {
		return 0
	}
	n := 0
	for _, r := range acctest.RequestLog(t, acct) {
		if r.Method == "POST" && r.Path == "/v1/payment_links/"+id {
			n++
		}
	}
	return n
}

// A line item names its price: `price_data` has no price to match the item by, so the
// schema does not offer it.
func TestAccPaymentLink_priceDataIsNotOffered(t *testing.T) {
	acct := acctest.NewAccount(t)
	resource.ParallelTest(t, resource.TestCase{
		ProtoV6ProviderFactories: tfacc.ProviderFactories,
		Steps: []resource.TestStep{{
			Config: linkConfig(acct, `[{
    quantity = 1
    price_data = { currency = "usd", product = stripe_product.test.id, unit_amount = 500 }
  }]`, ""),
			PlanOnly:    true,
			ExpectError: regexp.MustCompile(`price_data`),
		}},
	})
}

func TestAccPaymentLink_create(t *testing.T) {
	acct := acctest.NewAccount(t)
	resource.ParallelTest(t, resource.TestCase{
		ProtoV6ProviderFactories: tfacc.ProviderFactories,
		Steps: []resource.TestStep{{
			Config: linkConfig(acct, `[
    { price = stripe_price.a.id, quantity = 2 },
    { price = stripe_price.b.id, quantity = 3 },
  ]`, `  allow_promotion_codes = true
  billing_address_collection = "required"
  submit_type = "donate"
  metadata = { env = "test" }
  after_completion = {
    type = "redirect"
    redirect = {
      url = "https://example.com/done"
    }
  }
`) + `
data "stripe_payment_link" "by_id" {
  id = stripe_payment_link.test.id
}
`,
			Check: resource.ComposeAggregateTestCheckFunc(
				resource.TestCheckResourceAttrSet(address, "id"),
				// Stripe serves a donation link from donate.stripe.com.
				resource.TestMatchResourceAttr(address, "url", regexp.MustCompile(`^https://(buy|donate)\.stripe\.com/`)),
				resource.TestCheckResourceAttr(address, "active", "true"),
				resource.TestCheckResourceAttr(address, "currency", "usd"),
				resource.TestCheckResourceAttr(address, "allow_promotion_codes", "true"),
				resource.TestCheckResourceAttr(address, "billing_address_collection", "required"),
				resource.TestCheckResourceAttr(address, "submit_type", "donate"),
				resource.TestCheckResourceAttr(address, "customer_creation", "if_required"),
				resource.TestCheckResourceAttr(address, "metadata.env", "test"),
				resource.TestCheckResourceAttr(address, "after_completion.type", "redirect"),
				resource.TestCheckResourceAttr(address, "after_completion.redirect.url", "https://example.com/done"),
				resource.TestCheckResourceAttr(address, "line_items.#", "2"),
				resource.TestCheckResourceAttrPair(address, "line_items.0.price", "stripe_price.a", "id"),
				resource.TestCheckResourceAttr(address, "line_items.0.quantity", "2"),
				resource.TestCheckResourceAttr(address, "line_items.0.amount_total", "4000"),
				resource.TestCheckResourceAttrPair(address, "line_items.1.price", "stripe_price.b", "id"),
				resource.TestCheckResourceAttr(address, "line_items.1.amount_total", "3000"),
				resource.TestCheckResourceAttrPair("data.stripe_payment_link.by_id", "url", address, "url"),
				resource.TestCheckResourceAttr("data.stripe_payment_link.by_id", "line_items.#", "2"),
			),
		}},
	})
}

// A changed quantity is sent as `line_items[n][id]` plus the quantity, with every item named
// (suite: "update line item quantity"); a reorder sends nothing; the same price twice is
// refused at plan time.
func TestAccPaymentLink_update(t *testing.T) {
	acct := acctest.NewAccount(t)
	two := `[
    { price = stripe_price.a.id, quantity = 2 },
    { price = stripe_price.b.id, quantity = 3 },
  ]`
	var id string
	var sentBefore int
	resource.ParallelTest(t, resource.TestCase{
		ProtoV6ProviderFactories: tfacc.ProviderFactories,
		Steps: []resource.TestStep{
			{Config: linkConfig(acct, two, "  metadata = { keep = \"me\", drop = \"me\" }\n"), Check: idOf(&id)},
			{
				Config: linkConfig(acct, `[
    { price = stripe_price.a.id, quantity = 2 },
    { price = stripe_price.b.id, quantity = 7 },
  ]`, "  allow_promotion_codes = true\n  billing_address_collection = \"required\"\n  submit_type = \"pay\"\n  metadata = { keep = \"me\", new = \"v\" }\n"),
				ConfigPlanChecks: resource.ConfigPlanChecks{PreApply: []plancheck.PlanCheck{
					plancheck.ExpectResourceAction(address, plancheck.ResourceActionUpdate),
				}},
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttrWith(address, "id", func(v string) error {
						if v != id {
							return fmt.Errorf("the update replaced the link %s with %s", id, v)
						}
						return nil
					}),
					resource.TestCheckResourceAttr(address, "allow_promotion_codes", "true"),
					resource.TestCheckResourceAttr(address, "billing_address_collection", "required"),
					resource.TestCheckResourceAttr(address, "submit_type", "pay"),
					resource.TestCheckResourceAttr(address, "metadata.%", "2"),
					resource.TestCheckResourceAttr(address, "metadata.new", "v"),
					resource.TestCheckResourceAttr(address, "line_items.1.quantity", "7"),
					resource.TestCheckResourceAttr(address, "line_items.1.amount_total", "7000"),
					resource.TestCheckResourceAttr(address, "line_items.0.quantity", "2"),
				),
			},
			{
				// Listing the prices in the other order is not a change.
				Config: linkConfig(acct, `[
    { price = stripe_price.b.id, quantity = 7 },
    { price = stripe_price.a.id, quantity = 2 },
  ]`, "  allow_promotion_codes = true\n  billing_address_collection = \"required\"\n  submit_type = \"pay\"\n  metadata = { keep = \"me\", new = \"v\" }\n"),
				PreConfig: func() { sentBefore = updatesOf(t, acct, id) },
				Check: func(*terraform.State) error {
					if got := updatesOf(t, acct, id); got != sentBefore {
						return fmt.Errorf("a reorder sent %d update(s) to the payment link", got-sentBefore)
					}
					return nil
				},
			},
			{
				Config: linkConfig(acct, `[
    { price = stripe_price.b.id, quantity = 7 },
    { price = stripe_price.a.id, quantity = 2 },
  ]`, "  allow_promotion_codes = true\n  billing_address_collection = \"required\"\n  submit_type = \"pay\"\n  metadata = { keep = \"me\", new = \"v\" }\n"),
				PlanOnly: true,
			},
			{
				Config: linkConfig(acct, `[
    { price = stripe_price.a.id, quantity = 2 },
    { price = stripe_price.a.id, quantity = 3 },
  ]`, ""),
				PlanOnly:    true,
				ExpectError: regexp.MustCompile(`more than once`),
			},
		},
	})
}

// Adding, removing or repricing a line item is not proven (suite: "update line item
// quantity" is the only line item update), so each replaces the link.
func TestAccPaymentLink_replace(t *testing.T) {
	acct := acctest.NewAccount(t)
	var first string
	replaced := func(step string) resource.TestCheckFunc {
		return resource.TestCheckResourceAttrWith(address, "id", func(v string) error {
			if v == first {
				return fmt.Errorf("%s kept the payment link %s", step, v)
			}
			first = v
			return nil
		})
	}
	both := `[
    { price = stripe_price.a.id, quantity = 2 },
    { price = stripe_price.b.id, quantity = 3 },
  ]`
	replace := resource.ConfigPlanChecks{PreApply: []plancheck.PlanCheck{plancheck.ExpectResourceAction(address, plancheck.ResourceActionReplace)}}
	resource.ParallelTest(t, resource.TestCase{
		ProtoV6ProviderFactories: tfacc.ProviderFactories,
		Steps: []resource.TestStep{
			{Config: linkConfig(acct, oneItem, ""), Check: idOf(&first)},
			{Config: linkConfig(acct, both, ""), ConfigPlanChecks: replace, Check: replaced("adding a price")},
			{Config: linkConfig(acct, `[{ price = stripe_price.b.id, quantity = 3 }]`, ""), ConfigPlanChecks: replace, Check: replaced("dropping a price")},
			{Config: linkConfig(acct, `[{ price = stripe_price.a.id, quantity = 3 }]`, ""), ConfigPlanChecks: replace, Check: replaced("repricing")},
			{Config: linkConfig(acct, `[{ price = stripe_price.a.id, quantity = 3 }]`, "  after_completion = { type = \"redirect\", redirect = { url = \"https://example.com/done\" } }\n"), ConfigPlanChecks: replace, Check: replaced("an update the suite does not prove")},
		},
	})
}

func TestAccPaymentLink_import(t *testing.T) {
	acct := acctest.NewAccount(t)
	cfg := linkConfig(acct, oneItem, "  allow_promotion_codes = true\n  metadata = { team = \"a\" }\n")
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

// Stripe cannot delete a payment link (suite: "delete is not supported"), so destroy sets
// `active` to false (suite: "deactivate and reactivate") and the link stays in the account.
func TestAccPaymentLink_delete(t *testing.T) {
	acct := acctest.NewAccount(t)
	var id string
	resource.ParallelTest(t, resource.TestCase{
		ProtoV6ProviderFactories: tfacc.ProviderFactories,
		CheckDestroy: func(*terraform.State) error {
			resp, err := tfacc.Client(acct).Do(t.Context(), stripeapi.Request{Method: "GET", Path: "/v1/payment_links/" + id})
			if err != nil {
				return fmt.Errorf("a deactivated payment link still retrieves: %w", err)
			}
			if active, _ := resp.Body.(wire.Object).GetBool("active"); active {
				return fmt.Errorf("payment link %s is still active after destroy", id)
			}
			return nil
		},
		Steps: []resource.TestStep{{Config: linkConfig(acct, oneItem, ""), Check: idOf(&id)}},
	})
}

func TestAccPaymentLink_drift(t *testing.T) {
	acct := acctest.NewAccount(t)
	var id string
	cfg := linkConfig(acct, oneItem, "  active = true\n  allow_promotion_codes = true\n")
	resource.ParallelTest(t, resource.TestCase{
		ProtoV6ProviderFactories: tfacc.ProviderFactories,
		Steps: []resource.TestStep{
			{Config: cfg, Check: idOf(&id)},
			{
				PreConfig: func() {
					tfacc.Post(t, acct, "/v1/payment_links/"+id, wire.Object{
						{Name: "allow_promotion_codes", Value: wire.Bool(false)},
						{Name: "active", Value: wire.Bool(false)},
					})
				},
				Config: cfg,
				ConfigPlanChecks: resource.ConfigPlanChecks{PreApply: []plancheck.PlanCheck{
					plancheck.ExpectNonEmptyPlan(),
					plancheck.ExpectResourceAction(address, plancheck.ResourceActionUpdate),
				}},
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr(address, "allow_promotion_codes", "true"),
					resource.TestCheckResourceAttr(address, "active", "true"),
				),
			},
		},
	})
}

// An update restates the whole list, so a line item's `adjustable_quantity` must survive a
// quantity change. The suite does not exercise `adjustable_quantity` and the fake answers
// `fake_not_implemented` for it, so the test skips until the fake serves it.
func TestAccPaymentLink_adjustableQuantitySurvivesAQuantityChange(t *testing.T) {
	acct := acctest.NewAccount(t)
	cfg := func(quantity int) string {
		return linkConfig(acct, fmt.Sprintf(`[
    { price = stripe_price.a.id, quantity = %d, adjustable_quantity = { enabled = true, minimum = 1, maximum = 10 } },
    { price = stripe_price.b.id, quantity = 3 },
  ]`, quantity), "")
	}
	survives := resource.ComposeAggregateTestCheckFunc(
		resource.TestCheckResourceAttr(address, "line_items.0.adjustable_quantity.enabled", "true"),
		resource.TestCheckResourceAttr(address, "line_items.0.adjustable_quantity.maximum", "10"),
	)
	var id string
	resource.ParallelTest(t, resource.TestCase{
		ProtoV6ProviderFactories: tfacc.ProviderFactories,
		Steps: []resource.TestStep{
			{Config: cfg(2), Check: resource.ComposeAggregateTestCheckFunc(idOf(&id), survives)},
			{
				Config: cfg(5),
				ConfigPlanChecks: resource.ConfigPlanChecks{PreApply: []plancheck.PlanCheck{
					plancheck.ExpectResourceAction(address, plancheck.ResourceActionUpdate),
				}},
				Check: resource.ComposeAggregateTestCheckFunc(
					survives,
					resource.TestCheckResourceAttr(address, "line_items.0.quantity", "5"),
					func(*terraform.State) error {
						resp, err := tfacc.Client(acct).Do(t.Context(), stripeapi.Request{Method: "GET", Path: "/v1/payment_links/" + id + "/line_items"})
						if err != nil {
							return err
						}
						data, _ := resp.Body.(wire.Object).Get("data")
						items, _ := data.(wire.Array)
						for _, it := range items {
							if aq, _ := it.(wire.Object).GetObject("adjustable_quantity"); len(aq) > 0 {
								return nil
							}
						}
						return fmt.Errorf("no line item of %s still has adjustable_quantity in Stripe", id)
					},
				),
			},
		},
	})
}
