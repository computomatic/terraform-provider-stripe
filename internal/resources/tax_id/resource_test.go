package tax_id_test

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

func TestMain(m *testing.M) { os.Exit(acctest.Main(m)) }

const address = "stripe_tax_id.test"

// taxIDConfig is a customer and a tax ID it owns. extra lands inside the tax ID.
func taxIDConfig(c acctest.Config, value, extra string) string {
	return c.ProviderBlock() + fmt.Sprintf(`
resource "stripe_customer" "test" {
  name = %q
}

resource "stripe_tax_id" "test" {
  type  = "eu_vat"
  value = %q
  owner = {
    type     = "customer"
    customer = stripe_customer.test.id
  }
%s}
`, c.Name("Tax ID owner"), value, extra)
}

// idOf records the tax ID.
func idOf(id *string) resource.TestCheckFunc {
	return resource.TestCheckResourceAttrWith(address, "id", func(v string) error { *id = v; return nil })
}

// A tax ID is created through /v1/tax_ids with a customer owner (suite: "create for a customer
// owner"), and the data source reads it back. A customer-owned ID starts pending.
func TestAccTaxId_create(t *testing.T) {
	acct := acctest.NewAccount(t)
	resource.ParallelTest(t, resource.TestCase{
		ProtoV6ProviderFactories: tfacc.ProviderFactories,
		Steps: []resource.TestStep{{
			Config: taxIDConfig(acct, "DE123456789", "") + `
data "stripe_tax_id" "test" {
  id = stripe_tax_id.test.id
}

resource "stripe_tax_id" "own" {
  type  = "us_ein"
  value = "12-3456789"
}
`,
			Check: resource.ComposeAggregateTestCheckFunc(
				resource.TestCheckResourceAttrSet(address, "id"),
				resource.TestCheckResourceAttr(address, "type", "eu_vat"),
				resource.TestCheckResourceAttr(address, "value", "DE123456789"),
				resource.TestCheckResourceAttr(address, "country", "DE"),
				resource.TestCheckResourceAttr(address, "owner.type", "customer"),
				resource.TestCheckResourceAttrPair(address, "owner.customer", "stripe_customer.test", "id"),
				resource.TestCheckResourceAttrPair(address, "customer", "stripe_customer.test", "id"),
				resource.TestCheckResourceAttr(address, "verification.status", "pending"),
				resource.TestCheckResourceAttrPair("data.stripe_tax_id.test", "id", address, "id"),
				resource.TestCheckResourceAttrPair("data.stripe_tax_id.test", "owner.customer", "stripe_customer.test", "id"),
				resource.TestCheckResourceAttr("data.stripe_tax_id.test", "value", "DE123456789"),
				// Without an owner Stripe makes the ID the account's own.
				resource.TestCheckResourceAttr("stripe_tax_id.own", "owner.type", "self"),
			),
		}},
	})
}

// Stripe never updates a tax ID (suite: "update is not served"), so nothing in place can
// change except the provider's own timeouts, which a plan applies without a replacement.
func TestAccTaxId_update(t *testing.T) {
	acct := acctest.NewAccount(t)
	var first string
	resource.ParallelTest(t, resource.TestCase{
		ProtoV6ProviderFactories: tfacc.ProviderFactories,
		Steps: []resource.TestStep{
			{Config: taxIDConfig(acct, "DE123456789", ""), Check: idOf(&first)},
			{
				Config: taxIDConfig(acct, "DE123456789", "  timeouts = { create = \"10m\" }\n"),
				ConfigPlanChecks: resource.ConfigPlanChecks{PreApply: []plancheck.PlanCheck{
					plancheck.ExpectResourceAction(address, plancheck.ResourceActionUpdate),
				}},
				Check: resource.TestCheckResourceAttrWith(address, "id", func(v string) error {
					if v != first {
						return fmt.Errorf("the in-place update changed the tax ID from %s to %s", first, v)
					}
					return nil
				}),
			},
		},
	})
}

// Every argument is create-only, so a new value replaces the tax ID.
func TestAccTaxId_replace(t *testing.T) {
	acct := acctest.NewAccount(t)
	var first string
	resource.ParallelTest(t, resource.TestCase{
		ProtoV6ProviderFactories: tfacc.ProviderFactories,
		Steps: []resource.TestStep{
			{Config: taxIDConfig(acct, "DE123456789", ""), Check: idOf(&first)},
			{
				Config: taxIDConfig(acct, "DE987654321", ""),
				ConfigPlanChecks: resource.ConfigPlanChecks{PreApply: []plancheck.PlanCheck{
					plancheck.ExpectResourceAction(address, plancheck.ResourceActionReplace),
				}},
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr(address, "value", "DE987654321"),
					resource.TestCheckResourceAttrWith(address, "id", func(v string) error {
						if v == first {
							return fmt.Errorf("the replacement kept the tax ID %s", v)
						}
						return nil
					}),
				),
			},
		},
	})
}

func TestAccTaxId_import(t *testing.T) {
	acct := acctest.NewAccount(t)
	cfg := taxIDConfig(acct, "DE123456789", "")
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

// The identity form needs Terraform or OpenTofu 1.12.0 or later.
func TestAccTaxId_importIdentity(t *testing.T) {
	acct := acctest.NewAccount(t)
	cfg := taxIDConfig(acct, "DE123456789", "")
	resource.ParallelTest(t, resource.TestCase{
		ProtoV6ProviderFactories: tfacc.ProviderFactories,
		TerraformVersionChecks:   []tfversion.TerraformVersionCheck{tfversion.RequireAbove(version.Must(version.NewVersion("1.12.0")))},
		Steps: []resource.TestStep{
			{Config: cfg},
			{
				ResourceName:    address,
				ImportState:     true,
				ImportStateKind: resource.ImportBlockWithResourceIdentity,
				Config:          cfg,
			},
		},
	})
}

// Stripe deletes a tax ID with DELETE (suite: "delete"), and it is gone afterwards.
func TestAccTaxId_delete(t *testing.T) {
	acct := acctest.NewAccount(t)
	var id string
	resource.ParallelTest(t, resource.TestCase{
		ProtoV6ProviderFactories: tfacc.ProviderFactories,
		CheckDestroy: func(*terraform.State) error {
			_, err := tfacc.Client(acct).Do(t.Context(), stripeapi.Request{Method: "GET", Path: "/v1/tax_ids/" + id})
			if !stripeapi.IsNotFound(err) {
				return fmt.Errorf("the tax ID should be gone after destroy, got %v", err)
			}
			return nil
		},
		Steps: []resource.TestStep{{Config: taxIDConfig(acct, "DE123456789", ""), Check: idOf(&id)}},
	})
}

// A tax ID deleted out of band is planned for creation again.
func TestAccTaxId_drift(t *testing.T) {
	acct := acctest.NewAccount(t)
	var id string
	cfg := taxIDConfig(acct, "DE123456789", "")
	resource.ParallelTest(t, resource.TestCase{
		ProtoV6ProviderFactories: tfacc.ProviderFactories,
		Steps: []resource.TestStep{
			{Config: cfg, Check: idOf(&id)},
			{
				PreConfig: func() {
					if _, err := tfacc.Client(acct).Do(t.Context(), stripeapi.Request{Method: "DELETE", Path: "/v1/tax_ids/" + id}); err != nil {
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
						return fmt.Errorf("the tax ID kept the deleted ID %s", v)
					}
					return nil
				}),
			},
		},
	})
}

// A tax ID of a connected account names it in `owner.account`, and Stripe reads it back; a
// customer account must be a v2 account with a customer configuration, so Stripe refuses a
// connected account there (suite: TestTaxIDAttributes, variants `account` and `customer_account`,
// which send the transfers account with the minimal create's `us_ein` value). An account holds one
// tax ID of a value, so in smoke mode, where the transfers account is shared, what an earlier run
// left there is deleted first.
func TestAccTaxId_accountOwner(t *testing.T) {
	acct := acctest.NewAccount(t)
	connected := tfacc.TransfersAccount(t, acct)
	const value = "12-7654321"
	if acct.Smoke {
		deleteAccountTaxIDs(t, acct, connected, value)
	}
	config := func(owner string) string {
		return acct.ProviderBlock() + fmt.Sprintf(`
resource "stripe_tax_id" "test" {
  type  = "us_ein"
  value = %q
  owner = %s
}
`, value, owner)
	}
	resource.ParallelTest(t, resource.TestCase{
		ProtoV6ProviderFactories: tfacc.ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config:      config(fmt.Sprintf("{ type = \"customer\", customer_account = %q }", connected)),
				ExpectError: regexp.MustCompile(`V2\s+Accounts\s+must\s+be\s+configured\s+as\s+Customers`),
			},
			{
				Config: config(fmt.Sprintf("{ type = \"account\", account = %q }", connected)),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr(address, "owner.type", "account"),
					resource.TestCheckResourceAttr(address, "owner.account", connected),
					resource.TestCheckNoResourceAttr(address, "owner.customer"),
					resource.TestCheckNoResourceAttr(address, "owner.customer_account"),
					resource.TestCheckResourceAttr(address, "value", value),
				),
			},
			{Config: config(fmt.Sprintf("{ type = \"account\", account = %q }", connected)), PlanOnly: true},
		},
	})
}

// deleteAccountTaxIDs deletes the connected account's tax IDs of value, which an earlier smoke run
// may have left.
func deleteAccountTaxIDs(t *testing.T, c acctest.Config, account, value string) {
	t.Helper()
	client := tfacc.Client(c)
	resp, err := client.Do(t.Context(), stripeapi.Request{Method: "GET", Path: "/v1/tax_ids", Params: wire.Object{
		{Name: "owner", Value: wire.Object{{Name: "type", Value: wire.String("account")}, {Name: "account", Value: wire.String(account)}}},
		{Name: "limit", Value: wire.Int(100)},
	}})
	if err != nil {
		t.Fatalf("listing the tax IDs of %s: %v", account, err)
	}
	data, _ := resp.Body.(wire.Object).Get("data")
	items, _ := data.(wire.Array)
	for _, item := range items {
		taxID, _ := item.(wire.Object)
		if v, _ := taxID.GetString("value"); v != value {
			continue
		}
		id, _ := taxID.GetString("id")
		if _, err := client.Do(t.Context(), stripeapi.Request{Method: "DELETE", Path: "/v1/tax_ids/" + id}); err != nil {
			t.Fatalf("deleting tax ID %s: %v", id, err)
		}
	}
}
