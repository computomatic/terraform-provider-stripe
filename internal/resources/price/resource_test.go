package price_test

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

const address = "stripe_price.test"

// priceConfig is a product and a recurring price on it. extra lands inside the price block.
func priceConfig(c acctest.Config, amount int, extra string) string {
	return c.ProviderBlock() + fmt.Sprintf(`
resource "stripe_product" "test" {
  name = "Gold plan"
}

resource "stripe_price" "test" {
  product     = stripe_product.test.id
  currency    = "usd"
  unit_amount = %d
  recurring = {
    interval = "month"
  }
%s}
`, amount, extra)
}

func idOf(dst *string) resource.TestCheckFunc {
	return resource.TestCheckResourceAttrWith(address, "id", func(v string) error {
		*dst = v
		return nil
	})
}

func TestAccPrice_create(t *testing.T) {
	acct := acctest.NewAccount(t)
	resource.ParallelTest(t, resource.TestCase{
		ProtoV6ProviderFactories: tfacc.ProviderFactories,
		ErrorCheck:               tfacc.SkipWhenFakeLacks(t, "expand["),
		Steps: []resource.TestStep{{
			Config: priceConfig(acct, 2000, "  nickname   = \"Monthly\"\n  lookup_key = \"gold_monthly\"\n  metadata   = { team = \"a\" }\n") + `
data "stripe_price" "by_id" {
  id = stripe_price.test.id
}

data "stripe_price" "by_lookup_key" {
  lookup_key = stripe_price.test.lookup_key
}
`,
			Check: resource.ComposeAggregateTestCheckFunc(
				resource.TestCheckResourceAttrSet(address, "id"),
				resource.TestCheckResourceAttrPair(address, "product", "stripe_product.test", "id"),
				resource.TestCheckResourceAttr(address, "unit_amount", "2000"),
				resource.TestCheckResourceAttr(address, "currency", "usd"),
				resource.TestCheckResourceAttr(address, "active", "true"),
				resource.TestCheckResourceAttr(address, "type", "recurring"),
				resource.TestCheckResourceAttr(address, "recurring.interval", "month"),
				resource.TestCheckResourceAttr(address, "metadata.team", "a"),
				resource.TestCheckResourceAttrPair("data.stripe_price.by_id", "id", address, "id"),
				resource.TestCheckResourceAttr("data.stripe_price.by_id", "nickname", "Monthly"),
				resource.TestCheckResourceAttrPair("data.stripe_price.by_lookup_key", "id", address, "id"),
				resource.TestCheckResourceAttr("data.stripe_price.by_lookup_key", "unit_amount", "2000"),
			),
		}},
	})
}

func TestAccPrice_update(t *testing.T) {
	acct := acctest.NewAccount(t)
	resource.ParallelTest(t, resource.TestCase{
		ProtoV6ProviderFactories: tfacc.ProviderFactories,
		ErrorCheck:               tfacc.SkipWhenFakeLacks(t, "expand["),
		Steps: []resource.TestStep{
			{
				Config: priceConfig(acct, 2000, "  nickname = \"Monthly\"\n  metadata = { team = \"a\", tier = \"1\" }\n"),
				Check:  resource.TestCheckResourceAttr(address, "nickname", "Monthly"),
			},
			{
				Config: priceConfig(acct, 2000, "  nickname     = \"Monthly v2\"\n  metadata     = { team = \"b\" }\n  tax_behavior = \"exclusive\"\n"),
				ConfigPlanChecks: resource.ConfigPlanChecks{PreApply: []plancheck.PlanCheck{
					plancheck.ExpectResourceAction(address, plancheck.ResourceActionUpdate),
				}},
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr(address, "nickname", "Monthly v2"),
					resource.TestCheckResourceAttr(address, "tax_behavior", "exclusive"),
					resource.TestCheckResourceAttr(address, "metadata.%", "1"),
					resource.TestCheckResourceAttr(address, "metadata.team", "b"),
				),
			},
		},
	})
}

func TestAccPrice_replace(t *testing.T) {
	acct := acctest.NewAccount(t)
	var first string
	resource.ParallelTest(t, resource.TestCase{
		ProtoV6ProviderFactories: tfacc.ProviderFactories,
		ErrorCheck:               tfacc.SkipWhenFakeLacks(t, "expand["),
		Steps: []resource.TestStep{
			{Config: priceConfig(acct, 2000, ""), Check: idOf(&first)},
			{
				Config: priceConfig(acct, 2500, ""),
				ConfigPlanChecks: resource.ConfigPlanChecks{PreApply: []plancheck.PlanCheck{
					plancheck.ExpectResourceAction(address, plancheck.ResourceActionReplace),
				}},
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr(address, "unit_amount", "2500"),
					resource.TestCheckResourceAttrWith(address, "id", func(v string) error {
						if v == first {
							return fmt.Errorf("the replacement kept the price ID %s", v)
						}
						return nil
					}),
				),
			},
		},
	})
}

func TestAccPrice_import(t *testing.T) {
	acct := acctest.NewAccount(t)
	cfg := priceConfig(acct, 2000, "  nickname   = \"Monthly\"\n  lookup_key = \"gold_monthly\"\n  metadata   = { team = \"a\" }\n")
	resource.ParallelTest(t, resource.TestCase{
		ProtoV6ProviderFactories: tfacc.ProviderFactories,
		ErrorCheck:               tfacc.SkipWhenFakeLacks(t, "expand["),
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

// Stripe cannot delete a price, so destroy archives it and the price stays in the account.
func TestAccPrice_delete(t *testing.T) {
	acct := acctest.NewAccount(t)
	var id string
	resource.ParallelTest(t, resource.TestCase{
		ProtoV6ProviderFactories: tfacc.ProviderFactories,
		ErrorCheck:               tfacc.SkipWhenFakeLacks(t, "expand["),
		CheckDestroy: func(*terraform.State) error {
			if id == "" {
				return nil // the test skipped before creating the price
			}
			resp, err := tfacc.Client(acct).Do(t.Context(), stripeapi.Request{Method: "GET", Path: "/v1/prices/" + id})
			if err != nil {
				return fmt.Errorf("an archived price still retrieves: %w", err)
			}
			if active, _ := resp.Body.(wire.Object).GetBool("active"); active {
				return fmt.Errorf("price %s should be archived after destroy", id)
			}
			return nil
		},
		Steps: []resource.TestStep{{
			Config: priceConfig(acct, 2000, ""),
			Check:  idOf(&id),
		}},
	})
}

func TestAccPrice_drift(t *testing.T) {
	acct := acctest.NewAccount(t)
	var id string
	cfg := priceConfig(acct, 2000, "  nickname = \"Monthly\"\n")
	resource.ParallelTest(t, resource.TestCase{
		ProtoV6ProviderFactories: tfacc.ProviderFactories,
		ErrorCheck:               tfacc.SkipWhenFakeLacks(t, "expand["),
		Steps: []resource.TestStep{
			{Config: cfg, Check: idOf(&id)},
			{
				PreConfig: func() {
					tfacc.Post(t, acct, "/v1/prices/"+id, wire.Object{
						{Name: "nickname", Value: wire.String("Renamed")},
					})
				},
				Config: cfg,
				ConfigPlanChecks: resource.ConfigPlanChecks{PreApply: []plancheck.PlanCheck{
					plancheck.ExpectNonEmptyPlan(),
					plancheck.ExpectResourceAction(address, plancheck.ResourceActionUpdate),
				}},
				Check: resource.TestCheckResourceAttr(address, "nickname", "Monthly"),
			},
		},
	})
}

// Tiers and currency options are returned only when expanded. Stripe returns the unbounded
// last tier's `up_to` as null, which must read back as the configured `inf`. The fake does
// not serve tiers yet, so the test skips on its fake_* reply.
func TestAccPrice_tiersAndCurrencyOptions(t *testing.T) {
	acct := acctest.NewAccount(t)
	cfg := acct.ProviderBlock() + `
resource "stripe_product" "test" {
  name = "Metered plan"
}

resource "stripe_price" "test" {
  product       = stripe_product.test.id
  currency      = "usd"
  billing_scheme = "tiered"
  tiers_mode    = "graduated"
  tiers = [
    { up_to = "100", unit_amount = 1000 },
    { up_to = "inf", unit_amount = 500 },
  ]
  currency_options = {
    eur = {
      tiers = [
        { up_to = "100", unit_amount = 900 },
        { up_to = "inf", unit_amount = 450 },
      ]
    }
  }
  recurring = {
    interval = "month"
  }
}
`
	resource.ParallelTest(t, resource.TestCase{
		ProtoV6ProviderFactories: tfacc.ProviderFactories,
		ErrorCheck:               tfacc.SkipWhenFakeLacks(t, ""),
		Steps: []resource.TestStep{
			{
				Config: cfg,
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr(address, "tiers.#", "2"),
					resource.TestCheckResourceAttr(address, "tiers.0.up_to", "100"),
					resource.TestCheckResourceAttr(address, "tiers.1.up_to", "inf"),
					resource.TestCheckResourceAttr(address, "currency_options.eur.tiers.1.up_to", "inf"),
				),
			},
			{Config: cfg, PlanOnly: true},
		},
	})
}
