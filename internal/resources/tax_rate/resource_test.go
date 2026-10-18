package tax_rate_test

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

const address = "stripe_tax_rate.test"

// rateConfig is an exclusive tax rate. extra lands inside the resource block.
func rateConfig(c acctest.Config, percentage string, extra string) string {
	return c.ProviderBlock() + fmt.Sprintf(`
resource "stripe_tax_rate" "test" {
  display_name = "VAT"
  inclusive    = false
  percentage   = %s
%s}
`, percentage, extra)
}

func idOf(dst *string) resource.TestCheckFunc {
	return resource.TestCheckResourceAttrWith(address, "id", func(v string) error {
		*dst = v
		return nil
	})
}

func TestAccTaxRate_create(t *testing.T) {
	acct := acctest.NewAccount(t)
	resource.ParallelTest(t, resource.TestCase{
		ProtoV6ProviderFactories: tfacc.ProviderFactories,
		Steps: []resource.TestStep{{
			Config: rateConfig(acct, "20", `
  description  = "UK VAT"
  jurisdiction = "GB"
  country      = "GB"
  tax_type     = "vat"
  metadata     = { team = "a" }
`) + `
data "stripe_tax_rate" "test" {
  id = stripe_tax_rate.test.id
}
`,
			Check: resource.ComposeAggregateTestCheckFunc(
				resource.TestCheckResourceAttrSet(address, "id"),
				resource.TestCheckResourceAttr(address, "display_name", "VAT"),
				resource.TestCheckResourceAttr(address, "percentage", "20"),
				resource.TestCheckResourceAttr(address, "inclusive", "false"),
				resource.TestCheckResourceAttr(address, "active", "true"),
				resource.TestCheckResourceAttr(address, "country", "GB"),
				resource.TestCheckResourceAttr(address, "tax_type", "vat"),
				resource.TestCheckResourceAttr(address, "metadata.team", "a"),
				resource.TestCheckResourceAttrPair("data.stripe_tax_rate.test", "id", address, "id"),
				resource.TestCheckResourceAttr("data.stripe_tax_rate.test", "description", "UK VAT"),
			),
		}},
	})
}

func TestAccTaxRate_update(t *testing.T) {
	acct := acctest.NewAccount(t)
	resource.ParallelTest(t, resource.TestCase{
		ProtoV6ProviderFactories: tfacc.ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: rateConfig(acct, "20", "  description = \"one\"\n  metadata = { team = \"a\", tier = \"1\" }\n"),
				Check:  resource.TestCheckResourceAttr(address, "active", "true"),
			},
			{
				Config: rateConfig(acct, "20", "  description = \"two\"\n  jurisdiction = \"EU\"\n  metadata = { team = \"b\" }\n  active = false\n"),
				ConfigPlanChecks: resource.ConfigPlanChecks{PreApply: []plancheck.PlanCheck{
					plancheck.ExpectResourceAction(address, plancheck.ResourceActionUpdate),
				}},
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr(address, "description", "two"),
					resource.TestCheckResourceAttr(address, "jurisdiction", "EU"),
					resource.TestCheckResourceAttr(address, "active", "false"),
					resource.TestCheckResourceAttr(address, "metadata.%", "1"),
					resource.TestCheckResourceAttr(address, "metadata.team", "b"),
				),
			},
		},
	})
}

// percentage is accepted on create only, so changing it replaces the tax rate.
func TestAccTaxRate_replace(t *testing.T) {
	acct := acctest.NewAccount(t)
	var first string
	resource.ParallelTest(t, resource.TestCase{
		ProtoV6ProviderFactories: tfacc.ProviderFactories,
		Steps: []resource.TestStep{
			{Config: rateConfig(acct, "20", ""), Check: idOf(&first)},
			{
				Config: rateConfig(acct, "21.5", ""),
				ConfigPlanChecks: resource.ConfigPlanChecks{PreApply: []plancheck.PlanCheck{
					plancheck.ExpectResourceAction(address, plancheck.ResourceActionReplace),
				}},
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr(address, "percentage", "21.5"),
					resource.TestCheckResourceAttrWith(address, "id", func(v string) error {
						if v == first {
							return fmt.Errorf("the replacement kept the tax rate ID %s", v)
						}
						return nil
					}),
				),
			},
		},
	})
}

func TestAccTaxRate_import(t *testing.T) {
	acct := acctest.NewAccount(t)
	cfg := rateConfig(acct, "20", "  description = \"UK VAT\"\n  country = \"GB\"\n  metadata = { team = \"a\" }\n")
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

// Stripe cannot delete a tax rate (suite: "Shipping rates, tax rates and promotion codes
// cannot be deleted"), so destroy archives it and the tax rate stays in the account.
func TestAccTaxRate_delete(t *testing.T) {
	acct := acctest.NewAccount(t)
	var id string
	resource.ParallelTest(t, resource.TestCase{
		ProtoV6ProviderFactories: tfacc.ProviderFactories,
		CheckDestroy: func(*terraform.State) error {
			resp, err := tfacc.Client(acct).Do(t.Context(), stripeapi.Request{Method: "GET", Path: "/v1/tax_rates/" + id})
			if err != nil {
				return fmt.Errorf("an archived tax rate still retrieves: %w", err)
			}
			if active, _ := resp.Body.(wire.Object).GetBool("active"); active {
				return fmt.Errorf("tax rate %s should be archived after destroy", id)
			}
			return nil
		},
		Steps: []resource.TestStep{{Config: rateConfig(acct, "20", ""), Check: idOf(&id)}},
	})
}

func TestAccTaxRate_drift(t *testing.T) {
	acct := acctest.NewAccount(t)
	var id string
	cfg := rateConfig(acct, "20", "  description = \"UK VAT\"\n")
	resource.ParallelTest(t, resource.TestCase{
		ProtoV6ProviderFactories: tfacc.ProviderFactories,
		Steps: []resource.TestStep{
			{Config: cfg, Check: idOf(&id)},
			{
				PreConfig: func() {
					tfacc.Post(t, acct, "/v1/tax_rates/"+id, wire.Object{
						{Name: "description", Value: wire.String("Renamed")},
					})
				},
				Config: cfg,
				ConfigPlanChecks: resource.ConfigPlanChecks{PreApply: []plancheck.PlanCheck{
					plancheck.ExpectNonEmptyPlan(),
					plancheck.ExpectResourceAction(address, plancheck.ResourceActionUpdate),
				}},
				Check: resource.TestCheckResourceAttr(address, "description", "UK VAT"),
			},
		},
	})
}
