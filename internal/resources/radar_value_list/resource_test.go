package radar_value_list_test

import (
	"fmt"
	"os"
	"strings"
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

const address = "stripe_radar_value_list.test"

// listConfig is a value list with the test's own alias, named by test. extra lands inside
// the resource block.
func listConfig(c acctest.Config, test, name string, extra string) string {
	return c.ProviderBlock() + fmt.Sprintf(`
resource "stripe_radar_value_list" "test" {
  alias = %q
  name  = %q
%s}
`, alias(c, "radar_list_"+test), c.Name(name), extra)
}

// alias is c.Unique(base) as a snake case alias, the form the suite gives every alias
// ("uniqueSnake"), so the smoke prefix's hyphens become underscores.
func alias(c acctest.Config, base string) string {
	return strings.ReplaceAll(c.Unique(base), "-", "_")
}

func idOf(dst *string) resource.TestCheckFunc {
	return resource.TestCheckResourceAttrWith(address, "id", func(v string) error {
		*dst = v
		return nil
	})
}

func TestAccRadarValueList_create(t *testing.T) {
	acct := acctest.NewAccount(t)
	resource.ParallelTest(t, resource.TestCase{
		ProtoV6ProviderFactories: tfacc.ProviderFactories,
		Steps: []resource.TestStep{{
			Config: listConfig(acct, "create", "Blocked emails", "  item_type = \"email\"\n  metadata  = { team = \"risk\" }\n") + `
data "stripe_radar_value_list" "by_id" {
  id = stripe_radar_value_list.test.id
}

data "stripe_radar_value_list" "by_alias" {
  alias = stripe_radar_value_list.test.alias
}
`,
			Check: resource.ComposeAggregateTestCheckFunc(
				resource.TestCheckResourceAttrSet(address, "id"),
				resource.TestCheckResourceAttr(address, "alias", alias(acct, "radar_list_create")),
				resource.TestCheckResourceAttr(address, "name", acct.Name("Blocked emails")),
				resource.TestCheckResourceAttr(address, "item_type", "email"),
				resource.TestCheckResourceAttr(address, "metadata.team", "risk"),
				resource.TestCheckResourceAttrPair("data.stripe_radar_value_list.by_id", "name", address, "name"),
				resource.TestCheckResourceAttrPair("data.stripe_radar_value_list.by_alias", "id", address, "id"),
			),
		}},
	})
}

// item_type is optional, so Stripe's default of `string` is read back without a difference.
func TestAccRadarValueList_serverDefault(t *testing.T) {
	acct := acctest.NewAccount(t)
	resource.ParallelTest(t, resource.TestCase{
		ProtoV6ProviderFactories: tfacc.ProviderFactories,
		Steps: []resource.TestStep{
			{Config: listConfig(acct, "serverDefault", "Anything", ""), Check: resource.TestCheckResourceAttr(address, "item_type", "string")},
			{Config: listConfig(acct, "serverDefault", "Anything", ""), PlanOnly: true},
		},
	})
}

func TestAccRadarValueList_update(t *testing.T) {
	acct := acctest.NewAccount(t)
	var first string
	resource.ParallelTest(t, resource.TestCase{
		ProtoV6ProviderFactories: tfacc.ProviderFactories,
		Steps: []resource.TestStep{
			{Config: listConfig(acct, "update", "One", "  metadata = { team = \"a\", tier = \"1\" }\n"), Check: idOf(&first)},
			{
				Config: listConfig(acct, "update", "Two", "  metadata = { team = \"b\" }\n"),
				ConfigPlanChecks: resource.ConfigPlanChecks{PreApply: []plancheck.PlanCheck{
					plancheck.ExpectResourceAction(address, plancheck.ResourceActionUpdate),
				}},
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr(address, "name", acct.Name("Two")),
					resource.TestCheckResourceAttr(address, "metadata.%", "1"),
					resource.TestCheckResourceAttr(address, "metadata.team", "b"),
					resource.TestCheckResourceAttrWith(address, "id", func(v string) error {
						if v != first {
							return fmt.Errorf("the in-place update changed the ID from %s to %s", first, v)
						}
						return nil
					}),
				),
			},
		},
	})
}

// item_type is accepted on create only, so a new type replaces the list.
func TestAccRadarValueList_replace(t *testing.T) {
	acct := acctest.NewAccount(t)
	var first string
	resource.ParallelTest(t, resource.TestCase{
		ProtoV6ProviderFactories: tfacc.ProviderFactories,
		Steps: []resource.TestStep{
			{Config: listConfig(acct, "replace", "Blocked", "  item_type = \"email\"\n"), Check: idOf(&first)},
			{
				Config: listConfig(acct, "replace", "Blocked", "  item_type = \"ip_address\"\n"),
				ConfigPlanChecks: resource.ConfigPlanChecks{PreApply: []plancheck.PlanCheck{
					plancheck.ExpectResourceAction(address, plancheck.ResourceActionDestroyBeforeCreate),
				}},
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr(address, "item_type", "ip_address"),
					resource.TestCheckResourceAttrWith(address, "id", func(v string) error {
						if v == first {
							return fmt.Errorf("the replacement kept the list ID %s", v)
						}
						return nil
					}),
				),
			},
		},
	})
}

func TestAccRadarValueList_import(t *testing.T) {
	acct := acctest.NewAccount(t)
	cfg := listConfig(acct, "import", "Blocked emails", "  item_type = \"email\"\n  metadata  = { team = \"risk\" }\n")
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

// Stripe deletes a value list with DELETE (suite: radar_value_list_test.go, "delete"),
// which deletes its items too ("delete removes its items").
func TestAccRadarValueList_delete(t *testing.T) {
	acct := acctest.NewAccount(t)
	var id string
	resource.ParallelTest(t, resource.TestCase{
		ProtoV6ProviderFactories: tfacc.ProviderFactories,
		CheckDestroy: func(*terraform.State) error {
			_, err := tfacc.Client(acct).Do(t.Context(), stripeapi.Request{Method: "GET", Path: "/v1/radar/value_lists/" + id})
			if !stripeapi.IsNotFound(err) {
				return fmt.Errorf("value list %s should be deleted after destroy, got %v", id, err)
			}
			return nil
		},
		Steps: []resource.TestStep{{Config: listConfig(acct, "delete", "Blocked emails", ""), Check: idOf(&id)}},
	})
}

func TestAccRadarValueList_drift(t *testing.T) {
	acct := acctest.NewAccount(t)
	var id string
	cfg := listConfig(acct, "drift", "Blocked emails", "")
	resource.ParallelTest(t, resource.TestCase{
		ProtoV6ProviderFactories: tfacc.ProviderFactories,
		Steps: []resource.TestStep{
			{Config: cfg, Check: idOf(&id)},
			{
				PreConfig: func() {
					tfacc.Post(t, acct, "/v1/radar/value_lists/"+id, wire.Object{
						{Name: "name", Value: wire.String("Renamed")},
					})
				},
				Config: cfg,
				ConfigPlanChecks: resource.ConfigPlanChecks{PreApply: []plancheck.PlanCheck{
					plancheck.ExpectNonEmptyPlan(),
					plancheck.ExpectResourceAction(address, plancheck.ResourceActionUpdate),
				}},
				Check: resource.TestCheckResourceAttr(address, "name", acct.Name("Blocked emails")),
			},
		},
	})
}
