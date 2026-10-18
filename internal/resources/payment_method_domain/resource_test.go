package payment_method_domain_test

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

const address = "stripe_payment_method_domain.test"

func domainConfig(c acctest.Config, name string, extra string) string {
	return c.ProviderBlock() + fmt.Sprintf(`
resource "stripe_payment_method_domain" "test" {
  domain_name = %q
%s}
`, name, extra)
}

func idOf(dst *string) resource.TestCheckFunc {
	return resource.TestCheckResourceAttrWith(address, "id", func(v string) error {
		*dst = v
		return nil
	})
}

func TestAccPaymentMethodDomain_create(t *testing.T) {
	acct := acctest.NewAccount(t)
	resource.ParallelTest(t, resource.TestCase{
		ProtoV6ProviderFactories: tfacc.ProviderFactories,
		Steps: []resource.TestStep{{
			Config: domainConfig(acct, "pay.example.com", "") + `
data "stripe_payment_method_domain" "test" {
  id = stripe_payment_method_domain.test.id
}
`,
			Check: resource.ComposeAggregateTestCheckFunc(
				resource.TestCheckResourceAttrSet(address, "id"),
				resource.TestCheckResourceAttr(address, "domain_name", "pay.example.com"),
				resource.TestCheckResourceAttr(address, "enabled", "true"),
				resource.TestCheckResourceAttr(address, "apple_pay.status", "active"),
				resource.TestCheckResourceAttr(address, "link.status", "active"),
				resource.TestCheckResourceAttrPair("data.stripe_payment_method_domain.test", "id", address, "id"),
				resource.TestCheckResourceAttr("data.stripe_payment_method_domain.test", "domain_name", "pay.example.com"),
				resource.TestCheckResourceAttr("data.stripe_payment_method_domain.test", "google_pay.status", "active"),
			),
		}},
	})
}

func TestAccPaymentMethodDomain_update(t *testing.T) {
	acct := acctest.NewAccount(t)
	var first string
	resource.ParallelTest(t, resource.TestCase{
		ProtoV6ProviderFactories: tfacc.ProviderFactories,
		Steps: []resource.TestStep{
			{Config: domainConfig(acct, "pay.example.com", ""), Check: idOf(&first)},
			{
				Config: domainConfig(acct, "pay.example.com", "  enabled = false\n"),
				ConfigPlanChecks: resource.ConfigPlanChecks{PreApply: []plancheck.PlanCheck{
					plancheck.ExpectResourceAction(address, plancheck.ResourceActionUpdate),
				}},
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr(address, "enabled", "false"),
					resource.TestCheckResourceAttrWith(address, "id", func(v string) error {
						if v != first {
							return fmt.Errorf("the in-place update changed the ID from %s to %s", first, v)
						}
						return nil
					}),
				),
			},
			{
				Config: domainConfig(acct, "pay.example.com", "  enabled = true\n"),
				Check:  resource.TestCheckResourceAttr(address, "enabled", "true"),
			},
		},
	})
}

// domain_name is accepted on create only, so a new name replaces the domain.
func TestAccPaymentMethodDomain_replace(t *testing.T) {
	acct := acctest.NewAccount(t)
	var first string
	resource.ParallelTest(t, resource.TestCase{
		ProtoV6ProviderFactories: tfacc.ProviderFactories,
		Steps: []resource.TestStep{
			{Config: domainConfig(acct, "pay.example.com", ""), Check: idOf(&first)},
			{
				Config: domainConfig(acct, "shop.example.com", ""),
				ConfigPlanChecks: resource.ConfigPlanChecks{PreApply: []plancheck.PlanCheck{
					plancheck.ExpectResourceAction(address, plancheck.ResourceActionReplace),
				}},
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr(address, "domain_name", "shop.example.com"),
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

func TestAccPaymentMethodDomain_import(t *testing.T) {
	acct := acctest.NewAccount(t)
	cfg := domainConfig(acct, "pay.example.com", "")
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

// Stripe cannot delete a payment method domain (suite: payment_method_domain_test.go,
// "cannot be deleted"), so destroy disables it ("update enabled toggles") and the domain
// stays in the account.
func TestAccPaymentMethodDomain_delete(t *testing.T) {
	acct := acctest.NewAccount(t)
	var id string
	resource.ParallelTest(t, resource.TestCase{
		ProtoV6ProviderFactories: tfacc.ProviderFactories,
		CheckDestroy: func(*terraform.State) error {
			resp, err := tfacc.Client(acct).Do(t.Context(), stripeapi.Request{Method: "GET", Path: "/v1/payment_method_domains/" + id})
			if err != nil {
				return fmt.Errorf("a disabled payment method domain still retrieves: %w", err)
			}
			if enabled, _ := resp.Body.(wire.Object).GetBool("enabled"); enabled {
				return fmt.Errorf("payment method domain %s should be disabled after destroy", id)
			}
			return nil
		},
		Steps: []resource.TestStep{{Config: domainConfig(acct, "pay.example.com", ""), Check: idOf(&id)}},
	})
}

func TestAccPaymentMethodDomain_drift(t *testing.T) {
	acct := acctest.NewAccount(t)
	var id string
	cfg := domainConfig(acct, "pay.example.com", "  enabled = true\n")
	resource.ParallelTest(t, resource.TestCase{
		ProtoV6ProviderFactories: tfacc.ProviderFactories,
		Steps: []resource.TestStep{
			{Config: cfg, Check: idOf(&id)},
			{
				PreConfig: func() {
					tfacc.Post(t, acct, "/v1/payment_method_domains/"+id, wire.Object{
						{Name: "enabled", Value: wire.Bool(false)},
					})
				},
				Config: cfg,
				ConfigPlanChecks: resource.ConfigPlanChecks{PreApply: []plancheck.PlanCheck{
					plancheck.ExpectNonEmptyPlan(),
					plancheck.ExpectResourceAction(address, plancheck.ResourceActionUpdate),
				}},
				Check: resource.TestCheckResourceAttr(address, "enabled", "true"),
			},
		},
	})
}
