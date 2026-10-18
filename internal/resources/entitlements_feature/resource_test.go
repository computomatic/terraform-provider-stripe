package entitlements_feature_test

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

const address = "stripe_entitlements_feature.test"

// featureConfig is a feature. extra lands inside the resource block.
func featureConfig(c acctest.Config, key, name, extra string) string {
	return c.ProviderBlock() + fmt.Sprintf(`
resource "stripe_entitlements_feature" "test" {
  lookup_key = %q
  name       = %q
%s}
`, key, name, extra)
}

func idOf(dst *string) resource.TestCheckFunc {
	return resource.TestCheckResourceAttrWith(address, "id", func(v string) error {
		*dst = v
		return nil
	})
}

func TestAccEntitlementsFeature_create(t *testing.T) {
	acct := acctest.NewAccount(t)
	resource.ParallelTest(t, resource.TestCase{
		ProtoV6ProviderFactories: tfacc.ProviderFactories,
		Steps: []resource.TestStep{{
			Config: featureConfig(acct, "advanced_search", "Advanced search", "  metadata = { team = \"a\" }\n") + `
data "stripe_entitlements_feature" "by_id" {
  id = stripe_entitlements_feature.test.id
}

data "stripe_entitlements_feature" "by_lookup_key" {
  lookup_key = stripe_entitlements_feature.test.lookup_key
}
`,
			Check: resource.ComposeAggregateTestCheckFunc(
				resource.TestCheckResourceAttrSet(address, "id"),
				resource.TestCheckResourceAttr(address, "lookup_key", "advanced_search"),
				resource.TestCheckResourceAttr(address, "name", "Advanced search"),
				resource.TestCheckResourceAttr(address, "active", "true"),
				resource.TestCheckResourceAttr(address, "metadata.team", "a"),
				resource.TestCheckResourceAttrPair("data.stripe_entitlements_feature.by_id", "id", address, "id"),
				resource.TestCheckResourceAttr("data.stripe_entitlements_feature.by_id", "name", "Advanced search"),
				resource.TestCheckResourceAttrPair("data.stripe_entitlements_feature.by_lookup_key", "id", address, "id"),
			),
		}},
	})
}

// The last step archives the feature in place. An archived feature is frozen, so nothing
// updates it after that, and destroy finds it already archived.
func TestAccEntitlementsFeature_update(t *testing.T) {
	acct := acctest.NewAccount(t)
	resource.ParallelTest(t, resource.TestCase{
		ProtoV6ProviderFactories: tfacc.ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: featureConfig(acct, "advanced_search", "Advanced search", "  metadata = { team = \"a\", tier = \"1\" }\n"),
				Check:  resource.TestCheckResourceAttr(address, "name", "Advanced search"),
			},
			{
				Config: featureConfig(acct, "advanced_search", "Search", "  metadata = { team = \"b\" }\n"),
				ConfigPlanChecks: resource.ConfigPlanChecks{PreApply: []plancheck.PlanCheck{
					plancheck.ExpectResourceAction(address, plancheck.ResourceActionUpdate),
				}},
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr(address, "name", "Search"),
					resource.TestCheckResourceAttr(address, "metadata.%", "1"),
					resource.TestCheckResourceAttr(address, "metadata.team", "b"),
				),
			},
			{
				Config: featureConfig(acct, "advanced_search", "Search", "  metadata = { team = \"b\" }\n  active = false\n"),
				Check:  resource.TestCheckResourceAttr(address, "active", "false"),
			},
		},
	})
}

// lookup_key is accepted on create only, so changing it replaces the feature. The archived
// feature keeps its key, which is why the replacement needs a new one.
func TestAccEntitlementsFeature_replace(t *testing.T) {
	acct := acctest.NewAccount(t)
	var first string
	resource.ParallelTest(t, resource.TestCase{
		ProtoV6ProviderFactories: tfacc.ProviderFactories,
		Steps: []resource.TestStep{
			{Config: featureConfig(acct, "advanced_search", "Search", ""), Check: idOf(&first)},
			{
				Config: featureConfig(acct, "search_v2", "Search", ""),
				ConfigPlanChecks: resource.ConfigPlanChecks{PreApply: []plancheck.PlanCheck{
					plancheck.ExpectResourceAction(address, plancheck.ResourceActionReplace),
				}},
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr(address, "lookup_key", "search_v2"),
					resource.TestCheckResourceAttrWith(address, "id", func(v string) error {
						if v == first {
							return fmt.Errorf("the replacement kept the feature ID %s", v)
						}
						return nil
					}),
				),
			},
		},
	})
}

func TestAccEntitlementsFeature_import(t *testing.T) {
	acct := acctest.NewAccount(t)
	cfg := featureConfig(acct, "advanced_search", "Search", "  metadata = { team = \"a\" }\n")
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

// Stripe cannot delete a feature (suite: "Archived entitlement features are frozen"), so
// destroy archives it and the feature stays in the account.
func TestAccEntitlementsFeature_delete(t *testing.T) {
	acct := acctest.NewAccount(t)
	var id string
	resource.ParallelTest(t, resource.TestCase{
		ProtoV6ProviderFactories: tfacc.ProviderFactories,
		CheckDestroy: func(*terraform.State) error {
			resp, err := tfacc.Client(acct).Do(t.Context(), stripeapi.Request{Method: "GET", Path: "/v1/entitlements/features/" + id})
			if err != nil {
				return fmt.Errorf("an archived feature still retrieves: %w", err)
			}
			if active, _ := resp.Body.(wire.Object).GetBool("active"); active {
				return fmt.Errorf("feature %s should be archived after destroy", id)
			}
			return nil
		},
		Steps: []resource.TestStep{{Config: featureConfig(acct, "advanced_search", "Search", ""), Check: idOf(&id)}},
	})
}

func TestAccEntitlementsFeature_drift(t *testing.T) {
	acct := acctest.NewAccount(t)
	var id string
	cfg := featureConfig(acct, "advanced_search", "Search", "")
	resource.ParallelTest(t, resource.TestCase{
		ProtoV6ProviderFactories: tfacc.ProviderFactories,
		Steps: []resource.TestStep{
			{Config: cfg, Check: idOf(&id)},
			{
				PreConfig: func() {
					tfacc.Post(t, acct, "/v1/entitlements/features/"+id, wire.Object{
						{Name: "name", Value: wire.String("Renamed")},
					})
				},
				Config: cfg,
				ConfigPlanChecks: resource.ConfigPlanChecks{PreApply: []plancheck.PlanCheck{
					plancheck.ExpectNonEmptyPlan(),
					plancheck.ExpectResourceAction(address, plancheck.ResourceActionUpdate),
				}},
				Check: resource.TestCheckResourceAttr(address, "name", "Search"),
			},
		},
	})
}
