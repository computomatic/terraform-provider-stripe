package payment_intent_test

import (
	"fmt"
	"regexp"
	"strings"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/plancheck"

	"github.com/computomatic/terraform-provider-stripe/internal/acctest"
	"github.com/computomatic/terraform-provider-stripe/internal/acctest/tfacc"
	"github.com/computomatic/terraform-provider-stripe/internal/stripeapi"
	"github.com/computomatic/terraform-provider-stripe/internal/wire"
)

const address = "stripe_payment_intent.test"

// intentConfig is a customer and a 2000 cent card payment intent for it. extra lands inside
// the intent.
func intentConfig(c acctest.Config, extra string) string {
	return c.ProviderBlock() + intentBlocks(extra)
}

func intentBlocks(extra string) string {
	return fmt.Sprintf(`
resource "stripe_customer" "test" {}

resource "stripe_payment_intent" "test" {
  amount   = 2000
  currency = "usd"
  customer = stripe_customer.test.id

  automatic_payment_methods = {
    enabled         = true
    allow_redirects = "never"
  }
%s}
`, extra)
}

func idOf(dst *string) resource.TestCheckFunc {
	return resource.TestCheckResourceAttrWith(address, "id", func(v string) error {
		*dst = v
		return nil
	})
}

// sameIntent checks that the payment intent is still the one whose ID is *want, so an update
// did not replace it.
func sameIntent(want *string) resource.TestCheckFunc {
	return resource.TestCheckResourceAttrWith(address, "id", func(v string) error {
		if v != *want {
			return fmt.Errorf("the update replaced the payment intent %s with %s", *want, v)
		}
		return nil
	})
}

// pathOf records the payment intent's API path.
func pathOf(dst *string) resource.TestCheckFunc {
	return resource.TestCheckResourceAttrWith(address, "id", func(v string) error {
		*dst = "/v1/payment_intents/" + v
		return nil
	})
}

func TestAccPaymentIntent_create(t *testing.T) {
	acct := acctest.NewAccount(t)
	resource.ParallelTest(t, resource.TestCase{
		ProtoV6ProviderFactories: tfacc.ProviderFactories,
		Steps: acctest.Forget(t, []resource.TestStep{{
			Config: intentConfig(acct, "  description = \"Order 1\"\n  capture_method = \"manual\"\n  statement_descriptor_suffix = \"ORDER1\"\n  metadata = { env = \"test\" }\n") + `
data "stripe_payment_intent" "by_id" {
  id = stripe_payment_intent.test.id
}
`,
			Check: resource.ComposeAggregateTestCheckFunc(
				resource.TestMatchResourceAttr(address, "client_secret", regexp.MustCompile(`^pi_.+_secret_`)),
				resource.TestCheckResourceAttr(address, "status", "requires_payment_method"),
				resource.TestCheckResourceAttr(address, "amount", "2000"),
				resource.TestCheckResourceAttr(address, "description", "Order 1"),
				resource.TestCheckResourceAttr(address, "capture_method", "manual"),
				resource.TestCheckResourceAttr(address, "statement_descriptor_suffix", "ORDER1"),
				resource.TestCheckResourceAttr(address, "metadata.env", "test"),
				resource.TestCheckResourceAttr(address, "livemode", "false"),
				resource.TestCheckResourceAttrPair(address, "customer", "stripe_customer.test", "id"),
				resource.TestCheckResourceAttrPair("data.stripe_payment_intent.by_id", "id", address, "id"),
			),
		}}, nil, "stripe_payment_intent"),
	})
}

// `amount`, `description` and `metadata` change in place, and a payment method moves the
// intent to `requires_confirmation` (suite: "update", "update payment method moves status").
func TestAccPaymentIntent_update(t *testing.T) {
	acct := acctest.NewAccount(t)
	var id string
	resource.ParallelTest(t, resource.TestCase{
		ProtoV6ProviderFactories: tfacc.ProviderFactories,
		Steps: acctest.Forget(t, []resource.TestStep{
			{
				Config: intentConfig(acct, "  description = \"one\"\n  metadata = { keep = \"me\", drop = \"me\" }\n"),
				Check:  idOf(&id),
			},
			{
				Config: strings.Replace(intentConfig(acct, "  description = \"two\"\n  payment_method = \"pm_card_visa\"\n  metadata = { keep = \"me\", new = \"v\" }\n"), "amount   = 2000", "amount   = 2500", 1),
				ConfigPlanChecks: resource.ConfigPlanChecks{PreApply: []plancheck.PlanCheck{
					plancheck.ExpectResourceAction(address, plancheck.ResourceActionUpdate),
				}},
				Check: resource.ComposeAggregateTestCheckFunc(
					sameIntent(&id),
					resource.TestCheckResourceAttr(address, "amount", "2500"),
					resource.TestCheckResourceAttr(address, "description", "two"),
					resource.TestCheckResourceAttr(address, "status", "requires_confirmation"),
					resource.TestCheckResourceAttr(address, "metadata.%", "2"),
					resource.TestCheckResourceAttr(address, "metadata.new", "v"),
				),
			},
		}, nil, "stripe_payment_intent"),
	})
}

// `statement_descriptor` is not proven to update, so setting it would replace the intent, which
// Stripe cannot delete: the plan fails, and so does a taint, at apply.
func TestAccPaymentIntent_replace(t *testing.T) {
	acct := acctest.NewAccount(t)
	cfg := intentConfig(acct, "")
	resource.ParallelTest(t, resource.TestCase{
		ProtoV6ProviderFactories: tfacc.ProviderFactories,
		Steps: acctest.Forget(t, []resource.TestStep{
			{Config: cfg},
			{Config: intentConfig(acct, "  confirmation_method = \"manual\"\n"), PlanOnly: true, ExpectError: tfacc.RefusedReplace},
			{Config: cfg, Taint: []string{address}, ExpectError: tfacc.RefusedDestroy},
		}, nil, "stripe_payment_intent"),
	})
}

// bareIntent is a payment intent with what the record's minimal create sends, which leaves
// Stripe to choose the payment methods. extra lands inside it.
func bareIntent(c acctest.Config, extra string) string {
	return c.ProviderBlock() + fmt.Sprintf(`
resource "stripe_payment_intent" "test" {
  amount   = 500
  currency = "usd"
%s}
`, extra)
}

// inPlace applies created, then changed, which must update the same intent in place, and shows an
// empty plan after each.
func inPlace(t *testing.T, acct acctest.Config, created, changed string, atFirst, atLast resource.TestCheckFunc) {
	t.Helper()
	inPlaceBeside(t, acct, "", created, changed, atFirst, atLast)
}

// inPlaceBeside is inPlace with the blocks of beside in the configuration too.
func inPlaceBeside(t *testing.T, acct acctest.Config, beside, created, changed string, atFirst, atLast resource.TestCheckFunc) {
	t.Helper()
	var first string
	empty := []plancheck.PlanCheck{plancheck.ExpectEmptyPlan()}
	resource.ParallelTest(t, resource.TestCase{
		ProtoV6ProviderFactories: tfacc.ProviderFactories,
		Steps: acctest.Forget(t, []resource.TestStep{
			{
				Config:           beside + bareIntent(acct, created),
				ConfigPlanChecks: resource.ConfigPlanChecks{PostApplyPostRefresh: empty},
				Check:            resource.ComposeAggregateTestCheckFunc(idOf(&first), atFirst),
			},
			{
				Config: beside + bareIntent(acct, changed),
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply:             []plancheck.PlanCheck{plancheck.ExpectResourceAction(address, plancheck.ResourceActionUpdate)},
					PostApplyPostRefresh: empty,
				},
				Check: resource.ComposeAggregateTestCheckFunc(sameIntent(&first), atLast),
			},
		}, nil, "stripe_payment_intent"),
	})
}

// freshCard makes an unattached card payment method from a test token, as the suite's recipe does
// for each `payment_method` it sends: Stripe takes an unattached one once.
func freshCard(t *testing.T, acct acctest.Config) string {
	t.Helper()
	resp, err := tfacc.Client(acct).Do(t.Context(), stripeapi.Request{Method: "POST", Path: "/v1/payment_methods", Params: wire.Object{
		{Name: "type", Value: wire.String("card")},
		{Name: "card", Value: wire.Object{{Name: "token", Value: wire.String("tok_visa")}}},
	}})
	if err != nil {
		t.Fatalf("creating a payment method: %v", err)
	}
	id, _ := resp.Body.(wire.Object).GetString("id")
	return id
}

// Stripe refuses `statement_descriptor` on a card payment it picks the methods for ("not supported
// for the payment_method_type `card`", on the dev sandbox, and the suite's "create with
// statement_descriptor is refused for card"), though the fake takes that create. The only requests
// the record shows accepted are those of its `every_field/1` and `update/1` probes
// (observations/payment_intent.json, suite 127d409): a manually confirmed intent with a customer
// and a fresh card payment method, which this test sends, with their values. The record's four
// creates are one body, so it does not say which of these lets the descriptor through.
func TestAccPaymentIntent_descriptorChangesInPlace(t *testing.T) {
	acct := acctest.NewAccount(t)
	beside := "\nresource \"stripe_customer\" \"test\" {}\n"
	cfg := func(pm, capture, future, descriptor, suffix, excluded string) string {
		return fmt.Sprintf(`
  customer                      = stripe_customer.test.id
  confirmation_method           = "manual"
  payment_method                = %q
  capture_method                = %q
  setup_future_usage            = %q
  statement_descriptor          = %q
  statement_descriptor_suffix   = %q
  excluded_payment_method_types = [%s]
`, pm, capture, future, descriptor, suffix, excluded)
	}
	created := cfg(freshCard(t, acct), "automatic", "off_session", "ATTRS PROBE", "ATTRS", `"affirm", "cashapp"`)
	changed := cfg(freshCard(t, acct), "automatic_async", "on_session", "ATTRS UPDATED", "ATTRS TWO", `"cashapp", "affirm"`)
	inPlaceBeside(t, acct, beside, created, changed,
		resource.TestCheckResourceAttr(address, "statement_descriptor", "ATTRS PROBE"),
		resource.ComposeAggregateTestCheckFunc(
			resource.TestCheckResourceAttr(address, "statement_descriptor", "ATTRS UPDATED"),
			resource.TestCheckResourceAttr(address, "statement_descriptor_suffix", "ATTRS TWO"),
			resource.TestCheckResourceAttr(address, "excluded_payment_method_types.0", "cashapp"),
		))
}

// The types a payment intent allows take the place of automatic payment methods and change in
// place, with the record's enum values.
func TestAccPaymentIntent_allowedTypesChangeInPlace(t *testing.T) {
	acct := acctest.NewAccount(t)
	cfg := func(types string) string { return fmt.Sprintf("  allowed_payment_method_types = [%s]\n", types) }
	inPlace(t, acct, cfg(`"card"`), cfg(`"card", "link"`),
		resource.TestCheckResourceAttr(address, "allowed_payment_method_types.#", "1"),
		resource.TestCheckResourceAttr(address, "allowed_payment_method_types.1", "link"))
}

// The payment details change in place. The suite generates these strings.
func TestAccPaymentIntent_paymentDetailsChangeInPlace(t *testing.T) {
	acct := acctest.NewAccount(t)
	one, two := acct.Name("order"), acct.Name("order-2")
	cfg := func(order string) string {
		return fmt.Sprintf("  payment_details = {\n    customer_reference = %q\n    order_reference = %q\n  }\n", order, order)
	}
	inPlace(t, acct, cfg(one), cfg(two),
		resource.TestCheckResourceAttr(address, "payment_details.order_reference", one),
		resource.TestCheckResourceAttr(address, "payment_details.customer_reference", two))
}

// The amount details change in place (suite: the postal codes of TestPaymentIntentAttributes'
// recipe, and its generated integers, 7 and 8).
func TestAccPaymentIntent_amountDetailsChangeInPlace(t *testing.T) {
	acct := acctest.NewAccount(t)
	cfg := func(n int) string {
		return fmt.Sprintf(`
  amount_details = {
    discount_amount = %d
    shipping = {
      amount           = %d
      from_postal_code = "94080"
      to_postal_code   = "94610"
    }
    tax = { total_tax_amount = %d }
  }
`, n, n, n)
	}
	inPlace(t, acct, cfg(7), cfg(8),
		resource.ComposeAggregateTestCheckFunc(
			resource.TestCheckResourceAttr(address, "amount_details.shipping.to_postal_code", "94610"),
			resource.TestCheckResourceAttr(address, "amount_details.tax.total_tax_amount", "7"),
		),
		resource.TestCheckResourceAttr(address, "amount_details.discount_amount", "8"))
}

// The card's options change in place, with the record's enum values, and the update sends the
// whole hash, because the record's family clears the members an update leaves out.
func TestAccPaymentIntent_cardOptionsChangeInPlace(t *testing.T) {
	acct := acctest.NewAccount(t)
	cfg := func(secure, future string) string {
		return fmt.Sprintf(`
  payment_method_options = {
    card = {
      capture_method           = "manual"
      request_three_d_secure   = %q
      require_cvc_recollection = true
      setup_future_usage       = %q
    }
  }
`, secure, future)
	}
	inPlace(t, acct, cfg("any", "off_session"), cfg("challenge", "on_session"),
		resource.TestCheckResourceAttr(address, "payment_method_options.card.request_three_d_secure", "any"),
		resource.ComposeAggregateTestCheckFunc(
			resource.TestCheckResourceAttr(address, "payment_method_options.card.request_three_d_secure", "challenge"),
			resource.TestCheckResourceAttr(address, "payment_method_options.card.setup_future_usage", "on_session"),
			resource.TestCheckResourceAttr(address, "payment_method_options.card.capture_method", "manual"),
		))
}

// The fee and the transfer's description and metadata change in place on a destination payment.
// The fee is the record's 100, of its `destination` variant, and the suite's generated 8.
func TestAccPaymentIntent_transferDataChangesInPlace(t *testing.T) {
	acct := acctest.NewAccount(t)
	destination := tfacc.TransfersAccount(t, acct)
	one, two := acct.Name("first transfer"), acct.Name("second transfer")
	cfg := func(fee int, description string) string {
		return fmt.Sprintf(`
  application_fee_amount = %d
  transfer_data = {
    destination = %q
    description = %q
    metadata    = { attrs_a = %q }
  }
`, fee, destination, description, description)
	}
	inPlace(t, acct, cfg(100, one), cfg(8, two),
		resource.TestCheckResourceAttr(address, "transfer_data.description", one),
		resource.ComposeAggregateTestCheckFunc(
			resource.TestCheckResourceAttr(address, "application_fee_amount", "8"),
			resource.TestCheckResourceAttr(address, "transfer_data.metadata.attrs_a", two),
		))
}

// The transfer's amount changes in place on its own, as Stripe refuses a fee beside it (suite:
// the record's `transfer_amount` variant, 100, and the suite's generated 8).
func TestAccPaymentIntent_transferAmountChangesInPlace(t *testing.T) {
	acct := acctest.NewAccount(t)
	destination := tfacc.TransfersAccount(t, acct)
	cfg := func(amount int) string {
		return fmt.Sprintf("  transfer_data = {\n    destination = %q\n    amount = %d\n  }\n", destination, amount)
	}
	inPlace(t, acct, cfg(100), cfg(8),
		resource.TestCheckResourceAttr(address, "transfer_data.amount", "100"),
		resource.TestCheckResourceAttr(address, "transfer_data.amount", "8"))
}

// The record's `bancontact` variant: a payment method made inline, kept from the configuration,
// and a payment method configuration Stripe never returns (suite: minimal create `bancontact`).
func TestAccPaymentIntent_paymentMethodData(t *testing.T) {
	acct := acctest.NewAccount(t)
	cfg := func(name string) string {
		return acct.ProviderBlock() + fmt.Sprintf(`
resource "stripe_payment_method_configuration" "test" {
  name = %q
  bancontact = {
    display_preference = { preference = "on" }
  }
}

resource "stripe_payment_intent" "test" {
  amount                       = 500
  currency                     = "eur"
  payment_method_configuration = stripe_payment_method_configuration.test.id
  payment_method_data = {
    type            = "bancontact"
    billing_details = { name = %q }
  }
}
`, acct.Name("Bancontact"), name)
	}
	resource.ParallelTest(t, resource.TestCase{
		ProtoV6ProviderFactories: tfacc.ProviderFactories,
		Steps: acctest.Forget(t, []resource.TestStep{
			{
				Config:           cfg("Jenny Rosen"),
				ConfigPlanChecks: resource.ConfigPlanChecks{PostApplyPostRefresh: []plancheck.PlanCheck{plancheck.ExpectEmptyPlan()}},
				Check:            resource.TestCheckResourceAttr(address, "payment_method_data.billing_details.name", "Jenny Rosen"),
			},
		}, nil, "stripe_payment_intent"),
	})
}

// Before confirmation, `currency`, `receipt_email`, `statement_descriptor_suffix`,
// `setup_future_usage`, `capture_method` and `shipping` change in place, and
// removing `receipt_email`, `setup_future_usage` and `shipping` clears them (suite: "update
// further fields", "clearing optional fields").
func TestAccPaymentIntent_updateFurtherFields(t *testing.T) {
	acct := acctest.NewAccount(t)
	var id string
	kept := `  description = "kept"
  statement_descriptor_suffix = "UPDATED"
  capture_method = "manual"
`
	further := kept + `  receipt_email = "receipt@example.com"
  setup_future_usage = "off_session"

  shipping = {
    name = "Shipping name"
    address = {
      line1   = "1 Main St"
      country = "US"
    }
  }
`
	inEuros := func(extra string) string {
		return strings.Replace(intentConfig(acct, extra), `currency = "usd"`, `currency = "eur"`, 1)
	}
	resource.ParallelTest(t, resource.TestCase{
		ProtoV6ProviderFactories: tfacc.ProviderFactories,
		Steps: acctest.Forget(t, []resource.TestStep{
			{Config: intentConfig(acct, "  description = \"kept\"\n"), Check: idOf(&id)},
			{
				Config: inEuros(further),
				ConfigPlanChecks: resource.ConfigPlanChecks{PreApply: []plancheck.PlanCheck{
					plancheck.ExpectResourceAction(address, plancheck.ResourceActionUpdate),
				}},
				Check: resource.ComposeAggregateTestCheckFunc(
					sameIntent(&id),
					resource.TestCheckResourceAttr(address, "currency", "eur"),
					resource.TestCheckResourceAttr(address, "receipt_email", "receipt@example.com"),
					resource.TestCheckResourceAttr(address, "statement_descriptor_suffix", "UPDATED"),
					resource.TestCheckResourceAttr(address, "setup_future_usage", "off_session"),
					resource.TestCheckResourceAttr(address, "capture_method", "manual"),
					resource.TestCheckResourceAttr(address, "shipping.name", "Shipping name"),
					resource.TestCheckResourceAttr(address, "shipping.address.line1", "1 Main St"),
					resource.TestCheckResourceAttr(address, "status", "requires_payment_method"),
					resource.TestCheckResourceAttr(address, "amount", "2000"),
				),
			},
			{
				Config: inEuros(kept),
				ConfigPlanChecks: resource.ConfigPlanChecks{PreApply: []plancheck.PlanCheck{
					plancheck.ExpectResourceAction(address, plancheck.ResourceActionUpdate),
				}},
				Check: resource.ComposeAggregateTestCheckFunc(
					sameIntent(&id),
					resource.TestCheckNoResourceAttr(address, "receipt_email"),
					resource.TestCheckNoResourceAttr(address, "setup_future_usage"),
					resource.TestCheckNoResourceAttr(address, "shipping"),
				),
			},
			{Config: inEuros(kept), PlanOnly: true},
		}, nil, "stripe_payment_intent"),
	})
}

// `transfer_group` is sent on create. Stripe refuses to unset it (suite: "transfer_group
// cannot be unset") or to change it once set (suite: "update transfer_group once set"), so
// removing it from the configuration plans no change and the intent keeps its group, while a
// new group fails the plan.
func TestAccPaymentIntent_transferGroupKept(t *testing.T) {
	acct := acctest.NewAccount(t)
	var path string
	cfg := intentConfig(acct, "  description = \"grouped\"\n")
	grouped := func(group string) string {
		return intentConfig(acct, "  description = \"grouped\"\n  transfer_group = \""+group+"\"\n")
	}
	resource.ParallelTest(t, resource.TestCase{
		ProtoV6ProviderFactories: tfacc.ProviderFactories,
		Steps: acctest.Forget(t, []resource.TestStep{
			{
				Config: grouped("group-1"),
				Check: resource.ComposeAggregateTestCheckFunc(
					pathOf(&path),
					resource.TestCheckResourceAttr(address, "transfer_group", "group-1"),
					tfacc.Holds(t, acct, &path, "transfer_group", wire.String("group-1")),
				),
			},
			{
				Config: cfg,
				ConfigPlanChecks: resource.ConfigPlanChecks{PreApply: []plancheck.PlanCheck{
					plancheck.ExpectEmptyPlan(),
				}},
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr(address, "transfer_group", "group-1"),
					tfacc.Holds(t, acct, &path, "transfer_group", wire.String("group-1")),
				),
			},
			{
				Config:      grouped("group-2"),
				PlanOnly:    true,
				ExpectError: regexp.MustCompile("(?s)Changing `transfer_group` is not supported yet.*removed block"),
			},
		}, nil, "stripe_payment_intent"),
	})
}

// Removing `statement_descriptor_suffix` from the configuration clears it (suite: "clearing
// optional fields").
func TestAccPaymentIntent_clearStatementDescriptorSuffix(t *testing.T) {
	acct := acctest.NewAccount(t)
	var path string
	cfg := intentConfig(acct, "  description = \"suffixed\"\n")
	resource.ParallelTest(t, resource.TestCase{
		ProtoV6ProviderFactories: tfacc.ProviderFactories,
		Steps: acctest.Forget(t, []resource.TestStep{
			{
				Config: intentConfig(acct, "  description = \"suffixed\"\n  statement_descriptor_suffix = \"ORDER1\"\n"),
				Check: resource.ComposeAggregateTestCheckFunc(
					pathOf(&path),
					resource.TestCheckResourceAttr(address, "statement_descriptor_suffix", "ORDER1"),
				),
			},
			{
				Config: cfg,
				ConfigPlanChecks: resource.ConfigPlanChecks{PreApply: []plancheck.PlanCheck{
					plancheck.ExpectResourceAction(address, plancheck.ResourceActionUpdate),
				}},
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckNoResourceAttr(address, "statement_descriptor_suffix"),
					tfacc.Holds(t, acct, &path, "statement_descriptor_suffix", wire.Null{}),
				),
			},
			{Config: cfg, PlanOnly: true},
		}, nil, "stripe_payment_intent"),
	})
}

// Once the intent has succeeded, `receipt_email` and `description` still change in place, but
// Stripe refuses a change of `currency`, `setup_future_usage`, `statement_descriptor_suffix` or
// `capture_method`, and the apply fails (suite: "update once succeeded").
func TestAccPaymentIntent_updateOnceSucceeded(t *testing.T) {
	acct := acctest.NewAccount(t)
	succeeded := "  payment_method = \"pm_card_visa\"\n  status = \"succeeded\"\n"
	updated := succeeded + "  description = \"succeeded\"\n  receipt_email = \"receipt@example.com\"\n"
	refused := func(field string) *regexp.Regexp {
		return regexp.MustCompile(strings.ReplaceAll("PaymentIntent's "+field+" could not be updated because it has a status of succeeded", " ", `\s+`))
	}
	resource.ParallelTest(t, resource.TestCase{
		ProtoV6ProviderFactories: tfacc.ProviderFactories,
		Steps: acctest.Forget(t, []resource.TestStep{
			{Config: intentConfig(acct, succeeded)},
			{
				Config: intentConfig(acct, updated),
				ConfigPlanChecks: resource.ConfigPlanChecks{PreApply: []plancheck.PlanCheck{
					plancheck.ExpectResourceAction(address, plancheck.ResourceActionUpdate),
				}},
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr(address, "status", "succeeded"),
					resource.TestCheckResourceAttr(address, "description", "succeeded"),
					resource.TestCheckResourceAttr(address, "receipt_email", "receipt@example.com"),
				),
			},
			{
				Config:      strings.Replace(intentConfig(acct, updated), `currency = "usd"`, `currency = "eur"`, 1),
				ExpectError: refused("currency"),
			},
			{
				Config:      intentConfig(acct, updated+"  setup_future_usage = \"off_session\"\n"),
				ExpectError: refused("setup_future_usage"),
			},
			{
				Config:      intentConfig(acct, updated+"  statement_descriptor_suffix = \"LATE\"\n"),
				ExpectError: refused("statement_descriptor_suffix"),
			},
			{
				Config:      intentConfig(acct, updated+"  capture_method = \"manual\"\n"),
				ExpectError: refused("capture_method"),
			},
			{
				Config:   intentConfig(acct, updated),
				PlanOnly: true,
			},
		}, nil, "stripe_payment_intent"),
	})
}

func TestAccPaymentIntent_import(t *testing.T) {
	acct := acctest.NewAccount(t)
	cfg := intentConfig(acct, "  description = \"Order 1\"\n  metadata = { team = \"a\" }\n")
	resource.ParallelTest(t, resource.TestCase{
		ProtoV6ProviderFactories: tfacc.ProviderFactories,
		Steps: acctest.Forget(t, []resource.TestStep{
			{Config: cfg},
			{ResourceName: address, ImportState: true, ImportStateVerify: true},
			{ResourceName: address, ImportState: true, ImportStateKind: resource.ImportBlockWithID, Config: cfg},
		}, nil, "stripe_payment_intent"),
	})
}

// Stripe cannot delete a payment intent, so dropping it from the configuration, and `terraform
// destroy`, fail at plan time, and the intent stays as it was.
func TestAccPaymentIntent_delete(t *testing.T) {
	acct := acctest.NewAccount(t)
	var path string
	var mark int
	cfg := intentConfig(acct, "")
	resource.ParallelTest(t, resource.TestCase{
		ProtoV6ProviderFactories: tfacc.ProviderFactories,
		Steps: acctest.Forget(t, []resource.TestStep{
			{Config: cfg, Check: resource.ComposeTestCheckFunc(pathOf(&path), tfacc.LogMark(t, acct, &mark))},
			{Config: acctest.Without(t, cfg, "stripe_payment_intent"), PlanOnly: true, ExpectError: tfacc.RefusedDestroy},
			{Config: cfg, Destroy: true, ExpectError: tfacc.RefusedDestroy},
		}, tfacc.Untouched(t, acct, &path, &mark, "status", wire.String("requires_payment_method")), "stripe_payment_intent"),
	})
}

func TestAccPaymentIntent_drift(t *testing.T) {
	acct := acctest.NewAccount(t)
	var id string
	cfg := intentConfig(acct, "  description = \"Order 1\"\n")
	resource.ParallelTest(t, resource.TestCase{
		ProtoV6ProviderFactories: tfacc.ProviderFactories,
		Steps: acctest.Forget(t, []resource.TestStep{
			{Config: cfg, Check: idOf(&id)},
			{
				PreConfig: func() {
					tfacc.Post(t, acct, "/v1/payment_intents/"+id, wire.Object{{Name: "description", Value: wire.String("changed")}})
				},
				Config: cfg,
				ConfigPlanChecks: resource.ConfigPlanChecks{PreApply: []plancheck.PlanCheck{
					plancheck.ExpectNonEmptyPlan(),
					plancheck.ExpectResourceAction(address, plancheck.ResourceActionUpdate),
				}},
				Check: resource.TestCheckResourceAttr(address, "description", "Order 1"),
			},
		}, nil, "stripe_payment_intent"),
	})
}

// Setting `status` to `succeeded` on create confirms the intent with its payment method
// (suite: "confirm"); with manual capture the same apply also captures it (suite: "capture
// the full amount"). Later plans are empty: a succeeded intent stays succeeded.
func TestAccPaymentIntent_succeeded(t *testing.T) {
	for name, extra := range map[string]string{"automatic": "", "manual": "  capture_method = \"manual\"\n"} {
		t.Run(name, func(t *testing.T) {
			acct := acctest.NewAccount(t)
			var path string
			resource.ParallelTest(t, resource.TestCase{
				ProtoV6ProviderFactories: tfacc.ProviderFactories,
				Steps: acctest.Forget(t, []resource.TestStep{{
					Config: intentConfig(acct, extra+"  payment_method = \"pm_card_visa\"\n  status = \"succeeded\"\n"),
					Check: resource.ComposeAggregateTestCheckFunc(
						pathOf(&path),
						resource.TestCheckResourceAttr(address, "status", "succeeded"),
						resource.TestCheckResourceAttr(address, "amount_received", "2000"),
						resource.TestCheckResourceAttrSet(address, "latest_charge"),
						tfacc.Holds(t, acct, &path, "status", wire.String("succeeded")),
					),
				}}, nil, "stripe_payment_intent"),
			})
		})
	}
}

// Setting `status` to `canceled` cancels the intent (suite: "cancel"), and a succeeded intent
// cannot be canceled (suite: "already succeeded"), so that plan fails before Stripe is asked.
func TestAccPaymentIntent_cancel(t *testing.T) {
	acct := acctest.NewAccount(t)
	var path string
	succeeded := intentConfig(acct, "  payment_method = \"pm_card_visa\"\n  status = \"succeeded\"\n")
	resource.ParallelTest(t, resource.TestCase{
		ProtoV6ProviderFactories: tfacc.ProviderFactories,
		Steps: acctest.Forget(t, []resource.TestStep{
			{Config: intentConfig(acct, ""), Check: pathOf(&path)},
			{
				Config: intentConfig(acct, "  status = \"canceled\"\n"),
				ConfigPlanChecks: resource.ConfigPlanChecks{PreApply: []plancheck.PlanCheck{
					plancheck.ExpectResourceAction(address, plancheck.ResourceActionUpdate),
				}},
				Check: resource.ComposeTestCheckFunc(
					resource.TestCheckResourceAttr(address, "status", "canceled"),
					tfacc.Holds(t, acct, &path, "status", wire.String("canceled")),
				),
			},
			{
				Config:      succeeded,
				PlanOnly:    true,
				ExpectError: regexp.MustCompile("Cannot change `status` from `canceled`"),
			},
		}, nil, "stripe_payment_intent"),
	})
}

// A declined card leaves the intent where Stripe put it (suite: "declined card"). The apply
// fails with Stripe's error and says how to clear the taint on an intent that cannot be
// destroyed.
func TestAccPaymentIntent_declined(t *testing.T) {
	acct := acctest.NewAccount(t)
	resource.ParallelTest(t, resource.TestCase{
		ProtoV6ProviderFactories: tfacc.ProviderFactories,
		Steps: acctest.Forget(t, []resource.TestStep{{
			Config:      intentConfig(acct, "  payment_method = \"pm_card_chargeDeclined\"\n  status = \"succeeded\"\n"),
			ExpectError: regexp.MustCompile(`(?s)card_declined.*exists and is tainted`),
		}}, nil, "stripe_payment_intent"),
	})
}

// allowLive is the account's provider block with the live-mode switch on.
func allowLive(c acctest.Config) string {
	return strings.Replace(c.ProviderBlock(), "provider \"stripe\" {\n", "provider \"stripe\" {\n  allow_live_money_movement = true\n", 1)
}

// In an account whose balance reports live mode, a payment intent is refused at plan time,
// before any request that could move money, until the provider allows live money movement;
// a change to an existing intent is refused the same way (ADR
// 2026-10-22-money-moving-resources, D8).
func TestAccPaymentIntent_liveMode(t *testing.T) {
	acctest.RequireFake(t)
	acct := acctest.NewAccount(t)
	zero := wire.Array{wire.Object{{Name: "amount", Value: wire.Int(0)}, {Name: "currency", Value: wire.String("usd")}}}
	acctest.Seed(t, acct, wire.Object{
		{Name: "object", Value: wire.String("balance")},
		{Name: "livemode", Value: wire.Bool(true)},
		{Name: "available", Value: zero},
		{Name: "pending", Value: zero},
	})
	refused := regexp.MustCompile(`Live-mode money movement is not allowed`)
	var mark int
	resource.ParallelTest(t, resource.TestCase{
		ProtoV6ProviderFactories: tfacc.ProviderFactories,
		Steps: acctest.Forget(t, []resource.TestStep{
			{
				PreConfig: func() { tfacc.LogMark(t, acct, &mark)(nil) },
				Config:    intentConfig(acct, ""),
				PlanOnly:  true, ExpectError: refused,
			},
			{
				// The refused plan sent nothing but the mode check, and created no customer
				// either: Terraform stops the whole plan.
				PreConfig: func() {
					log := acctest.RequestLog(t, acct)
					for _, r := range log[:max(len(log)-mark, 0)] {
						if r.Method != "GET" {
							t.Errorf("the refused plan sent %s %s", r.Method, r.Path)
						}
					}
				},
				Config: allowLive(acct) + intentBlocks("  description = \"one\"\n"),
				Check:  resource.TestCheckResourceAttr(address, "description", "one"),
			},
			{Config: intentConfig(acct, "  description = \"two\"\n"), PlanOnly: true, ExpectError: refused},
		}, nil, "stripe_payment_intent"),
	})
}
