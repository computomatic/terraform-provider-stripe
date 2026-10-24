package subscription_item_test

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

const address = "stripe_subscription_item.test"

// itemConfig is a subscription with one item of its own and an item added to it. The
// subscription ignores `items`, because the added item shows up in them. extra lands inside
// the added item.
func itemConfig(c acctest.Config, price string, extra string) string {
	return c.ProviderBlock() + fmt.Sprintf(`
resource "stripe_customer" "test" {}

resource "stripe_product" "test" {
  name = %q
}

resource "stripe_price" "base" {
  product     = stripe_product.test.id
  currency    = "usd"
  unit_amount = 1000
  recurring = {
    interval = "month"
  }
}

resource "stripe_price" "one" {
  product     = stripe_product.test.id
  currency    = "usd"
  unit_amount = 500
  recurring = {
    interval = "month"
  }
}

resource "stripe_price" "two" {
  product     = stripe_product.test.id
  currency    = "usd"
  unit_amount = 700
  recurring = {
    interval = "month"
  }
}

resource "stripe_subscription" "test" {
  customer          = stripe_customer.test.id
  collection_method = "send_invoice"
  days_until_due    = 30
  items             = [{ price = stripe_price.base.id }]

  lifecycle {
    ignore_changes = [items]
  }
}

resource "stripe_subscription_item" "test" {
  subscription = stripe_subscription.test.id
  price        = stripe_price.%s.id
  proration_behavior = "none"
%s}
`, c.Name("Plan"), price, extra)
}

func idOf(dst *string) resource.TestCheckFunc {
	return resource.TestCheckResourceAttrWith(address, "id", func(v string) error {
		*dst = v
		return nil
	})
}

func TestAccSubscriptionItem_create(t *testing.T) {
	acct := acctest.NewAccount(t)
	resource.ParallelTest(t, resource.TestCase{
		ProtoV6ProviderFactories: tfacc.ProviderFactories,
		Steps: []resource.TestStep{{
			Config: itemConfig(acct, "one", "  quantity = 3\n  metadata = { team = \"a\" }\n") + `
data "stripe_subscription_item" "by_id" {
  id = stripe_subscription_item.test.id
}
`,
			Check: resource.ComposeAggregateTestCheckFunc(
				resource.TestCheckResourceAttrSet(address, "id"),
				resource.TestCheckResourceAttrPair(address, "subscription", "stripe_subscription.test", "id"),
				resource.TestCheckResourceAttrPair(address, "price", "stripe_price.one", "id"),
				resource.TestCheckResourceAttr(address, "quantity", "3"),
				resource.TestCheckResourceAttr(address, "metadata.team", "a"),
				resource.TestCheckResourceAttrPair("data.stripe_subscription_item.by_id", "id", address, "id"),
				resource.TestCheckResourceAttr("data.stripe_subscription_item.by_id", "quantity", "3"),
			),
		}},
	})
}

func TestAccSubscriptionItem_defaultQuantity(t *testing.T) {
	acct := acctest.NewAccount(t)
	resource.ParallelTest(t, resource.TestCase{
		ProtoV6ProviderFactories: tfacc.ProviderFactories,
		Steps: []resource.TestStep{{
			Config: itemConfig(acct, "one", ""),
			Check:  resource.TestCheckResourceAttr(address, "quantity", "1"),
		}},
	})
}

func TestAccSubscriptionItem_update(t *testing.T) {
	acct := acctest.NewAccount(t)
	resource.ParallelTest(t, resource.TestCase{
		ProtoV6ProviderFactories: tfacc.ProviderFactories,
		Steps: []resource.TestStep{
			{Config: itemConfig(acct, "one", "  quantity = 1\n  metadata = { team = \"a\", tier = \"1\" }\n")},
			{
				Config: itemConfig(acct, "one", "  quantity = 4\n  metadata = { team = \"b\" }\n"),
				ConfigPlanChecks: resource.ConfigPlanChecks{PreApply: []plancheck.PlanCheck{
					plancheck.ExpectResourceAction(address, plancheck.ResourceActionUpdate),
				}},
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr(address, "quantity", "4"),
					resource.TestCheckResourceAttr(address, "metadata.%", "1"),
					resource.TestCheckResourceAttr(address, "metadata.team", "b"),
				),
			},
		},
	})
}

// subscription is accepted on create only (suite: "`subscription` on update is
// `parameter_unknown`"), so moving an item to another subscription replaces it.
func TestAccSubscriptionItem_replace(t *testing.T) {
	acct := acctest.NewAccount(t)
	var first string
	other := acct.ProviderBlock() + `
resource "stripe_customer" "test" {}

resource "stripe_product" "test" {
  name = %q
}

resource "stripe_price" "base" {
  product     = stripe_product.test.id
  currency    = "usd"
  unit_amount = 1000
  recurring = {
    interval = "month"
  }
}

resource "stripe_price" "one" {
  product     = stripe_product.test.id
  currency    = "usd"
  unit_amount = 500
  recurring = {
    interval = "month"
  }
}

resource "stripe_subscription" "a" {
  customer          = stripe_customer.test.id
  collection_method = "send_invoice"
  days_until_due    = 30
  items             = [{ price = stripe_price.base.id }]
  lifecycle {
    ignore_changes = [items]
  }
}

resource "stripe_subscription" "b" {
  customer          = stripe_customer.test.id
  collection_method = "send_invoice"
  days_until_due    = 30
  items             = [{ price = stripe_price.base.id }]
  lifecycle {
    ignore_changes = [items]
  }
}

resource "stripe_subscription_item" "test" {
  subscription = stripe_subscription.%s.id
  price        = stripe_price.one.id
  proration_behavior = "none"
}
`
	resource.ParallelTest(t, resource.TestCase{
		ProtoV6ProviderFactories: tfacc.ProviderFactories,
		Steps: []resource.TestStep{
			{Config: fmt.Sprintf(other, acct.Name("Plan"), "a"), Check: idOf(&first)},
			{
				Config: fmt.Sprintf(other, acct.Name("Plan"), "b"),
				ConfigPlanChecks: resource.ConfigPlanChecks{PreApply: []plancheck.PlanCheck{
					plancheck.ExpectResourceAction(address, plancheck.ResourceActionReplace),
				}},
				Check: resource.TestCheckResourceAttrWith(address, "id", func(v string) error {
					if v == first {
						return fmt.Errorf("the replacement kept the item ID %s", v)
					}
					return nil
				}),
			},
		},
	})
}

func TestAccSubscriptionItem_import(t *testing.T) {
	acct := acctest.NewAccount(t)
	cfg := itemConfig(acct, "one", "  quantity = 2\n  metadata = { team = \"a\" }\n")
	resource.ParallelTest(t, resource.TestCase{
		ProtoV6ProviderFactories: tfacc.ProviderFactories,
		Steps: []resource.TestStep{
			{Config: cfg},
			{
				ResourceName:      address,
				ImportState:       true,
				ImportStateVerify: true,
				// Stripe never returns how a change is prorated.
				ImportStateVerifyIgnore: []string{"proration_behavior"},
			},
			{
				ResourceName:    address,
				ImportState:     true,
				ImportStateKind: resource.ImportBlockWithID,
				// Import cannot recover how a change is prorated.
				Config: strings.ReplaceAll(cfg, "  proration_behavior = \"none\"\n", ""),
			},
		},
	})
}

// Stripe deletes an item that is one of several (suite: "Subscription items", `delete`,
// "one of several items"), so destroy deletes it. The subscription keeps its own item.
func TestAccSubscriptionItem_delete(t *testing.T) {
	acct := acctest.NewAccount(t)
	var id string
	resource.ParallelTest(t, resource.TestCase{
		ProtoV6ProviderFactories: tfacc.ProviderFactories,
		CheckDestroy: func(*terraform.State) error {
			_, err := tfacc.Client(acct).Do(t.Context(), stripeapi.Request{Method: "GET", Path: "/v1/subscription_items/" + id})
			if err == nil {
				return fmt.Errorf("subscription item %s should be gone after destroy", id)
			}
			return nil
		},
		Steps: []resource.TestStep{{Config: itemConfig(acct, "one", ""), Check: idOf(&id)}},
	})
}

func TestAccSubscriptionItem_drift(t *testing.T) {
	acct := acctest.NewAccount(t)
	var id string
	cfg := itemConfig(acct, "one", "  quantity = 2\n")
	resource.ParallelTest(t, resource.TestCase{
		ProtoV6ProviderFactories: tfacc.ProviderFactories,
		Steps: []resource.TestStep{
			{Config: cfg, Check: idOf(&id)},
			{
				PreConfig: func() {
					tfacc.Post(t, acct, "/v1/subscription_items/"+id, wire.Object{
						{Name: "quantity", Value: wire.Int(5)},
						{Name: "proration_behavior", Value: wire.String("none")},
					})
				},
				Config: cfg,
				ConfigPlanChecks: resource.ConfigPlanChecks{PreApply: []plancheck.PlanCheck{
					plancheck.ExpectNonEmptyPlan(),
					plancheck.ExpectResourceAction(address, plancheck.ResourceActionUpdate),
				}},
				Check: resource.TestCheckResourceAttr(address, "quantity", "2"),
			},
		},
	})
}
