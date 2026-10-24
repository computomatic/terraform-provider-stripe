package billing_credit_grant_test

import (
	"fmt"
	"os"
	"regexp"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/plancheck"

	"github.com/computomatic/terraform-provider-stripe/internal/acctest"
	"github.com/computomatic/terraform-provider-stripe/internal/acctest/tfacc"
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
`, c.Name(name), extra)
}

func idOf(dst *string) resource.TestCheckFunc {
	return resource.TestCheckResourceAttrWith(address, "id", func(v string) error {
		*dst = v
		return nil
	})
}

// pathOf records the object's API path.
func pathOf(dst *string) resource.TestCheckFunc {
	return resource.TestCheckResourceAttrWith(address, "id", func(v string) error {
		*dst = "/v1/billing/credit_grants/" + v
		return nil
	})
}

func TestAccBillingCreditGrant_create(t *testing.T) {
	acct := acctest.NewAccount(t)
	resource.ParallelTest(t, resource.TestCase{
		ProtoV6ProviderFactories: tfacc.ProviderFactories,
		Steps: acctest.Forget(t, []resource.TestStep{{
			Config: grantConfig(acct, "Welcome", "  metadata = { team = \"a\" }\n") + `
data "stripe_billing_credit_grant" "by_id" {
  id = stripe_billing_credit_grant.test.id
}
`,
			Check: resource.ComposeAggregateTestCheckFunc(
				resource.TestCheckResourceAttrSet(address, "id"),
				resource.TestCheckResourceAttr(address, "name", acct.Name("Welcome")),
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
		}}, nil, "stripe_billing_credit_grant"),
	})
}

// Stripe keeps a metadata key sent with an empty value (suite: update "metadata empty value
// with expires_at"), so dropping one fails the plan, and setting it to "" keeps it empty.
func TestAccBillingCreditGrant_update(t *testing.T) {
	acct := acctest.NewAccount(t)
	resource.ParallelTest(t, resource.TestCase{
		ProtoV6ProviderFactories: tfacc.ProviderFactories,
		Steps: acctest.Forget(t, []resource.TestStep{
			{Config: grantConfig(acct, "Welcome", "  metadata = { team = \"a\", tier = \"1\" }\n")},
			{
				Config:      grantConfig(acct, "Welcome", "  metadata = { team = \"b\" }\n  expires_at = 4102444800\n"),
				PlanOnly:    true,
				ExpectError: regexp.MustCompile("Cannot remove a metadata key"),
			},
			{
				Config: grantConfig(acct, "Welcome", "  metadata = { team = \"b\", tier = \"\" }\n  expires_at = 4102444800\n"),
				ConfigPlanChecks: resource.ConfigPlanChecks{PreApply: []plancheck.PlanCheck{
					plancheck.ExpectResourceAction(address, plancheck.ResourceActionUpdate),
				}},
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr(address, "expires_at", "4102444800"),
					resource.TestCheckResourceAttr(address, "metadata.%", "2"),
					resource.TestCheckResourceAttr(address, "metadata.team", "b"),
					resource.TestCheckResourceAttr(address, "metadata.tier", ""),
				),
			},
		}, nil, "stripe_billing_credit_grant"),
	})
}

// `name` is create-only, so changing it would replace the grant, which Stripe cannot delete:
// the plan fails, and so does a taint, at apply.
func TestAccBillingCreditGrant_replace(t *testing.T) {
	acct := acctest.NewAccount(t)
	cfg := grantConfig(acct, "Welcome", "")
	resource.ParallelTest(t, resource.TestCase{
		ProtoV6ProviderFactories: tfacc.ProviderFactories,
		Steps: acctest.Forget(t, []resource.TestStep{
			{Config: cfg},
			{Config: grantConfig(acct, "Renewal", ""), PlanOnly: true, ExpectError: tfacc.RefusedReplace},
			{Config: cfg, Taint: []string{address}, ExpectError: tfacc.RefusedDestroy},
		}, nil, "stripe_billing_credit_grant"),
	})
}

func TestAccBillingCreditGrant_import(t *testing.T) {
	acct := acctest.NewAccount(t)
	cfg := grantConfig(acct, "Welcome", "  metadata = { team = \"a\" }\n")
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
		}, nil, "stripe_billing_credit_grant"),
	})
}

// Stripe cannot delete a credit grant, so dropping it from the configuration, and `terraform
// destroy`, fail at plan time, and the credit grant stays as it was.
func TestAccBillingCreditGrant_delete(t *testing.T) {
	acct := acctest.NewAccount(t)
	var path string
	var mark int
	cfg := grantConfig(acct, "Welcome", "")
	resource.ParallelTest(t, resource.TestCase{
		ProtoV6ProviderFactories: tfacc.ProviderFactories,
		Steps: acctest.Forget(t, []resource.TestStep{
			{Config: cfg, Check: resource.ComposeTestCheckFunc(pathOf(&path), tfacc.LogMark(t, acct, &mark))},
			{Config: acctest.Without(t, cfg, "stripe_billing_credit_grant"), PlanOnly: true, ExpectError: tfacc.RefusedDestroy},
			{Config: cfg, Destroy: true, ExpectError: tfacc.RefusedDestroy},
		}, tfacc.Untouched(t, acct, &path, &mark, "voided_at", wire.Null{}), "stripe_billing_credit_grant"),
	})
}

func TestAccBillingCreditGrant_drift(t *testing.T) {
	acct := acctest.NewAccount(t)
	var id string
	cfg := grantConfig(acct, "Welcome", "  metadata = { team = \"a\" }\n")
	resource.ParallelTest(t, resource.TestCase{
		ProtoV6ProviderFactories: tfacc.ProviderFactories,
		Steps: acctest.Forget(t, []resource.TestStep{
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
		}, nil, "stripe_billing_credit_grant"),
	})
}
