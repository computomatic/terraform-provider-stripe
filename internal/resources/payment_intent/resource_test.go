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
					resource.TestCheckResourceAttrWith(address, "id", func(v string) error {
						if v != id {
							return fmt.Errorf("the update replaced the payment intent %s with %s", id, v)
						}
						return nil
					}),
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

// `currency` is not proven to update, so changing it would replace the intent, which Stripe
// cannot delete: the plan fails, and so does a taint, at apply.
func TestAccPaymentIntent_replace(t *testing.T) {
	acct := acctest.NewAccount(t)
	cfg := intentConfig(acct, "")
	resource.ParallelTest(t, resource.TestCase{
		ProtoV6ProviderFactories: tfacc.ProviderFactories,
		Steps: acctest.Forget(t, []resource.TestStep{
			{Config: cfg},
			{Config: strings.Replace(cfg, `currency = "usd"`, `currency = "eur"`, 1), PlanOnly: true, ExpectError: tfacc.RefusedReplace},
			{Config: cfg, Taint: []string{address}, ExpectError: tfacc.RefusedDestroy},
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
