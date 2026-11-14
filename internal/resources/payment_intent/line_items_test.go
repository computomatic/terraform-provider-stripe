package payment_intent_test

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

const lineItemsExpand = "amount_details.line_items"

// everyMemberLine is a line with every member, as the record's probe `update/56` sends it: n is
// 7 on its create and 8 on its update, with the codes, unit and PayPal category of each. Its
// `$gen` values are fixed here. Arithmetic validation is off, as the probe's base turns it off.
func everyMemberLine(n int, card, reader, unit, category string) string {
	return fmt.Sprintf(`
  amount_details = {
    enforce_arithmetic_validation = false
    line_items = [{
      discount_amount = %[1]d
      payment_method_options = {
        card         = { commodity_code = %[2]q }
        card_present = { commodity_code = %[3]q }
        klarna = {
          image_url              = "https://example.com/attrs-%[1]d.png"
          product_url            = "https://example.com/product-%[1]d"
          reference              = "attrs-reference-%[1]d"
          subscription_reference = "attrs-subscription-%[1]d"
        }
        paypal = {
          category    = %[5]q
          description = "Attrs description %[1]d"
          sold_by     = "Attrs seller %[1]d"
        }
      }
      product_code    = "ATTRS%[1]d"
      product_name    = "Attrs line %[1]d"
      quantity        = %[1]d
      tax             = { total_tax_amount = %[1]d }
      unit_cost       = %[1]d
      unit_of_measure = %[4]q
    }]
  }
`, n, card, reader, unit, category)
}

// expanded returns a check that the newest request the fake logged with method and *path asked
// for the line items, or did not.
func expanded(t *testing.T, acct acctest.Config, method string, path *string, want bool) resource.TestCheckFunc {
	return func(*terraform.State) error {
		if acct.Smoke {
			return nil
		}
		for _, r := range acctest.RequestLog(t, acct) {
			if r.Method != method || r.Path != *path {
				continue
			}
			params, _ := r.Params.(wire.Object)
			v, _ := params.Get("expand")
			paths, _ := v.(wire.Array)
			if got := slices.Contains(paths, wire.Value(wire.String(lineItemsExpand))); got != want {
				return fmt.Errorf("%s %s expanded %s: %t, want %t", method, *path, lineItemsExpand, got, want)
			}
			return nil
		}
		return fmt.Errorf("the fake logged no %s %s", method, *path)
	}
}

// Every member of a line is set on create and changed by an update, which sends the whole list
// (the record's `update/56`, whose update replaces the line), and Stripe reads lines that do not
// add up with arithmetic validation off back with `amount_details.error` (the record's
// `every_field/76`). An update that sends no line items does not ask for them, and the state
// keeps them; every refresh asks for them, because the state holds them.
func TestAccPaymentIntent_lineItems(t *testing.T) {
	acct := acctest.NewAccount(t)
	var id, path string
	created := everyMemberLine(7, "ATTRSCARD1", "ATTRSREAD1", "EACH", "digital_goods")
	changed := everyMemberLine(8, "ATTRSCARD2", "ATTRSREAD2", "KILOGRAM", "donation")
	create, retrieve := "/v1/payment_intents", ""
	empty := []plancheck.PlanCheck{plancheck.ExpectEmptyPlan()}
	line := func(member string) string { return "amount_details.line_items.0." + member }
	resource.ParallelTest(t, resource.TestCase{
		ProtoV6ProviderFactories: tfacc.ProviderFactories,
		Steps: acctest.Forget(t, []resource.TestStep{
			{
				Config:           bareIntent(acct, created),
				ConfigPlanChecks: resource.ConfigPlanChecks{PostApplyPostRefresh: empty},
				Check: resource.ComposeAggregateTestCheckFunc(
					idOf(&id), pathOf(&path),
					func(*terraform.State) error { retrieve = path; return nil },
					resource.TestCheckResourceAttr(address, "amount_details.line_items.#", "1"),
					resource.TestMatchResourceAttr(address, line("id"), uliID),
					resource.TestCheckResourceAttr(address, line("object"), "payment_intent_amount_details_line_item"),
					resource.TestCheckResourceAttr(address, line("discount_amount"), "7"),
					resource.TestCheckResourceAttr(address, line("payment_method_options.card.commodity_code"), "ATTRSCARD1"),
					resource.TestCheckResourceAttr(address, line("payment_method_options.card_present.commodity_code"), "ATTRSREAD1"),
					resource.TestCheckResourceAttr(address, line("payment_method_options.klarna.image_url"), "https://example.com/attrs-7.png"),
					resource.TestCheckResourceAttr(address, line("payment_method_options.klarna.product_url"), "https://example.com/product-7"),
					resource.TestCheckResourceAttr(address, line("payment_method_options.klarna.reference"), "attrs-reference-7"),
					resource.TestCheckResourceAttr(address, line("payment_method_options.klarna.subscription_reference"), "attrs-subscription-7"),
					resource.TestCheckResourceAttr(address, line("payment_method_options.paypal.category"), "digital_goods"),
					resource.TestCheckResourceAttr(address, line("payment_method_options.paypal.description"), "Attrs description 7"),
					resource.TestCheckResourceAttr(address, line("payment_method_options.paypal.sold_by"), "Attrs seller 7"),
					resource.TestCheckResourceAttr(address, line("product_code"), "ATTRS7"),
					resource.TestCheckResourceAttr(address, line("product_name"), "Attrs line 7"),
					resource.TestCheckResourceAttr(address, line("quantity"), "7"),
					resource.TestCheckResourceAttr(address, line("tax.total_tax_amount"), "7"),
					resource.TestCheckResourceAttr(address, line("unit_cost"), "7"),
					resource.TestCheckResourceAttr(address, line("unit_of_measure"), "EACH"),
					// 7 x 7 - 7 + 7 = 49, against an amount of 500.
					resource.TestCheckResourceAttr(address, "amount_details.error.code", "amount_details_amount_mismatch"),
					resource.TestMatchResourceAttr(address, "amount_details.error.message", mismatch(49, 500)),
					expanded(t, acct, "POST", &create, true),
				),
			},
			{
				Config: bareIntent(acct, changed),
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply:             []plancheck.PlanCheck{plancheck.ExpectResourceAction(address, plancheck.ResourceActionUpdate)},
					PostApplyPostRefresh: empty,
				},
				Check: resource.ComposeAggregateTestCheckFunc(
					sameIntent(&id),
					resource.TestCheckResourceAttr(address, "amount_details.line_items.#", "1"),
					resource.TestMatchResourceAttr(address, line("id"), uliID),
					resource.TestCheckResourceAttr(address, line("discount_amount"), "8"),
					resource.TestCheckResourceAttr(address, line("payment_method_options.card.commodity_code"), "ATTRSCARD2"),
					resource.TestCheckResourceAttr(address, line("payment_method_options.paypal.category"), "donation"),
					resource.TestCheckResourceAttr(address, line("unit_of_measure"), "KILOGRAM"),
					resource.TestMatchResourceAttr(address, "amount_details.error.message", mismatch(64, 500)),
					// The update sends the whole line, beside the validation it is written with.
					tfacc.Sent(t, acct, "POST", &path, map[string]string{
						"amount_details.enforce_arithmetic_validation":                           "false",
						"amount_details.line_items.0.quantity":                                   "8",
						"amount_details.line_items.0.product_name":                               "Attrs line 8",
						"amount_details.line_items.0.payment_method_options.klarna.reference":    "attrs-reference-8",
						"amount_details.line_items.0.payment_method_options.card.commodity_code": "ATTRSCARD2",
					}),
					expanded(t, acct, "POST", &path, true),
					// The refresh before this apply read the lines the state held.
					expanded(t, acct, "GET", &retrieve, true),
				),
			},
			{
				Config: bareIntent(acct, changed+"  description = \"Lines kept\"\n"),
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply:             []plancheck.PlanCheck{plancheck.ExpectResourceAction(address, plancheck.ResourceActionUpdate)},
					PostApplyPostRefresh: empty,
				},
				Check: resource.ComposeAggregateTestCheckFunc(
					sameIntent(&id),
					resource.TestCheckResourceAttr(address, "description", "Lines kept"),
					resource.TestCheckResourceAttr(address, "amount_details.line_items.#", "1"),
					resource.TestCheckResourceAttr(address, line("product_name"), "Attrs line 8"),
					expanded(t, acct, "POST", &path, false),
				),
			},
		}, nil, "stripe_payment_intent"),
	})
}

// uliID matches a line item's ID.
var uliID = regexp.MustCompile(`^uli_`)

// mismatch matches Stripe's message for lines whose total is not the amount (the record's
// `every_field/76` and `update/56`).
func mismatch(total, amount int) *regexp.Regexp {
	return regexp.MustCompile(regexp.QuoteMeta(fmt.Sprintf("The total value of `amount_details` (%d) does not match the `amount` (%d).", total, amount)))
}

// addingUp is a 2000 cent intent's two lines, which add up to its amount, so they pass arithmetic
// validation (suite: TestPaymentIntentAmountDetailsLineItem, "intent with line items").
func addingUp(first, second string) string {
	lines := map[string]string{
		"widget": `{ product_name = "Conformance widget", unit_cost = 600, quantity = 2 }`,
		"gadget": `{ product_name = "Conformance gadget", unit_cost = 800, quantity = 1 }`,
	}
	return fmt.Sprintf("  amount_details = {\n    line_items = [\n      %s,\n      %s,\n    ]\n  }\n", lines[first], lines[second])
}

// The lines are matched by position: a reorder is an update that sends the list in its new order,
// and Stripe answers with new lines. Confirming the intent keeps them in the state, though the
// confirm does not return them.
func TestAccPaymentIntent_lineItemsReorderAndConfirm(t *testing.T) {
	acct := acctest.NewAccount(t)
	var id, path, firstLine, confirm string
	empty := []plancheck.PlanCheck{plancheck.ExpectEmptyPlan()}
	resource.ParallelTest(t, resource.TestCase{
		ProtoV6ProviderFactories: tfacc.ProviderFactories,
		Steps: acctest.Forget(t, []resource.TestStep{
			{
				Config:           intentConfig(acct, addingUp("widget", "gadget")),
				ConfigPlanChecks: resource.ConfigPlanChecks{PostApplyPostRefresh: empty},
				Check: resource.ComposeAggregateTestCheckFunc(
					idOf(&id), pathOf(&path),
					resource.TestCheckResourceAttr(address, "amount_details.line_items.#", "2"),
					resource.TestCheckResourceAttr(address, "amount_details.line_items.0.product_name", "Conformance widget"),
					resource.TestCheckResourceAttr(address, "amount_details.line_items.1.product_name", "Conformance gadget"),
					resource.TestCheckResourceAttrWith(address, "amount_details.line_items.0.id", func(v string) error { firstLine = v; return nil }),
					resource.TestCheckNoResourceAttr(address, "amount_details.error.code"),
				),
			},
			{
				Config: intentConfig(acct, addingUp("gadget", "widget")),
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply:             []plancheck.PlanCheck{plancheck.ExpectResourceAction(address, plancheck.ResourceActionUpdate)},
					PostApplyPostRefresh: empty,
				},
				Check: resource.ComposeAggregateTestCheckFunc(
					sameIntent(&id),
					resource.TestCheckResourceAttr(address, "amount_details.line_items.0.product_name", "Conformance gadget"),
					resource.TestCheckResourceAttr(address, "amount_details.line_items.0.unit_cost", "800"),
					resource.TestCheckResourceAttr(address, "amount_details.line_items.1.product_name", "Conformance widget"),
					resource.TestCheckResourceAttrWith(address, "amount_details.line_items.1.id", func(v string) error {
						if v == firstLine {
							return fmt.Errorf("the reordered widget kept its line ID %s; Stripe replaces the lines", v)
						}
						return nil
					}),
					tfacc.Sent(t, acct, "POST", &path, map[string]string{
						"amount_details.line_items.0.product_name": "Conformance gadget",
						"amount_details.line_items.1.product_name": "Conformance widget",
					}),
				),
			},
			{
				Config: intentConfig(acct, addingUp("gadget", "widget")+"  payment_method = \"pm_card_visa\"\n  status = \"succeeded\"\n"),
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply:             []plancheck.PlanCheck{plancheck.ExpectResourceAction(address, plancheck.ResourceActionUpdate)},
					PostApplyPostRefresh: empty,
				},
				Check: resource.ComposeAggregateTestCheckFunc(
					sameIntent(&id),
					func(*terraform.State) error { confirm = path + "/confirm"; return nil },
					resource.TestCheckResourceAttr(address, "status", "succeeded"),
					resource.TestCheckResourceAttr(address, "amount_details.line_items.#", "2"),
					resource.TestCheckResourceAttr(address, "amount_details.line_items.0.product_name", "Conformance gadget"),
					expanded(t, acct, "POST", &confirm, false),
					expanded(t, acct, "POST", &path, false),
				),
			},
		}, nil, "stripe_payment_intent"),
	})
}

// A create with line items that is confirmed in the same apply keeps the lines its create read,
// though the confirm does not return them.
func TestAccPaymentIntent_lineItemsConfirmedOnCreate(t *testing.T) {
	acct := acctest.NewAccount(t)
	var confirm string
	resource.ParallelTest(t, resource.TestCase{
		ProtoV6ProviderFactories: tfacc.ProviderFactories,
		Steps: acctest.Forget(t, []resource.TestStep{{
			Config:           intentConfig(acct, addingUp("widget", "gadget")+"  payment_method = \"pm_card_visa\"\n  status = \"succeeded\"\n"),
			ConfigPlanChecks: resource.ConfigPlanChecks{PostApplyPostRefresh: []plancheck.PlanCheck{plancheck.ExpectEmptyPlan()}},
			Check: resource.ComposeAggregateTestCheckFunc(
				resource.TestCheckResourceAttrWith(address, "id", func(v string) error { confirm = "/v1/payment_intents/" + v + "/confirm"; return nil }),
				resource.TestCheckResourceAttr(address, "status", "succeeded"),
				resource.TestCheckResourceAttr(address, "amount_details.line_items.#", "2"),
				resource.TestMatchResourceAttr(address, "amount_details.line_items.1.id", uliID),
				expanded(t, acct, "POST", &confirm, false),
			),
		}}, nil, "stripe_payment_intent"),
	})
}

// An import cannot know whether the intent holds line items, and the record proves their
// expansion only on a retrieve of one that does, so the imported state holds none.
func TestAccPaymentIntent_lineItemsImport(t *testing.T) {
	acct := acctest.NewAccount(t)
	cfg := intentConfig(acct, addingUp("widget", "gadget"))
	resource.ParallelTest(t, resource.TestCase{
		ProtoV6ProviderFactories: tfacc.ProviderFactories,
		Steps: acctest.Forget(t, []resource.TestStep{
			{Config: cfg},
			{
				ResourceName: address, ImportState: true, ImportStateVerify: true,
				// An intent created with line items alone reads `amount_details` null without them.
				ImportStateVerifyIgnore: []string{"amount_details"},
				ImportStateCheck: func(states []*terraform.InstanceState) error {
					for k := range states[0].Attributes {
						if strings.HasPrefix(k, "amount_details.line_items.") && k != "amount_details.line_items.#" {
							return fmt.Errorf("the imported state holds %s", k)
						}
					}
					return nil
				},
			},
		}, nil, "stripe_payment_intent"),
	})
}
