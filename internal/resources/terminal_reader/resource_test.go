package terminal_reader_test

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

const address = "stripe_terminal_reader.test"

// simulatedCode registers a simulated reader in test mode (suite: "TerminalReader").
const simulatedCode = "simulated-wpe"

// readerConfig is two locations and a simulated reader at the one named by location, `test`
// or `other`. extra lands inside the reader.
func readerConfig(c acctest.Config, location, extra string) string {
	return c.ProviderBlock() + fmt.Sprintf(`
resource "stripe_terminal_location" "test" {
  display_name = %q
  address = {
    line1       = "1 Main St"
    city        = "Springfield"
    postal_code = "12345"
    country     = "US"
  }
}

resource "stripe_terminal_location" "other" {
  display_name = %q
  address = {
    line1       = "2 Main St"
    city        = "Springfield"
    postal_code = "12345"
    country     = "US"
  }
}

resource "stripe_terminal_reader" "test" {
  registration_code = %q
  location          = stripe_terminal_location.%s.id
%s}
`, c.Name("Reader store"), c.Name("Reader store two"), simulatedCode, location, extra)
}

func idOf(dst *string) resource.TestCheckFunc {
	return resource.TestCheckResourceAttrWith(address, "id", func(v string) error {
		*dst = v
		return nil
	})
}

func TestAccTerminalReader_create(t *testing.T) {
	acct := acctest.NewAccount(t)
	resource.ParallelTest(t, resource.TestCase{
		ProtoV6ProviderFactories: tfacc.ProviderFactories,
		Steps: []resource.TestStep{{
			Config: readerConfig(acct, "test", fmt.Sprintf("  label = %q\n  metadata = { lane = \"1\" }\n", acct.Name("Front Counter"))) + `
data "stripe_terminal_reader" "test" {
  id = stripe_terminal_reader.test.id
}
`,
			Check: resource.ComposeAggregateTestCheckFunc(
				resource.TestCheckResourceAttrSet(address, "id"),
				resource.TestCheckResourceAttr(address, "label", acct.Name("Front Counter")),
				resource.TestCheckResourceAttrPair(address, "location", "stripe_terminal_location.test", "id"),
				resource.TestCheckResourceAttr(address, "metadata.lane", "1"),
				resource.TestCheckResourceAttr(address, "status", "online"),
				resource.TestCheckResourceAttrSet(address, "serial_number"),
				resource.TestCheckResourceAttrSet(address, "device_type"),
				resource.TestCheckResourceAttrPair("data.stripe_terminal_reader.test", "id", address, "id"),
				resource.TestCheckResourceAttrPair("data.stripe_terminal_reader.test", "serial_number", address, "serial_number"),
				resource.TestCheckResourceAttr("data.stripe_terminal_reader.test", "label", acct.Name("Front Counter")),
			),
		}},
	})
}

// A reader without a label gets the registration code with a UUID appended (suite: "create
// minimal"), which the plan reads back instead of reporting a difference.
func TestAccTerminalReader_defaultLabel(t *testing.T) {
	acct := acctest.NewAccount(t)
	cfg := readerConfig(acct, "test", "")
	resource.ParallelTest(t, resource.TestCase{
		ProtoV6ProviderFactories: tfacc.ProviderFactories,
		Steps: []resource.TestStep{
			{Config: cfg, Check: resource.TestCheckResourceAttrSet(address, "label")},
			{Config: cfg, PlanOnly: true},
		},
	})
}

// A label and metadata update in place (suite: "update").
func TestAccTerminalReader_update(t *testing.T) {
	acct := acctest.NewAccount(t)
	var first string
	resource.ParallelTest(t, resource.TestCase{
		ProtoV6ProviderFactories: tfacc.ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: readerConfig(acct, "test", fmt.Sprintf("  label = %q\n  metadata = { keep = \"me\", drop = \"me\" }\n", acct.Name("Before"))),
				Check:  idOf(&first),
			},
			{
				Config: readerConfig(acct, "test", fmt.Sprintf("  label = %q\n  metadata = { keep = \"me\", new = \"v\" }\n", acct.Name("After"))),
				ConfigPlanChecks: resource.ConfigPlanChecks{PreApply: []plancheck.PlanCheck{
					plancheck.ExpectResourceAction(address, plancheck.ResourceActionUpdate),
				}},
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr(address, "label", acct.Name("After")),
					resource.TestCheckResourceAttr(address, "metadata.%", "2"),
					resource.TestCheckResourceAttr(address, "metadata.new", "v"),
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

// Stripe refuses to change a reader's location (suite: "update errors", "immutable or unknown
// location"), so a new location replaces the reader.
func TestAccTerminalReader_replace(t *testing.T) {
	acct := acctest.NewAccount(t)
	var first string
	resource.ParallelTest(t, resource.TestCase{
		ProtoV6ProviderFactories: tfacc.ProviderFactories,
		Steps: []resource.TestStep{
			{Config: readerConfig(acct, "test", ""), Check: idOf(&first)},
			{
				Config: readerConfig(acct, "other", ""),
				ConfigPlanChecks: resource.ConfigPlanChecks{PreApply: []plancheck.PlanCheck{
					plancheck.ExpectResourceAction(address, plancheck.ResourceActionReplace),
				}},
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttrPair(address, "location", "stripe_terminal_location.other", "id"),
					resource.TestCheckResourceAttrWith(address, "id", func(v string) error {
						if v == first {
							return fmt.Errorf("the replacement kept the reader ID %s", v)
						}
						return nil
					}),
				),
			},
		},
	})
}

// Stripe never returns the registration code, so an import leaves it out of state and a
// configuration that sets it ignores changes to it.
func TestAccTerminalReader_import(t *testing.T) {
	acct := acctest.NewAccount(t)
	extra := fmt.Sprintf("  label = %q\n  metadata = { lane = \"1\" }\n", acct.Name("Front Counter"))
	cfg := readerConfig(acct, "test", extra)
	imported := readerConfig(acct, "test", extra+"  lifecycle {\n    ignore_changes = [registration_code]\n  }\n")
	resource.ParallelTest(t, resource.TestCase{
		ProtoV6ProviderFactories: tfacc.ProviderFactories,
		Steps: []resource.TestStep{
			{Config: cfg},
			{
				ResourceName:            address,
				ImportState:             true,
				ImportStateVerify:       true,
				ImportStateVerifyIgnore: []string{"registration_code"},
			},
			{
				ResourceName:    address,
				ImportState:     true,
				ImportStateKind: resource.ImportBlockWithID,
				Config:          imported,
			},
		},
	})
}

// Stripe deletes a reader with DELETE and answers a later GET with the deleted stub (suite:
// "delete").
func TestAccTerminalReader_delete(t *testing.T) {
	acct := acctest.NewAccount(t)
	var id string
	resource.ParallelTest(t, resource.TestCase{
		ProtoV6ProviderFactories: tfacc.ProviderFactories,
		CheckDestroy: func(*terraform.State) error {
			resp, err := tfacc.Client(acct).Do(t.Context(), stripeapi.Request{Method: "GET", Path: "/v1/terminal/readers/" + id})
			if err != nil && !stripeapi.IsNotFound(err) {
				return err
			}
			if err == nil {
				if deleted, _ := resp.Body.(wire.Object).GetBool("deleted"); !deleted {
					return fmt.Errorf("reader %s should be deleted after destroy", id)
				}
			}
			return nil
		},
		Steps: []resource.TestStep{{Config: readerConfig(acct, "test", ""), Check: idOf(&id)}},
	})
}

func TestAccTerminalReader_drift(t *testing.T) {
	acct := acctest.NewAccount(t)
	var id string
	cfg := readerConfig(acct, "test", fmt.Sprintf("  label = %q\n", acct.Name("Front Counter")))
	resource.ParallelTest(t, resource.TestCase{
		ProtoV6ProviderFactories: tfacc.ProviderFactories,
		Steps: []resource.TestStep{
			{Config: cfg, Check: idOf(&id)},
			{
				PreConfig: func() {
					tfacc.Post(t, acct, "/v1/terminal/readers/"+id, wire.Object{{Name: "label", Value: wire.String("Renamed")}})
				},
				Config: cfg,
				ConfigPlanChecks: resource.ConfigPlanChecks{PreApply: []plancheck.PlanCheck{
					plancheck.ExpectNonEmptyPlan(),
					plancheck.ExpectResourceAction(address, plancheck.ResourceActionUpdate),
				}},
				Check: resource.TestCheckResourceAttr(address, "label", acct.Name("Front Counter")),
			},
		},
	})
}

// A reader deleted out of band reads back as the deleted stub, which Terraform treats as
// gone and plans to create again.
func TestAccTerminalReader_driftDeleted(t *testing.T) {
	acct := acctest.NewAccount(t)
	var id string
	cfg := readerConfig(acct, "test", "")
	resource.ParallelTest(t, resource.TestCase{
		ProtoV6ProviderFactories: tfacc.ProviderFactories,
		Steps: []resource.TestStep{
			{Config: cfg, Check: idOf(&id)},
			{
				PreConfig: func() {
					if _, err := tfacc.Client(acct).Do(t.Context(), stripeapi.Request{Method: "DELETE", Path: "/v1/terminal/readers/" + id}); err != nil {
						t.Fatal(err)
					}
				},
				Config: cfg,
				ConfigPlanChecks: resource.ConfigPlanChecks{PreApply: []plancheck.PlanCheck{
					plancheck.ExpectNonEmptyPlan(),
					plancheck.ExpectResourceAction(address, plancheck.ResourceActionCreate),
				}},
			},
		},
	})
}
