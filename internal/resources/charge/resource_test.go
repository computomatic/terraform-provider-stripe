package charge_test

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

const resAddress = "stripe_charge.test"

// chargeConfig is a direct charge of amount cents of Stripe's test card (suite: "create").
// extra lands inside it.
func chargeConfig(c acctest.Config, amount int, extra string) string {
	return c.ProviderBlock() + fmt.Sprintf(`
resource "stripe_charge" "test" {
  amount   = %d
  currency = "usd"
  source   = "tok_visa"
%s}
`, amount, extra)
}

func chargePath(dst *string) resource.TestCheckFunc {
	return resource.TestCheckResourceAttrWith(resAddress, "id", func(v string) error {
		*dst = "/v1/charges/" + v
		return nil
	})
}

// A direct charge of a test token succeeds and is captured, with no payment intent, and
// echoes what it was sent (suite: "create").
func TestAccCharge_create(t *testing.T) {
	acct := acctest.NewAccount(t)
	resource.ParallelTest(t, resource.TestCase{
		ProtoV6ProviderFactories: tfacc.ProviderFactories,
		Steps: acctest.Forget(t, []resource.TestStep{{
			Config: chargeConfig(acct, 1000, `  description                 = "Direct charge"
  metadata                    = { env = "test" }
  receipt_email               = "receipt@example.com"
  statement_descriptor_suffix = "TFACC"
  transfer_group              = "tfacc"

  shipping = {
    name    = "Test"
    address = { line1 = "1 Main St", country = "US" }
  }
`) + `
data "stripe_charge" "by_id" {
  id = stripe_charge.test.id
}
`,
			Check: resource.ComposeAggregateTestCheckFunc(
				resource.TestMatchResourceAttr(resAddress, "id", regexp.MustCompile(`^ch_`)),
				resource.TestCheckResourceAttr(resAddress, "status", "succeeded"),
				resource.TestCheckResourceAttr(resAddress, "paid", "true"),
				resource.TestCheckResourceAttr(resAddress, "captured", "true"),
				resource.TestCheckResourceAttr(resAddress, "amount_captured", "1000"),
				resource.TestCheckResourceAttr(resAddress, "description", "Direct charge"),
				resource.TestCheckResourceAttr(resAddress, "metadata.env", "test"),
				resource.TestCheckResourceAttr(resAddress, "receipt_email", "receipt@example.com"),
				resource.TestCheckResourceAttr(resAddress, "shipping.name", "Test"),
				resource.TestCheckNoResourceAttr(resAddress, "payment_intent"),
				resource.TestMatchResourceAttr(resAddress, "payment_method", regexp.MustCompile(`^card_`)),
				resource.TestCheckResourceAttrPair("data.stripe_charge.by_id", "id", resAddress, "id"),
			),
		}}, nil, "stripe_charge"),
	})
}

// `description` and `metadata` change in place (suite: "update").
func TestAccCharge_update(t *testing.T) {
	acct := acctest.NewAccount(t)
	resource.ParallelTest(t, resource.TestCase{
		ProtoV6ProviderFactories: tfacc.ProviderFactories,
		Steps: acctest.Forget(t, []resource.TestStep{
			{Config: chargeConfig(acct, 1000, "  description = \"one\"\n  metadata = { keep = \"me\", drop = \"me\" }\n")},
			{
				Config: chargeConfig(acct, 1000, "  description = \"two\"\n  metadata = { keep = \"me\", new = \"v\" }\n"),
				ConfigPlanChecks: resource.ConfigPlanChecks{PreApply: []plancheck.PlanCheck{
					plancheck.ExpectResourceAction(resAddress, plancheck.ResourceActionUpdate),
				}},
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr(resAddress, "description", "two"),
					resource.TestCheckResourceAttr(resAddress, "metadata.%", "2"),
					resource.TestCheckResourceAttr(resAddress, "metadata.new", "v"),
				),
			},
		}, nil, "stripe_charge"),
	})
}

// `amount` cannot change (suite: "immutable amount"), and a charge cannot be destroyed.
func TestAccCharge_replace(t *testing.T) {
	acct := acctest.NewAccount(t)
	cfg := chargeConfig(acct, 1000, "")
	resource.ParallelTest(t, resource.TestCase{
		ProtoV6ProviderFactories: tfacc.ProviderFactories,
		Steps: acctest.Forget(t, []resource.TestStep{
			{Config: cfg},
			{Config: chargeConfig(acct, 2000, ""), PlanOnly: true, ExpectError: tfacc.RefusedReplace},
			{Config: cfg, Taint: []string{resAddress}, ExpectError: tfacc.RefusedDestroy},
		}, nil, "stripe_charge"),
	})
}

// `receipt_email` and `shipping` change in place, and `fraud_details` reports a charge, with the
// record's `safe` (suite: TestChargeAttributes). The update sends `shipping` whole, because the
// record's family clears the members of `shipping.address` it leaves out. The strings, which the
// suite generates, are unique per run.
func TestAccCharge_verifiedUpdates(t *testing.T) {
	acct := acctest.NewAccount(t)
	var first string
	email := func(name string) string { return fmt.Sprintf("e2e+%s@example.com", acct.Name(name)) }
	shipping := func(receipt, name, carrier string) string {
		return fmt.Sprintf(`
  receipt_email = %q
  shipping = {
    name    = %q
    carrier = %q
    address = {
      line1       = "354 Oyster Point Blvd"
      city        = "South San Francisco"
      state       = "CA"
      postal_code = "94080"
      country     = "US"
    }
  }
`, receipt, name, carrier)
	}
	resource.ParallelTest(t, resource.TestCase{
		ProtoV6ProviderFactories: tfacc.ProviderFactories,
		Steps: acctest.Forget(t, []resource.TestStep{
			{
				Config: chargeConfig(acct, 1000, shipping(email("first"), "Jenny Rosen", acct.Name("carrier"))),
				Check:  chargePath(&first),
			},
			{
				Config: chargeConfig(acct, 1000, shipping(email("second"), "Jenny Rosen", acct.Name("other carrier"))+"  fraud_details = { user_report = \"safe\" }\n"),
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply:             []plancheck.PlanCheck{plancheck.ExpectResourceAction(resAddress, plancheck.ResourceActionUpdate)},
					PostApplyPostRefresh: []plancheck.PlanCheck{plancheck.ExpectEmptyPlan()},
				},
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr(resAddress, "receipt_email", email("second")),
					resource.TestCheckResourceAttr(resAddress, "shipping.carrier", acct.Name("other carrier")),
					resource.TestCheckResourceAttr(resAddress, "shipping.address.city", "South San Francisco"),
					resource.TestCheckResourceAttr(resAddress, "fraud_details.user_report", "safe"),
				),
			},
		}, nil, "stripe_charge"),
	})
}

func TestAccCharge_import(t *testing.T) {
	acct := acctest.NewAccount(t)
	cfg := chargeConfig(acct, 1000, "  description = \"Direct charge\"\n  metadata = { team = \"a\" }\n")
	resource.ParallelTest(t, resource.TestCase{
		ProtoV6ProviderFactories: tfacc.ProviderFactories,
		Steps: acctest.Forget(t, []resource.TestStep{
			{Config: cfg},
			// Stripe signs `receipt_url` anew on every read.
			{ResourceName: resAddress, ImportState: true, ImportStateVerify: true, ImportStateVerifyIgnore: []string{"source", "receipt_url"}},
			// An imported charge has no `source`, so its configuration leaves it out.
			{ResourceName: resAddress, ImportState: true, ImportStateKind: resource.ImportBlockWithID, Config: strings.Replace(cfg, "  source   = \"tok_visa\"\n", "", 1)},
		}, nil, "stripe_charge"),
	})
}

// Stripe cannot delete a charge, so dropping it from the configuration, and `terraform
// destroy`, fail at plan time, and the charge stays as it was.
func TestAccCharge_delete(t *testing.T) {
	acct := acctest.NewAccount(t)
	var path string
	var mark int
	cfg := chargeConfig(acct, 1000, "  description = \"Direct charge\"\n")
	resource.ParallelTest(t, resource.TestCase{
		ProtoV6ProviderFactories: tfacc.ProviderFactories,
		Steps: acctest.Forget(t, []resource.TestStep{
			{Config: cfg, Check: resource.ComposeTestCheckFunc(chargePath(&path), tfacc.LogMark(t, acct, &mark))},
			{Config: acctest.Without(t, cfg, "stripe_charge"), PlanOnly: true, ExpectError: tfacc.RefusedDestroy},
			{Config: cfg, Destroy: true, ExpectError: tfacc.RefusedDestroy},
		}, tfacc.Untouched(t, acct, &path, &mark, "description", wire.String("Direct charge")), "stripe_charge"),
	})
}

func TestAccCharge_drift(t *testing.T) {
	acct := acctest.NewAccount(t)
	var path string
	cfg := chargeConfig(acct, 1000, "  description = \"Direct charge\"\n")
	resource.ParallelTest(t, resource.TestCase{
		ProtoV6ProviderFactories: tfacc.ProviderFactories,
		Steps: acctest.Forget(t, []resource.TestStep{
			{Config: cfg, Check: chargePath(&path)},
			{
				PreConfig: func() {
					tfacc.Post(t, acct, path, wire.Object{{Name: "description", Value: wire.String("changed")}})
				},
				Config: cfg,
				ConfigPlanChecks: resource.ConfigPlanChecks{PreApply: []plancheck.PlanCheck{
					plancheck.ExpectNonEmptyPlan(),
					plancheck.ExpectResourceAction(resAddress, plancheck.ResourceActionUpdate),
				}},
				Check: resource.TestCheckResourceAttr(resAddress, "description", "Direct charge"),
			},
		}, nil, "stripe_charge"),
	})
}

// `capture = false` only authorizes the charge (suite: "create uncaptured"), and setting
// `captured` captures it in full (suite: "capture the full amount"). `captured = false` is not
// a value the resource can set.
func TestAccCharge_capture(t *testing.T) {
	acct := acctest.NewAccount(t)
	var path string
	resource.ParallelTest(t, resource.TestCase{
		ProtoV6ProviderFactories: tfacc.ProviderFactories,
		Steps: acctest.Forget(t, []resource.TestStep{
			{
				Config: chargeConfig(acct, 1000, "  capture = false\n"),
				Check: resource.ComposeAggregateTestCheckFunc(
					chargePath(&path),
					resource.TestCheckResourceAttr(resAddress, "captured", "false"),
					resource.TestCheckResourceAttr(resAddress, "paid", "true"),
					resource.TestCheckResourceAttr(resAddress, "amount_captured", "0"),
				),
			},
			{
				Config: chargeConfig(acct, 1000, "  capture = false\n  captured = true\n"),
				ConfigPlanChecks: resource.ConfigPlanChecks{PreApply: []plancheck.PlanCheck{
					plancheck.ExpectResourceAction(resAddress, plancheck.ResourceActionUpdate),
				}},
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr(resAddress, "captured", "true"),
					resource.TestCheckResourceAttr(resAddress, "amount_captured", "1000"),
					tfacc.Holds(t, acct, &path, "captured", wire.Bool(true)),
				),
			},
			{Config: chargeConfig(acct, 1000, "  capture = false\n  captured = true\n"), PlanOnly: true},
			{Config: chargeConfig(acct, 1000, "  capture = false\n  captured = false\n"), PlanOnly: true, ExpectError: regexp.MustCompile(`captured`)},
		}, nil, "stripe_charge"),
	})
}

// In a live-mode account, creating a charge and changing one fail at plan time unless the
// provider allows live money movement.
func TestAccCharge_liveMode(t *testing.T) {
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
	allow := strings.Replace(chargeConfig(acct, 1000, ""), "provider \"stripe\" {\n", "provider \"stripe\" {\n  allow_live_money_movement = true\n", 1)
	resource.ParallelTest(t, resource.TestCase{
		ProtoV6ProviderFactories: tfacc.ProviderFactories,
		Steps: acctest.Forget(t, []resource.TestStep{
			{Config: chargeConfig(acct, 1000, ""), PlanOnly: true, ExpectError: refused},
			{Config: allow, Check: resource.TestCheckResourceAttr(resAddress, "captured", "true")},
			{Config: chargeConfig(acct, 1000, "  description = \"live\"\n"), PlanOnly: true, ExpectError: refused},
		}, nil, "stripe_charge"),
	})
}
