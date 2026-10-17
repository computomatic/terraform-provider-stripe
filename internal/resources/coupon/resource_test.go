package coupon_test

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

const address = "stripe_coupon.test"

func couponConfig(c acctest.Config, id string, extra string) string {
	return c.ProviderBlock() + fmt.Sprintf(`
resource "stripe_coupon" "test" {
  id          = %q
  percent_off = 25
  duration    = "once"
%s}
`, id, extra)
}

func TestAccCoupon_create(t *testing.T) {
	acct := acctest.NewAccount(t)
	resource.ParallelTest(t, resource.TestCase{
		ProtoV6ProviderFactories: tfacc.ProviderFactories,
		ErrorCheck:               tfacc.SkipWhenFakeLacks(t, "expand["),
		Steps: []resource.TestStep{{
			Config: acct.ProviderBlock() + `
resource "stripe_coupon" "test" {
  name               = "Launch offer"
  percent_off        = 25
  duration           = "repeating"
  duration_in_months = 3
  max_redemptions    = 10
  metadata           = { team = "a" }
}

data "stripe_coupon" "test" {
  id = stripe_coupon.test.id
}
`,
			Check: resource.ComposeAggregateTestCheckFunc(
				resource.TestCheckResourceAttrSet(address, "id"),
				resource.TestCheckResourceAttr(address, "name", "Launch offer"),
				resource.TestCheckResourceAttr(address, "percent_off", "25"),
				resource.TestCheckResourceAttr(address, "duration", "repeating"),
				resource.TestCheckResourceAttr(address, "duration_in_months", "3"),
				resource.TestCheckResourceAttr(address, "valid", "true"),
				resource.TestCheckResourceAttr(address, "times_redeemed", "0"),
				resource.TestCheckResourceAttr(address, "metadata.team", "a"),
				resource.TestCheckNoResourceAttr(address, "amount_off"),
				resource.TestCheckResourceAttrPair("data.stripe_coupon.test", "id", address, "id"),
				resource.TestCheckResourceAttr("data.stripe_coupon.test", "name", "Launch offer"),
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
				Config: couponConfig(acct, "launch", "  name     = \"Launch\"\n  metadata = { team = \"a\", tier = \"1\" }\n"),
				Check:  resource.TestCheckResourceAttr(address, "name", "Launch"),
			},
			{
				Config: couponConfig(acct, "launch", "  name     = \"Launch v2\"\n  metadata = { team = \"b\" }\n"),
				ConfigPlanChecks: resource.ConfigPlanChecks{PreApply: []plancheck.PlanCheck{
					plancheck.ExpectResourceAction(address, plancheck.ResourceActionUpdate),
				}},
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr(address, "name", "Launch v2"),
					resource.TestCheckResourceAttr(address, "metadata.%", "1"),
					resource.TestCheckResourceAttr(address, "metadata.team", "b"),
				),
			},
		},
	})
}

// percent_off cannot be changed on a coupon, so changing it replaces the coupon.
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
				Check: resource.TestCheckResourceAttr(address, "id", "second"),
			},
			{
				Config: acct.ProviderBlock() + `
resource "stripe_coupon" "test" {
  id          = "second"
  percent_off = 50
  duration    = "once"
}
`,
				ConfigPlanChecks: resource.ConfigPlanChecks{PreApply: []plancheck.PlanCheck{
					plancheck.ExpectResourceAction(address, plancheck.ResourceActionReplace),
				}},
				Check: resource.TestCheckResourceAttr(address, "percent_off", "50"),
			},
		},
	})
}

func TestAccCoupon_import(t *testing.T) {
	acct := acctest.NewAccount(t)
	cfg := couponConfig(acct, "launch", "  name     = \"Launch\"\n  metadata = { team = \"a\" }\n")
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

func TestAccCoupon_delete(t *testing.T) {
	acct := acctest.NewAccount(t)
	resource.ParallelTest(t, resource.TestCase{
		ProtoV6ProviderFactories: tfacc.ProviderFactories,
		ErrorCheck:               tfacc.SkipWhenFakeLacks(t, "expand["),
		CheckDestroy: func(*terraform.State) error {
			_, err := tfacc.Client(acct).Do(t.Context(), stripeapi.Request{Method: "GET", Path: "/v1/coupons/doomed"})
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
	cfg := couponConfig(acct, "drifty", "  name = \"Launch\"\n")
	resource.ParallelTest(t, resource.TestCase{
		ProtoV6ProviderFactories: tfacc.ProviderFactories,
		ErrorCheck:               tfacc.SkipWhenFakeLacks(t, "expand["),
		Steps: []resource.TestStep{
			{Config: cfg},
			{
				PreConfig: func() {
					tfacc.Post(t, acct, "/v1/coupons/drifty", wire.Object{
						{Name: "name", Value: wire.String("Renamed")},
					})
				},
				Config: cfg,
				ConfigPlanChecks: resource.ConfigPlanChecks{PreApply: []plancheck.PlanCheck{
					plancheck.ExpectNonEmptyPlan(),
					plancheck.ExpectResourceAction(address, plancheck.ResourceActionUpdate),
				}},
				Check: resource.TestCheckResourceAttr(address, "name", "Launch"),
			},
		},
	})
}

// applies_to and currency_options are returned only when expanded. The fake does not serve
// them yet, so the test skips on its fake_* reply.
func TestAccCoupon_appliesToAndCurrencyOptions(t *testing.T) {
	acct := acctest.NewAccount(t)
	cfg := acct.ProviderBlock() + `
resource "stripe_product" "test" {
  name = "Gold plan"
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
`
	resource.ParallelTest(t, resource.TestCase{
		ProtoV6ProviderFactories: tfacc.ProviderFactories,
		ErrorCheck:               tfacc.SkipWhenFakeLacks(t, ""),
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
