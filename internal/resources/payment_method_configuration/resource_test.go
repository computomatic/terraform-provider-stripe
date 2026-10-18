package payment_method_configuration_test

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

const address = "stripe_payment_method_configuration.test"

// configurationConfig is a payment method configuration. extra lands inside the resource
// block.
func configurationConfig(c acctest.Config, name string, extra string) string {
	return c.ProviderBlock() + fmt.Sprintf(`
resource "stripe_payment_method_configuration" "test" {
  name = %q
%s}
`, name, extra)
}

func idOf(dst *string) resource.TestCheckFunc {
	return resource.TestCheckResourceAttrWith(address, "id", func(v string) error {
		*dst = v
		return nil
	})
}

func preference(method, value string) string {
	return fmt.Sprintf("  %s = {\n    display_preference = { preference = %q }\n  }\n", method, value)
}

func TestAccPaymentMethodConfiguration_create(t *testing.T) {
	acct := acctest.NewAccount(t)
	resource.ParallelTest(t, resource.TestCase{
		ProtoV6ProviderFactories: tfacc.ProviderFactories,
		Steps: []resource.TestStep{{
			Config: configurationConfig(acct, "Checkout", preference("card", "on")) + `
data "stripe_payment_method_configuration" "test" {
  id = stripe_payment_method_configuration.test.id
}
`,
			Check: resource.ComposeAggregateTestCheckFunc(
				resource.TestCheckResourceAttrSet(address, "id"),
				resource.TestCheckResourceAttr(address, "name", "Checkout"),
				resource.TestCheckResourceAttr(address, "active", "true"),
				resource.TestCheckResourceAttr(address, "is_default", "false"),
				resource.TestCheckResourceAttr(address, "card.display_preference.preference", "on"),
				resource.TestCheckResourceAttrSet(address, "card.display_preference.value"),
				resource.TestCheckResourceAttrPair("data.stripe_payment_method_configuration.test", "id", address, "id"),
				resource.TestCheckResourceAttr("data.stripe_payment_method_configuration.test", "name", "Checkout"),
			),
		}},
	})
}

// Stripe fills in a preference for every payment method, so a configuration with only a
// name reads back without a difference.
func TestAccPaymentMethodConfiguration_serverDefaults(t *testing.T) {
	acct := acctest.NewAccount(t)
	cfg := configurationConfig(acct, "Checkout", "")
	resource.ParallelTest(t, resource.TestCase{
		ProtoV6ProviderFactories: tfacc.ProviderFactories,
		Steps: []resource.TestStep{
			{Config: cfg, Check: resource.TestCheckResourceAttrSet(address, "card.display_preference.value")},
			{Config: cfg, PlanOnly: true},
		},
	})
}

// The fake serves the card payment method only (fake_not_implemented for the others), so
// this test skips until it serves `link`.
func TestAccPaymentMethodConfiguration_otherMethods(t *testing.T) {
	acct := acctest.NewAccount(t)
	cfg := configurationConfig(acct, "Checkout", preference("link", "off"))
	resource.ParallelTest(t, resource.TestCase{
		ProtoV6ProviderFactories: tfacc.ProviderFactories,
		ErrorCheck:               tfacc.SkipWhenFakeLacks(t, "link"),
		Steps: []resource.TestStep{
			{Config: cfg, Check: resource.TestCheckResourceAttr(address, "link.display_preference.preference", "off")},
			{Config: cfg, PlanOnly: true},
		},
	})
}

func TestAccPaymentMethodConfiguration_update(t *testing.T) {
	acct := acctest.NewAccount(t)
	var first string
	resource.ParallelTest(t, resource.TestCase{
		ProtoV6ProviderFactories: tfacc.ProviderFactories,
		Steps: []resource.TestStep{
			{Config: configurationConfig(acct, "Checkout", preference("card", "on")), Check: idOf(&first)},
			{
				Config: configurationConfig(acct, "Checkout v2", preference("card", "off")+"  active = false\n"),
				ConfigPlanChecks: resource.ConfigPlanChecks{PreApply: []plancheck.PlanCheck{
					plancheck.ExpectResourceAction(address, plancheck.ResourceActionUpdate),
				}},
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr(address, "name", "Checkout v2"),
					resource.TestCheckResourceAttr(address, "active", "false"),
					resource.TestCheckResourceAttr(address, "card.display_preference.preference", "off"),
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

// Stripe takes `parent` on create only, but this account is refused child configurations
// (suite: payment_method_configuration_test.go), so no argument can force a replacement
// here. A replacement forced from outside (a changed trigger) deactivates the configuration
// and creates another.
func TestAccPaymentMethodConfiguration_replace(t *testing.T) {
	acct := acctest.NewAccount(t)
	var first string
	config := func(trigger string) string {
		return configurationConfig(acct, "Checkout", `
  lifecycle {
    replace_triggered_by = [terraform_data.trigger]
  }
`) + fmt.Sprintf(`
resource "terraform_data" "trigger" {
  input = %q
}
`, trigger)
	}
	resource.ParallelTest(t, resource.TestCase{
		ProtoV6ProviderFactories: tfacc.ProviderFactories,
		Steps: []resource.TestStep{
			{Config: config("one"), Check: idOf(&first)},
			{
				Config: config("two"),
				ConfigPlanChecks: resource.ConfigPlanChecks{PreApply: []plancheck.PlanCheck{
					plancheck.ExpectResourceAction(address, plancheck.ResourceActionDestroyBeforeCreate),
				}},
				Check: resource.TestCheckResourceAttrWith(address, "id", func(v string) error {
					if v == first {
						return fmt.Errorf("the replacement kept the configuration ID %s", v)
					}
					return nil
				}),
			},
		},
	})
}

func TestAccPaymentMethodConfiguration_import(t *testing.T) {
	acct := acctest.NewAccount(t)
	cfg := configurationConfig(acct, "Checkout", preference("card", "on"))
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

// Stripe cannot delete a payment method configuration (suite:
// payment_method_configuration_test.go, "cannot be deleted"), so destroy deactivates it
// and the configuration stays in the account.
func TestAccPaymentMethodConfiguration_delete(t *testing.T) {
	acct := acctest.NewAccount(t)
	var id string
	resource.ParallelTest(t, resource.TestCase{
		ProtoV6ProviderFactories: tfacc.ProviderFactories,
		CheckDestroy: func(*terraform.State) error {
			resp, err := tfacc.Client(acct).Do(t.Context(), stripeapi.Request{Method: "GET", Path: "/v1/payment_method_configurations/" + id})
			if err != nil {
				return fmt.Errorf("a deactivated configuration still retrieves: %w", err)
			}
			if active, _ := resp.Body.(wire.Object).GetBool("active"); active {
				return fmt.Errorf("payment method configuration %s should be deactivated after destroy", id)
			}
			return nil
		},
		Steps: []resource.TestStep{{Config: configurationConfig(acct, "Checkout", ""), Check: idOf(&id)}},
	})
}

func TestAccPaymentMethodConfiguration_drift(t *testing.T) {
	acct := acctest.NewAccount(t)
	var id string
	cfg := configurationConfig(acct, "Checkout", preference("card", "on"))
	resource.ParallelTest(t, resource.TestCase{
		ProtoV6ProviderFactories: tfacc.ProviderFactories,
		Steps: []resource.TestStep{
			{Config: cfg, Check: idOf(&id)},
			{
				PreConfig: func() {
					tfacc.Post(t, acct, "/v1/payment_method_configurations/"+id, wire.Object{
						{Name: "card", Value: wire.Object{{Name: "display_preference", Value: wire.Object{{Name: "preference", Value: wire.String("off")}}}}},
					})
				},
				Config: cfg,
				ConfigPlanChecks: resource.ConfigPlanChecks{PreApply: []plancheck.PlanCheck{
					plancheck.ExpectNonEmptyPlan(),
					plancheck.ExpectResourceAction(address, plancheck.ResourceActionUpdate),
				}},
				Check: resource.TestCheckResourceAttr(address, "card.display_preference.preference", "on"),
			},
		},
	})
}
