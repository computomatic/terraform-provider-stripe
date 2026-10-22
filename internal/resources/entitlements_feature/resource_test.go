package entitlements_feature_test

import (
	"fmt"
	"os"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/plancheck"

	"github.com/computomatic/terraform-provider-stripe/internal/acctest"
	"github.com/computomatic/terraform-provider-stripe/internal/acctest/tfacc"
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

// pathOf records the feature's API path.
func pathOf(dst *string) resource.TestCheckFunc {
	return resource.TestCheckResourceAttrWith(address, "id", func(v string) error {
		*dst = "/v1/entitlements/features/" + v
		return nil
	})
}

func TestAccEntitlementsFeature_create(t *testing.T) {
	acct := acctest.NewAccount(t)
	resource.ParallelTest(t, resource.TestCase{
		ProtoV6ProviderFactories: tfacc.ProviderFactories,
		Steps: acctest.Forget(t, []resource.TestStep{{
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
		}}, nil, "stripe_entitlements_feature"),
	})
}

// The last step archives the feature in place. An archived feature is frozen, so nothing
// updates it after that, and destroy finds it already archived.
func TestAccEntitlementsFeature_update(t *testing.T) {
	acct := acctest.NewAccount(t)
	resource.ParallelTest(t, resource.TestCase{
		ProtoV6ProviderFactories: tfacc.ProviderFactories,
		Steps: acctest.Forget(t, []resource.TestStep{
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
		}, nil, "stripe_entitlements_feature"),
	})
}

// `lookup_key` is create-only, so changing it would replace the feature, which Stripe cannot
// delete: the plan fails, and so does a taint, at apply.
func TestAccEntitlementsFeature_replace(t *testing.T) {
	acct := acctest.NewAccount(t)
	cfg := featureConfig(acct, "advanced_search", "Search", "")
	resource.ParallelTest(t, resource.TestCase{
		ProtoV6ProviderFactories: tfacc.ProviderFactories,
		Steps: acctest.Forget(t, []resource.TestStep{
			{Config: cfg},
			{Config: featureConfig(acct, "search_v2", "Search", ""), PlanOnly: true, ExpectError: tfacc.RefusedReplace},
			{Config: cfg, Taint: []string{address}, ExpectError: tfacc.RefusedDestroy},
		}, nil, "stripe_entitlements_feature"),
	})
}

func TestAccEntitlementsFeature_import(t *testing.T) {
	acct := acctest.NewAccount(t)
	cfg := featureConfig(acct, "advanced_search", "Search", "  metadata = { team = \"a\" }\n")
	resource.ParallelTest(t, resource.TestCase{
		ProtoV6ProviderFactories: tfacc.ProviderFactories,
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
		}, nil, "stripe_entitlements_feature"),
	})
}

// Stripe cannot delete a feature, and archiving one is final, so dropping it from the
// configuration, and `terraform destroy`, fail at plan time, and the feature stays active.
func TestAccEntitlementsFeature_delete(t *testing.T) {
	acct := acctest.NewAccount(t)
	var path string
	var mark int
	cfg := featureConfig(acct, "advanced_search", "Search", "")
	resource.ParallelTest(t, resource.TestCase{
		ProtoV6ProviderFactories: tfacc.ProviderFactories,
		Steps: acctest.Forget(t, []resource.TestStep{
			{Config: cfg, Check: resource.ComposeTestCheckFunc(pathOf(&path), tfacc.LogMark(t, acct, &mark))},
			{Config: acctest.Without(t, cfg, "stripe_entitlements_feature"), PlanOnly: true, ExpectError: tfacc.RefusedDestroy},
			{Config: cfg, Destroy: true, ExpectError: tfacc.RefusedDestroy},
		}, tfacc.Untouched(t, acct, &path, &mark, "active", wire.Bool(true)), "stripe_entitlements_feature"),
	})
}

// Setting `active` to `false` archives the feature, which Stripe then freezes (suite: "update
// archives").
func TestAccEntitlementsFeature_archive(t *testing.T) {
	acct := acctest.NewAccount(t)
	var path string
	resource.ParallelTest(t, resource.TestCase{
		ProtoV6ProviderFactories: tfacc.ProviderFactories,
		Steps: acctest.Forget(t, []resource.TestStep{
			{Config: featureConfig(acct, "advanced_search", "Search", ""), Check: pathOf(&path)},
			{
				Config: featureConfig(acct, "advanced_search", "Search", "  active = false\n"),
				Check: resource.ComposeTestCheckFunc(
					resource.TestCheckResourceAttr(address, "active", "false"),
					tfacc.Holds(t, acct, &path, "active", wire.Bool(false)),
				),
			},
		}, nil, "stripe_entitlements_feature"),
	})
}

func TestAccEntitlementsFeature_drift(t *testing.T) {
	acct := acctest.NewAccount(t)
	var id string
	cfg := featureConfig(acct, "advanced_search", "Search", "")
	resource.ParallelTest(t, resource.TestCase{
		ProtoV6ProviderFactories: tfacc.ProviderFactories,
		Steps: acctest.Forget(t, []resource.TestStep{
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
		}, nil, "stripe_entitlements_feature"),
	})
}
