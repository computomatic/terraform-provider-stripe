package quote_test

import (
	"fmt"
	"os"
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

const address = "stripe_quote.test"

// quoteConfig is a customer, a price and a quote invoiced by email. lines and extra land
// inside the quote.
func quoteConfig(c acctest.Config, lines string, extra string) string {
	return c.ProviderBlock() + fmt.Sprintf(`
resource "stripe_customer" "test" {}

resource "stripe_product" "test" {
  name = "Package"
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
`, lines, extra)
}

const oneLine = `[{ price = stripe_price.test.id, quantity = 1 }]`

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
		Steps: []resource.TestStep{{
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
		}},
	})
}

func TestAccQuote_update(t *testing.T) {
	acct := acctest.NewAccount(t)
	resource.ParallelTest(t, resource.TestCase{
		ProtoV6ProviderFactories: tfacc.ProviderFactories,
		ErrorCheck:               tfacc.SkipWhenFakeLacks(t, "line_items"),
		Steps: []resource.TestStep{
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
		},
	})
}

// from_quote is accepted on create only (suite: "`from_quote` and `test_clock` on update are
// `parameter_unknown`"), so pointing a quote at another quote replaces it.
func TestAccQuote_replace(t *testing.T) {
	acct := acctest.NewAccount(t)
	var first string
	cfg := func(from string) string {
		return quoteConfig(acct, oneLine, "") + fmt.Sprintf(`
resource "stripe_quote" "source_a" {
  customer = stripe_customer.test.id
}

resource "stripe_quote" "source_b" {
  customer = stripe_customer.test.id
}

resource "stripe_quote" "revision" {
  customer = stripe_customer.test.id
  from_quote = {
    quote = stripe_quote.source_%s.id
  }
}
`, from)
	}
	const revision = "stripe_quote.revision"
	resource.ParallelTest(t, resource.TestCase{
		ProtoV6ProviderFactories: tfacc.ProviderFactories,
		ErrorCheck: func(err error) error {
			return tfacc.SkipWhenFakeLacks(t, "from_quote")(tfacc.SkipWhenFakeLacks(t, "line_items")(err))
		},
		Steps: []resource.TestStep{
			{
				Config: cfg("a"),
				Check:  resource.TestCheckResourceAttrWith(revision, "id", func(v string) error { first = v; return nil }),
			},
			{
				Config: cfg("b"),
				ConfigPlanChecks: resource.ConfigPlanChecks{PreApply: []plancheck.PlanCheck{
					plancheck.ExpectResourceAction(revision, plancheck.ResourceActionReplace),
				}},
				Check: resource.TestCheckResourceAttrWith(revision, "id", func(v string) error {
					if v == first {
						return fmt.Errorf("the replacement kept the quote ID %s", v)
					}
					return nil
				}),
			},
		},
	})
}

func TestAccQuote_import(t *testing.T) {
	acct := acctest.NewAccount(t)
	cfg := quoteConfig(acct, `[{ price = stripe_price.test.id, quantity = 2 }]`, "  description = \"Package\"\n  metadata = { team = \"a\" }\n")
	resource.ParallelTest(t, resource.TestCase{
		ProtoV6ProviderFactories: tfacc.ProviderFactories,
		ErrorCheck:               tfacc.SkipWhenFakeLacks(t, "line_items"),
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

// Stripe cannot delete a quote; a draft quote cancels (suite: "Quotes", "Draft and open
// quotes cancel"), so destroy cancels it and the quote stays in the account.
func TestAccQuote_delete(t *testing.T) {
	acct := acctest.NewAccount(t)
	var id string
	resource.ParallelTest(t, resource.TestCase{
		ProtoV6ProviderFactories: tfacc.ProviderFactories,
		ErrorCheck:               tfacc.SkipWhenFakeLacks(t, "line_items"),
		CheckDestroy: func(*terraform.State) error {
			// The test skipped before it created a quote.
			if id == "" {
				return nil
			}
			resp, err := tfacc.Client(acct).Do(t.Context(), stripeapi.Request{Method: "GET", Path: "/v1/quotes/" + id})
			if err != nil {
				return fmt.Errorf("a cancelled quote still retrieves: %w", err)
			}
			if status, _ := resp.Body.(wire.Object).GetString("status"); status != "canceled" {
				return fmt.Errorf("quote %s is %q after destroy, want canceled", id, status)
			}
			return nil
		},
		Steps: []resource.TestStep{{Config: quoteConfig(acct, oneLine, ""), Check: idOf(&id)}},
	})
}

func TestAccQuote_drift(t *testing.T) {
	acct := acctest.NewAccount(t)
	var id string
	cfg := quoteConfig(acct, oneLine, "  description = \"Package\"\n")
	resource.ParallelTest(t, resource.TestCase{
		ProtoV6ProviderFactories: tfacc.ProviderFactories,
		ErrorCheck:               tfacc.SkipWhenFakeLacks(t, "line_items"),
		Steps: []resource.TestStep{
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
		},
	})
}

// An accepted quote cannot be cancelled (suite: `quote_cancellation_already_accepted`) and
// has become an invoice, so destroy leaves it as it is.
func TestAccQuote_deleteAfterAccept(t *testing.T) {
	acct := acctest.NewAccount(t)
	var id string
	cfg := quoteConfig(acct, oneLine, "")
	resource.ParallelTest(t, resource.TestCase{
		ProtoV6ProviderFactories: tfacc.ProviderFactories,
		ErrorCheck:               tfacc.SkipWhenFakeLacks(t, "line_items"),
		Steps: []resource.TestStep{
			{Config: cfg, Check: idOf(&id)},
			{
				PreConfig: func() {
					tfacc.Post(t, acct, "/v1/quotes/"+id+"/finalize", nil)
					tfacc.Post(t, acct, "/v1/quotes/"+id+"/accept", nil)
				},
				Config: cfg,
				// The refresh reads the accepted status; destroy then has nothing to cancel.
				Check: resource.TestCheckResourceAttr(address, "status", "accepted"),
			},
		},
	})
}
