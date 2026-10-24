package billing_portal_configuration_test

import (
	"fmt"
	"os"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/plancheck"

	"github.com/computomatic/terraform-provider-stripe/internal/acctest"
	"github.com/computomatic/terraform-provider-stripe/internal/acctest/tfacc"
	"github.com/computomatic/terraform-provider-stripe/internal/wire"
)

func TestMain(m *testing.M) { os.Exit(acctest.Main(m)) }

const address = "stripe_billing_portal_configuration.test"

// configurationConfig is a portal configuration with invoice history on. extra lands inside
// the features block, and block inside the resource.
func configurationConfig(c acctest.Config, headline, features, block string) string {
	return c.ProviderBlock() + fmt.Sprintf(`
resource "stripe_billing_portal_configuration" "test" {
  business_profile = {
    headline = %q
  }
  features = {
    invoice_history = { enabled = true }
%s  }
%s}
`, headline, features, block)
}

func idOf(dst *string) resource.TestCheckFunc {
	return resource.TestCheckResourceAttrWith(address, "id", func(v string) error {
		*dst = v
		return nil
	})
}

func TestAccBillingPortalConfiguration_create(t *testing.T) {
	acct := acctest.NewAccount(t)
	resource.ParallelTest(t, resource.TestCase{
		ProtoV6ProviderFactories: tfacc.ProviderFactories,
		Steps: acctest.Forget(t, []resource.TestStep{{
			Config: configurationConfig(acct, "Manage your plan", `
    customer_update = {
      enabled         = true
      allowed_updates = ["email", "address"]
    }
`, fmt.Sprintf(`
  name               = %q
  default_return_url = "https://example.com/account"
  metadata           = { team = "a" }
`, acct.Name("Default portal"))) + `
data "stripe_billing_portal_configuration" "test" {
  id = stripe_billing_portal_configuration.test.id
}
`,
			Check: resource.ComposeAggregateTestCheckFunc(
				resource.TestCheckResourceAttrSet(address, "id"),
				resource.TestCheckResourceAttr(address, "name", acct.Name("Default portal")),
				resource.TestCheckResourceAttr(address, "active", "true"),
				resource.TestCheckResourceAttr(address, "business_profile.headline", "Manage your plan"),
				resource.TestCheckResourceAttr(address, "default_return_url", "https://example.com/account"),
				resource.TestCheckResourceAttr(address, "features.invoice_history.enabled", "true"),
				resource.TestCheckResourceAttr(address, "features.customer_update.enabled", "true"),
				resource.TestCheckResourceAttr(address, "features.customer_update.allowed_updates.#", "2"),
				// Stripe turns off the features the request leaves out.
				resource.TestCheckResourceAttr(address, "features.payment_method_update.enabled", "false"),
				resource.TestCheckResourceAttr(address, "metadata.team", "a"),
				resource.TestCheckResourceAttrPair("data.stripe_billing_portal_configuration.test", "id", address, "id"),
				resource.TestCheckResourceAttr("data.stripe_billing_portal_configuration.test", "name", acct.Name("Default portal")),
			),
		}}, nil, "stripe_billing_portal_configuration"),
	})
}

// withDefault prepends a portal configuration that Stripe makes the account's default,
// because it is the first one created. The configuration under test depends on it, so it
// is an ordinary one that destroy can deactivate. The sandbox has a default already, so in
// smoke mode the prepended configuration is an ordinary one too.
func withDefault(c acctest.Config, headline, features, block string) string {
	cfg := configurationConfig(c, headline, features, "  depends_on = [stripe_billing_portal_configuration.default]\n"+block)
	return cfg + `
resource "stripe_billing_portal_configuration" "default" {
  features = {
    invoice_history = { enabled = true }
  }
}
`
}

func TestAccBillingPortalConfiguration_update(t *testing.T) {
	acct := acctest.NewAccount(t)
	resource.ParallelTest(t, resource.TestCase{
		ProtoV6ProviderFactories: tfacc.ProviderFactories,
		Steps: acctest.Forget(t, []resource.TestStep{
			{
				Config: withDefault(acct, "Manage your plan", "", "  metadata = { team = \"a\", tier = \"1\" }\n"),
				Check:  resource.TestCheckResourceAttr(address, "business_profile.headline", "Manage your plan"),
			},
			{
				Config: withDefault(acct, "Billing", `
    customer_update = {
      enabled         = true
      allowed_updates = ["email"]
    }
`, fmt.Sprintf(`
  name     = %q
  metadata = { team = "b" }
  active   = false
`, acct.Name("Renamed"))),
				ConfigPlanChecks: resource.ConfigPlanChecks{PreApply: []plancheck.PlanCheck{
					plancheck.ExpectResourceAction(address, plancheck.ResourceActionUpdate),
				}},
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr(address, "business_profile.headline", "Billing"),
					resource.TestCheckResourceAttr(address, "name", acct.Name("Renamed")),
					resource.TestCheckResourceAttr(address, "active", "false"),
					resource.TestCheckResourceAttr(address, "features.customer_update.enabled", "true"),
					resource.TestCheckResourceAttr(address, "metadata.%", "1"),
					resource.TestCheckResourceAttr(address, "metadata.team", "b"),
				),
			},
		}, nil, "stripe_billing_portal_configuration.default"),
	})
}

// A configuration has no field that forces replacement; `replace_triggered_by` replaces an
// ordinary one by deactivating it, and a taint of the default fails at apply, because Stripe
// refuses to deactivate the default.
func TestAccBillingPortalConfiguration_replace(t *testing.T) {
	acct := acctest.NewAccount(t)
	var first string
	config := func(trigger string) string {
		return withDefault(acct, "Manage your plan", "", `
  lifecycle {
    replace_triggered_by = [terraform_data.trigger]
  }
`) + fmt.Sprintf(`
resource "terraform_data" "trigger" {
  input = %q
}
`, trigger)
	}
	steps := []resource.TestStep{
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
	}
	// The sandbox's default predates the test, so in smoke mode the prepended configuration is
	// ordinary and a taint of it succeeds.
	if !acct.Smoke {
		steps = append(steps, resource.TestStep{Config: config("two"), Taint: []string{"stripe_billing_portal_configuration.default"}, ExpectError: tfacc.RefusedDestroy})
	}
	resource.ParallelTest(t, resource.TestCase{
		ProtoV6ProviderFactories: tfacc.ProviderFactories,
		Steps:                    acctest.Forget(t, steps, nil, "stripe_billing_portal_configuration.default"),
	})
}

func TestAccBillingPortalConfiguration_import(t *testing.T) {
	acct := acctest.NewAccount(t)
	cfg := configurationConfig(acct, "Manage your plan", `
    customer_update = {
      enabled         = true
      allowed_updates = ["email", "address"]
    }
`, fmt.Sprintf("  name = %q\n  metadata = { team = \"a\" }\n", acct.Name("Default portal")))
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
		}, nil, "stripe_billing_portal_configuration"),
	})
}

// Stripe cannot delete a configuration, so destroy deactivates an ordinary one. It refuses to
// deactivate the account's default, so dropping the default from the configuration, and
// `terraform destroy`, fail at plan time, and the default stays active. Only a fresh account
// makes the test's first configuration its default, so the test needs the fake.
func TestAccBillingPortalConfiguration_delete(t *testing.T) {
	acctest.RequireFake(t)
	acct := acctest.NewAccount(t)
	var ordinary, def string
	var mark int
	cfg := withDefault(acct, "Manage your plan", "", "")
	pathOf := func(dst *string, at string) resource.TestCheckFunc {
		return resource.TestCheckResourceAttrWith(at, "id", func(v string) error {
			*dst = "/v1/billing_portal/configurations/" + v
			return nil
		})
	}
	resource.ParallelTest(t, resource.TestCase{
		ProtoV6ProviderFactories: tfacc.ProviderFactories,
		Steps: acctest.Forget(t, []resource.TestStep{
			{
				Config: cfg,
				Check: resource.ComposeAggregateTestCheckFunc(
					pathOf(&ordinary, address),
					pathOf(&def, "stripe_billing_portal_configuration.default"),
					resource.TestCheckResourceAttr("stripe_billing_portal_configuration.default", "is_default", "true"),
					resource.TestCheckResourceAttr(address, "is_default", "false"),
					tfacc.LogMark(t, acct, &mark),
				),
			},
			{Config: acctest.Without(t, cfg, "stripe_billing_portal_configuration.default"), PlanOnly: true, ExpectError: tfacc.RefusedDestroy},
			{Config: cfg, Destroy: true, ExpectError: tfacc.RefusedDestroy},
		}, resource.ComposeAggregateTestCheckFunc(
			// The ordinary configuration depends on the default, so the last step destroys it.
			tfacc.Holds(t, acct, &ordinary, "active", wire.Bool(false)),
			tfacc.Untouched(t, acct, &def, &mark, "active", wire.Bool(true)),
		), "stripe_billing_portal_configuration.default"),
	})
}

func TestAccBillingPortalConfiguration_drift(t *testing.T) {
	acct := acctest.NewAccount(t)
	var id string
	cfg := configurationConfig(acct, "Manage your plan", "", fmt.Sprintf("  name = %q\n", acct.Name("Default portal")))
	resource.ParallelTest(t, resource.TestCase{
		ProtoV6ProviderFactories: tfacc.ProviderFactories,
		Steps: acctest.Forget(t, []resource.TestStep{
			{Config: cfg, Check: idOf(&id)},
			{
				PreConfig: func() {
					tfacc.Post(t, acct, "/v1/billing_portal/configurations/"+id, wire.Object{
						{Name: "name", Value: wire.String("Renamed")},
					})
				},
				Config: cfg,
				ConfigPlanChecks: resource.ConfigPlanChecks{PreApply: []plancheck.PlanCheck{
					plancheck.ExpectNonEmptyPlan(),
					plancheck.ExpectResourceAction(address, plancheck.ResourceActionUpdate),
				}},
				Check: resource.TestCheckResourceAttr(address, "name", acct.Name("Default portal")),
			},
		}, nil, "stripe_billing_portal_configuration"),
	})
}
