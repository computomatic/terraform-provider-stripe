package coupon_test

import (
	"fmt"
	"os"
	"regexp"
	"testing"

	"github.com/hashicorp/go-version"
	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/plancheck"
	"github.com/hashicorp/terraform-plugin-testing/terraform"
	"github.com/hashicorp/terraform-plugin-testing/tfversion"

	"github.com/computomatic/terraform-provider-stripe/internal/acctest"
	"github.com/computomatic/terraform-provider-stripe/internal/acctest/tfacc"
	"github.com/computomatic/terraform-provider-stripe/internal/stripeapi"
	"github.com/computomatic/terraform-provider-stripe/internal/wire"
)

func TestMain(m *testing.M) { os.Exit(acctest.Main(m)) }

const address = "stripe_coupon.test"

func couponConfig(c acctest.Config, id string, extra string) string {
	return c.ProviderBlock() + fmt.Sprintf(`
resource "stripe_coupon" "test" {
  id          = %q
  percent_off = 25
  duration    = "once"
%s}
`, c.ID("", id), extra)
}

// named is the extra for couponConfig that sets the coupon's name, then the rest.
func named(c acctest.Config, name, rest string) string {
	return fmt.Sprintf("  name     = %q\n%s", c.Name(name), rest)
}

func TestAccCoupon_create(t *testing.T) {
	acct := acctest.NewAccount(t)
	name := acct.Name("Launch offer")
	resource.ParallelTest(t, resource.TestCase{
		ProtoV6ProviderFactories: tfacc.ProviderFactories,
		ErrorCheck:               tfacc.SkipWhenFakeLacks(t, "expand["),
		Steps: []resource.TestStep{{
			Config: acct.ProviderBlock() + fmt.Sprintf(`
resource "stripe_coupon" "test" {
  name               = %q
  percent_off        = 25
  duration           = "repeating"
  duration_in_months = 3
  max_redemptions    = 10
  metadata           = { team = "a" }
}

data "stripe_coupon" "test" {
  id = stripe_coupon.test.id
}
`, name),
			Check: resource.ComposeAggregateTestCheckFunc(
				resource.TestCheckResourceAttrSet(address, "id"),
				resource.TestCheckResourceAttr(address, "name", name),
				resource.TestCheckResourceAttr(address, "percent_off", "25"),
				resource.TestCheckResourceAttr(address, "duration", "repeating"),
				resource.TestCheckResourceAttr(address, "duration_in_months", "3"),
				resource.TestCheckResourceAttr(address, "valid", "true"),
				resource.TestCheckResourceAttr(address, "times_redeemed", "0"),
				resource.TestCheckResourceAttr(address, "metadata.team", "a"),
				resource.TestCheckNoResourceAttr(address, "amount_off"),
				resource.TestCheckResourceAttrPair("data.stripe_coupon.test", "id", address, "id"),
				resource.TestCheckResourceAttr("data.stripe_coupon.test", "name", name),
				resource.TestCheckResourceAttr("data.stripe_coupon.test", "percent_off", "25"),
			),
		}},
	})
}

func TestAccCoupon_update(t *testing.T) {
	acct := acctest.NewAccount(t)
	resource.ParallelTest(t, resource.TestCase{
		ProtoV6ProviderFactories: tfacc.ProviderFactories,
		ErrorCheck:               tfacc.SkipWhenFakeLacks(t, "expand["),
		Steps: []resource.TestStep{
			{
				Config: couponConfig(acct, "launch", named(acct, "Launch", "  metadata = { team = \"a\", tier = \"1\" }\n")),
				Check:  resource.TestCheckResourceAttr(address, "name", acct.Name("Launch")),
			},
			{
				Config: couponConfig(acct, "launch", named(acct, "Launch v2", "  metadata = { team = \"b\" }\n")),
				ConfigPlanChecks: resource.ConfigPlanChecks{PreApply: []plancheck.PlanCheck{
					plancheck.ExpectResourceAction(address, plancheck.ResourceActionUpdate),
				}},
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr(address, "name", acct.Name("Launch v2")),
					resource.TestCheckResourceAttr(address, "metadata.%", "1"),
					resource.TestCheckResourceAttr(address, "metadata.team", "b"),
				),
			},
		},
	})
}

// percent_off cannot be changed on a coupon, so changing it replaces the coupon. A fractional
// percentage applies without a difference from the plan, which Terraform parses at a higher
// precision than Stripe's answer is read at.
func TestAccCoupon_replace(t *testing.T) {
	acct := acctest.NewAccount(t)
	resource.ParallelTest(t, resource.TestCase{
		ProtoV6ProviderFactories: tfacc.ProviderFactories,
		ErrorCheck:               tfacc.SkipWhenFakeLacks(t, "expand["),
		Steps: []resource.TestStep{
			{Config: couponConfig(acct, "first", "")},
			{
				Config: couponConfig(acct, "second", ""),
				ConfigPlanChecks: resource.ConfigPlanChecks{PreApply: []plancheck.PlanCheck{
					plancheck.ExpectResourceAction(address, plancheck.ResourceActionReplace),
				}},
				Check: resource.TestCheckResourceAttr(address, "id", acct.ID("", "second")),
			},
			fractionalCoupon(acct, "33.3"),
			fractionalCoupon(acct, "0.1"),
		},
	})
}

// fractionalCoupon replaces the coupon with one at a fractional percent_off.
func fractionalCoupon(acct acctest.Config, percent string) resource.TestStep {
	return resource.TestStep{
		Config: acct.ProviderBlock() + fmt.Sprintf(`
resource "stripe_coupon" "test" {
  id          = %q
  percent_off = %s
  duration    = "once"
}
`, acct.ID("", "second"), percent),
		ConfigPlanChecks: resource.ConfigPlanChecks{PreApply: []plancheck.PlanCheck{
			plancheck.ExpectResourceAction(address, plancheck.ResourceActionReplace),
		}},
		Check: resource.TestCheckResourceAttr(address, "percent_off", percent),
	}
}

func TestAccCoupon_import(t *testing.T) {
	acct := acctest.NewAccount(t)
	cfg := couponConfig(acct, "import", named(acct, "Launch", "  metadata = { team = \"a\" }\n"))
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

// The identity form needs Terraform or OpenTofu 1.12.0 or later.
func TestAccCoupon_importIdentity(t *testing.T) {
	acct := acctest.NewAccount(t)
	cfg := couponConfig(acct, "identity", named(acct, "Launch", ""))
	resource.ParallelTest(t, resource.TestCase{
		ProtoV6ProviderFactories: tfacc.ProviderFactories,
		ErrorCheck:               tfacc.SkipWhenFakeLacks(t, "expand["),
		TerraformVersionChecks:   []tfversion.TerraformVersionCheck{tfversion.RequireAbove(version.Must(version.NewVersion("1.12.0")))},
		Steps: []resource.TestStep{
			{Config: cfg},
			{
				ResourceName:    address,
				ImportState:     true,
				ImportStateKind: resource.ImportBlockWithResourceIdentity,
				Config:          cfg,
			},
		},
	})
}

func TestAccCoupon_delete(t *testing.T) {
	acct := acctest.NewAccount(t)
	resource.ParallelTest(t, resource.TestCase{
		ProtoV6ProviderFactories: tfacc.ProviderFactories,
		ErrorCheck:               tfacc.SkipWhenFakeLacks(t, "expand["),
		CheckDestroy: func(*terraform.State) error {
			_, err := tfacc.Client(acct).Do(t.Context(), stripeapi.Request{Method: "GET", Path: "/v1/coupons/" + acct.ID("", "doomed")})
			if !stripeapi.IsNotFound(err) {
				return fmt.Errorf("the coupon should be gone after destroy, got %v", err)
			}
			return nil
		},
		Steps: []resource.TestStep{{Config: couponConfig(acct, "doomed", "")}},
	})
}

func TestAccCoupon_drift(t *testing.T) {
	acct := acctest.NewAccount(t)
	cfg := couponConfig(acct, "drifty", named(acct, "Launch", ""))
	resource.ParallelTest(t, resource.TestCase{
		ProtoV6ProviderFactories: tfacc.ProviderFactories,
		ErrorCheck:               tfacc.SkipWhenFakeLacks(t, "expand["),
		Steps: []resource.TestStep{
			{Config: cfg},
			{
				PreConfig: func() {
					tfacc.Post(t, acct, "/v1/coupons/"+acct.ID("", "drifty"), wire.Object{
						{Name: "name", Value: wire.String("Renamed")},
					})
				},
				Config: cfg,
				ConfigPlanChecks: resource.ConfigPlanChecks{PreApply: []plancheck.PlanCheck{
					plancheck.ExpectNonEmptyPlan(),
					plancheck.ExpectResourceAction(address, plancheck.ResourceActionUpdate),
				}},
				Check: resource.TestCheckResourceAttr(address, "name", acct.Name("Launch")),
			},
		},
	})
}

// applies_to and currency_options are returned only when expanded. The fake does not serve
// them yet, so the test skips on its fake_* reply.
func TestAccCoupon_appliesToAndCurrencyOptions(t *testing.T) {
	acct := acctest.NewAccount(t)
	cfg := acct.ProviderBlock() + fmt.Sprintf(`
resource "stripe_product" "test" {
  name = %q
}

resource "stripe_coupon" "test" {
  amount_off = 500
  currency   = "usd"
  duration   = "once"
  applies_to = {
    products = [stripe_product.test.id]
  }
  currency_options = {
    eur = { amount_off = 450 }
  }
}
`, acct.Name("Gold plan"))
	resource.ParallelTest(t, resource.TestCase{
		ProtoV6ProviderFactories: tfacc.ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: cfg,
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr(address, "applies_to.products.#", "1"),
					resource.TestCheckResourceAttr(address, "currency_options.eur.amount_off", "450"),
				),
			},
			{Config: cfg, PlanOnly: true},
		},
	})
}

// couponOptionsConfig is a coupon with the given currency options.
func couponOptionsConfig(c acctest.Config, options string) string {
	return c.ProviderBlock() + fmt.Sprintf(`
resource "stripe_coupon" "test" {
  name       = %q
  amount_off = 500
  currency   = "usd"
  duration   = "once"
  currency_options = {
%s  }
}
`, c.Name("Options"), options)
}

// Stripe lists the coupon's own currency among the expanded options. It stays out of state,
// so a configuration with only eur applies cleanly and plans empty.
func TestAccCoupon_currencyOptions(t *testing.T) {
	acct := acctest.NewAccount(t)
	cfg := couponOptionsConfig(acct, "    eur = { amount_off = 400 }\n")
	resource.ParallelTest(t, resource.TestCase{
		ProtoV6ProviderFactories: tfacc.ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: cfg,
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr(address, "currency_options.%", "1"),
					resource.TestCheckResourceAttr(address, "currency_options.eur.amount_off", "400"),
				),
			},
			{Config: cfg, PlanOnly: true},
		},
	})
}

func TestAccCoupon_currencyOptionsAddInPlace(t *testing.T) {
	acct := acctest.NewAccount(t)
	var first string
	both := couponOptionsConfig(acct, "    eur = { amount_off = 400 }\n    gbp = { amount_off = 300 }\n")
	resource.ParallelTest(t, resource.TestCase{
		ProtoV6ProviderFactories: tfacc.ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: couponOptionsConfig(acct, "    eur = { amount_off = 400 }\n"),
				Check: resource.TestCheckResourceAttrWith(address, "id", func(v string) error {
					first = v
					return nil
				}),
			},
			{
				Config: both,
				ConfigPlanChecks: resource.ConfigPlanChecks{PreApply: []plancheck.PlanCheck{
					plancheck.ExpectResourceAction(address, plancheck.ResourceActionUpdate),
				}},
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr(address, "currency_options.%", "2"),
					resource.TestCheckResourceAttr(address, "currency_options.gbp.amount_off", "300"),
					resource.TestCheckResourceAttrWith(address, "id", func(v string) error {
						if v != first {
							return fmt.Errorf("adding a currency replaced the coupon: %s became %s", first, v)
						}
						return nil
					}),
				),
			},
			{Config: both, PlanOnly: true},
		},
	})
}

func TestAccCoupon_currencyOptionsChangeReplaces(t *testing.T) {
	acct := acctest.NewAccount(t)
	resource.ParallelTest(t, resource.TestCase{
		ProtoV6ProviderFactories: tfacc.ProviderFactories,
		Steps: []resource.TestStep{
			{Config: couponOptionsConfig(acct, "    eur = { amount_off = 400 }\n    gbp = { amount_off = 300 }\n")},
			{
				Config: couponOptionsConfig(acct, "    eur = { amount_off = 450 }\n    gbp = { amount_off = 300 }\n"),
				ConfigPlanChecks: resource.ConfigPlanChecks{PreApply: []plancheck.PlanCheck{
					plancheck.ExpectResourceAction(address, plancheck.ResourceActionReplace),
				}},
				Check: resource.TestCheckResourceAttr(address, "currency_options.eur.amount_off", "450"),
			},
			// Stripe's suite does not prove removing an option, so it replaces as well.
			{
				Config: couponOptionsConfig(acct, "    eur = { amount_off = 450 }\n"),
				ConfigPlanChecks: resource.ConfigPlanChecks{PreApply: []plancheck.PlanCheck{
					plancheck.ExpectResourceAction(address, plancheck.ResourceActionReplace),
				}},
				Check: resource.TestCheckResourceAttr(address, "currency_options.%", "1"),
			},
		},
	})
}

func TestAccCoupon_currencyOptionsRejectOwnCurrency(t *testing.T) {
	acct := acctest.NewAccount(t)
	resource.ParallelTest(t, resource.TestCase{
		ProtoV6ProviderFactories: tfacc.ProviderFactories,
		Steps: []resource.TestStep{{
			Config:      couponOptionsConfig(acct, "    usd = { amount_off = 400 }\n"),
			PlanOnly:    true,
			ExpectError: regexp.MustCompile(`(?s)Own currency in currency options.*currency of this object`),
		}},
	})
}
