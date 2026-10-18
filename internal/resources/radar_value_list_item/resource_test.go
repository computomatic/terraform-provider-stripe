package radar_value_list_item_test

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
)

func TestMain(m *testing.M) { os.Exit(acctest.Main(m)) }

const address = "stripe_radar_value_list_item.test"

// itemConfig is an email list and an item in it. extra lands inside the item.
func itemConfig(c acctest.Config, value string, extra string) string {
	return c.ProviderBlock() + fmt.Sprintf(`
resource "stripe_radar_value_list" "test" {
  alias     = "blocked_emails"
  name      = "Blocked emails"
  item_type = "email"
}

resource "stripe_radar_value_list_item" "test" {
  value_list = stripe_radar_value_list.test.id
  value      = %q
%s}
`, value, extra)
}

func idOf(dst *string) resource.TestCheckFunc {
	return resource.TestCheckResourceAttrWith(address, "id", func(v string) error {
		*dst = v
		return nil
	})
}

func TestAccRadarValueListItem_create(t *testing.T) {
	acct := acctest.NewAccount(t)
	resource.ParallelTest(t, resource.TestCase{
		ProtoV6ProviderFactories: tfacc.ProviderFactories,
		Steps: []resource.TestStep{{
			Config: itemConfig(acct, "spammer@example.com", "") + `
data "stripe_radar_value_list_item" "test" {
  id = stripe_radar_value_list_item.test.id
}
`,
			Check: resource.ComposeAggregateTestCheckFunc(
				resource.TestCheckResourceAttrSet(address, "id"),
				resource.TestCheckResourceAttr(address, "value", "spammer@example.com"),
				resource.TestCheckResourceAttrPair(address, "value_list", "stripe_radar_value_list.test", "id"),
				resource.TestCheckResourceAttrSet(address, "created"),
				resource.TestCheckResourceAttrPair("data.stripe_radar_value_list_item.test", "id", address, "id"),
				resource.TestCheckResourceAttr("data.stripe_radar_value_list_item.test", "value", "spammer@example.com"),
			),
		}},
	})
}

// Stripe never updates an item, so nothing in place can change except the provider's own
// timeouts, which a plan applies without a replacement.
func TestAccRadarValueListItem_update(t *testing.T) {
	acct := acctest.NewAccount(t)
	var first string
	resource.ParallelTest(t, resource.TestCase{
		ProtoV6ProviderFactories: tfacc.ProviderFactories,
		Steps: []resource.TestStep{
			{Config: itemConfig(acct, "spammer@example.com", ""), Check: idOf(&first)},
			{
				Config: itemConfig(acct, "spammer@example.com", "  timeouts = { create = \"10m\" }\n"),
				ConfigPlanChecks: resource.ConfigPlanChecks{PreApply: []plancheck.PlanCheck{
					plancheck.ExpectResourceAction(address, plancheck.ResourceActionUpdate),
				}},
				Check: resource.TestCheckResourceAttrWith(address, "id", func(v string) error {
					if v != first {
						return fmt.Errorf("the in-place update changed the ID from %s to %s", first, v)
					}
					return nil
				}),
			},
		},
	})
}

// value is accepted on create only, so a new value replaces the item.
func TestAccRadarValueListItem_replace(t *testing.T) {
	acct := acctest.NewAccount(t)
	var first string
	resource.ParallelTest(t, resource.TestCase{
		ProtoV6ProviderFactories: tfacc.ProviderFactories,
		Steps: []resource.TestStep{
			{Config: itemConfig(acct, "spammer@example.com", ""), Check: idOf(&first)},
			{
				Config: itemConfig(acct, "scammer@example.com", ""),
				ConfigPlanChecks: resource.ConfigPlanChecks{PreApply: []plancheck.PlanCheck{
					plancheck.ExpectResourceAction(address, plancheck.ResourceActionReplace),
				}},
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr(address, "value", "scammer@example.com"),
					resource.TestCheckResourceAttrWith(address, "id", func(v string) error {
						if v == first {
							return fmt.Errorf("the replacement kept the item ID %s", v)
						}
						return nil
					}),
				),
			},
		},
	})
}

func TestAccRadarValueListItem_import(t *testing.T) {
	acct := acctest.NewAccount(t)
	cfg := itemConfig(acct, "spammer@example.com", "")
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

// Stripe deletes an item with DELETE (suite: radar_value_list_item_test.go, "delete").
func TestAccRadarValueListItem_delete(t *testing.T) {
	acct := acctest.NewAccount(t)
	var id string
	resource.ParallelTest(t, resource.TestCase{
		ProtoV6ProviderFactories: tfacc.ProviderFactories,
		CheckDestroy: func(*terraform.State) error {
			_, err := tfacc.Client(acct).Do(t.Context(), stripeapi.Request{Method: "GET", Path: "/v1/radar/value_list_items/" + id})
			if !stripeapi.IsNotFound(err) {
				return fmt.Errorf("item %s should be deleted after destroy, got %v", id, err)
			}
			return nil
		},
		Steps: []resource.TestStep{{Config: itemConfig(acct, "spammer@example.com", ""), Check: idOf(&id)}},
	})
}

// An item deleted out of band is gone, so Terraform plans to add it again.
func TestAccRadarValueListItem_drift(t *testing.T) {
	acct := acctest.NewAccount(t)
	var id string
	cfg := itemConfig(acct, "spammer@example.com", "")
	resource.ParallelTest(t, resource.TestCase{
		ProtoV6ProviderFactories: tfacc.ProviderFactories,
		Steps: []resource.TestStep{
			{Config: cfg, Check: idOf(&id)},
			{
				PreConfig: func() {
					if _, err := tfacc.Client(acct).Do(t.Context(), stripeapi.Request{Method: "DELETE", Path: "/v1/radar/value_list_items/" + id}); err != nil {
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
						return fmt.Errorf("the item kept the deleted ID %s", v)
					}
					return nil
				}),
			},
		},
	})
}
