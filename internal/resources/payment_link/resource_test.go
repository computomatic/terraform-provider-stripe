package payment_link_test

import (
	"fmt"
	"os"
	"regexp"
	"strings"
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

// An item that sets `price_data` has the price Stripe makes for it, is matched by its data, and
// keeps its item when only the quantity changes; changing the data replaces the link (suite
// variants `inline_product` and `decimal_price`).
func TestAccPaymentLink_priceData(t *testing.T) {
	acct := acctest.NewAccount(t)
	item := func(quantity int, productData string) string {
		return fmt.Sprintf(`[{
    quantity = %d
    price_data = { currency = "usd", unit_amount = 5000, product_data = { name = %q%s } }
  }]`, quantity, acct.Name("Inline"), productData)
	}
	var first, price string
	resource.ParallelTest(t, resource.TestCase{
		ProtoV6ProviderFactories: tfacc.ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: linkConfig(acct, item(2, ""), ""),
				Check: resource.ComposeAggregateTestCheckFunc(
					idOf(&first),
					resource.TestCheckResourceAttr(address, "line_items.0.amount_total", "10000"),
					resource.TestCheckResourceAttrWith(address, "line_items.0.price", func(v string) error {
						if !strings.HasPrefix(v, "price_") {
							return fmt.Errorf("line item has price %q, want one Stripe made", v)
						}
						price = v
						return nil
					}),
				),
			},
			{Config: linkConfig(acct, item(2, ""), ""), PlanOnly: true},
			{
				Config: linkConfig(acct, item(3, ""), ""),
				ConfigPlanChecks: resource.ConfigPlanChecks{PreApply: []plancheck.PlanCheck{
					plancheck.ExpectResourceAction(address, plancheck.ResourceActionUpdate),
				}},
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr(address, "line_items.0.quantity", "3"),
					resource.TestCheckResourceAttrWith(address, "line_items.0.price", func(v string) error {
						if v != price {
							return fmt.Errorf("a quantity change moved the item from price %s to %s", price, v)
						}
						return nil
					}),
				),
			},
			{
				Config: linkConfig(acct, item(3, `, tax_code = "txcd_10000000"`), ""),
				ConfigPlanChecks: resource.ConfigPlanChecks{PreApply: []plancheck.PlanCheck{
					plancheck.ExpectResourceAction(address, plancheck.ResourceActionReplace),
				}},
				Check: resource.TestCheckResourceAttrWith(address, "id", func(v string) error {
					if v == first {
						return fmt.Errorf("changed price_data kept the link %s", v)
					}
					return nil
				}),
			},
		},
	})
}

// A value not known at plan may yet resolve to an item that names its price one way, so the plan
// passes and the plan Terraform makes at apply decides: `terraform_data`'s `id` is known only then.
func TestAccPaymentLink_itemKnownAtApply(t *testing.T) {
	const (
		data    = `price_data = { currency = "usd", unit_amount = 5000, product_data = { name = %q } }`
		toNull  = `terraform_data.late.id != "" ? null : stripe_price.a.id`
		toPrice = `terraform_data.late.id != "" ? stripe_price.a.id : null`
	)
	for name, tc := range map[string]struct {
		item    string
		refused bool
	}{
		"an unknown price beside data": {item: "price = " + toNull + ", " + data},
		"an unknown price alone":       {item: "price = " + toPrice},
		"both, known at apply":         {item: "price = " + toPrice + ", " + data, refused: true},
		"neither, known at apply":      {item: "price = " + toNull, refused: true},
	} {
		t.Run(name, func(t *testing.T) {
			acct := acctest.NewAccount(t)
			item := tc.item
			if strings.Contains(item, "%q") {
				item = fmt.Sprintf(item, acct.Name("Late"))
			}
			cfg := linkConfig(acct, "[{ quantity = 2, "+item+" }]", "") + lateResource
			steps := []resource.TestStep{{Config: cfg}}
			if tc.refused {
				steps[0].ExpectError = regexp.MustCompile("sets exactly one")
			} else {
				steps[0].Check = resource.TestCheckResourceAttr(address, "line_items.0.quantity", "2")
				steps = append(steps, resource.TestStep{Config: cfg, PlanOnly: true})
			}
			resource.ParallelTest(t, resource.TestCase{ProtoV6ProviderFactories: tfacc.ProviderFactories, Steps: steps})
		})
	}
}

// lateResource is known only at apply.
const lateResource = `
resource "terraform_data" "late" {
  input = "late"
}
`

// An element unknown as a whole never reaches the provider's plan: the framework's validation
// reads its `quantity`, which a link requires, as missing. A subscription's items require nothing,
// so TestAccSubscription_itemKnownAtApply plans such an element.
func TestAccPaymentLink_unknownElementFailsValidation(t *testing.T) {
	acct := acctest.NewAccount(t)
	resource.ParallelTest(t, resource.TestCase{
		ProtoV6ProviderFactories: tfacc.ProviderFactories,
		Steps: []resource.TestStep{{
			Config: linkConfig(acct, `[terraform_data.item.output]`, "") + `
resource "terraform_data" "item" {
  input = { price = stripe_price.a.id, quantity = 2 }
}
`,
			ExpectError: regexp.MustCompile("Missing Configuration for Required Attribute"),
		}},
	})
}

// An item names its price one way only: Stripe refuses both, and neither. These IDs are literals
// because a value still unknown waits for apply (TestAccPaymentLink_itemKnownAtApply), and the
// refusal comes before anything reaches Stripe.
func TestAccPaymentLink_itemNamesOnePrice(t *testing.T) {
	acct := acctest.NewAccount(t)
	for name, items := range map[string]string{
		"both": `[{
    quantity   = 1
    price      = "price_known"
    price_data = { currency = "usd", product = "prod_known", unit_amount = 5000 }
  }]`,
		"neither": `[{ quantity = 1 }]`,
	} {
		t.Run(name, func(t *testing.T) {
			resource.ParallelTest(t, resource.TestCase{
				ProtoV6ProviderFactories: tfacc.ProviderFactories,
				Steps: []resource.TestStep{{
					Config:      linkConfig(acct, items, ""),
					PlanOnly:    true,
					ExpectError: regexp.MustCompile("sets exactly one"),
				}},
			})
		})
	}
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
// quantity" is the only line item update), so each replaces the link, and so does removing a hash
// Stripe cannot unset (`cannot be unset`, or no record of an unset).
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
			{Config: linkConfig(acct, `[{ price = stripe_price.a.id, quantity = 3 }]`, "  shipping_address_collection = { allowed_countries = [\"US\"] }\n"), Check: idOf(&first)},
			{Config: linkConfig(acct, `[{ price = stripe_price.a.id, quantity = 3 }]`, ""), ConfigPlanChecks: replace, Check: replaced("removing a hash Stripe cannot unset")},
		},
	})
}

// inPlace applies before and then after, each inside the link, and checks that the change is an
// update of the same link that the next plan leaves alone.
func inPlace(t *testing.T, acct acctest.Config, config func(extra string) string, before, after string, checks ...resource.TestCheckFunc) {
	t.Helper()
	var id string
	same := resource.TestCheckResourceAttrWith(address, "id", func(v string) error {
		if v != id {
			return fmt.Errorf("the update replaced the link %s with %s", id, v)
		}
		return nil
	})
	resource.ParallelTest(t, resource.TestCase{
		ProtoV6ProviderFactories: tfacc.ProviderFactories,
		Steps: []resource.TestStep{
			{Config: config(before), Check: idOf(&id)},
			{
				Config: config(after),
				ConfigPlanChecks: resource.ConfigPlanChecks{PreApply: []plancheck.PlanCheck{
					plancheck.ExpectResourceAction(address, plancheck.ResourceActionUpdate),
				}},
				Check: resource.ComposeAggregateTestCheckFunc(append(checks, same)...),
			},
			{Config: config(after), PlanOnly: true},
		},
	})
}

// A monthly price Stripe makes with the link, which a subscription link needs: a recurring
// `price` is not proven (suite variant `subscription`).
const monthlyItem = `[{
    quantity = 1
    price_data = { currency = "usd", product = stripe_product.test.id, unit_amount = 5000, recurring = { interval = "month" } }
  }]`

// taxCodedItem is a tax-coded item, which automatic tax needs (suite variant `tax_coded`).
func taxCodedItem(acct acctest.Config) string {
	return fmt.Sprintf(`[{
    quantity = 2
    price_data = { currency = "usd", unit_amount = 5000, product_data = { name = %q, tax_code = "txcd_10000000" } }
  }]`, acct.Name("Coded"))
}

// What the conformance suite proves an update of changes in place, each in the context its probe
// sent (TestPaymentLinkAttributes, the recipe's `Updates` and `Exclusive`). A member removed from
// a hash Stripe keeps goes as null, and a hash Stripe replaces leaves it out.
func TestAccPaymentLink_updatesInPlace(t *testing.T) {
	for _, tc := range []struct {
		name          string
		items         func(acctest.Config) string
		before, after string
		check         []resource.TestCheckFunc
	}{
		{name: "after_completion", before: "  after_completion = { type = \"redirect\", redirect = { url = \"https://example.com/done\" } }\n",
			after: "  after_completion = { type = \"hosted_confirmation\", hosted_confirmation = { custom_message = \"attrs\" } }\n",
			check: []resource.TestCheckFunc{
				resource.TestCheckResourceAttr(address, "after_completion.type", "hosted_confirmation"),
				resource.TestCheckResourceAttr(address, "after_completion.hosted_confirmation.custom_message", "attrs"),
				resource.TestCheckNoResourceAttr(address, "after_completion.redirect.url"),
			}},
		{name: "automatic_tax", items: taxCodedItem, after: "  automatic_tax = { enabled = true }\n",
			check: []resource.TestCheckFunc{resource.TestCheckResourceAttr(address, "automatic_tax.enabled", "true")}},
		{name: "consent_collection", before: "  consent_collection = { terms_of_service = \"none\" }\n",
			after: "  consent_collection = {\n    terms_of_service = \"none\"\n    payment_method_reuse_agreement = { position = \"hidden\" }\n  }\n",
			check: []resource.TestCheckFunc{resource.TestCheckResourceAttr(address, "consent_collection.payment_method_reuse_agreement.position", "hidden")}},
		{name: "custom_fields", after: "  custom_fields = [{\n    key = \"note\"\n    type = \"text\"\n    label = { type = \"custom\", custom = \"Note\" }\n    text = { default_value = \"attrs\" }\n  }]\n",
			check: []resource.TestCheckFunc{resource.TestCheckResourceAttr(address, "custom_fields.0.text.default_value", "attrs")}},
		{name: "custom_text", after: "  custom_text = { submit = { message = \"attrs\" } }\n",
			check: []resource.TestCheckFunc{resource.TestCheckResourceAttr(address, "custom_text.submit.message", "attrs")}},
		{name: "customer_creation", after: "  customer_creation = \"always\"\n",
			check: []resource.TestCheckFunc{resource.TestCheckResourceAttr(address, "customer_creation", "always")}},
		{name: "inactive_message", before: "  inactive_message = \"attrs\"\n",
			check: []resource.TestCheckFunc{resource.TestCheckNoResourceAttr(address, "inactive_message")}},
		{name: "invoice_creation", before: "  invoice_creation = {\n    enabled = true\n    invoice_data = { description = \"attrs\", footer = \"attrs\", metadata = { a = \"b\" } }\n  }\n",
			after: "  invoice_creation = {\n    enabled = true\n    invoice_data = { description = \"other\", metadata = { c = \"d\" } }\n  }\n",
			check: []resource.TestCheckFunc{
				resource.TestCheckResourceAttr(address, "invoice_creation.invoice_data.description", "other"),
				resource.TestCheckNoResourceAttr(address, "invoice_creation.invoice_data.footer"),
				resource.TestCheckResourceAttr(address, "invoice_creation.invoice_data.metadata.%", "1"),
				resource.TestCheckResourceAttr(address, "invoice_creation.invoice_data.metadata.c", "d"),
			}},
		{name: "name_collection", after: "  name_collection = { business = { enabled = true } }\n",
			check: []resource.TestCheckFunc{resource.TestCheckResourceAttr(address, "name_collection.business.enabled", "true")}},
		{name: "payment_intent_data", before: "  payment_intent_data = { description = \"attrs\", transfer_group = \"attrs\", metadata = { a = \"b\" } }\n",
			after: "  payment_intent_data = { statement_descriptor = \"ATTRS PROBE\", statement_descriptor_suffix = \"ATTRS\", transfer_group = \"attrs\", metadata = { c = \"d\" } }\n",
			check: []resource.TestCheckFunc{
				resource.TestCheckNoResourceAttr(address, "payment_intent_data.description"),
				resource.TestCheckResourceAttr(address, "payment_intent_data.statement_descriptor", "ATTRS PROBE"),
				resource.TestCheckResourceAttr(address, "payment_intent_data.metadata.%", "1"),
				resource.TestCheckResourceAttr(address, "payment_intent_data.metadata.c", "d"),
			}},
		// Stripe takes it only on a link with a recurring price (dev sandbox), beside the
		// subscription's data (suite: `update/9`).
		{name: "payment_method_collection", items: func(acctest.Config) string { return monthlyItem },
			before: "  payment_method_collection = \"always\"\n  subscription_data = { trial_period_days = 7 }\n",
			after:  "  payment_method_collection = \"if_required\"\n  subscription_data = { trial_period_days = 7 }\n",
			check:  []resource.TestCheckFunc{resource.TestCheckResourceAttr(address, "payment_method_collection", "if_required")}},
		{name: "payment_method_options", after: "  payment_method_options = { card = { restrictions = { brands_blocked = [\"american_express\"] } } }\n",
			check: []resource.TestCheckFunc{resource.TestCheckResourceAttr(address, "payment_method_options.card.restrictions.brands_blocked.0", "american_express")}},
		{name: "payment_method_types", before: "  payment_method_types = [\"card\"]\n", after: "  payment_method_types = [\"card\", \"us_bank_account\"]\n",
			check: []resource.TestCheckFunc{resource.TestCheckResourceAttr(address, "payment_method_types.#", "2")}},
		{name: "phone_number_collection", after: "  phone_number_collection = { enabled = true }\n",
			check: []resource.TestCheckFunc{resource.TestCheckResourceAttr(address, "phone_number_collection.enabled", "true")}},
		{name: "restrictions", after: "  restrictions = { completed_sessions = { limit = 7 } }\n",
			check: []resource.TestCheckFunc{resource.TestCheckResourceAttr(address, "restrictions.completed_sessions.limit", "7")}},
		{name: "shipping_address_collection", before: "  shipping_address_collection = { allowed_countries = [\"US\"] }\n",
			after: "  shipping_address_collection = { allowed_countries = [\"US\", \"CA\"] }\n",
			check: []resource.TestCheckFunc{resource.TestCheckResourceAttr(address, "shipping_address_collection.allowed_countries.#", "2")}},
		{name: "subscription_data", items: func(acctest.Config) string { return monthlyItem }, before: "  payment_method_collection = \"always\"\n  subscription_data = { trial_period_days = 7, metadata = { a = \"b\" } }\n",
			after: "  payment_method_collection = \"always\"\n  subscription_data = {\n    metadata = { c = \"d\" }\n    trial_settings = { end_behavior = { missing_payment_method = \"cancel\" } }\n  }\n",
			check: []resource.TestCheckFunc{
				resource.TestCheckNoResourceAttr(address, "subscription_data.trial_period_days"),
				resource.TestCheckResourceAttr(address, "subscription_data.metadata.%", "1"),
				resource.TestCheckResourceAttr(address, "subscription_data.trial_settings.end_behavior.missing_payment_method", "cancel"),
			}},
		{name: "tax_id_collection", after: "  tax_id_collection = { enabled = true, required = \"if_supported\" }\n",
			check: []resource.TestCheckFunc{resource.TestCheckResourceAttr(address, "tax_id_collection.required", "if_supported")}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			acct := acctest.NewAccount(t)
			items := oneItem
			if tc.items != nil {
				items = tc.items(acct)
			}
			inPlace(t, acct, func(extra string) string { return linkConfig(acct, items, extra) }, tc.before, tc.after, tc.check...)
		})
	}
}

// Stripe fills the rest of a hash a create sends in part, and the docs say what it holds. Each
// goes beside the line item the suite sent it with: a price of its own (`nested_minimal/1`), and an
// inline price for an optional item, which must be another price (`nested_minimal/11`).
func TestAccPaymentLink_hashesStripeFills(t *testing.T) {
	inline := `[{ quantity = 2, price_data = { currency = "usd", product = stripe_product.test.id, unit_amount = 5000 } }]`
	for name, tc := range map[string]struct {
		items, extra string
		check        resource.TestCheckFunc
	}{
		"consent and name": {oneItem, `  consent_collection = { payment_method_reuse_agreement = { position = "auto" } }
  name_collection    = { business = { enabled = true }, individual = { enabled = true } }
`, resource.ComposeAggregateTestCheckFunc(
			resource.TestCheckResourceAttr(address, "consent_collection.promotions", "none"),
			resource.TestCheckResourceAttr(address, "consent_collection.terms_of_service", "none"),
			resource.TestCheckResourceAttr(address, "name_collection.business.optional", "false"),
		)},
		"optional items": {inline, `  optional_items = [{ price = stripe_price.b.id, quantity = 1, adjustable_quantity = { enabled = true } }]
`, resource.ComposeAggregateTestCheckFunc(
			resource.TestCheckResourceAttr(address, "optional_items.0.adjustable_quantity.maximum", "99"),
			resource.TestCheckResourceAttr(address, "optional_items.0.adjustable_quantity.minimum", "0"),
		)},
	} {
		t.Run(name, func(t *testing.T) {
			acct := acctest.NewAccount(t)
			cfg := linkConfig(acct, tc.items, tc.extra)
			resource.ParallelTest(t, resource.TestCase{
				ProtoV6ProviderFactories: tfacc.ProviderFactories,
				Steps: []resource.TestStep{
					{Config: cfg, Check: tc.check},
					{Config: cfg, PlanOnly: true},
				},
			})
		})
	}
}

// Shipping options name a rate of the account's (suite: `shipping_options.shipping_rate`, sent on
// its own).
func TestAccPaymentLink_shippingOptionsInPlace(t *testing.T) {
	acct := acctest.NewAccount(t)
	config := func(extra string) string {
		return linkConfig(acct, oneItem, extra) + fmt.Sprintf(`
resource "stripe_shipping_rate" "ground" {
  display_name = %q
  type         = "fixed_amount"
  fixed_amount = { amount = 500, currency = "usd" }
}
`, acct.Name("Ground"))
	}
	inPlace(t, acct, config, "", "  shipping_options = [{ shipping_rate = stripe_shipping_rate.ground.id }]\n",
		resource.TestCheckResourceAttrPair(address, "shipping_options.0.shipping_rate", "stripe_shipping_rate.ground", "id"))
}

// The fee and the transfer, each in the variant that sends it: Stripe wants a destination beside a
// fee, and refuses a fee beside a transfer amount (suite variants `destination`, `transfer_amount`
// and `subscription_fee`).
func TestAccPaymentLink_connectUpdates(t *testing.T) {
	t.Run("application_fee_amount", func(t *testing.T) {
		acct := acctest.NewAccount(t)
		account := tfacc.TransfersAccount(t, acct)
		destination := fmt.Sprintf("  transfer_data = { destination = %q }\n", account)
		inPlace(t, acct, func(extra string) string { return linkConfig(acct, oneItem, destination+extra) }, "", "  application_fee_amount = 100\n",
			resource.TestCheckResourceAttr(address, "application_fee_amount", "100"))
	})
	t.Run("transfer_data.amount", func(t *testing.T) {
		acct := acctest.NewAccount(t)
		account := tfacc.TransfersAccount(t, acct)
		inPlace(t, acct, func(extra string) string { return linkConfig(acct, oneItem, extra) },
			fmt.Sprintf("  transfer_data = { amount = 100, destination = %q }\n", account), fmt.Sprintf("  transfer_data = { destination = %q }\n", account),
			resource.TestCheckNoResourceAttr(address, "transfer_data.amount"))
	})
	t.Run("application_fee_percent", func(t *testing.T) {
		acct := acctest.NewAccount(t)
		account := tfacc.TransfersAccount(t, acct)
		destination := fmt.Sprintf("  transfer_data = { destination = %q }\n", account)
		inPlace(t, acct, func(extra string) string { return linkConfig(acct, monthlyItem, destination+extra) }, "  application_fee_percent = 5.5\n", "  application_fee_percent = 6.5\n",
			resource.TestCheckResourceAttr(address, "application_fee_percent", "6.5"))
	})
	t.Run("on_behalf_of", func(t *testing.T) {
		acct := acctest.NewAccount(t)
		account := tfacc.TransfersAccount(t, acct)
		fee := fmt.Sprintf("  application_fee_amount = 100\n  transfer_data = { destination = %q }\n", account)
		inPlace(t, acct, func(extra string) string { return linkConfig(acct, oneItem, fee+extra) }, "", fmt.Sprintf("  on_behalf_of = %q\n", account),
			resource.TestCheckResourceAttr(address, "on_behalf_of", account))
	})
	t.Run("issuer", func(t *testing.T) {
		acct := acctest.NewAccount(t)
		account := tfacc.TransfersAccount(t, acct)
		inPlace(t, acct, func(extra string) string {
			return linkConfig(acct, monthlyItem, "  payment_method_collection = \"always\"\n"+extra)
		}, "",
			fmt.Sprintf("  subscription_data = { invoice_settings = { issuer = { type = \"account\", account = %q } } }\n", account),
			resource.TestCheckResourceAttr(address, "subscription_data.invoice_settings.issuer.account", account))
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
// quantity change.
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
