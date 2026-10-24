package product_feature_test

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

var regexpInvalidImportID = regexp.MustCompile(`expected .<product id>/<id>.`)

func TestMain(m *testing.M) { os.Exit(acctest.Main(m)) }

const address = "stripe_product_feature.test"

// featureConfig is a product, two entitlement features and a product feature that attaches
// the feature named by attach ("a" or "b"). The features' lookup keys are the test's own,
// named by test. extra lands inside the product feature.
func featureConfig(c acctest.Config, test, attach, extra string) string {
	return c.ProviderBlock() + fmt.Sprintf(`
resource "stripe_product" "test" {
  name = %q
}

resource "stripe_entitlements_feature" "a" {
  lookup_key = %q
  name       = %q
}

resource "stripe_entitlements_feature" "b" {
  lookup_key = %q
  name       = %q
}

resource "stripe_product_feature" "test" {
  product             = stripe_product.test.id
  entitlement_feature = stripe_entitlements_feature.%s.id
%s}
`, c.Name("Gold plan"), c.Unique("product-feature-"+test+"-a"), c.Name("Feature A"),
		c.Unique("product-feature-"+test+"-b"), c.Name("Feature B"), attach, extra)
}

// importID is the product ID and the product feature ID, as the import ID wants them.
func importID(s *terraform.State) (string, error) {
	rs, ok := s.RootModule().Resources[address]
	if !ok {
		return "", fmt.Errorf("%s is not in state", address)
	}
	return rs.Primary.Attributes["product"] + "/" + rs.Primary.ID, nil
}

// ids records the product and product feature IDs.
func ids(product, id *string) resource.TestCheckFunc {
	return func(s *terraform.State) error {
		rs, ok := s.RootModule().Resources[address]
		if !ok {
			return fmt.Errorf("%s is not in state", address)
		}
		*product, *id = rs.Primary.Attributes["product"], rs.Primary.ID
		return nil
	}
}

func TestAccProductFeature_create(t *testing.T) {
	acct := acctest.NewAccount(t)
	resource.ParallelTest(t, resource.TestCase{
		ProtoV6ProviderFactories: tfacc.ProviderFactories,
		Steps: acctest.Forget(t, []resource.TestStep{{
			Config: featureConfig(acct, "create", "a", "") + `
data "stripe_product_feature" "test" {
  product = stripe_product.test.id
  id      = stripe_product_feature.test.id
}
`,
			Check: resource.ComposeAggregateTestCheckFunc(
				resource.TestCheckResourceAttrSet(address, "id"),
				resource.TestCheckResourceAttrPair(address, "product", "stripe_product.test", "id"),
				// The response embeds the whole feature; the attribute holds its ID.
				resource.TestCheckResourceAttrPair(address, "entitlement_feature", "stripe_entitlements_feature.a", "id"),
				resource.TestCheckResourceAttrPair("data.stripe_product_feature.test", "id", address, "id"),
				resource.TestCheckResourceAttrPair("data.stripe_product_feature.test", "entitlement_feature", "stripe_entitlements_feature.a", "id"),
			),
		}}, nil, "stripe_entitlements_feature"),
	})
}

// Stripe never updates a product feature, so nothing in place can change except the
// provider's own timeouts, which a plan applies without a replacement.
func TestAccProductFeature_update(t *testing.T) {
	acct := acctest.NewAccount(t)
	var first string
	resource.ParallelTest(t, resource.TestCase{
		ProtoV6ProviderFactories: tfacc.ProviderFactories,
		Steps: acctest.Forget(t, []resource.TestStep{
			{
				Config: featureConfig(acct, "update", "a", ""),
				Check:  resource.TestCheckResourceAttrWith(address, "id", func(v string) error { first = v; return nil }),
			},
			{
				Config: featureConfig(acct, "update", "a", "  timeouts = { create = \"10m\" }\n"),
				ConfigPlanChecks: resource.ConfigPlanChecks{PreApply: []plancheck.PlanCheck{
					plancheck.ExpectResourceAction(address, plancheck.ResourceActionUpdate),
				}},
				Check: resource.TestCheckResourceAttrWith(address, "id", func(v string) error {
					if v != first {
						return fmt.Errorf("the in-place update changed the product feature ID from %s to %s", first, v)
					}
					return nil
				}),
			},
		}, nil, "stripe_entitlements_feature"),
	})
}

// entitlement_feature is accepted on create only, so attaching another feature replaces the
// product feature. Destroy detaches the old one, so the feature is free to attach again.
func TestAccProductFeature_replace(t *testing.T) {
	acct := acctest.NewAccount(t)
	var first string
	resource.ParallelTest(t, resource.TestCase{
		ProtoV6ProviderFactories: tfacc.ProviderFactories,
		Steps: acctest.Forget(t, []resource.TestStep{
			{
				Config: featureConfig(acct, "replace", "a", ""),
				Check:  resource.TestCheckResourceAttrWith(address, "id", func(v string) error { first = v; return nil }),
			},
			{
				Config: featureConfig(acct, "replace", "b", ""),
				ConfigPlanChecks: resource.ConfigPlanChecks{PreApply: []plancheck.PlanCheck{
					plancheck.ExpectResourceAction(address, plancheck.ResourceActionReplace),
				}},
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttrPair(address, "entitlement_feature", "stripe_entitlements_feature.b", "id"),
					resource.TestCheckResourceAttrWith(address, "id", func(v string) error {
						if v == first {
							return fmt.Errorf("the replacement kept the product feature ID %s", v)
						}
						return nil
					}),
				),
			},
			// Attaching the first feature again works because the replacement detached it.
			{Config: featureConfig(acct, "replace", "a", ""), Check: resource.TestCheckResourceAttrPair(address, "entitlement_feature", "stripe_entitlements_feature.a", "id")},
		}, nil, "stripe_entitlements_feature"),
	})
}

// The import ID is `<product_id>/<product_feature_id>`.
func TestAccProductFeature_import(t *testing.T) {
	acct := acctest.NewAccount(t)
	cfg := featureConfig(acct, "import", "a", "")
	resource.ParallelTest(t, resource.TestCase{
		ProtoV6ProviderFactories: tfacc.ProviderFactories,
		Steps: acctest.Forget(t, []resource.TestStep{
			{Config: cfg},
			{
				ResourceName:      address,
				ImportState:       true,
				ImportStateIdFunc: importID,
				ImportStateVerify: true,
			},
			{
				ResourceName:      address,
				ImportState:       true,
				ImportStateKind:   resource.ImportBlockWithID,
				ImportStateIdFunc: importID,
				Config:            cfg,
			},
			{
				ResourceName:  address,
				ImportState:   true,
				ImportStateId: "not-a-compound-id",
				ExpectError:   regexpInvalidImportID,
			},
		}, nil, "stripe_entitlements_feature"),
	})
}

// The identity form needs Terraform or OpenTofu 1.12.0 or later. The identity carries the
// product as well as the feature ID.
func TestAccProductFeature_importIdentity(t *testing.T) {
	acct := acctest.NewAccount(t)
	cfg := featureConfig(acct, "importIdentity", "a", "")
	resource.ParallelTest(t, resource.TestCase{
		ProtoV6ProviderFactories: tfacc.ProviderFactories,
		TerraformVersionChecks:   []tfversion.TerraformVersionCheck{tfversion.RequireAbove(version.Must(version.NewVersion("1.12.0")))},
		Steps: acctest.Forget(t, []resource.TestStep{
			{Config: cfg},
			{
				ResourceName:    address,
				ImportState:     true,
				ImportStateKind: resource.ImportBlockWithResourceIdentity,
				Config:          cfg,
			},
		}, nil, "stripe_entitlements_feature"),
	})
}

// Stripe detaches a product feature with DELETE (suite: product_feature_test.go, "detach"),
// and the entitlement feature itself stays.
func TestAccProductFeature_delete(t *testing.T) {
	acct := acctest.NewAccount(t)
	var product, id string
	resource.ParallelTest(t, resource.TestCase{
		ProtoV6ProviderFactories: tfacc.ProviderFactories,
		CheckDestroy: func(*terraform.State) error {
			// A detached product feature still retrieves, as the deleted stub.
			resp, err := tfacc.Client(acct).Do(t.Context(), stripeapi.Request{Method: "GET", Path: "/v1/products/" + product + "/features/" + id})
			if err != nil && !stripeapi.IsNotFound(err) {
				return err
			}
			if err == nil {
				if deleted, _ := resp.Body.(wire.Object).GetBool("deleted"); !deleted {
					return fmt.Errorf("product feature %s should be detached after destroy", id)
				}
			}
			return nil
		},
		Steps: acctest.Forget(t, []resource.TestStep{{Config: featureConfig(acct, "delete", "a", ""), Check: ids(&product, &id)}}, nil, "stripe_entitlements_feature"),
	})
}

// A product feature detached out of band reads back as the deleted stub, which Terraform
// treats as gone and plans to attach again.
func TestAccProductFeature_drift(t *testing.T) {
	acct := acctest.NewAccount(t)
	var product, id string
	cfg := featureConfig(acct, "drift", "a", "")
	resource.ParallelTest(t, resource.TestCase{
		ProtoV6ProviderFactories: tfacc.ProviderFactories,
		Steps: acctest.Forget(t, []resource.TestStep{
			{Config: cfg, Check: ids(&product, &id)},
			{
				PreConfig: func() {
					if _, err := tfacc.Client(acct).Do(t.Context(), stripeapi.Request{Method: "DELETE", Path: "/v1/products/" + product + "/features/" + id}); err != nil {
						t.Fatal(err)
					}
				},
				Config: cfg,
				ConfigPlanChecks: resource.ConfigPlanChecks{PreApply: []plancheck.PlanCheck{
					plancheck.ExpectNonEmptyPlan(),
					plancheck.ExpectResourceAction(address, plancheck.ResourceActionCreate),
				}},
				Check: resource.TestCheckResourceAttrWith(address, "id", func(v string) error {
					if v == id {
						return fmt.Errorf("the product feature kept the detached ID %s", v)
					}
					return nil
				}),
			},
		}, nil, "stripe_entitlements_feature"),
	})
}
