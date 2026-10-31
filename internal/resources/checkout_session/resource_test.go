package checkout_session_test

import (
	"fmt"
	"regexp"
	"slices"
	"strings"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/plancheck"
	"github.com/hashicorp/terraform-plugin-testing/terraform"

	"github.com/computomatic/terraform-provider-stripe/internal/acctest"
	"github.com/computomatic/terraform-provider-stripe/internal/acctest/tfacc"
	"github.com/computomatic/terraform-provider-stripe/internal/wire"
)

const resAddress = "stripe_checkout_session.test"

// sessionConfig is a product, its price and a payment-mode session for two of it. extra lands
// inside the session.
func sessionConfig(c acctest.Config, extra string) string {
	return c.ProviderBlock() + fmt.Sprintf(`
resource "stripe_product" "test" {
  name = %q
}

resource "stripe_price" "test" {
  product     = stripe_product.test.id
  currency    = "usd"
  unit_amount = 2000
}

resource "stripe_checkout_session" "test" {
  mode        = "payment"
  success_url = "https://example.com/thanks"

  line_items = [{
    price    = stripe_price.test.id
    quantity = 2
  }]
%s}
`, c.Name("Checkout product"), extra)
}

func sessionPath(dst *string) resource.TestCheckFunc {
	return resource.TestCheckResourceAttrWith(resAddress, "id", func(v string) error {
		*dst = "/v1/checkout/sessions/" + v
		return nil
	})
}

// An open, unpaid session with a hosted page (suite: "create minimal", "create with optional
// fields").
func TestAccCheckoutSession_create(t *testing.T) {
	acct := acctest.NewAccount(t)
	resource.ParallelTest(t, resource.TestCase{
		ProtoV6ProviderFactories: tfacc.ProviderFactories,
		Steps: acctest.Forget(t, []resource.TestStep{{
			Config: sessionConfig(acct, "  cancel_url = \"https://example.com/cart\"\n  client_reference_id = \"order-1\"\n  metadata = { env = \"test\" }\n") + `
data "stripe_checkout_session" "by_id" {
  id = stripe_checkout_session.test.id
}
`,
			Check: resource.ComposeAggregateTestCheckFunc(
				resource.TestCheckResourceAttr(resAddress, "status", "open"),
				resource.TestCheckResourceAttr(resAddress, "payment_status", "unpaid"),
				resource.TestCheckResourceAttr(resAddress, "amount_total", "4000"),
				resource.TestCheckResourceAttrSet(resAddress, "url"),
				resource.TestCheckResourceAttr(resAddress, "cancel_url", "https://example.com/cart"),
				resource.TestCheckResourceAttr(resAddress, "client_reference_id", "order-1"),
				resource.TestCheckResourceAttr(resAddress, "metadata.env", "test"),
				resource.TestCheckResourceAttr(resAddress, "line_items.#", "1"),
				resource.TestCheckResourceAttr(resAddress, "line_items.0.quantity", "2"),
				resource.TestCheckResourceAttrPair(resAddress, "line_items.0.price", "stripe_price.test", "id"),
				resource.TestCheckResourceAttrPair("data.stripe_checkout_session.by_id", "id", resAddress, "id"),
			),
		}}, nil, "stripe_checkout_session"),
	})
}

// `metadata` changes in place (suite: "update metadata").
func TestAccCheckoutSession_update(t *testing.T) {
	acct := acctest.NewAccount(t)
	resource.ParallelTest(t, resource.TestCase{
		ProtoV6ProviderFactories: tfacc.ProviderFactories,
		Steps: acctest.Forget(t, []resource.TestStep{
			{Config: sessionConfig(acct, "  metadata = { keep = \"me\", drop = \"me\" }\n")},
			{
				Config: sessionConfig(acct, "  metadata = { keep = \"me\", new = \"v\" }\n"),
				ConfigPlanChecks: resource.ConfigPlanChecks{PreApply: []plancheck.PlanCheck{
					plancheck.ExpectResourceAction(resAddress, plancheck.ResourceActionUpdate),
				}},
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr(resAddress, "metadata.%", "2"),
					resource.TestCheckResourceAttr(resAddress, "metadata.new", "v"),
				),
			},
		}, nil, "stripe_checkout_session"),
	})
}

// elementsConfig is a session of the `elements` UI mode, which the hosted page refuses
// `collected_information` on (suite variant `elements`). extra lands inside the session.
func elementsConfig(c acctest.Config, extra string) string {
	return c.ProviderBlock() + fmt.Sprintf(`
resource "stripe_product" "test" {
  name = %q
}

resource "stripe_price" "test" {
  product     = stripe_product.test.id
  currency    = "usd"
  unit_amount = 2000
}

resource "stripe_checkout_session" "test" {
  mode       = "payment"
  ui_mode    = "elements"
  return_url = "https://example.com/return"

  line_items = [{
    price    = stripe_price.test.id
    quantity = 2
  }]

  permissions                 = { update_shipping_details = "server_only" }
  shipping_address_collection = { allowed_countries = ["US"] }
%s}
`, c.Name("Checkout product"), extra)
}

// shippingDetails is what the suite's update sends, and movedDetails the same hash with the address
// the suite's create sends elsewhere: an update sends the hash whole.
const (
	shippingDetails = `
  collected_information = {
    shipping_details = {
      name    = "Jenny Rosen"
      address = { line1 = "400 Pine St", city = "Seattle", state = "WA", postal_code = "98101", country = "US" }
    }
  }
`
	movedDetails = `
  collected_information = {
    shipping_details = {
      name    = "Jenny Rosen"
      address = { line1 = "354 Oyster Point Blvd", city = "South San Francisco", state = "CA", postal_code = "94080", country = "US" }
    }
  }
`
)

// `collected_information` is an update's: it goes after the create on a new session and changes
// in place on an open one (suite: TestCheckoutSessionAttributes, base `elements`).
func TestAccCheckoutSession_collectedInformation(t *testing.T) {
	acct := acctest.NewAccount(t)
	collected := resource.ComposeAggregateTestCheckFunc(
		resource.TestCheckResourceAttr(resAddress, "collected_information.shipping_details.name", "Jenny Rosen"),
		resource.TestCheckResourceAttr(resAddress, "collected_information.shipping_details.address.city", "Seattle"),
		resource.TestCheckResourceAttr(resAddress, "status", "open"),
	)
	resource.ParallelTest(t, resource.TestCase{
		ProtoV6ProviderFactories: tfacc.ProviderFactories,
		Steps: acctest.Forget(t, []resource.TestStep{
			{Config: elementsConfig(acct, "")},
			{
				Config: elementsConfig(acct, shippingDetails),
				ConfigPlanChecks: resource.ConfigPlanChecks{PreApply: []plancheck.PlanCheck{
					plancheck.ExpectResourceAction(resAddress, plancheck.ResourceActionUpdate),
				}},
				Check: collected,
			},
			{Config: elementsConfig(acct, shippingDetails), PlanOnly: true},
			{
				Config: elementsConfig(acct, movedDetails),
				Check:  resource.TestCheckResourceAttr(resAddress, "collected_information.shipping_details.address.city", "South San Francisco"),
			},
			{Config: elementsConfig(acct, ""), PlanOnly: true},
		}, nil, "stripe_checkout_session"),
	})
}

func TestAccCheckoutSession_collectedInformationOnCreate(t *testing.T) {
	acct := acctest.NewAccount(t)
	resource.ParallelTest(t, resource.TestCase{
		ProtoV6ProviderFactories: tfacc.ProviderFactories,
		Steps: acctest.Forget(t, []resource.TestStep{{
			Config: elementsConfig(acct, shippingDetails),
			Check:  resource.TestCheckResourceAttr(resAddress, "collected_information.shipping_details.name", "Jenny Rosen"),
		}}, nil, "stripe_checkout_session"),
	})
}

// The hosted page refuses it, with Stripe's words (suite: "update errors").
func TestAccCheckoutSession_collectedInformationRefusedOnTheHostedPage(t *testing.T) {
	acct := acctest.NewAccount(t)
	resource.ParallelTest(t, resource.TestCase{
		ProtoV6ProviderFactories: tfacc.ProviderFactories,
		Steps: acctest.Forget(t, []resource.TestStep{
			{Config: sessionConfig(acct, "")},
			{Config: sessionConfig(acct, shippingDetails), ExpectError: regexp.MustCompile("not supported with")},
		}, nil, "stripe_checkout_session"),
	})
}

// An item that sets `price_data` has the price Stripe makes for it (suite variant `inline_product`).
func TestAccCheckoutSession_priceData(t *testing.T) {
	acct := acctest.NewAccount(t)
	cfg := acct.ProviderBlock() + fmt.Sprintf(`
resource "stripe_checkout_session" "test" {
  mode        = "payment"
  success_url = "https://example.com/thanks"

  line_items = [{
    quantity   = 2
    price_data = { currency = "usd", unit_amount = 5000, product_data = { name = %q } }
  }]
}
`, acct.Name("Inline"))
	resource.ParallelTest(t, resource.TestCase{
		ProtoV6ProviderFactories: tfacc.ProviderFactories,
		Steps: acctest.Forget(t, []resource.TestStep{
			{Config: cfg, Check: resource.TestCheckResourceAttr(resAddress, "amount_total", "10000")},
			{Config: cfg, PlanOnly: true},
		}, nil, "stripe_checkout_session"),
	})
}

// sessionProduct is the product of a session's inline price.
func sessionProduct(c acctest.Config) string {
	return c.ProviderBlock() + fmt.Sprintf(`
resource "stripe_product" "test" {
  name = %q
}
`, c.Name("Checkout product"))
}

// What a session takes and Stripe never returns stays as configured, in the context the suite sent
// it: `every_field/19` and `/25`, `/7`, `/26` and `/28`, and the `elements` of a line item. A
// configuration turns a method on, because Stripe refuses a session with no method to offer in its
// currency (dev sandbox): the suite's is `bancontact` beside a price in `eur` (`methodConfiguration`).
func TestAccCheckoutSession_keptFromConfiguration(t *testing.T) {
	monthly := `[{
    quantity   = 2
    price_data = { currency = "usd", product = stripe_product.test.id, unit_amount = 5000, recurring = { interval = "month" } }
  }]`
	for _, tc := range []struct {
		name   string
		config func(acctest.Config) string
		check  resource.TestCheckFunc
	}{
		{"payment_intent_data", func(c acctest.Config) string {
			return sessionConfig(c, `  payment_intent_data = {
    description                 = "attrs"
    metadata                    = { a = "b" }
    setup_future_usage          = "off_session"
    statement_descriptor        = "ATTRS PROBE"
    statement_descriptor_suffix = "ATTRS"
    transfer_group              = "attrs"
    shipping = {
      name    = "Jenny Rosen"
      address = { line1 = "354 Oyster Point Blvd", city = "South San Francisco", state = "CA", postal_code = "94080", country = "US" }
    }
  }
`)
		}, resource.TestCheckResourceAttr(resAddress, "payment_intent_data.statement_descriptor", "ATTRS PROBE")},
		{"subscription_data", func(c acctest.Config) string {
			return sessionProduct(c) + fmt.Sprintf(`
resource "stripe_checkout_session" "test" {
  mode                      = "subscription"
  success_url               = "https://example.com/thanks"
  payment_method_collection = "always"
  line_items                = %s

  subscription_data = {
    description       = "attrs"
    metadata          = { a = "b" }
    trial_period_days = 7
    trial_settings    = { end_behavior = { missing_payment_method = "cancel" } }
  }
}
`, monthly)
		}, resource.TestCheckResourceAttr(resAddress, "subscription_data.trial_period_days", "7")},
		{"setup_intent_data", func(c acctest.Config) string {
			return c.ProviderBlock() + `
resource "stripe_checkout_session" "test" {
  mode        = "setup"
  currency    = "usd"
  success_url = "https://example.com/thanks"

  setup_intent_data = { description = "attrs", metadata = { a = "b" } }
}
`
		}, resource.TestCheckResourceAttr(resAddress, "setup_intent_data.description", "attrs")},
		{"customer_update", func(c acctest.Config) string {
			return sessionConfig(c, `  customer        = stripe_customer.test.id
  customer_update = { address = "auto", name = "auto", shipping = "auto" }
`) + `
resource "stripe_customer" "test" {
  email = "kept@example.com"
}
`
		}, resource.TestCheckResourceAttr(resAddress, "customer_update.address", "auto")},
		{"payment_method_configuration", func(c acctest.Config) string {
			return sessionProduct(c) + fmt.Sprintf(`
resource "stripe_payment_method_configuration" "test" {
  name       = %q
  bancontact = { display_preference = { preference = "on" } }
}

resource "stripe_checkout_session" "test" {
  mode                         = "payment"
  success_url                  = "https://example.com/thanks"
  payment_method_configuration = stripe_payment_method_configuration.test.id

  line_items = [{
    quantity   = 2
    price_data = { currency = "eur", product = stripe_product.test.id, unit_amount = 5000 }
  }]
}
`, c.Name("Kept"))
		}, resource.TestCheckResourceAttrPair(resAddress, "payment_method_configuration", "stripe_payment_method_configuration.test", "id")},
		{"line_items.tax_rates", func(c acctest.Config) string {
			return sessionProduct(c) + fmt.Sprintf(`
resource "stripe_price" "test" {
  product     = stripe_product.test.id
  currency    = "usd"
  unit_amount = 2000
}

resource "stripe_tax_rate" "test" {
  display_name = %q
  inclusive    = false
  percentage   = 7
}

resource "stripe_checkout_session" "test" {
  mode        = "payment"
  success_url = "https://example.com/thanks"

  line_items = [{
    price     = stripe_price.test.id
    quantity  = 2
    tax_rates = [stripe_tax_rate.test.id]
  }]
}
`, c.Name("Tax"))
		}, resource.TestCheckResourceAttrPair(resAddress, "line_items.0.tax_rates.0", "stripe_tax_rate.test", "id")},
	} {
		t.Run(tc.name, func(t *testing.T) {
			acct := acctest.NewAccount(t)
			cfg := tc.config(acct)
			resource.ParallelTest(t, resource.TestCase{
				ProtoV6ProviderFactories: tfacc.ProviderFactories,
				Steps: acctest.Forget(t, []resource.TestStep{
					{Config: cfg, Check: tc.check},
					{Config: cfg, PlanOnly: true},
				}, nil, "stripe_checkout_session"),
			})
		})
	}
}

// A kept value cannot change, because a session cannot be updated in it or replaced.
func TestAccCheckoutSession_keptArgumentsCannotChange(t *testing.T) {
	acct := acctest.NewAccount(t)
	resource.ParallelTest(t, resource.TestCase{
		ProtoV6ProviderFactories: tfacc.ProviderFactories,
		Steps: acctest.Forget(t, []resource.TestStep{
			{Config: sessionConfig(acct, "  payment_intent_data = { description = \"attrs\" }\n")},
			{
				Config:      sessionConfig(acct, "  payment_intent_data = { description = \"other\" }\n"),
				PlanOnly:    true,
				ExpectError: regexp.MustCompile("(?i)cannot delete a checkout session"),
			},
		}, nil, "stripe_checkout_session"),
	})
}

// Stripe fills the rest of a hash a create sends in part, and the docs say what it holds (suite:
// `nested_minimal/2`).
func TestAccCheckoutSession_hashesStripeFills(t *testing.T) {
	acct := acctest.NewAccount(t)
	cfg := sessionConfig(acct, `  after_expiration   = { recovery = { enabled = true } }
  consent_collection = { payment_method_reuse_agreement = { position = "auto" } }
  name_collection    = { business = { enabled = true }, individual = { enabled = true } }
  tax_id_collection  = { enabled = true }
`)
	resource.ParallelTest(t, resource.TestCase{
		ProtoV6ProviderFactories: tfacc.ProviderFactories,
		Steps: acctest.Forget(t, []resource.TestStep{
			{
				Config: cfg,
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr(resAddress, "after_expiration.recovery.allow_promotion_codes", "false"),
					resource.TestCheckResourceAttr(resAddress, "consent_collection.promotions", "none"),
					resource.TestCheckResourceAttr(resAddress, "consent_collection.terms_of_service", "none"),
					resource.TestCheckResourceAttr(resAddress, "name_collection.business.optional", "false"),
					resource.TestCheckResourceAttr(resAddress, "tax_id_collection.required", "never"),
				),
			},
			{Config: cfg, PlanOnly: true},
		}, nil, "stripe_checkout_session"),
	})
}

// So does a payment method's hash (suite: `enums/51`).
func TestAccCheckoutSession_paymentMethodOptionsStripeFills(t *testing.T) {
	acct := acctest.NewAccount(t)
	cfg := sessionProduct(acct) + `
resource "stripe_checkout_session" "test" {
  mode        = "payment"
  success_url = "https://example.com/thanks"

  line_items = [{
    quantity   = 2
    price_data = { currency = "cad", product = stripe_product.test.id, unit_amount = 5000 }
  }]

  payment_method_options = { acss_debit = { setup_future_usage = "on_session" } }
}
`
	resource.ParallelTest(t, resource.TestCase{
		ProtoV6ProviderFactories: tfacc.ProviderFactories,
		Steps: acctest.Forget(t, []resource.TestStep{
			{Config: cfg, Check: resource.TestCheckResourceAttr(resAddress, "payment_method_options.acss_debit.verification_method", "automatic")},
			{Config: cfg, PlanOnly: true},
		}, nil, "stripe_checkout_session"),
	})
}

// What the product owner dropped of a session leaves it, and a configuration that sets it fails
// validation (rule X6).
func TestAccCheckoutSession_acceptedOnlyArgumentsAreGone(t *testing.T) {
	acct := acctest.NewAccount(t)
	for name, argument := range map[string]string{
		"payment_method_data":  "payment_method_data = { allow_redisplay = \"always\" }",
		"blik":                 "payment_method_options = { blik = { setup_future_usage = \"none\" } }",
		"crypto":               "payment_method_options = { crypto = { setup_future_usage = \"none\" } }",
		"demo_pay":             "payment_method_options = { demo_pay = { setup_future_usage = \"none\" } }",
		"klarna.subscriptions": "payment_method_options = { klarna = { subscriptions = [] } }",
		"p24":                  "payment_method_options = { p24 = { tos_shown_and_accepted = true } }",
		"paypal":               "payment_method_options = { paypal = { risk_correlation_id = \"attrs\" } }",
	} {
		t.Run(name, func(t *testing.T) {
			resource.ParallelTest(t, resource.TestCase{
				ProtoV6ProviderFactories: tfacc.ProviderFactories,
				Steps: []resource.TestStep{{
					Config:      sessionConfig(acct, "  "+argument+"\n"),
					PlanOnly:    true,
					ExpectError: regexp.MustCompile(`(?i)unsupported|Object Attribute|Invalid|Empty object|Incorrect attribute`),
				}},
			})
		})
	}
}

// shippingOption is a fixed-amount shipping option in usd, for a `shipping_options` list.
func shippingOption(name string, amount int) string {
	return fmt.Sprintf(`{
    shipping_rate_data = {
      display_name = %q
      type         = "fixed_amount"
      fixed_amount = {
        amount   = %d
        currency = "usd"
      }
    }
  }`, name, amount)
}

// shippingRates checks that each shipping option, by index, has a shipping rate Stripe made
// and the amount given, and records the rates in ids.
func shippingRates(ids []string, amounts ...string) resource.TestCheckFunc {
	checks := []resource.TestCheckFunc{resource.TestCheckResourceAttr(resAddress, "shipping_options.#", fmt.Sprint(len(amounts)))}
	for i, amount := range amounts {
		checks = append(checks,
			resource.TestCheckResourceAttrWith(resAddress, fmt.Sprintf("shipping_options.%d.shipping_rate", i), func(v string) error {
				if !strings.HasPrefix(v, "shr_") {
					return fmt.Errorf("shipping option %d has shipping rate %q, want a shr_ ID", i, v)
				}
				ids[i] = v
				return nil
			}),
			resource.TestCheckResourceAttr(resAddress, fmt.Sprintf("shipping_options.%d.shipping_amount", i), amount),
		)
	}
	return resource.ComposeAggregateTestCheckFunc(checks...)
}

// Shipping options set on create, and reordered on an open session, change it in place.
// Stripe makes a shipping rate from each option's data on every request, so the reordered
// options have new rates (suite: "create with shipping options", "update shipping options with
// the whole list").
func TestAccCheckoutSession_shippingOptions(t *testing.T) {
	acct := acctest.NewAccount(t)
	ground, express := shippingOption(acct.Name("Ground"), 500), shippingOption(acct.Name("Express"), 1500)
	created := sessionConfig(acct, "\n  shipping_options = ["+ground+", "+express+"]\n")
	reordered := sessionConfig(acct, "\n  shipping_options = ["+express+", "+ground+"]\n")
	first, second := make([]string, 2), make([]string, 2)
	resource.ParallelTest(t, resource.TestCase{
		ProtoV6ProviderFactories: tfacc.ProviderFactories,
		Steps: acctest.Forget(t, []resource.TestStep{
			{Config: created, Check: shippingRates(first, "500", "1500")},
			{Config: created, PlanOnly: true},
			{
				Config: reordered,
				ConfigPlanChecks: resource.ConfigPlanChecks{PreApply: []plancheck.PlanCheck{
					plancheck.ExpectResourceAction(resAddress, plancheck.ResourceActionUpdate),
				}},
				Check: resource.ComposeAggregateTestCheckFunc(
					shippingRates(second, "1500", "500"),
					func(*terraform.State) error {
						for _, id := range second {
							if slices.Contains(first, id) {
								return fmt.Errorf("the update kept shipping rate %s, want a new rate for every option", id)
							}
						}
						return nil
					},
				),
			},
			{Config: reordered, PlanOnly: true},
		}, nil, "stripe_checkout_session"),
	})
}

// `success_url` is create-only, Stripe refuses to change a hosted session's line items (suite:
// "update errors" / "line items"), and a session cannot be destroyed, so changing either fails
// the plan, and a taint fails at apply.
func TestAccCheckoutSession_replace(t *testing.T) {
	acct := acctest.NewAccount(t)
	cfg := sessionConfig(acct, "")
	resource.ParallelTest(t, resource.TestCase{
		ProtoV6ProviderFactories: tfacc.ProviderFactories,
		Steps: acctest.Forget(t, []resource.TestStep{
			{Config: cfg},
			{Config: strings.Replace(cfg, "https://example.com/thanks", "https://example.com/done", 1), PlanOnly: true, ExpectError: tfacc.RefusedReplace},
			{Config: strings.Replace(cfg, "quantity = 2", "quantity = 3", 1), PlanOnly: true, ExpectError: tfacc.RefusedReplace},
			{Config: cfg, Taint: []string{resAddress}, ExpectError: tfacc.RefusedDestroy},
		}, nil, "stripe_checkout_session"),
	})
}

func TestAccCheckoutSession_import(t *testing.T) {
	acct := acctest.NewAccount(t)
	cfg := sessionConfig(acct, "  client_reference_id = \"order-1\"\n  metadata = { team = \"a\" }\n")
	resource.ParallelTest(t, resource.TestCase{
		ProtoV6ProviderFactories: tfacc.ProviderFactories,
		Steps: acctest.Forget(t, []resource.TestStep{
			{Config: cfg},
			{ResourceName: resAddress, ImportState: true, ImportStateVerify: true},
			{ResourceName: resAddress, ImportState: true, ImportStateKind: resource.ImportBlockWithID, Config: cfg},
		}, nil, "stripe_checkout_session"),
	})
}

// Stripe cannot delete a checkout session, so dropping it from the configuration, and
// `terraform destroy`, fail at plan time, and the session stays open.
func TestAccCheckoutSession_delete(t *testing.T) {
	acct := acctest.NewAccount(t)
	var path string
	var mark int
	cfg := sessionConfig(acct, "")
	resource.ParallelTest(t, resource.TestCase{
		ProtoV6ProviderFactories: tfacc.ProviderFactories,
		Steps: acctest.Forget(t, []resource.TestStep{
			{Config: cfg, Check: resource.ComposeTestCheckFunc(sessionPath(&path), tfacc.LogMark(t, acct, &mark))},
			{Config: acctest.Without(t, cfg, "stripe_checkout_session"), PlanOnly: true, ExpectError: tfacc.RefusedDestroy},
			{Config: cfg, Destroy: true, ExpectError: tfacc.RefusedDestroy},
		}, tfacc.Untouched(t, acct, &path, &mark, "status", wire.String("open")), "stripe_checkout_session"),
	})
}

func TestAccCheckoutSession_drift(t *testing.T) {
	acct := acctest.NewAccount(t)
	var path string
	cfg := sessionConfig(acct, "  metadata = { order = \"1\" }\n")
	resource.ParallelTest(t, resource.TestCase{
		ProtoV6ProviderFactories: tfacc.ProviderFactories,
		Steps: acctest.Forget(t, []resource.TestStep{
			{Config: cfg, Check: sessionPath(&path)},
			{
				PreConfig: func() {
					tfacc.Post(t, acct, path, wire.Object{{Name: "metadata", Value: wire.Object{{Name: "order", Value: wire.String("2")}}}})
				},
				Config: cfg,
				ConfigPlanChecks: resource.ConfigPlanChecks{PreApply: []plancheck.PlanCheck{
					plancheck.ExpectNonEmptyPlan(),
					plancheck.ExpectResourceAction(resAddress, plancheck.ResourceActionUpdate),
				}},
				Check: resource.TestCheckResourceAttr(resAddress, "metadata.order", "1"),
			},
		}, nil, "stripe_checkout_session"),
	})
}

// Setting `status` to `expired` expires the session (suite: "expire"), after which a plan
// that keeps it expired is empty.
func TestAccCheckoutSession_expire(t *testing.T) {
	acct := acctest.NewAccount(t)
	var path string
	expired := sessionConfig(acct, "  status = \"expired\"\n")
	resource.ParallelTest(t, resource.TestCase{
		ProtoV6ProviderFactories: tfacc.ProviderFactories,
		Steps: acctest.Forget(t, []resource.TestStep{
			{Config: sessionConfig(acct, ""), Check: sessionPath(&path)},
			{
				Config: expired,
				Check: resource.ComposeTestCheckFunc(
					resource.TestCheckResourceAttr(resAddress, "status", "expired"),
					resource.TestCheckNoResourceAttr(resAddress, "url"),
					tfacc.Holds(t, acct, &path, "status", wire.String("expired")),
				),
			},
			{Config: expired, PlanOnly: true},
		}, nil, "stripe_checkout_session"),
	})
}

// A session that expires on its own, outside Terraform, reads back expired with no planned
// change when the configuration leaves `status` unset.
func TestAccCheckoutSession_expiredElsewhere(t *testing.T) {
	acct := acctest.NewAccount(t)
	var path string
	cfg := sessionConfig(acct, "")
	resource.ParallelTest(t, resource.TestCase{
		ProtoV6ProviderFactories: tfacc.ProviderFactories,
		Steps: acctest.Forget(t, []resource.TestStep{
			{Config: cfg, Check: sessionPath(&path)},
			{
				PreConfig: func() { tfacc.Post(t, acct, path+"/expire", nil) },
				Config:    cfg,
				PlanOnly:  true,
			},
			{Config: cfg, Check: resource.TestCheckResourceAttr(resAddress, "status", "expired")},
			{Config: sessionConfig(acct, "  status = \"expired\"\n"), PlanOnly: true},
			{Config: sessionConfig(acct, ""), Check: resource.TestMatchResourceAttr(resAddress, "id", regexp.MustCompile(`^cs_`))},
		}, nil, "stripe_checkout_session"),
	})
}
