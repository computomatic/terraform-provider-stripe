package billing_credit_grant_test

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

const address = "stripe_billing_credit_grant.test"

// grantConfig is a customer and a credit grant for it. extra lands inside the grant.
func grantConfig(c acctest.Config, name string, extra string) string {
	return c.ProviderBlock() + fmt.Sprintf(`
resource "stripe_customer" "test" {}

resource "stripe_billing_credit_grant" "test" {
  name     = %q
  customer = stripe_customer.test.id
  amount = {
    type = "monetary"
    monetary = {
      currency = "usd"
      value    = 1000
    }
  }
  applicability_config = {
    scope = {
      price_type = "metered"
    }
  }
%s}
`, name, extra)
}

func idOf(dst *string) resource.TestCheckFunc {
	return resource.TestCheckResourceAttrWith(address, "id", func(v string) error {
		*dst = v
		return nil
	})
}

func TestAccBillingCreditGrant_create(t *testing.T) {
	acct := acctest.NewAccount(t)
	resource.ParallelTest(t, resource.TestCase{
		ProtoV6ProviderFactories: tfacc.ProviderFactories,
		Steps: []resource.TestStep{{
			Config: grantConfig(acct, "Welcome", "  metadata = { team = \"a\" }\n") + `
data "stripe_billing_credit_grant" "by_id" {
  id = stripe_billing_credit_grant.test.id
}
`,
			Check: resource.ComposeAggregateTestCheckFunc(
				resource.TestCheckResourceAttrSet(address, "id"),
				resource.TestCheckResourceAttr(address, "name", "Welcome"),
				resource.TestCheckResourceAttrPair(address, "customer", "stripe_customer.test", "id"),
				resource.TestCheckResourceAttr(address, "amount.monetary.value", "1000"),
				resource.TestCheckResourceAttr(address, "amount.monetary.currency", "usd"),
				resource.TestCheckResourceAttr(address, "category", "paid"),
				resource.TestCheckResourceAttr(address, "priority", "50"),
				resource.TestCheckResourceAttr(address, "metadata.team", "a"),
				resource.TestCheckNoResourceAttr(address, "voided_at"),
				resource.TestCheckResourceAttrPair("data.stripe_billing_credit_grant.by_id", "id", address, "id"),
				resource.TestCheckResourceAttr("data.stripe_billing_credit_grant.by_id", "amount.monetary.value", "1000"),
			),
		}},
	})
}

func TestAccBillingCreditGrant_update(t *testing.T) {
	acct := acctest.NewAccount(t)
	resource.ParallelTest(t, resource.TestCase{
		ProtoV6ProviderFactories: tfacc.ProviderFactories,
		Steps: []resource.TestStep{
			{Config: grantConfig(acct, "Welcome", "  metadata = { team = \"a\", tier = \"1\" }\n")},
			{
				Config: grantConfig(acct, "Welcome", "  metadata = { team = \"b\" }\n  expires_at = 4102444800\n"),
				ConfigPlanChecks: resource.ConfigPlanChecks{PreApply: []plancheck.PlanCheck{
					plancheck.ExpectResourceAction(address, plancheck.ResourceActionUpdate),
				}},
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr(address, "expires_at", "4102444800"),
					resource.TestCheckResourceAttr(address, "metadata.%", "1"),
					resource.TestCheckResourceAttr(address, "metadata.team", "b"),
				),
			},
		},
	})
}

// name is accepted on create only (suite: "Only `expires_at` and `metadata` are updatable"),
// so changing it replaces the grant. The replaced grant is voided.
func TestAccBillingCreditGrant_replace(t *testing.T) {
	acct := acctest.NewAccount(t)
	var first string
	resource.ParallelTest(t, resource.TestCase{
		ProtoV6ProviderFactories: tfacc.ProviderFactories,
		Steps: []resource.TestStep{
			{Config: grantConfig(acct, "Welcome", ""), Check: idOf(&first)},
			{
				Config: grantConfig(acct, "Renewal", ""),
				ConfigPlanChecks: resource.ConfigPlanChecks{PreApply: []plancheck.PlanCheck{
					plancheck.ExpectResourceAction(address, plancheck.ResourceActionReplace),
				}},
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr(address, "name", "Renewal"),
					resource.TestCheckResourceAttrWith(address, "id", func(v string) error {
						if v == first {
							return fmt.Errorf("the replacement kept the grant ID %s", v)
						}
						return nil
					}),
				),
			},
		},
	})
}

func TestAccBillingCreditGrant_import(t *testing.T) {
	acct := acctest.NewAccount(t)
	cfg := grantConfig(acct, "Welcome", "  metadata = { team = \"a\" }\n")
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

// Stripe cannot delete a credit grant, so destroy voids it (suite: "Voided grants stay in
// the list") and the grant stays in the account.
func TestAccBillingCreditGrant_delete(t *testing.T) {
	acct := acctest.NewAccount(t)
	var id string
	resource.ParallelTest(t, resource.TestCase{
		ProtoV6ProviderFactories: tfacc.ProviderFactories,
		CheckDestroy: func(*terraform.State) error {
			resp, err := tfacc.Client(acct).Do(t.Context(), stripeapi.Request{Method: "GET", Path: "/v1/billing/credit_grants/" + id})
			if err != nil {
				return fmt.Errorf("a voided grant still retrieves: %w", err)
			}
			if at, ok := resp.Body.(wire.Object).Get("voided_at"); !ok || at == (wire.Null{}) {
				return fmt.Errorf("credit grant %s should be voided after destroy", id)
			}
			return nil
		},
		Steps: []resource.TestStep{{Config: grantConfig(acct, "Welcome", ""), Check: idOf(&id)}},
	})
}

// A grant that expired or was voided out of band cannot be voided again (suite: "Expiring
// twice ... voiding an expired grant ... are 400"), so destroy leaves it as it is.
func TestAccBillingCreditGrant_deleteAfterExpiry(t *testing.T) {
	acct := acctest.NewAccount(t)
	var id string
	cfg := grantConfig(acct, "Welcome", "")
	resource.ParallelTest(t, resource.TestCase{
		ProtoV6ProviderFactories: tfacc.ProviderFactories,
		Steps: []resource.TestStep{
			{Config: cfg, Check: idOf(&id)},
			{
				PreConfig: func() { tfacc.Post(t, acct, "/v1/billing/credit_grants/"+id+"/expire", nil) },
				Config:    cfg,
				// The expiry shows as drift in `expires_at`; destroy then has nothing to void.
				ExpectNonEmptyPlan: true,
				PlanOnly:           true,
			},
		},
	})
}

func TestAccBillingCreditGrant_drift(t *testing.T) {
	acct := acctest.NewAccount(t)
	var id string
	cfg := grantConfig(acct, "Welcome", "  metadata = { team = \"a\" }\n")
	resource.ParallelTest(t, resource.TestCase{
		ProtoV6ProviderFactories: tfacc.ProviderFactories,
		Steps: []resource.TestStep{
			{Config: cfg, Check: idOf(&id)},
			{
				PreConfig: func() {
					tfacc.Post(t, acct, "/v1/billing/credit_grants/"+id, wire.Object{
						{Name: "metadata", Value: wire.Object{{Name: "team", Value: wire.String("b")}}},
					})
				},
				Config: cfg,
				ConfigPlanChecks: resource.ConfigPlanChecks{PreApply: []plancheck.PlanCheck{
					plancheck.ExpectNonEmptyPlan(),
					plancheck.ExpectResourceAction(address, plancheck.ResourceActionUpdate),
				}},
				Check: resource.TestCheckResourceAttr(address, "metadata.team", "a"),
			},
		},
	})
}
