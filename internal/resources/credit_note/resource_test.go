package credit_note_test

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

const resAddress = "stripe_credit_note.test"

// noteConfig is a credit note of one custom line of unitAmount on the open invoice, which asks
// for 1500 (suite: "setup note"). extra lands inside it.
func noteConfig(c acctest.Config, invoice string, unitAmount int, extra string) string {
	return c.ProviderBlock() + fmt.Sprintf(`
resource "stripe_credit_note" "test" {
  invoice = %q

  lines = [{
    type        = "custom_line_item"
    description = "Goodwill credit"
    quantity    = 1
    unit_amount = %d
  }]
%s}
`, invoice, unitAmount, extra)
}

func notePath(dst *string) resource.TestCheckFunc {
	return resource.TestCheckResourceAttrWith(resAddress, "id", func(v string) error {
		*dst = "/v1/credit_notes/" + v
		return nil
	})
}

// A credit note on an open invoice is issued, and takes its amount off what the invoice asks
// for (suite: "setup note", "void").
func TestAccCreditNote_create(t *testing.T) {
	acct := acctest.NewAccount(t)
	customer, invoice := tfacc.OpenInvoice(t, acct, "usd")
	invoicePath := "/v1/invoices/" + invoice
	resource.ParallelTest(t, resource.TestCase{
		ProtoV6ProviderFactories: tfacc.ProviderFactories,
		Steps: acctest.Forget(t, []resource.TestStep{{
			Config: noteConfig(acct, invoice, 500, "  memo = \"Goodwill\"\n  metadata = { env = \"test\" }\n") + `
data "stripe_credit_note" "by_id" {
  id = stripe_credit_note.test.id
}
`,
			Check: resource.ComposeAggregateTestCheckFunc(
				resource.TestMatchResourceAttr(resAddress, "id", regexp.MustCompile(`^cn_`)),
				resource.TestCheckResourceAttr(resAddress, "status", "issued"),
				resource.TestCheckResourceAttr(resAddress, "amount", "500"),
				resource.TestCheckResourceAttr(resAddress, "customer", customer),
				resource.TestCheckResourceAttr(resAddress, "currency", "usd"),
				resource.TestCheckResourceAttr(resAddress, "memo", "Goodwill"),
				resource.TestCheckResourceAttr(resAddress, "metadata.env", "test"),
				resource.TestCheckResourceAttr(resAddress, "lines.#", "1"),
				resource.TestCheckResourceAttr(resAddress, "lines.0.amount", "500"),
				resource.TestCheckResourceAttrSet(resAddress, "number"),
				resource.TestCheckResourceAttrPair("data.stripe_credit_note.by_id", "id", resAddress, "id"),
				tfacc.Holds(t, acct, &invoicePath, "amount_remaining", wire.Int(1000)),
			),
		}}, nil, "stripe_credit_note"),
	})
}

// `memo` and `metadata` change in place (suite: "update"), and an empty memo clears it.
func TestAccCreditNote_update(t *testing.T) {
	acct := acctest.NewAccount(t)
	_, invoice := tfacc.OpenInvoice(t, acct, "usd")
	resource.ParallelTest(t, resource.TestCase{
		ProtoV6ProviderFactories: tfacc.ProviderFactories,
		Steps: acctest.Forget(t, []resource.TestStep{
			{Config: noteConfig(acct, invoice, 500, "  memo = \"one\"\n  metadata = { keep = \"me\", drop = \"me\" }\n")},
			{
				Config: noteConfig(acct, invoice, 500, "  memo = \"two\"\n  metadata = { keep = \"me\", new = \"v\" }\n"),
				ConfigPlanChecks: resource.ConfigPlanChecks{PreApply: []plancheck.PlanCheck{
					plancheck.ExpectResourceAction(resAddress, plancheck.ResourceActionUpdate),
				}},
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr(resAddress, "memo", "two"),
					resource.TestCheckResourceAttr(resAddress, "metadata.%", "2"),
					resource.TestCheckResourceAttr(resAddress, "metadata.new", "v"),
					resource.TestCheckResourceAttr(resAddress, "status", "issued"),
				),
			},
			{
				Config: noteConfig(acct, invoice, 500, "  metadata = { keep = \"me\", new = \"v\" }\n"),
				Check:  resource.TestCheckNoResourceAttr(resAddress, "memo"),
			},
		}, nil, "stripe_credit_note"),
	})
}

// The lines cannot change (suite: "immutable lines"), and a credit note cannot be destroyed.
func TestAccCreditNote_replace(t *testing.T) {
	acct := acctest.NewAccount(t)
	_, invoice := tfacc.OpenInvoice(t, acct, "usd")
	cfg := noteConfig(acct, invoice, 500, "")
	resource.ParallelTest(t, resource.TestCase{
		ProtoV6ProviderFactories: tfacc.ProviderFactories,
		Steps: acctest.Forget(t, []resource.TestStep{
			{Config: cfg},
			{Config: noteConfig(acct, invoice, 600, ""), PlanOnly: true, ExpectError: tfacc.RefusedReplace},
			{Config: cfg, Taint: []string{resAddress}, ExpectError: tfacc.RefusedDestroy},
		}, nil, "stripe_credit_note"),
	})
}

func TestAccCreditNote_import(t *testing.T) {
	acct := acctest.NewAccount(t)
	_, invoice := tfacc.OpenInvoice(t, acct, "usd")
	cfg := noteConfig(acct, invoice, 500, "  memo = \"Goodwill\"\n  reason = \"order_change\"\n  metadata = { team = \"a\" }\n")
	resource.ParallelTest(t, resource.TestCase{
		ProtoV6ProviderFactories: tfacc.ProviderFactories,
		Steps: acctest.Forget(t, []resource.TestStep{
			{Config: cfg},
			{ResourceName: resAddress, ImportState: true, ImportStateVerify: true},
			{ResourceName: resAddress, ImportState: true, ImportStateKind: resource.ImportBlockWithID, Config: cfg},
		}, nil, "stripe_credit_note"),
	})
}

// Stripe cannot delete a credit note, so dropping it from the configuration, and `terraform
// destroy`, fail at plan time, and the note stays issued.
func TestAccCreditNote_delete(t *testing.T) {
	acct := acctest.NewAccount(t)
	_, invoice := tfacc.OpenInvoice(t, acct, "usd")
	var path string
	var mark int
	cfg := noteConfig(acct, invoice, 500, "")
	resource.ParallelTest(t, resource.TestCase{
		ProtoV6ProviderFactories: tfacc.ProviderFactories,
		Steps: acctest.Forget(t, []resource.TestStep{
			{Config: cfg, Check: resource.ComposeTestCheckFunc(notePath(&path), tfacc.LogMark(t, acct, &mark))},
			{Config: acctest.Without(t, cfg, "stripe_credit_note"), PlanOnly: true, ExpectError: tfacc.RefusedDestroy},
			{Config: cfg, Destroy: true, ExpectError: tfacc.RefusedDestroy},
		}, tfacc.Untouched(t, acct, &path, &mark, "status", wire.String("issued")), "stripe_credit_note"),
	})
}

func TestAccCreditNote_drift(t *testing.T) {
	acct := acctest.NewAccount(t)
	_, invoice := tfacc.OpenInvoice(t, acct, "usd")
	var path string
	cfg := noteConfig(acct, invoice, 500, "  memo = \"Goodwill\"\n")
	resource.ParallelTest(t, resource.TestCase{
		ProtoV6ProviderFactories: tfacc.ProviderFactories,
		Steps: acctest.Forget(t, []resource.TestStep{
			{Config: cfg, Check: notePath(&path)},
			{
				PreConfig: func() {
					tfacc.Post(t, acct, path, wire.Object{{Name: "memo", Value: wire.String("changed")}})
				},
				Config: cfg,
				ConfigPlanChecks: resource.ConfigPlanChecks{PreApply: []plancheck.PlanCheck{
					plancheck.ExpectNonEmptyPlan(),
					plancheck.ExpectResourceAction(resAddress, plancheck.ResourceActionUpdate),
				}},
				Check: resource.TestCheckResourceAttr(resAddress, "memo", "Goodwill"),
			},
		}, nil, "stripe_credit_note"),
	})
}

// Setting `status` to `void` voids the note and gives the invoice back what it credited
// (suite: "void"). A void note still takes a memo (suite: "update a voided note"), and
// cannot be issued again, so a plan back to `issued` is refused at validation.
func TestAccCreditNote_void(t *testing.T) {
	acct := acctest.NewAccount(t)
	_, invoice := tfacc.OpenInvoice(t, acct, "usd")
	var path string
	invoicePath := "/v1/invoices/" + invoice
	resource.ParallelTest(t, resource.TestCase{
		ProtoV6ProviderFactories: tfacc.ProviderFactories,
		Steps: acctest.Forget(t, []resource.TestStep{
			{Config: noteConfig(acct, invoice, 500, ""), Check: notePath(&path)},
			{
				Config: noteConfig(acct, invoice, 500, "  status = \"void\"\n"),
				ConfigPlanChecks: resource.ConfigPlanChecks{PreApply: []plancheck.PlanCheck{
					plancheck.ExpectResourceAction(resAddress, plancheck.ResourceActionUpdate),
				}},
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr(resAddress, "status", "void"),
					resource.TestCheckResourceAttrSet(resAddress, "voided_at"),
					tfacc.Holds(t, acct, &path, "status", wire.String("void")),
					tfacc.Holds(t, acct, &invoicePath, "amount_remaining", wire.Int(1500)),
				),
			},
			{
				Config: noteConfig(acct, invoice, 500, "  status = \"void\"\n  memo = \"after void\"\n"),
				Check:  resource.TestCheckResourceAttr(resAddress, "memo", "after void"),
			},
			{Config: noteConfig(acct, invoice, 500, "  status = \"void\"\n  memo = \"after void\"\n"), PlanOnly: true},
			{Config: noteConfig(acct, invoice, 500, "  status = \"issued\"\n"), PlanOnly: true, ExpectError: regexp.MustCompile(`status`)},
		}, nil, "stripe_credit_note"),
	})
}

// In a live-mode account, issuing a credit note and changing one fail at plan time unless
// the provider allows live money movement.
func TestAccCreditNote_liveMode(t *testing.T) {
	acctest.RequireFake(t)
	acct := acctest.NewAccount(t)
	_, invoice := tfacc.OpenInvoice(t, acct, "usd")
	zero := wire.Array{wire.Object{{Name: "amount", Value: wire.Int(0)}, {Name: "currency", Value: wire.String("usd")}}}
	acctest.Seed(t, acct, wire.Object{
		{Name: "object", Value: wire.String("balance")},
		{Name: "livemode", Value: wire.Bool(true)},
		{Name: "available", Value: zero},
		{Name: "pending", Value: zero},
	})
	refused := regexp.MustCompile(`Live-mode money movement is not allowed`)
	allow := strings.Replace(noteConfig(acct, invoice, 500, ""), "provider \"stripe\" {\n", "provider \"stripe\" {\n  allow_live_money_movement = true\n", 1)
	resource.ParallelTest(t, resource.TestCase{
		ProtoV6ProviderFactories: tfacc.ProviderFactories,
		Steps: acctest.Forget(t, []resource.TestStep{
			{Config: noteConfig(acct, invoice, 500, ""), PlanOnly: true, ExpectError: refused},
			{Config: allow, Check: resource.TestCheckResourceAttr(resAddress, "status", "issued")},
			{Config: noteConfig(acct, invoice, 500, "  status = \"void\"\n"), PlanOnly: true, ExpectError: refused},
		}, nil, "stripe_credit_note"),
	})
}
