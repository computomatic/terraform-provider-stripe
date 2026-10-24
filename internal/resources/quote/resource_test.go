package quote_test

import (
	"fmt"
	"os"
	"regexp"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/plancheck"

	"github.com/computomatic/terraform-provider-stripe/internal/acctest"
	"github.com/computomatic/terraform-provider-stripe/internal/acctest/tfacc"
	"github.com/computomatic/terraform-provider-stripe/internal/wire"
)

func TestMain(m *testing.M) { os.Exit(acctest.Main(m)) }

const address = "stripe_quote.test"

// quoteConfig is a customer, a price and a quote invoiced by email. lines and extra land
// inside the quote.
func quoteConfig(c acctest.Config, lines string, extra string) string {
	return c.ProviderBlock() + fmt.Sprintf(`
resource "stripe_customer" "test" {
  email = "quote@example.com"
}

resource "stripe_product" "test" {
  name = %q
}

resource "stripe_price" "test" {
  product     = stripe_product.test.id
  currency    = "usd"
  unit_amount = 2000
}

resource "stripe_quote" "test" {
  customer          = stripe_customer.test.id
  collection_method = "send_invoice"
  invoice_settings = {
    days_until_due = 30
  }
  line_items = %s
%s}
`, c.Name("Package"), lines, extra)
}

const oneLine = `[{ price = stripe_price.test.id, quantity = 1 }]`

// pathOf records the quote's API path.
func pathOf(dst *string) resource.TestCheckFunc {
	return resource.TestCheckResourceAttrWith(address, "id", func(v string) error {
		*dst = "/v1/quotes/" + v
		return nil
	})
}

func idOf(dst *string) resource.TestCheckFunc {
	return resource.TestCheckResourceAttrWith(address, "id", func(v string) error {
		*dst = v
		return nil
	})
}

func TestAccQuote_create(t *testing.T) {
	acct := acctest.NewAccount(t)
	resource.ParallelTest(t, resource.TestCase{
		ProtoV6ProviderFactories: tfacc.ProviderFactories,
		ErrorCheck:               tfacc.SkipWhenFakeLacks(t, "line_items"),
		Steps: acctest.Forget(t, []resource.TestStep{{
			Config: quoteConfig(acct, `[{ price = stripe_price.test.id, quantity = 3 }]`, "  description = \"Package\"\n  header = \"Hello\"\n  metadata = { team = \"a\" }\n") + `
data "stripe_quote" "by_id" {
  id = stripe_quote.test.id
}
`,
			Check: resource.ComposeAggregateTestCheckFunc(
				resource.TestCheckResourceAttrSet(address, "id"),
				resource.TestCheckResourceAttrPair(address, "customer", "stripe_customer.test", "id"),
				resource.TestCheckResourceAttr(address, "status", "draft"),
				resource.TestCheckResourceAttr(address, "collection_method", "send_invoice"),
				resource.TestCheckResourceAttr(address, "invoice_settings.days_until_due", "30"),
				resource.TestCheckResourceAttr(address, "description", "Package"),
				resource.TestCheckResourceAttr(address, "amount_total", "6000"),
				resource.TestCheckResourceAttr(address, "line_items.#", "1"),
				resource.TestCheckResourceAttrPair(address, "line_items.0.price", "stripe_price.test", "id"),
				resource.TestCheckResourceAttr(address, "line_items.0.quantity", "3"),
				resource.TestCheckResourceAttr(address, "metadata.team", "a"),
				resource.TestCheckResourceAttrPair("data.stripe_quote.by_id", "id", address, "id"),
				resource.TestCheckResourceAttr("data.stripe_quote.by_id", "line_items.0.quantity", "3"),
			),
		}}, nil, "stripe_quote"),
	})
}

func TestAccQuote_update(t *testing.T) {
	acct := acctest.NewAccount(t)
	resource.ParallelTest(t, resource.TestCase{
		ProtoV6ProviderFactories: tfacc.ProviderFactories,
		ErrorCheck:               tfacc.SkipWhenFakeLacks(t, "line_items"),
		Steps: acctest.Forget(t, []resource.TestStep{
			{Config: quoteConfig(acct, oneLine, "  metadata = { team = \"a\", tier = \"1\" }\n")},
			{
				// The line items are replaced as a whole (suite: "line items are replaced").
				Config: quoteConfig(acct, `[{ price = stripe_price.test.id, quantity = 5 }]`, "  description = \"Updated\"\n  footer = \"Thanks\"\n  expires_at = 4102444800\n  metadata = { team = \"b\" }\n"),
				ConfigPlanChecks: resource.ConfigPlanChecks{PreApply: []plancheck.PlanCheck{
					plancheck.ExpectResourceAction(address, plancheck.ResourceActionUpdate),
				}},
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr(address, "description", "Updated"),
					resource.TestCheckResourceAttr(address, "footer", "Thanks"),
					resource.TestCheckResourceAttr(address, "expires_at", "4102444800"),
					resource.TestCheckResourceAttr(address, "line_items.0.quantity", "5"),
					resource.TestCheckResourceAttr(address, "amount_total", "10000"),
					resource.TestCheckResourceAttr(address, "metadata.%", "1"),
					resource.TestCheckResourceAttr(address, "metadata.team", "b"),
				),
			},
		}, nil, "stripe_quote"),
	})
}

// A quote created from another one takes `from_quote` on create only (suite: "`from_quote` and
// `test_clock` on update are `parameter_unknown`"), so changing it would replace the revision,
// which Stripe cannot delete: the plan fails, and so does a taint, at apply.
func TestAccQuote_replace(t *testing.T) {
	acct := acctest.NewAccount(t)
	cfg := func(from string) string {
		return quoteConfig(acct, oneLine, "") + fmt.Sprintf(`
resource "stripe_quote" "source_a" {
  customer = stripe_customer.test.id
}

resource "stripe_quote" "source_b" {
  customer = stripe_customer.test.id
}

resource "stripe_quote" "revision" {
  from_quote = {
    quote = stripe_quote.source_%s.id
  }
}
`, from)
	}
	resource.ParallelTest(t, resource.TestCase{
		ProtoV6ProviderFactories: tfacc.ProviderFactories,
		ErrorCheck:               tfacc.SkipWhenFakeLacks(t, "line_items"),
		Steps: acctest.Forget(t, []resource.TestStep{
			{Config: cfg("a")},
			{Config: cfg("b"), PlanOnly: true, ExpectError: tfacc.RefusedReplace},
			{Config: cfg("a"), Taint: []string{"stripe_quote.revision"}, ExpectError: tfacc.RefusedDestroy},
		}, nil, "stripe_quote"),
	})
}

func TestAccQuote_import(t *testing.T) {
	acct := acctest.NewAccount(t)
	cfg := quoteConfig(acct, `[{ price = stripe_price.test.id, quantity = 2 }]`, "  description = \"Package\"\n  metadata = { team = \"a\" }\n")
	resource.ParallelTest(t, resource.TestCase{
		ProtoV6ProviderFactories: tfacc.ProviderFactories,
		ErrorCheck:               tfacc.SkipWhenFakeLacks(t, "line_items"),
		Steps: acctest.Forget(t, []resource.TestStep{
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
		}, nil, "stripe_quote"),
	})
}

// Stripe cannot delete a quote, so dropping it from the configuration, and `terraform
// destroy`, fail at plan time, and the quote stays as it was.
func TestAccQuote_delete(t *testing.T) {
	acct := acctest.NewAccount(t)
	var path string
	var mark int
	cfg := quoteConfig(acct, oneLine, "")
	resource.ParallelTest(t, resource.TestCase{
		ProtoV6ProviderFactories: tfacc.ProviderFactories,
		ErrorCheck:               tfacc.SkipWhenFakeLacks(t, "line_items"),
		Steps: acctest.Forget(t, []resource.TestStep{
			{Config: cfg, Check: resource.ComposeTestCheckFunc(pathOf(&path), tfacc.LogMark(t, acct, &mark))},
			{Config: acctest.Without(t, cfg, "stripe_quote"), PlanOnly: true, ExpectError: tfacc.RefusedDestroy},
			{Config: cfg, Destroy: true, ExpectError: tfacc.RefusedDestroy},
		}, tfacc.Untouched(t, acct, &path, &mark, "status", wire.String("draft")), "stripe_quote"),
	})
}

func TestAccQuote_drift(t *testing.T) {
	acct := acctest.NewAccount(t)
	var id string
	cfg := quoteConfig(acct, oneLine, "  description = \"Package\"\n")
	resource.ParallelTest(t, resource.TestCase{
		ProtoV6ProviderFactories: tfacc.ProviderFactories,
		ErrorCheck:               tfacc.SkipWhenFakeLacks(t, "line_items"),
		Steps: acctest.Forget(t, []resource.TestStep{
			{Config: cfg, Check: idOf(&id)},
			{
				PreConfig: func() {
					tfacc.Post(t, acct, "/v1/quotes/"+id, wire.Object{{Name: "description", Value: wire.String("changed")}})
				},
				Config: cfg,
				ConfigPlanChecks: resource.ConfigPlanChecks{PreApply: []plancheck.PlanCheck{
					plancheck.ExpectNonEmptyPlan(),
					plancheck.ExpectResourceAction(address, plancheck.ResourceActionUpdate),
				}},
				Check: resource.TestCheckResourceAttr(address, "description", "Package"),
			},
		}, nil, "stripe_quote"),
	})
}

// Setting `status` to `canceled` cancels the quote through its action endpoint (suite:
// "cancel"), after which it reads back canceled.
func TestAccQuote_cancel(t *testing.T) {
	acct := acctest.NewAccount(t)
	var path string
	resource.ParallelTest(t, resource.TestCase{
		ProtoV6ProviderFactories: tfacc.ProviderFactories,
		ErrorCheck:               tfacc.SkipWhenFakeLacks(t, "line_items"),
		Steps: acctest.Forget(t, []resource.TestStep{
			{Config: quoteConfig(acct, oneLine, ""), Check: pathOf(&path)},
			{
				Config: quoteConfig(acct, oneLine, "  status = \"canceled\"\n"),
				ConfigPlanChecks: resource.ConfigPlanChecks{PreApply: []plancheck.PlanCheck{
					plancheck.ExpectResourceAction(address, plancheck.ResourceActionUpdate),
				}},
				Check: resource.ComposeTestCheckFunc(
					resource.TestCheckResourceAttr(address, "status", "canceled"),
					tfacc.Holds(t, acct, &path, "status", wire.String("canceled")),
				),
			},
		}, nil, "stripe_quote"),
	})
}

// An accepted quote cannot be cancelled (suite: "cancel once accepted"), so a configuration
// that sets `status` to `canceled` fails at plan time.
func TestAccQuote_cancelAccepted(t *testing.T) {
	acct := acctest.NewAccount(t)
	var id string
	cfg := quoteConfig(acct, oneLine, "")
	resource.ParallelTest(t, resource.TestCase{
		ProtoV6ProviderFactories: tfacc.ProviderFactories,
		ErrorCheck:               tfacc.SkipWhenFakeLacks(t, "line_items"),
		Steps: acctest.Forget(t, []resource.TestStep{
			{Config: cfg, Check: idOf(&id)},
			{
				PreConfig: func() {
					tfacc.Post(t, acct, "/v1/quotes/"+id+"/finalize", nil)
					tfacc.Post(t, acct, "/v1/quotes/"+id+"/accept", nil)
				},
				Config: cfg,
				Check:  resource.TestCheckResourceAttr(address, "status", "accepted"),
			},
			{
				Config:      quoteConfig(acct, oneLine, "  status = \"canceled\"\n"),
				PlanOnly:    true,
				ExpectError: regexp.MustCompile("Cannot change `status` from `accepted`"),
			},
		}, nil, "stripe_quote"),
	})
}
