package product_catalog_trial_offer_test

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

const address = "stripe_product_catalog_trial_offer.test"

// offerConfig is a product with a trial price and a transition price, and a trial offer on
// them. extra lands inside the trial offer.
func offerConfig(c acctest.Config, iterations int, extra string) string {
	return c.ProviderBlock() + fmt.Sprintf(`
resource "stripe_product" "test" {
  name = %q
}

resource "stripe_price" "trial" {
  product     = stripe_product.test.id
  currency    = "usd"
  unit_amount = 100
  recurring = {
    interval = "month"
  }
}

resource "stripe_price" "full" {
  product     = stripe_product.test.id
  currency    = "usd"
  unit_amount = 1000
  recurring = {
    interval = "month"
  }
}

resource "stripe_product_catalog_trial_offer" "test" {
  price = stripe_price.trial.id
  duration = {
    type     = "relative"
    relative = { iterations = %d }
  }
  end_behavior = {
    transition = { price = stripe_price.full.id }
  }
%s}
`, c.Name("Gold plan"), iterations, extra)
}

func idOf(dst *string) resource.TestCheckFunc {
	return resource.TestCheckResourceAttrWith(address, "id", func(v string) error {
		*dst = v
		return nil
	})
}

func TestAccProductCatalogTrialOffer_create(t *testing.T) {
	acct := acctest.NewAccount(t)
	resource.ParallelTest(t, resource.TestCase{
		ProtoV6ProviderFactories: tfacc.ProviderFactories,
		Steps: []resource.TestStep{{
			Config: offerConfig(acct, 2, "  nickname = \"Two months\"\n") + `
data "stripe_product_catalog_trial_offer" "test" {
  id = stripe_product_catalog_trial_offer.test.id
}
`,
			Check: resource.ComposeAggregateTestCheckFunc(
				resource.TestCheckResourceAttrSet(address, "id"),
				resource.TestCheckResourceAttrPair(address, "price", "stripe_price.trial", "id"),
				resource.TestCheckResourceAttr(address, "active", "true"),
				resource.TestCheckResourceAttr(address, "nickname", "Two months"),
				resource.TestCheckResourceAttr(address, "duration.type", "relative"),
				resource.TestCheckResourceAttr(address, "duration.relative.iterations", "2"),
				resource.TestCheckResourceAttr(address, "end_behavior.type", "transition"),
				resource.TestCheckResourceAttrPair(address, "end_behavior.transition.price", "stripe_price.full", "id"),
				resource.TestCheckResourceAttrPair("data.stripe_product_catalog_trial_offer.test", "id", address, "id"),
				resource.TestCheckResourceAttr("data.stripe_product_catalog_trial_offer.test", "duration.relative.iterations", "2"),
			),
		}},
	})
}

// Only `active` updates in place; Stripe toggles it both ways.
func TestAccProductCatalogTrialOffer_update(t *testing.T) {
	acct := acctest.NewAccount(t)
	resource.ParallelTest(t, resource.TestCase{
		ProtoV6ProviderFactories: tfacc.ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: offerConfig(acct, 1, ""),
				Check:  resource.TestCheckResourceAttr(address, "active", "true"),
			},
			{
				Config: offerConfig(acct, 1, "  active = false\n"),
				ConfigPlanChecks: resource.ConfigPlanChecks{PreApply: []plancheck.PlanCheck{
					plancheck.ExpectResourceAction(address, plancheck.ResourceActionUpdate),
				}},
				Check: resource.TestCheckResourceAttr(address, "active", "false"),
			},
			{
				Config: offerConfig(acct, 1, "  active = true\n"),
				ConfigPlanChecks: resource.ConfigPlanChecks{PreApply: []plancheck.PlanCheck{
					plancheck.ExpectResourceAction(address, plancheck.ResourceActionUpdate),
				}},
				Check: resource.TestCheckResourceAttr(address, "active", "true"),
			},
		},
	})
}

// duration is accepted on create only (Stripe answers `parameter_unknown` to an update),
// so changing the iterations replaces the trial offer.
func TestAccProductCatalogTrialOffer_replace(t *testing.T) {
	acct := acctest.NewAccount(t)
	var first string
	resource.ParallelTest(t, resource.TestCase{
		ProtoV6ProviderFactories: tfacc.ProviderFactories,
		Steps: []resource.TestStep{
			{Config: offerConfig(acct, 1, ""), Check: idOf(&first)},
			{
				Config: offerConfig(acct, 3, ""),
				ConfigPlanChecks: resource.ConfigPlanChecks{PreApply: []plancheck.PlanCheck{
					plancheck.ExpectResourceAction(address, plancheck.ResourceActionReplace),
				}},
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr(address, "duration.relative.iterations", "3"),
					resource.TestCheckResourceAttrWith(address, "id", func(v string) error {
						if v == first {
							return fmt.Errorf("the replacement kept the trial offer ID %s", v)
						}
						return nil
					}),
				),
			},
		},
	})
}

func TestAccProductCatalogTrialOffer_import(t *testing.T) {
	acct := acctest.NewAccount(t)
	cfg := offerConfig(acct, 2, "  nickname = \"Two months\"\n")
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

// Stripe cannot delete a trial offer (suite: product_catalog_trial_offer_test.go, "cannot
// be deleted"), so destroy archives it and the trial offer stays in the account.
func TestAccProductCatalogTrialOffer_delete(t *testing.T) {
	acct := acctest.NewAccount(t)
	var id string
	resource.ParallelTest(t, resource.TestCase{
		ProtoV6ProviderFactories: tfacc.ProviderFactories,
		CheckDestroy: func(*terraform.State) error {
			resp, err := tfacc.Client(acct).Do(t.Context(), stripeapi.Request{Method: "GET", Path: "/v1/product_catalog/trial_offers/" + id})
			if err != nil {
				return fmt.Errorf("an archived trial offer still retrieves: %w", err)
			}
			if active, _ := resp.Body.(wire.Object).GetBool("active"); active {
				return fmt.Errorf("trial offer %s should be archived after destroy", id)
			}
			return nil
		},
		Steps: []resource.TestStep{{Config: offerConfig(acct, 1, ""), Check: idOf(&id)}},
	})
}

// Out of band, someone archives the trial offer; the plan sets it active again.
func TestAccProductCatalogTrialOffer_drift(t *testing.T) {
	acct := acctest.NewAccount(t)
	var id string
	cfg := offerConfig(acct, 1, "  active = true\n")
	resource.ParallelTest(t, resource.TestCase{
		ProtoV6ProviderFactories: tfacc.ProviderFactories,
		Steps: []resource.TestStep{
			{Config: cfg, Check: idOf(&id)},
			{
				PreConfig: func() {
					tfacc.Post(t, acct, "/v1/product_catalog/trial_offers/"+id, wire.Object{
						{Name: "active", Value: wire.Bool(false)},
					})
				},
				Config: cfg,
				ConfigPlanChecks: resource.ConfigPlanChecks{PreApply: []plancheck.PlanCheck{
					plancheck.ExpectNonEmptyPlan(),
					plancheck.ExpectResourceAction(address, plancheck.ResourceActionUpdate),
				}},
				Check: resource.TestCheckResourceAttr(address, "active", "true"),
			},
		},
	})
}
