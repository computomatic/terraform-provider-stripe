package invoiceitem_test

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

const address = "stripe_invoiceitem.test"

// itemConfig is a customer and a pending invoice item for them. extra lands inside the item.
func itemConfig(c acctest.Config, currency string, extra string) string {
	return c.ProviderBlock() + fmt.Sprintf(`
resource "stripe_customer" "test" {}

resource "stripe_invoiceitem" "test" {
  customer = stripe_customer.test.id
  currency = %q
  amount   = 1000
%s}
`, currency, extra)
}

func idOf(dst *string) resource.TestCheckFunc {
	return resource.TestCheckResourceAttrWith(address, "id", func(v string) error {
		*dst = v
		return nil
	})
}

func TestAccInvoiceitem_create(t *testing.T) {
	acct := acctest.NewAccount(t)
	resource.ParallelTest(t, resource.TestCase{
		ProtoV6ProviderFactories: tfacc.ProviderFactories,
		Steps: []resource.TestStep{{
			Config: itemConfig(acct, "usd", "  description = \"Setup fee\"\n  metadata    = { team = \"a\" }\n") + `
data "stripe_invoiceitem" "by_id" {
  id = stripe_invoiceitem.test.id
}
`,
			Check: resource.ComposeAggregateTestCheckFunc(
				resource.TestCheckResourceAttrSet(address, "id"),
				resource.TestCheckResourceAttrPair(address, "customer", "stripe_customer.test", "id"),
				resource.TestCheckResourceAttr(address, "amount", "1000"),
				resource.TestCheckResourceAttr(address, "currency", "usd"),
				resource.TestCheckResourceAttr(address, "description", "Setup fee"),
				resource.TestCheckResourceAttr(address, "discountable", "true"),
				resource.TestCheckResourceAttr(address, "metadata.team", "a"),
				resource.TestCheckNoResourceAttr(address, "invoice"),
				resource.TestCheckResourceAttrPair("data.stripe_invoiceitem.by_id", "id", address, "id"),
				resource.TestCheckResourceAttr("data.stripe_invoiceitem.by_id", "description", "Setup fee"),
			),
		}},
	})
}

func TestAccInvoiceitem_update(t *testing.T) {
	acct := acctest.NewAccount(t)
	resource.ParallelTest(t, resource.TestCase{
		ProtoV6ProviderFactories: tfacc.ProviderFactories,
		Steps: []resource.TestStep{
			{Config: itemConfig(acct, "usd", "  metadata = { team = \"a\", tier = \"1\" }\n")},
			{
				Config: itemConfig(acct, "usd", "  description = \"Updated\"\n  discountable = false\n  metadata = { team = \"b\" }\n"),
				ConfigPlanChecks: resource.ConfigPlanChecks{PreApply: []plancheck.PlanCheck{
					plancheck.ExpectResourceAction(address, plancheck.ResourceActionUpdate),
				}},
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr(address, "description", "Updated"),
					resource.TestCheckResourceAttr(address, "discountable", "false"),
					resource.TestCheckResourceAttr(address, "metadata.%", "1"),
					resource.TestCheckResourceAttr(address, "metadata.team", "b"),
				),
			},
		},
	})
}

// currency is accepted on create only (suite: "`customer`, `invoice` and `subscription` on
// update are `parameter_unknown`"; a repeated `currency` is accepted, a new one is not
// offered), so changing it replaces the item.
func TestAccInvoiceitem_replace(t *testing.T) {
	acct := acctest.NewAccount(t)
	var first string
	resource.ParallelTest(t, resource.TestCase{
		ProtoV6ProviderFactories: tfacc.ProviderFactories,
		Steps: []resource.TestStep{
			{Config: itemConfig(acct, "usd", ""), Check: idOf(&first)},
			{
				Config: itemConfig(acct, "eur", ""),
				ConfigPlanChecks: resource.ConfigPlanChecks{PreApply: []plancheck.PlanCheck{
					plancheck.ExpectResourceAction(address, plancheck.ResourceActionReplace),
				}},
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr(address, "currency", "eur"),
					resource.TestCheckResourceAttrWith(address, "id", func(v string) error {
						if v == first {
							return fmt.Errorf("the replacement kept the invoice item ID %s", v)
						}
						return nil
					}),
				),
			},
		},
	})
}

func TestAccInvoiceitem_import(t *testing.T) {
	acct := acctest.NewAccount(t)
	cfg := itemConfig(acct, "usd", "  description = \"Setup fee\"\n  metadata = { team = \"a\" }\n")
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

// Stripe deletes a pending invoice item (suite: "Invoice items", `delete`, "a deleted item
// reads the same" 404), so destroy deletes it.
func TestAccInvoiceitem_delete(t *testing.T) {
	acct := acctest.NewAccount(t)
	var id string
	resource.ParallelTest(t, resource.TestCase{
		ProtoV6ProviderFactories: tfacc.ProviderFactories,
		CheckDestroy: func(*terraform.State) error {
			_, err := tfacc.Client(acct).Do(t.Context(), stripeapi.Request{Method: "GET", Path: "/v1/invoiceitems/" + id})
			if !stripeapi.IsNotFound(err) {
				return fmt.Errorf("invoice item %s should be gone after destroy, got %v", id, err)
			}
			return nil
		},
		Steps: []resource.TestStep{{Config: itemConfig(acct, "usd", ""), Check: idOf(&id)}},
	})
}

func TestAccInvoiceitem_drift(t *testing.T) {
	acct := acctest.NewAccount(t)
	var id string
	cfg := itemConfig(acct, "usd", "  description = \"Setup fee\"\n")
	resource.ParallelTest(t, resource.TestCase{
		ProtoV6ProviderFactories: tfacc.ProviderFactories,
		Steps: []resource.TestStep{
			{Config: cfg, Check: idOf(&id)},
			{
				PreConfig: func() {
					tfacc.Post(t, acct, "/v1/invoiceitems/"+id, wire.Object{{Name: "description", Value: wire.String("changed")}})
				},
				Config: cfg,
				ConfigPlanChecks: resource.ConfigPlanChecks{PreApply: []plancheck.PlanCheck{
					plancheck.ExpectNonEmptyPlan(),
					plancheck.ExpectResourceAction(address, plancheck.ResourceActionUpdate),
				}},
				Check: resource.TestCheckResourceAttr(address, "description", "Setup fee"),
			},
		},
	})
}
