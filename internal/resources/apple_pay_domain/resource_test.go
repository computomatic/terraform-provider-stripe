package apple_pay_domain_test

import (
	"fmt"
	"os"
	"regexp"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/plancheck"
	"github.com/hashicorp/terraform-plugin-testing/terraform"

	"github.com/computomatic/terraform-provider-stripe/internal/acctest"
	"github.com/computomatic/terraform-provider-stripe/internal/acctest/tfacc"
	"github.com/computomatic/terraform-provider-stripe/internal/stripeapi"
)

func TestMain(m *testing.M) { os.Exit(acctest.Main(m)) }

const address = "stripe_apple_pay_domain.test"

// domainConfig registers the domain c.Unique(base): an account holds a domain once, so each
// test gives a base of its own.
func domainConfig(c acctest.Config, base string, extra string) string {
	return c.ProviderBlock() + fmt.Sprintf(`
resource "stripe_apple_pay_domain" "test" {
  domain_name = %q
%s}
`, c.Unique(base), extra)
}

func idOf(dst *string) resource.TestCheckFunc {
	return resource.TestCheckResourceAttrWith(address, "id", func(v string) error {
		*dst = v
		return nil
	})
}

func TestAccApplePayDomain_create(t *testing.T) {
	acct := acctest.NewAccount(t)
	resource.ParallelTest(t, resource.TestCase{
		ProtoV6ProviderFactories: tfacc.ProviderFactories,
		Steps: []resource.TestStep{{
			Config: domainConfig(acct, "apd-create.example.com", "") + `
data "stripe_apple_pay_domain" "by_id" {
  id = stripe_apple_pay_domain.test.id
}

data "stripe_apple_pay_domain" "by_name" {
  domain_name = stripe_apple_pay_domain.test.domain_name
}
`,
			Check: resource.ComposeAggregateTestCheckFunc(
				resource.TestMatchResourceAttr(address, "id", regexp.MustCompile(`^apwc_`)),
				resource.TestCheckResourceAttr(address, "domain_name", acct.Unique("apd-create.example.com")),
				resource.TestCheckResourceAttr(address, "livemode", "false"),
				resource.TestCheckResourceAttrSet(address, "created"),
				resource.TestCheckResourceAttrPair("data.stripe_apple_pay_domain.by_id", "domain_name", address, "domain_name"),
				resource.TestCheckResourceAttrPair("data.stripe_apple_pay_domain.by_name", "id", address, "id"),
			),
		}},
	})
}

// Stripe never updates an Apple Pay domain, so nothing in place can change except the
// provider's own timeouts, which a plan applies without a replacement.
func TestAccApplePayDomain_update(t *testing.T) {
	acct := acctest.NewAccount(t)
	var first string
	resource.ParallelTest(t, resource.TestCase{
		ProtoV6ProviderFactories: tfacc.ProviderFactories,
		Steps: []resource.TestStep{
			{Config: domainConfig(acct, "apd-update.example.com", ""), Check: idOf(&first)},
			{
				Config: domainConfig(acct, "apd-update.example.com", "  timeouts = { create = \"10m\" }\n"),
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

// domain_name is accepted on create only, so a new name replaces the registration.
func TestAccApplePayDomain_replace(t *testing.T) {
	acct := acctest.NewAccount(t)
	var first string
	resource.ParallelTest(t, resource.TestCase{
		ProtoV6ProviderFactories: tfacc.ProviderFactories,
		Steps: []resource.TestStep{
			{Config: domainConfig(acct, "apd-replace.example.com", ""), Check: idOf(&first)},
			{
				Config: domainConfig(acct, "apd-replace-2.example.com", ""),
				ConfigPlanChecks: resource.ConfigPlanChecks{PreApply: []plancheck.PlanCheck{
					plancheck.ExpectResourceAction(address, plancheck.ResourceActionReplace),
				}},
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr(address, "domain_name", acct.Unique("apd-replace-2.example.com")),
					resource.TestCheckResourceAttrWith(address, "id", func(v string) error {
						if v == first {
							return fmt.Errorf("the replacement kept the ID %s", v)
						}
						return nil
					}),
				),
			},
		},
	})
}

func TestAccApplePayDomain_import(t *testing.T) {
	acct := acctest.NewAccount(t)
	cfg := domainConfig(acct, "apd-import.example.com", "")
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

// Stripe deletes an Apple Pay domain with DELETE and answers 404 afterwards (suite:
// apple_pay_domain_test.go, "delete").
func TestAccApplePayDomain_delete(t *testing.T) {
	acct := acctest.NewAccount(t)
	var id string
	resource.ParallelTest(t, resource.TestCase{
		ProtoV6ProviderFactories: tfacc.ProviderFactories,
		CheckDestroy: func(*terraform.State) error {
			_, err := tfacc.Client(acct).Do(t.Context(), stripeapi.Request{Method: "GET", Path: "/v1/apple_pay/domains/" + id})
			if !stripeapi.IsNotFound(err) {
				return fmt.Errorf("Apple Pay domain %s should be deleted after destroy, got %v", id, err)
			}
			return nil
		},
		Steps: []resource.TestStep{{Config: domainConfig(acct, "apd-delete.example.com", ""), Check: idOf(&id)}},
	})
}

// A domain deleted out of band is gone, so Terraform plans to register it again.
func TestAccApplePayDomain_drift(t *testing.T) {
	acct := acctest.NewAccount(t)
	var id string
	cfg := domainConfig(acct, "apd-drift.example.com", "")
	resource.ParallelTest(t, resource.TestCase{
		ProtoV6ProviderFactories: tfacc.ProviderFactories,
		Steps: []resource.TestStep{
			{Config: cfg, Check: idOf(&id)},
			{
				PreConfig: func() {
					if _, err := tfacc.Client(acct).Do(t.Context(), stripeapi.Request{Method: "DELETE", Path: "/v1/apple_pay/domains/" + id}); err != nil {
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
						return fmt.Errorf("the domain kept the deleted ID %s", v)
					}
					return nil
				}),
			},
		},
	})
}
