package invoice_test

import (
	"errors"
	"fmt"
	"net/http"
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

const (
	resAddress  = "stripe_invoice.test"
	itemAddress = "stripe_invoiceitem.test"
)

// invoiceConfig is a pending item of 1500 and a send_invoice draft that sweeps it up and does
// not advance by itself (suite: "draft produced by a pending invoice item"). extra lands inside
// the invoice.
func invoiceConfig(c acctest.Config, extra string) string {
	return c.ProviderBlock() + fmt.Sprintf(`
resource "stripe_customer" "test" {
  email = "invoice@example.com"
}

resource "stripe_invoiceitem" "test" {
  customer    = stripe_customer.test.id
  amount      = 1500
  currency    = "usd"
  description = "Setup"
}

resource "stripe_invoice" "test" {
  customer                       = stripe_customer.test.id
  currency                       = "usd"
  collection_method              = "send_invoice"
  days_until_due                 = 30
  auto_advance                   = false
  pending_invoice_items_behavior = "include"
%s
  depends_on = [stripe_invoiceitem.test]
}
`, extra)
}

// emptyDraftConfig is an empty charge_automatically draft that does not advance by itself, for
// the customer of the given address. extra lands inside it.
func emptyDraftConfig(c acctest.Config, customer, extra string) string {
	return c.ProviderBlock() + fmt.Sprintf(`
resource "stripe_customer" "a" {
  email = "a@example.com"
}

resource "stripe_customer" "b" {
  email = "b@example.com"
}

resource "stripe_invoice" "test" {
  customer          = %s.id
  currency          = "usd"
  collection_method = "charge_automatically"
  auto_advance      = false
%s}
`, customer, extra)
}

func idPath(address, prefix string, dst *string) resource.TestCheckFunc {
	return resource.TestCheckResourceAttrWith(address, "id", func(v string) error {
		*dst = prefix + v
		return nil
	})
}

// gone checks that the object at *path no longer retrieves.
func gone(t *testing.T, acct acctest.Config, path *string) resource.TestCheckFunc {
	return func(*terraform.State) error {
		_, err := tfacc.Client(acct).Do(t.Context(), stripeapi.Request{Method: "GET", Path: *path})
		var apiErr *stripeapi.APIError
		if !errors.As(err, &apiErr) || apiErr.Status != http.StatusNotFound {
			return fmt.Errorf("%s should be gone: %v", *path, err)
		}
		return nil
	}
}

// A draft sweeps up the customer's pending item (suite: "draft produced by a pending invoice
// item").
func TestAccInvoice_create(t *testing.T) {
	acct := acctest.NewAccount(t)
	cfg := invoiceConfig(acct, "  description = \"Setup\"\n  metadata = { env = \"test\" }\n")
	resource.ParallelTest(t, resource.TestCase{
		ProtoV6ProviderFactories: tfacc.ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: cfg + `
data "stripe_invoice" "by_id" {
  id = stripe_invoice.test.id
}
`,
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestMatchResourceAttr(resAddress, "id", regexp.MustCompile(`^in_`)),
					resource.TestCheckResourceAttr(resAddress, "status", "draft"),
					resource.TestCheckResourceAttr(resAddress, "amount_due", "1500"),
					resource.TestCheckResourceAttr(resAddress, "collection_method", "send_invoice"),
					resource.TestCheckResourceAttr(resAddress, "description", "Setup"),
					resource.TestCheckResourceAttr(resAddress, "metadata.env", "test"),
					resource.TestCheckNoResourceAttr(resAddress, "number"),
					resource.TestCheckResourceAttrPair("data.stripe_invoice.by_id", "id", resAddress, "id"),
				),
			},
			// The refreshed item names the draft, and that is no change of it.
			{Config: cfg, Check: resource.TestCheckResourceAttrPair(itemAddress, "invoice", resAddress, "id")},
		},
	})
}

// A draft's fields change in place (suite: "update a draft"), and an empty value clears them.
func TestAccInvoice_update(t *testing.T) {
	acct := acctest.NewAccount(t)
	resource.ParallelTest(t, resource.TestCase{
		ProtoV6ProviderFactories: tfacc.ProviderFactories,
		Steps: []resource.TestStep{
			{Config: invoiceConfig(acct, "  description = \"one\"\n  metadata = { keep = \"me\", drop = \"me\" }\n")},
			{
				Config: invoiceConfig(acct, `  description          = "two"
  footer               = "Thanks"
  statement_descriptor = "TFACC"
  metadata             = { keep = "me", new = "v" }
  custom_fields        = [{ name = "PO", value = "123" }]
`),
				ConfigPlanChecks: resource.ConfigPlanChecks{PreApply: []plancheck.PlanCheck{
					plancheck.ExpectResourceAction(resAddress, plancheck.ResourceActionUpdate),
				}},
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr(resAddress, "status", "draft"),
					resource.TestCheckResourceAttr(resAddress, "description", "two"),
					resource.TestCheckResourceAttr(resAddress, "footer", "Thanks"),
					resource.TestCheckResourceAttr(resAddress, "statement_descriptor", "TFACC"),
					resource.TestCheckResourceAttr(resAddress, "metadata.%", "2"),
					resource.TestCheckResourceAttr(resAddress, "metadata.new", "v"),
					resource.TestCheckResourceAttr(resAddress, "custom_fields.0.name", "PO"),
				),
			},
			{
				Config: invoiceConfig(acct, "  statement_descriptor = \"TFACC\"\n  metadata = { keep = \"me\", new = \"v\" }\n"),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckNoResourceAttr(resAddress, "description"),
					resource.TestCheckNoResourceAttr(resAddress, "footer"),
					resource.TestCheckResourceAttr(resAddress, "custom_fields.#", "0"),
				),
			},
		},
	})
}

// A create-only change replaces a draft: Stripe deletes the old one.
func TestAccInvoice_replaceDraft(t *testing.T) {
	acct := acctest.NewAccount(t)
	var first string
	resource.ParallelTest(t, resource.TestCase{
		ProtoV6ProviderFactories: tfacc.ProviderFactories,
		Steps: []resource.TestStep{
			{Config: emptyDraftConfig(acct, "stripe_customer.a", ""), Check: idPath(resAddress, "/v1/invoices/", &first)},
			{
				Config: emptyDraftConfig(acct, "stripe_customer.b", ""),
				ConfigPlanChecks: resource.ConfigPlanChecks{PreApply: []plancheck.PlanCheck{
					plancheck.ExpectResourceAction(resAddress, plancheck.ResourceActionDestroyBeforeCreate),
				}},
				Check: gone(t, acct, &first),
			},
		},
	})
}

// A finalized invoice cannot be destroyed, so a create-only change fails the plan, and a taint
// fails at apply.
func TestAccInvoice_replace(t *testing.T) {
	acct := acctest.NewAccount(t)
	cfg := invoiceConfig(acct, "  status = \"open\"\n")
	resource.ParallelTest(t, resource.TestCase{
		ProtoV6ProviderFactories: tfacc.ProviderFactories,
		Steps: acctest.Forget(t, []resource.TestStep{
			{Config: cfg},
			{Config: strings.Replace(cfg, `currency                       = "usd"`, `currency                       = "eur"`, 1), PlanOnly: true, ExpectError: tfacc.RefusedReplace},
			{Config: cfg, Taint: []string{resAddress}, ExpectError: tfacc.RefusedDestroy},
		}, nil, "stripe_invoice", "stripe_invoiceitem"),
	})
}

func TestAccInvoice_import(t *testing.T) {
	acct := acctest.NewAccount(t)
	cfg := emptyDraftConfig(acct, "stripe_customer.a", "  description = \"Setup\"\n  metadata = { team = \"a\" }\n")
	resource.ParallelTest(t, resource.TestCase{
		ProtoV6ProviderFactories: tfacc.ProviderFactories,
		Steps: []resource.TestStep{
			{Config: cfg},
			{ResourceName: resAddress, ImportState: true, ImportStateVerify: true},
			{ResourceName: resAddress, ImportState: true, ImportStateKind: resource.ImportBlockWithID, Config: cfg},
		},
	})
}

// Destroying a draft deletes it, and Stripe deletes the item it swept up with it (suite:
// "delete a draft with items"), so the next plan creates the item again.
func TestAccInvoice_delete(t *testing.T) {
	acct := acctest.NewAccount(t)
	var invoicePath, itemPath string
	cfg := invoiceConfig(acct, "")
	resource.ParallelTest(t, resource.TestCase{
		ProtoV6ProviderFactories: tfacc.ProviderFactories,
		Steps: []resource.TestStep{
			{Config: cfg, Check: resource.ComposeTestCheckFunc(
				idPath(resAddress, "/v1/invoices/", &invoicePath), idPath(itemAddress, "/v1/invoiceitems/", &itemPath))},
			{
				Config: acctest.Without(t, cfg, "stripe_invoice"),
				ConfigPlanChecks: resource.ConfigPlanChecks{PreApply: []plancheck.PlanCheck{
					plancheck.ExpectResourceAction(resAddress, plancheck.ResourceActionDestroy),
				}},
				Check:              resource.ComposeTestCheckFunc(gone(t, acct, &invoicePath), gone(t, acct, &itemPath)),
				ExpectNonEmptyPlan: true,
			},
		},
	})
}

// Stripe refuses to delete a finalized invoice, so dropping one from the configuration, and
// `terraform destroy`, fail at plan time, and the invoice stays open (suite: "delete errors").
func TestAccInvoice_deleteFinalized(t *testing.T) {
	acct := acctest.NewAccount(t)
	var path string
	var mark int
	cfg := invoiceConfig(acct, "  status = \"open\"\n")
	resource.ParallelTest(t, resource.TestCase{
		ProtoV6ProviderFactories: tfacc.ProviderFactories,
		Steps: acctest.Forget(t, []resource.TestStep{
			{Config: cfg, Check: resource.ComposeTestCheckFunc(idPath(resAddress, "/v1/invoices/", &path), tfacc.LogMark(t, acct, &mark))},
			{Config: acctest.Without(t, cfg, "stripe_invoice"), PlanOnly: true, ExpectError: tfacc.RefusedDestroy},
			{Config: cfg, Destroy: true, ExpectError: tfacc.RefusedDestroy},
		}, tfacc.Untouched(t, acct, &path, &mark, "status", wire.String("open")), "stripe_invoice", "stripe_invoiceitem"),
	})
}

func TestAccInvoice_drift(t *testing.T) {
	acct := acctest.NewAccount(t)
	var path string
	cfg := invoiceConfig(acct, "  description = \"Setup\"\n")
	resource.ParallelTest(t, resource.TestCase{
		ProtoV6ProviderFactories: tfacc.ProviderFactories,
		Steps: []resource.TestStep{
			{Config: cfg, Check: idPath(resAddress, "/v1/invoices/", &path)},
			{
				PreConfig: func() {
					tfacc.Post(t, acct, path, wire.Object{{Name: "description", Value: wire.String("changed")}})
				},
				Config: cfg,
				ConfigPlanChecks: resource.ConfigPlanChecks{PreApply: []plancheck.PlanCheck{
					plancheck.ExpectNonEmptyPlan(),
					plancheck.ExpectResourceAction(resAddress, plancheck.ResourceActionUpdate),
				}},
				Check: resource.TestCheckResourceAttr(resAddress, "description", "Setup"),
			},
		},
	})
}

// `open` finalizes the draft, which numbers it and resolves its page size of `auto` (suite:
// "finalize"), with nothing planned after; an open invoice still
// takes `metadata` (suite: "update an open invoice's metadata"); `void` voids it (suite:
// "void"), and a void invoice cannot be opened again, which the plan refuses.
func TestAccInvoice_finalizeAndVoid(t *testing.T) {
	acct := acctest.NewAccount(t)
	var path string
	resource.ParallelTest(t, resource.TestCase{
		ProtoV6ProviderFactories: tfacc.ProviderFactories,
		Steps: acctest.Forget(t, []resource.TestStep{
			{Config: invoiceConfig(acct, ""), Check: idPath(resAddress, "/v1/invoices/", &path)},
			{
				Config: invoiceConfig(acct, "  status = \"open\"\n"),
				ConfigPlanChecks: resource.ConfigPlanChecks{PreApply: []plancheck.PlanCheck{
					plancheck.ExpectResourceAction(resAddress, plancheck.ResourceActionUpdate),
				}},
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr(resAddress, "status", "open"),
					resource.TestCheckResourceAttrSet(resAddress, "number"),
					resource.TestCheckResourceAttrSet(resAddress, "due_date"),
					resource.TestCheckResourceAttr(resAddress, "amount_remaining", "1500"),
					resource.TestCheckResourceAttr(resAddress, "rendering.pdf.page_size", "letter"),
					tfacc.Holds(t, acct, &path, "status", wire.String("open")),
				),
			},
			{Config: invoiceConfig(acct, "  status = \"open\"\n"), PlanOnly: true},
			{
				Config: invoiceConfig(acct, "  status = \"open\"\n  metadata = { env = \"test\" }\n"),
				Check:  resource.TestCheckResourceAttr(resAddress, "metadata.env", "test"),
			},
			{
				Config: invoiceConfig(acct, "  status = \"void\"\n  metadata = { env = \"test\" }\n"),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr(resAddress, "status", "void"),
					tfacc.Holds(t, acct, &path, "status", wire.String("void")),
				),
			},
			{Config: invoiceConfig(acct, "  status = \"open\"\n  metadata = { env = \"test\" }\n"), PlanOnly: true, ExpectError: regexp.MustCompile("out of `void`")},
		}, nil, "stripe_invoice", "stripe_invoiceitem"),
	})
}

// `paid` finalizes a draft and pays it with its `default_payment_method` in one apply (suite:
// "finalize", "pay with no parameters"); a paid invoice cannot be voided, which the plan
// refuses (suite: "void errors").
func TestAccInvoice_pay(t *testing.T) {
	acct := acctest.NewAccount(t)
	var path string
	card := `
resource "stripe_customer_payment_method" "card" {
  customer       = stripe_customer.test.id
  payment_method = "pm_card_visa"
}
`
	resource.ParallelTest(t, resource.TestCase{
		ProtoV6ProviderFactories: tfacc.ProviderFactories,
		Steps: acctest.Forget(t, []resource.TestStep{
			{Config: invoiceConfig(acct, "") + card, Check: idPath(resAddress, "/v1/invoices/", &path)},
			{
				Config: invoiceConfig(acct, "  default_payment_method = stripe_customer_payment_method.card.id\n  status = \"paid\"\n") + card,
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr(resAddress, "status", "paid"),
					resource.TestCheckResourceAttr(resAddress, "amount_paid", "1500"),
					resource.TestCheckResourceAttr(resAddress, "amount_remaining", "0"),
					resource.TestCheckResourceAttrSet(resAddress, "number"),
					tfacc.Holds(t, acct, &path, "status", wire.String("paid")),
				),
			},
			{Config: invoiceConfig(acct, "  default_payment_method = stripe_customer_payment_method.card.id\n  status = \"paid\"\n") + card, PlanOnly: true},
			{
				Config:      invoiceConfig(acct, "  default_payment_method = stripe_customer_payment_method.card.id\n  status = \"void\"\n") + card,
				PlanOnly:    true,
				ExpectError: regexp.MustCompile("out of `paid`"),
			},
		}, nil, "stripe_invoice", "stripe_invoiceitem", "stripe_customer_payment_method"),
	})
}

// With no payment method anywhere, `paid` finalizes the draft and Stripe refuses the payment
// (suite: "pay errors" / "no payment method"), so the apply fails, and the refreshed invoice is
// open.
func TestAccInvoice_payWithoutPaymentMethod(t *testing.T) {
	acct := acctest.NewAccount(t)
	resource.ParallelTest(t, resource.TestCase{
		ProtoV6ProviderFactories: tfacc.ProviderFactories,
		Steps: acctest.Forget(t, []resource.TestStep{
			{Config: invoiceConfig(acct, "")},
			{Config: invoiceConfig(acct, "  status = \"paid\"\n"), ExpectError: regexp.MustCompile("default_payment_method")},
			{Config: invoiceConfig(acct, "  status = \"open\"\n"), Check: resource.TestCheckResourceAttr(resAddress, "status", "open")},
		}, nil, "stripe_invoice", "stripe_invoiceitem"),
	})
}

// `uncollectible` marks an open invoice uncollectible (suite: "mark uncollectible"), and an
// uncollectible invoice can still be voided (suite: "void an uncollectible invoice").
func TestAccInvoice_uncollectible(t *testing.T) {
	acct := acctest.NewAccount(t)
	var path string
	resource.ParallelTest(t, resource.TestCase{
		ProtoV6ProviderFactories: tfacc.ProviderFactories,
		Steps: acctest.Forget(t, []resource.TestStep{
			{Config: invoiceConfig(acct, "  status = \"open\"\n"), Check: idPath(resAddress, "/v1/invoices/", &path)},
			{
				Config: invoiceConfig(acct, "  status = \"uncollectible\"\n"),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr(resAddress, "status", "uncollectible"),
					resource.TestCheckResourceAttr(resAddress, "amount_remaining", "1500"),
					tfacc.Holds(t, acct, &path, "status", wire.String("uncollectible")),
				),
			},
			{
				Config: invoiceConfig(acct, "  status = \"void\"\n"),
				Check:  tfacc.Holds(t, acct, &path, "status", wire.String("void")),
			},
		}, nil, "stripe_invoice", "stripe_invoiceitem"),
	})
}

// A draft cannot be voided or marked uncollectible: no action moves it there, so the apply
// fails, naming the status Stripe left (suite: "void errors", "mark uncollectible errors").
func TestAccInvoice_draftCannotBeVoided(t *testing.T) {
	acct := acctest.NewAccount(t)
	resource.ParallelTest(t, resource.TestCase{
		ProtoV6ProviderFactories: tfacc.ProviderFactories,
		Steps: []resource.TestStep{
			{Config: invoiceConfig(acct, "")},
			{Config: invoiceConfig(acct, "  status = \"void\"\n"), ExpectError: regexp.MustCompile("Stripe left the invoice at `draft`")},
			{Config: invoiceConfig(acct, "")},
		},
	})
}

// In a live-mode account, creating an invoice and changing one fail at plan time unless the
// provider allows live money movement.
func TestAccInvoice_liveMode(t *testing.T) {
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
	allow := func(cfg string) string {
		return strings.Replace(cfg, "provider \"stripe\" {\n", "provider \"stripe\" {\n  allow_live_money_movement = true\n", 1)
	}
	resource.ParallelTest(t, resource.TestCase{
		ProtoV6ProviderFactories: tfacc.ProviderFactories,
		Steps: []resource.TestStep{
			{Config: invoiceConfig(acct, ""), PlanOnly: true, ExpectError: refused},
			{Config: allow(invoiceConfig(acct, "")), Check: resource.TestCheckResourceAttr(resAddress, "status", "draft")},
			{Config: invoiceConfig(acct, "  status = \"open\"\n"), PlanOnly: true, ExpectError: refused},
			// Deleting a draft moves no money, so destroy is never checked.
			{Config: acctest.Without(t, invoiceConfig(acct, ""), "stripe_invoice", "stripe_invoiceitem")},
		},
	})
}
