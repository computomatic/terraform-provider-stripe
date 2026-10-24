package terminal_location_test

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

const address = "stripe_terminal_location.test"

// locationConfig is a location in the given country with the given street line. extra
// lands inside the resource block. A location in the US has the suite's city, state and
// postal code, because Stripe requires its state (suite: "create in the US without address
// state").
func locationConfig(c acctest.Config, name, country, line1, extra string) string {
	place := `    city        = "Springfield"
    postal_code = "12345"
`
	if country == "US" {
		place = `    city        = "South San Francisco"
    state       = "CA"
    postal_code = "94080"
`
	}
	return c.ProviderBlock() + fmt.Sprintf(`
resource "stripe_terminal_location" "test" {
  display_name = %q
  address = {
    line1       = %q
%s    country     = %q
  }
%s}
`, c.Name(name), line1, place, country, extra)
}

func idOf(dst *string) resource.TestCheckFunc {
	return resource.TestCheckResourceAttrWith(address, "id", func(v string) error {
		*dst = v
		return nil
	})
}

func TestAccTerminalLocation_create(t *testing.T) {
	acct := acctest.NewAccount(t)
	resource.ParallelTest(t, resource.TestCase{
		ProtoV6ProviderFactories: tfacc.ProviderFactories,
		Steps: []resource.TestStep{{
			Config: locationConfig(acct, "Main store", "US", "1 Main St", "  phone = \"+15555550100\"\n  metadata = { team = \"a\" }\n") + `
data "stripe_terminal_location" "test" {
  id = stripe_terminal_location.test.id
}
`,
			Check: resource.ComposeAggregateTestCheckFunc(
				resource.TestCheckResourceAttrSet(address, "id"),
				resource.TestCheckResourceAttr(address, "display_name", acct.Name("Main store")),
				resource.TestCheckResourceAttr(address, "address.line1", "1 Main St"),
				resource.TestCheckResourceAttr(address, "address.country", "US"),
				resource.TestCheckResourceAttr(address, "phone", "+15555550100"),
				resource.TestCheckResourceAttr(address, "metadata.team", "a"),
				resource.TestCheckResourceAttrPair("data.stripe_terminal_location.test", "id", address, "id"),
				resource.TestCheckResourceAttr("data.stripe_terminal_location.test", "address.city", "South San Francisco"),
				// Stripe answers an omitted second line with an empty string.
				resource.TestCheckResourceAttr(address, "address.line2", ""),
			),
		}},
	})
}

func TestAccTerminalLocation_update(t *testing.T) {
	acct := acctest.NewAccount(t)
	var first string
	resource.ParallelTest(t, resource.TestCase{
		ProtoV6ProviderFactories: tfacc.ProviderFactories,
		Steps: []resource.TestStep{
			{Config: locationConfig(acct, "Main store", "US", "1 Main St", "  metadata = { team = \"a\", tier = \"1\" }\n"), Check: idOf(&first)},
			{
				Config: locationConfig(acct, "Flagship", "US", "2 Main St", "  metadata = { team = \"b\" }\n"),
				ConfigPlanChecks: resource.ConfigPlanChecks{PreApply: []plancheck.PlanCheck{
					plancheck.ExpectResourceAction(address, plancheck.ResourceActionUpdate),
				}},
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr(address, "display_name", acct.Name("Flagship")),
					resource.TestCheckResourceAttr(address, "address.line1", "2 Main St"),
					resource.TestCheckResourceAttr(address, "metadata.%", "1"),
					resource.TestCheckResourceAttr(address, "metadata.team", "b"),
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

// Stripe refuses to change a location's country (suite: terminal_location_test.go, the
// address[country] update error), so a new country replaces the location.
func TestAccTerminalLocation_replace(t *testing.T) {
	acct := acctest.NewAccount(t)
	var first string
	resource.ParallelTest(t, resource.TestCase{
		ProtoV6ProviderFactories: tfacc.ProviderFactories,
		Steps: []resource.TestStep{
			{Config: locationConfig(acct, "Main store", "US", "1 Main St", ""), Check: idOf(&first)},
			{
				Config: locationConfig(acct, "Main store", "CA", "1 Main St", ""),
				ConfigPlanChecks: resource.ConfigPlanChecks{PreApply: []plancheck.PlanCheck{
					plancheck.ExpectResourceAction(address, plancheck.ResourceActionReplace),
				}},
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr(address, "address.country", "CA"),
					resource.TestCheckResourceAttrWith(address, "id", func(v string) error {
						if v == first {
							return fmt.Errorf("the replacement kept the location ID %s", v)
						}
						return nil
					}),
				),
			},
		},
	})
}

func TestAccTerminalLocation_import(t *testing.T) {
	acct := acctest.NewAccount(t)
	cfg := locationConfig(acct, "Main store", "US", "1 Main St", "  metadata = { team = \"a\" }\n")
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

// Stripe deletes a location with DELETE and answers a later GET with the deleted stub
// (suite: terminal_location_test.go, "delete").
func TestAccTerminalLocation_delete(t *testing.T) {
	acct := acctest.NewAccount(t)
	var id string
	resource.ParallelTest(t, resource.TestCase{
		ProtoV6ProviderFactories: tfacc.ProviderFactories,
		CheckDestroy: func(*terraform.State) error {
			resp, err := tfacc.Client(acct).Do(t.Context(), stripeapi.Request{Method: "GET", Path: "/v1/terminal/locations/" + id})
			if err != nil && !stripeapi.IsNotFound(err) {
				return err
			}
			if err == nil {
				if deleted, _ := resp.Body.(wire.Object).GetBool("deleted"); !deleted {
					return fmt.Errorf("location %s should be deleted after destroy", id)
				}
			}
			return nil
		},
		Steps: []resource.TestStep{{Config: locationConfig(acct, "Main store", "US", "1 Main St", ""), Check: idOf(&id)}},
	})
}

func TestAccTerminalLocation_drift(t *testing.T) {
	acct := acctest.NewAccount(t)
	var id string
	cfg := locationConfig(acct, "Main store", "US", "1 Main St", "")
	resource.ParallelTest(t, resource.TestCase{
		ProtoV6ProviderFactories: tfacc.ProviderFactories,
		Steps: []resource.TestStep{
			{Config: cfg, Check: idOf(&id)},
			{
				PreConfig: func() {
					tfacc.Post(t, acct, "/v1/terminal/locations/"+id, wire.Object{
						{Name: "display_name", Value: wire.String("Renamed")},
					})
				},
				Config: cfg,
				ConfigPlanChecks: resource.ConfigPlanChecks{PreApply: []plancheck.PlanCheck{
					plancheck.ExpectNonEmptyPlan(),
					plancheck.ExpectResourceAction(address, plancheck.ResourceActionUpdate),
				}},
				Check: resource.TestCheckResourceAttr(address, "display_name", acct.Name("Main store")),
			},
		},
	})
}

// A location deleted out of band reads back as the deleted stub, which Terraform treats as
// gone and plans to create again.
func TestAccTerminalLocation_driftDeleted(t *testing.T) {
	acct := acctest.NewAccount(t)
	var id string
	cfg := locationConfig(acct, "Main store", "US", "1 Main St", "")
	resource.ParallelTest(t, resource.TestCase{
		ProtoV6ProviderFactories: tfacc.ProviderFactories,
		Steps: []resource.TestStep{
			{Config: cfg, Check: idOf(&id)},
			{
				PreConfig: func() {
					if _, err := tfacc.Client(acct).Do(t.Context(), stripeapi.Request{Method: "DELETE", Path: "/v1/terminal/locations/" + id}); err != nil {
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
