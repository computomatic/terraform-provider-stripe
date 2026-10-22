package test_helpers_test_clock_test

import (
	"fmt"
	"os"
	"regexp"
	"strconv"
	"testing"
	"time"

	"github.com/hashicorp/go-version"
	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/plancheck"
	"github.com/hashicorp/terraform-plugin-testing/terraform"
	"github.com/hashicorp/terraform-plugin-testing/tfversion"

	"github.com/computomatic/terraform-provider-stripe/internal/acctest"
	"github.com/computomatic/terraform-provider-stripe/internal/acctest/tfacc"
	"github.com/computomatic/terraform-provider-stripe/internal/stripeapi"
	"github.com/computomatic/terraform-provider-stripe/internal/wire"
)

func TestMain(m *testing.M) { os.Exit(acctest.Main(m)) }

const (
	address = "stripe_test_helpers_test_clock.test"
	// start is a Unix time inside the range Stripe accepts (2000-01-01 up to 2100-01-01;
	// suite: "Test clocks"). A clock created with a time in the past is allowed.
	start int64 = 1_790_000_000
	day   int64 = 24 * 60 * 60
)

// clockConfig is a clock frozen at the given time, with a customer attached when withCustomer
// is set.
func clockConfig(c acctest.Config, name string, frozen int64, withCustomer bool) string {
	cfg := c.ProviderBlock() + fmt.Sprintf(`
resource "stripe_test_helpers_test_clock" "test" {
  name        = %q
  frozen_time = %d
}
`, c.Name(name), frozen)
	if withCustomer {
		cfg += fmt.Sprintf(`
resource "stripe_customer" "test" {
  name       = %q
  test_clock = stripe_test_helpers_test_clock.test.id
}
`, c.Name("Clock customer"))
	}
	return cfg
}

func idOf(id *string) resource.TestCheckFunc {
	return resource.TestCheckResourceAttrWith(address, "id", func(v string) error { *id = v; return nil })
}

// awaitReady waits until the clock has finished advancing, because a refresh during an
// advance reads the time the clock still has.
func awaitReady(t *testing.T, c acctest.Config, id string) {
	t.Helper()
	deadline := time.Now().Add(time.Minute)
	for time.Now().Before(deadline) {
		resp, err := tfacc.Client(c).Do(t.Context(), stripeapi.Request{Method: "GET", Path: "/v1/test_helpers/test_clocks/" + id})
		if err != nil {
			t.Fatal(err)
		}
		if status, _ := resp.Body.(wire.Object).GetString("status"); status == "ready" {
			return
		}
		time.Sleep(200 * time.Millisecond)
	}
	t.Fatalf("clock %s did not become ready", id)
}

func TestAccTestHelpersTestClock_create(t *testing.T) {
	acct := acctest.NewAccount(t)
	resource.ParallelTest(t, resource.TestCase{
		ProtoV6ProviderFactories: tfacc.ProviderFactories,
		Steps: []resource.TestStep{{
			Config: clockConfig(acct, "Renewals", start, true) + `
data "stripe_test_helpers_test_clock" "test" {
  id = stripe_test_helpers_test_clock.test.id
}

resource "stripe_test_helpers_test_clock" "unnamed" {
  frozen_time = 946684800
}
`,
			Check: resource.ComposeAggregateTestCheckFunc(
				resource.TestCheckResourceAttrSet(address, "id"),
				resource.TestCheckResourceAttr(address, "name", acct.Name("Renewals")),
				resource.TestCheckResourceAttr(address, "frozen_time", strconv.FormatInt(start, 10)),
				resource.TestCheckResourceAttr(address, "status", "ready"),
				resource.TestCheckResourceAttrSet(address, "deletes_after"),
				resource.TestCheckResourceAttrPair("stripe_customer.test", "test_clock", address, "id"),
				resource.TestCheckResourceAttrPair("data.stripe_test_helpers_test_clock.test", "id", address, "id"),
				resource.TestCheckResourceAttr("data.stripe_test_helpers_test_clock.test", "frozen_time", strconv.FormatInt(start, 10)),
				resource.TestCheckResourceAttr("data.stripe_test_helpers_test_clock.test", "status", "ready"),
				// The earliest time Stripe accepts, and no name.
				resource.TestCheckResourceAttr("stripe_test_helpers_test_clock.unnamed", "frozen_time", "946684800"),
				resource.TestCheckNoResourceAttr("stripe_test_helpers_test_clock.unnamed", "name"),
			),
		}},
	})
}

// A later frozen_time advances the clock in place and the apply waits for it to be ready
// (suite: "Advancing a test clock"). A time before the current one is refused at plan time.
func TestAccTestHelpersTestClock_update(t *testing.T) {
	acct := acctest.NewAccount(t)
	var first string
	inPlace := resource.ConfigPlanChecks{PreApply: []plancheck.PlanCheck{
		plancheck.ExpectResourceAction(address, plancheck.ResourceActionUpdate),
	}}
	sameClock := resource.TestCheckResourceAttrWith(address, "id", func(v string) error {
		if v != first {
			return fmt.Errorf("the advance replaced the clock %s with %s", first, v)
		}
		return nil
	})
	resource.ParallelTest(t, resource.TestCase{
		ProtoV6ProviderFactories: tfacc.ProviderFactories,
		Steps: []resource.TestStep{
			{Config: clockConfig(acct, "Renewals", start, true), Check: idOf(&first)},
			{
				Config:           clockConfig(acct, "Renewals", start+day, true),
				ConfigPlanChecks: inPlace,
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr(address, "frozen_time", strconv.FormatInt(start+day, 10)),
					resource.TestCheckResourceAttr(address, "status", "ready"),
					resource.TestCheckResourceAttrPair("stripe_customer.test", "test_clock", address, "id"),
					sameClock,
				),
			},
			{
				Config:      clockConfig(acct, "Renewals", start, true),
				PlanOnly:    true,
				ExpectError: regexp.MustCompile("Cannot move `frozen_time` backwards"),
			},
			// The same time again changes nothing.
			{Config: clockConfig(acct, "Renewals", start+day, true), PlanOnly: true},
			{
				Config:           clockConfig(acct, "Renewals", start+3*day, true),
				ConfigPlanChecks: inPlace,
				Check:            resource.ComposeAggregateTestCheckFunc(resource.TestCheckResourceAttr(address, "frozen_time", strconv.FormatInt(start+3*day, 10)), sameClock),
			},
		},
	})
}

// Stripe takes a clock's name on create only, so a new name replaces the clock.
func TestAccTestHelpersTestClock_replace(t *testing.T) {
	acct := acctest.NewAccount(t)
	var first string
	resource.ParallelTest(t, resource.TestCase{
		ProtoV6ProviderFactories: tfacc.ProviderFactories,
		Steps: []resource.TestStep{
			{Config: clockConfig(acct, "Renewals", start, false), Check: idOf(&first)},
			{
				Config: clockConfig(acct, "Renewals again", start, false),
				ConfigPlanChecks: resource.ConfigPlanChecks{PreApply: []plancheck.PlanCheck{
					plancheck.ExpectResourceAction(address, plancheck.ResourceActionReplace),
				}},
				Check: resource.TestCheckResourceAttrWith(address, "id", func(v string) error {
					if v == first {
						return fmt.Errorf("the replacement kept the clock %s", v)
					}
					return nil
				}),
			},
		},
	})
}

func TestAccTestHelpersTestClock_import(t *testing.T) {
	acct := acctest.NewAccount(t)
	cfg := clockConfig(acct, "Renewals", start, false)
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

// The identity form needs Terraform or OpenTofu 1.12.0 or later.
func TestAccTestHelpersTestClock_importIdentity(t *testing.T) {
	acct := acctest.NewAccount(t)
	cfg := clockConfig(acct, "Renewals", start, false)
	resource.ParallelTest(t, resource.TestCase{
		ProtoV6ProviderFactories: tfacc.ProviderFactories,
		TerraformVersionChecks:   []tfversion.TerraformVersionCheck{tfversion.RequireAbove(version.Must(version.NewVersion("1.12.0")))},
		Steps: []resource.TestStep{
			{Config: cfg},
			{
				ResourceName:    address,
				ImportState:     true,
				ImportStateKind: resource.ImportBlockWithResourceIdentity,
				Config:          cfg,
			},
		},
	})
}

// Destroy deletes the clock (suite: "delete"), and with it the customer attached to it
// (suite: "deleting the clock deletes what is attached to it"). The customer answers the
// deleted stub or a 404, depending on how far the deletion has got.
func TestAccTestHelpersTestClock_delete(t *testing.T) {
	acct := acctest.NewAccount(t)
	var clock, customer string
	resource.ParallelTest(t, resource.TestCase{
		ProtoV6ProviderFactories: tfacc.ProviderFactories,
		CheckDestroy: func(*terraform.State) error {
			ctx := t.Context()
			if _, err := tfacc.Client(acct).Do(ctx, stripeapi.Request{Method: "GET", Path: "/v1/test_helpers/test_clocks/" + clock}); err == nil {
				return fmt.Errorf("clock %s still exists after destroy", clock)
			}
			resp, err := tfacc.Client(acct).Do(ctx, stripeapi.Request{Method: "GET", Path: "/v1/customers/" + customer})
			if err != nil {
				return nil
			}
			if deleted, _ := resp.Body.(wire.Object).GetBool("deleted"); !deleted {
				return fmt.Errorf("customer %s survived the deletion of its clock", customer)
			}
			return nil
		},
		Steps: []resource.TestStep{{
			Config: clockConfig(acct, "Renewals", start, true),
			Check: resource.ComposeTestCheckFunc(
				idOf(&clock),
				resource.TestCheckResourceAttrWith("stripe_customer.test", "id", func(v string) error { customer = v; return nil }),
			),
		}},
	})
}

// A clock deleted out of band is created again.
func TestAccTestHelpersTestClock_drift(t *testing.T) {
	acct := acctest.NewAccount(t)
	var id string
	cfg := clockConfig(acct, "Renewals", start, false)
	resource.ParallelTest(t, resource.TestCase{
		ProtoV6ProviderFactories: tfacc.ProviderFactories,
		Steps: []resource.TestStep{
			{Config: cfg, Check: idOf(&id)},
			{
				PreConfig: func() {
					if _, err := tfacc.Client(acct).Do(t.Context(), stripeapi.Request{Method: "DELETE", Path: "/v1/test_helpers/test_clocks/" + id}); err != nil {
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
						return fmt.Errorf("the clock %s was not created again", v)
					}
					return nil
				}),
			},
		},
	})
}

// A clock advanced out of band is ahead of the configuration, which Stripe cannot undo: the
// plan says so instead of planning a change. Raising the configuration to the clock's time
// settles it with an advance to the time it already has.
func TestAccTestHelpersTestClock_driftAdvanced(t *testing.T) {
	acct := acctest.NewAccount(t)
	var id string
	resource.ParallelTest(t, resource.TestCase{
		ProtoV6ProviderFactories: tfacc.ProviderFactories,
		Steps: []resource.TestStep{
			{Config: clockConfig(acct, "Renewals", start, false), Check: idOf(&id)},
			{
				PreConfig: func() {
					tfacc.Post(t, acct, "/v1/test_helpers/test_clocks/"+id+"/advance", wire.Object{{Name: "frozen_time", Value: wire.Int(start + day)}})
					awaitReady(t, acct, id)
				},
				Config:      clockConfig(acct, "Renewals", start, false),
				PlanOnly:    true,
				ExpectError: regexp.MustCompile("Cannot move `frozen_time` backwards"),
			},
			{Config: clockConfig(acct, "Renewals", start+day, false), Check: resource.TestCheckResourceAttr(address, "frozen_time", strconv.FormatInt(start+day, 10))},
		},
	})
}
