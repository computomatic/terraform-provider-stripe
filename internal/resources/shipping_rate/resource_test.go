package shipping_rate_test

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

const address = "stripe_shipping_rate.test"

// rateConfig is a fixed-amount shipping rate named c.Name(name). extra lands inside the
// resource block.
func rateConfig(c acctest.Config, name string, extra string) string {
	return c.ProviderBlock() + fmt.Sprintf(`
resource "stripe_shipping_rate" "test" {
  display_name = %q
  type         = "fixed_amount"
  fixed_amount = {
    amount   = 500
    currency = "usd"
  }
%s}
`, c.Name(name), extra)
}

func idOf(dst *string) resource.TestCheckFunc {
	return resource.TestCheckResourceAttrWith(address, "id", func(v string) error {
		*dst = v
		return nil
	})
}

func TestAccShippingRate_create(t *testing.T) {
	acct := acctest.NewAccount(t)
	resource.ParallelTest(t, resource.TestCase{
		ProtoV6ProviderFactories: tfacc.ProviderFactories,
		Steps: []resource.TestStep{{
			Config: rateConfig(acct, "Ground", `
  tax_behavior = "exclusive"
  metadata     = { team = "a" }
  delivery_estimate = {
    minimum = { unit = "business_day", value = 3 }
    maximum = { unit = "business_day", value = 5 }
  }
`) + `
data "stripe_shipping_rate" "test" {
  id = stripe_shipping_rate.test.id
}
`,
			Check: resource.ComposeAggregateTestCheckFunc(
				resource.TestCheckResourceAttrSet(address, "id"),
				resource.TestCheckResourceAttr(address, "display_name", acct.Name("Ground")),
				resource.TestCheckResourceAttr(address, "type", "fixed_amount"),
				resource.TestCheckResourceAttr(address, "active", "true"),
				resource.TestCheckResourceAttr(address, "fixed_amount.amount", "500"),
				resource.TestCheckResourceAttr(address, "fixed_amount.currency", "usd"),
				resource.TestCheckResourceAttr(address, "tax_behavior", "exclusive"),
				resource.TestCheckResourceAttr(address, "delivery_estimate.maximum.value", "5"),
				resource.TestCheckResourceAttr(address, "metadata.team", "a"),
				resource.TestCheckResourceAttrPair("data.stripe_shipping_rate.test", "id", address, "id"),
				resource.TestCheckResourceAttr("data.stripe_shipping_rate.test", "display_name", acct.Name("Ground")),
				resource.TestCheckResourceAttr("data.stripe_shipping_rate.test", "fixed_amount.amount", "500"),
			),
		}},
	})
}

func TestAccShippingRate_update(t *testing.T) {
	acct := acctest.NewAccount(t)
	resource.ParallelTest(t, resource.TestCase{
		ProtoV6ProviderFactories: tfacc.ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: rateConfig(acct, "Ground", "  metadata = { team = \"a\", tier = \"1\" }\n"),
				Check:  resource.TestCheckResourceAttr(address, "tax_behavior", "unspecified"),
			},
			{
				Config: rateConfig(acct, "Ground", "  metadata = { team = \"b\" }\n  tax_behavior = \"inclusive\"\n  active = false\n"),
				ConfigPlanChecks: resource.ConfigPlanChecks{PreApply: []plancheck.PlanCheck{
					plancheck.ExpectResourceAction(address, plancheck.ResourceActionUpdate),
				}},
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr(address, "tax_behavior", "inclusive"),
					resource.TestCheckResourceAttr(address, "active", "false"),
					resource.TestCheckResourceAttr(address, "metadata.%", "1"),
					resource.TestCheckResourceAttr(address, "metadata.team", "b"),
				),
			},
		},
	})
}

// display_name is accepted on create only, so changing it replaces the shipping rate.
func TestAccShippingRate_replace(t *testing.T) {
	acct := acctest.NewAccount(t)
	var first string
	resource.ParallelTest(t, resource.TestCase{
		ProtoV6ProviderFactories: tfacc.ProviderFactories,
		Steps: []resource.TestStep{
			{Config: rateConfig(acct, "Ground", ""), Check: idOf(&first)},
			{
				Config: rateConfig(acct, "Express", ""),
				ConfigPlanChecks: resource.ConfigPlanChecks{PreApply: []plancheck.PlanCheck{
					plancheck.ExpectResourceAction(address, plancheck.ResourceActionReplace),
				}},
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr(address, "display_name", acct.Name("Express")),
					resource.TestCheckResourceAttrWith(address, "id", func(v string) error {
						if v == first {
							return fmt.Errorf("the replacement kept the shipping rate ID %s", v)
						}
						return nil
					}),
				),
			},
		},
	})
}

func TestAccShippingRate_import(t *testing.T) {
	acct := acctest.NewAccount(t)
	cfg := rateConfig(acct, "Ground", "  tax_behavior = \"exclusive\"\n  metadata = { team = \"a\" }\n")
	resource.ParallelTest(t, resource.TestCase{
		ProtoV6ProviderFactories: tfacc.ProviderFactories,
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

// Stripe cannot delete a shipping rate (suite: "Shipping rates, tax rates and promotion
// codes cannot be deleted"), so destroy archives it and the shipping rate stays in the account.
func TestAccShippingRate_delete(t *testing.T) {
	acct := acctest.NewAccount(t)
	var id string
	resource.ParallelTest(t, resource.TestCase{
		ProtoV6ProviderFactories: tfacc.ProviderFactories,
		CheckDestroy: func(*terraform.State) error {
			resp, err := tfacc.Client(acct).Do(t.Context(), stripeapi.Request{Method: "GET", Path: "/v1/shipping_rates/" + id})
			if err != nil {
				return fmt.Errorf("an archived shipping rate still retrieves: %w", err)
			}
			if active, _ := resp.Body.(wire.Object).GetBool("active"); active {
				return fmt.Errorf("shipping rate %s should be archived after destroy", id)
			}
			return nil
		},
		Steps: []resource.TestStep{{Config: rateConfig(acct, "Ground", ""), Check: idOf(&id)}},
	})
}

func TestAccShippingRate_drift(t *testing.T) {
	acct := acctest.NewAccount(t)
	var id string
	cfg := rateConfig(acct, "Ground", "  tax_behavior = \"exclusive\"\n")
	resource.ParallelTest(t, resource.TestCase{
		ProtoV6ProviderFactories: tfacc.ProviderFactories,
		Steps: []resource.TestStep{
			{Config: cfg, Check: idOf(&id)},
			{
				PreConfig: func() {
					tfacc.Post(t, acct, "/v1/shipping_rates/"+id, wire.Object{
						{Name: "tax_behavior", Value: wire.String("inclusive")},
					})
				},
				Config: cfg,
				ConfigPlanChecks: resource.ConfigPlanChecks{PreApply: []plancheck.PlanCheck{
					plancheck.ExpectNonEmptyPlan(),
					plancheck.ExpectResourceAction(address, plancheck.ResourceActionUpdate),
				}},
				Check: resource.TestCheckResourceAttr(address, "tax_behavior", "exclusive"),
			},
		},
	})
}
