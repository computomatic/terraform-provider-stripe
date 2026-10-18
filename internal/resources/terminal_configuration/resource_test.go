package terminal_configuration_test

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

const address = "stripe_terminal_configuration.test"

// configurationConfig is a terminal configuration. extra lands inside the resource block.
func configurationConfig(c acctest.Config, name string, extra string) string {
	return c.ProviderBlock() + fmt.Sprintf(`
resource "stripe_terminal_configuration" "test" {
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

const tipping = `
  tipping = {
    usd = {
      percentages         = [10, 15, 20]
      fixed_amounts       = [100, 200, 300]
      smart_tip_threshold = 1000
    }
  }
`

func TestAccTerminalConfiguration_create(t *testing.T) {
	acct := acctest.NewAccount(t)
	resource.ParallelTest(t, resource.TestCase{
		ProtoV6ProviderFactories: tfacc.ProviderFactories,
		Steps: []resource.TestStep{{
			Config: configurationConfig(acct, "Store readers", `
  offline       = { enabled = true }
  reboot_window = { start_hour = 2, end_hour = 5 }
`+tipping) + `
data "stripe_terminal_configuration" "test" {
  id = stripe_terminal_configuration.test.id
}
`,
			Check: resource.ComposeAggregateTestCheckFunc(
				resource.TestCheckResourceAttrSet(address, "id"),
				resource.TestCheckResourceAttr(address, "name", "Store readers"),
				resource.TestCheckResourceAttr(address, "offline.enabled", "true"),
				resource.TestCheckResourceAttr(address, "reboot_window.end_hour", "5"),
				resource.TestCheckResourceAttr(address, "tipping.usd.percentages.#", "3"),
				resource.TestCheckResourceAttr(address, "tipping.usd.smart_tip_threshold", "1000"),
				resource.TestCheckResourceAttr(address, "is_account_default", "false"),
				resource.TestCheckResourceAttrPair("data.stripe_terminal_configuration.test", "id", address, "id"),
				resource.TestCheckResourceAttr("data.stripe_terminal_configuration.test", "tipping.usd.fixed_amounts.#", "3"),
			),
		}},
	})
}

// A configuration needs no argument at all, and Stripe fills nothing into the ones left out.
func TestAccTerminalConfiguration_empty(t *testing.T) {
	acct := acctest.NewAccount(t)
	cfg := acct.ProviderBlock() + `
resource "stripe_terminal_configuration" "test" {}
`
	resource.ParallelTest(t, resource.TestCase{
		ProtoV6ProviderFactories: tfacc.ProviderFactories,
		Steps: []resource.TestStep{
			{Config: cfg, Check: resource.TestCheckResourceAttrSet(address, "id")},
			{Config: cfg, PlanOnly: true},
		},
	})
}

// The fake does not serve `wifi` yet (fake_not_implemented), so this test skips until it does.
func TestAccTerminalConfiguration_wifi(t *testing.T) {
	acct := acctest.NewAccount(t)
	cfg := configurationConfig(acct, "Wi-Fi readers", `
  wifi = {
    type = "personal_psk"
    personal_psk = {
      ssid     = "store"
      password = "correct horse battery"
    }
  }
`)
	resource.ParallelTest(t, resource.TestCase{
		ProtoV6ProviderFactories: tfacc.ProviderFactories,
		ErrorCheck:               tfacc.SkipWhenFakeLacks(t, "wifi"),
		Steps: []resource.TestStep{
			{Config: cfg, Check: resource.TestCheckResourceAttr(address, "wifi.personal_psk.ssid", "store")},
			{Config: cfg, PlanOnly: true},
		},
	})
}

func TestAccTerminalConfiguration_update(t *testing.T) {
	acct := acctest.NewAccount(t)
	var first string
	resource.ParallelTest(t, resource.TestCase{
		ProtoV6ProviderFactories: tfacc.ProviderFactories,
		Steps: []resource.TestStep{
			{Config: configurationConfig(acct, "Store readers", "  offline = { enabled = true }\n"+tipping), Check: idOf(&first)},
			{
				Config: configurationConfig(acct, "Flagship readers", "  offline = { enabled = false }\n  reboot_window = { start_hour = 1, end_hour = 4 }\n"+tipping),
				ConfigPlanChecks: resource.ConfigPlanChecks{PreApply: []plancheck.PlanCheck{
					plancheck.ExpectResourceAction(address, plancheck.ResourceActionUpdate),
				}},
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr(address, "name", "Flagship readers"),
					resource.TestCheckResourceAttr(address, "offline.enabled", "false"),
					resource.TestCheckResourceAttr(address, "reboot_window.start_hour", "1"),
					resource.TestCheckResourceAttr(address, "tipping.usd.percentages.#", "3"),
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

// Removing an object from the configuration unsets it. The fake's handler panics on that
// request for tipping (fake_internal_error), so this test skips, naming the fake bug, until
// the fake is fixed. Any other error fails the test.
func TestAccTerminalConfiguration_unsetTipping(t *testing.T) {
	acct := acctest.NewAccount(t)
	resource.ParallelTest(t, resource.TestCase{
		ProtoV6ProviderFactories: tfacc.ProviderFactories,
		ErrorCheck: func(err error) error {
			if err != nil && strings.Contains(err.Error(), "fake_internal_error") && strings.Contains(err.Error(), "panicked") {
				t.Skip("the fake panics when tipping is removed from a terminal configuration; fake bug, to be fixed")
			}
			return err
		},
		Steps: []resource.TestStep{
			{Config: configurationConfig(acct, "Store readers", tipping), Check: resource.TestCheckResourceAttr(address, "tipping.usd.smart_tip_threshold", "1000")},
			{Config: configurationConfig(acct, "Store readers", ""), Check: resource.TestCheckNoResourceAttr(address, "tipping.usd.smart_tip_threshold")},
		},
	})
}

// Stripe accepts every create field on update, so no argument forces a replacement. A
// replacement forced from outside (a changed trigger) deletes the configuration and
// creates another.
func TestAccTerminalConfiguration_replace(t *testing.T) {
	acct := acctest.NewAccount(t)
	var first string
	config := func(trigger string) string {
		return configurationConfig(acct, "Store readers", `
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

func TestAccTerminalConfiguration_import(t *testing.T) {
	acct := acctest.NewAccount(t)
	cfg := configurationConfig(acct, "Store readers", "  offline = { enabled = true }\n"+tipping)
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

// Stripe deletes a configuration with DELETE and answers a later GET with the deleted stub
// (suite: terminal_configuration_test.go, "delete").
func TestAccTerminalConfiguration_delete(t *testing.T) {
	acct := acctest.NewAccount(t)
	var id string
	resource.ParallelTest(t, resource.TestCase{
		ProtoV6ProviderFactories: tfacc.ProviderFactories,
		CheckDestroy: func(*terraform.State) error {
			resp, err := tfacc.Client(acct).Do(t.Context(), stripeapi.Request{Method: "GET", Path: "/v1/terminal/configurations/" + id})
			if err != nil && !stripeapi.IsNotFound(err) {
				return err
			}
			if err == nil {
				if deleted, _ := resp.Body.(wire.Object).GetBool("deleted"); !deleted {
					return fmt.Errorf("configuration %s should be deleted after destroy", id)
				}
			}
			return nil
		},
		Steps: []resource.TestStep{{Config: configurationConfig(acct, "Store readers", ""), Check: idOf(&id)}},
	})
}

func TestAccTerminalConfiguration_drift(t *testing.T) {
	acct := acctest.NewAccount(t)
	var id string
	cfg := configurationConfig(acct, "Store readers", "  offline = { enabled = true }\n")
	resource.ParallelTest(t, resource.TestCase{
		ProtoV6ProviderFactories: tfacc.ProviderFactories,
		Steps: []resource.TestStep{
			{Config: cfg, Check: idOf(&id)},
			{
				PreConfig: func() {
					tfacc.Post(t, acct, "/v1/terminal/configurations/"+id, wire.Object{
						{Name: "offline", Value: wire.Object{{Name: "enabled", Value: wire.Bool(false)}}},
					})
				},
				Config: cfg,
				ConfigPlanChecks: resource.ConfigPlanChecks{PreApply: []plancheck.PlanCheck{
					plancheck.ExpectNonEmptyPlan(),
					plancheck.ExpectResourceAction(address, plancheck.ResourceActionUpdate),
				}},
				Check: resource.TestCheckResourceAttr(address, "offline.enabled", "true"),
			},
		},
	})
}
