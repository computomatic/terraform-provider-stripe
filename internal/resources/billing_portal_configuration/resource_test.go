package billing_portal_configuration_test

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
		Steps: []resource.TestStep{{
			Config: configurationConfig(acct, "Manage your plan", `
    customer_update = {
      enabled         = true
      allowed_updates = ["email", "address"]
    }
`, `
  name               = "Default portal"
  default_return_url = "https://example.com/account"
  metadata           = { team = "a" }
`) + `
data "stripe_billing_portal_configuration" "test" {
  id = stripe_billing_portal_configuration.test.id
}
`,
			Check: resource.ComposeAggregateTestCheckFunc(
				resource.TestCheckResourceAttrSet(address, "id"),
				resource.TestCheckResourceAttr(address, "name", "Default portal"),
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
				resource.TestCheckResourceAttr("data.stripe_billing_portal_configuration.test", "name", "Default portal"),
			),
		}},
	})
}

// withDefault prepends a portal configuration that Stripe makes the account's default,
// because it is the first one created. The configuration under test depends on it, so it
// is an ordinary one that destroy can deactivate.
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
		Steps: []resource.TestStep{
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
`, `
  name     = "Renamed"
  metadata = { team = "b" }
  active   = false
`),
				ConfigPlanChecks: resource.ConfigPlanChecks{PreApply: []plancheck.PlanCheck{
					plancheck.ExpectResourceAction(address, plancheck.ResourceActionUpdate),
				}},
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr(address, "business_profile.headline", "Billing"),
					resource.TestCheckResourceAttr(address, "name", "Renamed"),
					resource.TestCheckResourceAttr(address, "active", "false"),
					resource.TestCheckResourceAttr(address, "features.customer_update.enabled", "true"),
					resource.TestCheckResourceAttr(address, "metadata.%", "1"),
					resource.TestCheckResourceAttr(address, "metadata.team", "b"),
				),
			},
		},
	})
}

// Stripe accepts every create field on update, so no argument forces a replacement. A
// replacement forced from outside (a changed trigger) destroys the configuration, which
// leaves the default one active with a warning, and creates another.
func TestAccBillingPortalConfiguration_replace(t *testing.T) {
	acct := acctest.NewAccount(t)
	var first string
	config := func(trigger string) string {
		return configurationConfig(acct, "Manage your plan", "", `
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

func TestAccBillingPortalConfiguration_import(t *testing.T) {
	acct := acctest.NewAccount(t)
	cfg := configurationConfig(acct, "Manage your plan", `
    customer_update = {
      enabled         = true
      allowed_updates = ["email", "address"]
    }
`, "  name = \"Default portal\"\n  metadata = { team = \"a\" }\n")
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

// Stripe cannot delete a portal configuration, so destroy deactivates it (suite:
// billing_portal_configuration_test.go). It refuses to deactivate the account's default,
// the first one created (suite: "The first portal configuration an account creates becomes
// its default"): destroy then leaves that one active and says so in a warning.
func TestAccBillingPortalConfiguration_delete(t *testing.T) {
	acct := acctest.NewAccount(t)
	var ordinary, def string
	active := func(id string) (bool, error) {
		resp, err := tfacc.Client(acct).Do(t.Context(), stripeapi.Request{Method: "GET", Path: "/v1/billing_portal/configurations/" + id})
		if err != nil {
			return false, fmt.Errorf("a portal configuration is never deleted: %w", err)
		}
		isActive, _ := resp.Body.(wire.Object).GetBool("active")
		return isActive, nil
	}
	resource.ParallelTest(t, resource.TestCase{
		ProtoV6ProviderFactories: tfacc.ProviderFactories,
		CheckDestroy: func(*terraform.State) error {
			if on, err := active(ordinary); err != nil || on {
				return fmt.Errorf("configuration %s should be deactivated after destroy (err %v)", ordinary, err)
			}
			if on, err := active(def); err != nil || !on {
				return fmt.Errorf("the default configuration %s should stay active after destroy (err %v)", def, err)
			}
			return nil
		},
		Steps: []resource.TestStep{{
			Config: withDefault(acct, "Manage your plan", "", ""),
			Check: resource.ComposeAggregateTestCheckFunc(
				idOf(&ordinary),
				resource.TestCheckResourceAttrWith("stripe_billing_portal_configuration.default", "id", func(v string) error {
					def = v
					return nil
				}),
				resource.TestCheckResourceAttr("stripe_billing_portal_configuration.default", "is_default", "true"),
				resource.TestCheckResourceAttr(address, "is_default", "false"),
			),
		}},
	})
}

func TestAccBillingPortalConfiguration_drift(t *testing.T) {
	acct := acctest.NewAccount(t)
	var id string
	cfg := configurationConfig(acct, "Manage your plan", "", "  name = \"Default portal\"\n")
	resource.ParallelTest(t, resource.TestCase{
		ProtoV6ProviderFactories: tfacc.ProviderFactories,
		Steps: []resource.TestStep{
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
				Check: resource.TestCheckResourceAttr(address, "name", "Default portal"),
			},
		},
	})
}
