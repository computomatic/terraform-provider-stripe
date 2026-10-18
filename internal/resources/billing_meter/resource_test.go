package billing_meter_test

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

const address = "stripe_billing_meter.test"

// meterConfig is a sum meter. Both names are made unique to a smoke run.
func meterConfig(c acctest.Config, eventName, displayName string) string {
	return c.ProviderBlock() + fmt.Sprintf(`
resource "stripe_billing_meter" "test" {
  display_name = %q
  event_name   = %q
  default_aggregation = {
    formula = "sum"
  }
  customer_mapping = {
    type              = "by_id"
    event_payload_key = "stripe_customer_id"
  }
  value_settings = {
    event_payload_key = "value"
  }
}
`, c.Name(displayName), c.Unique(eventName))
}

func idOf(dst *string) resource.TestCheckFunc {
	return resource.TestCheckResourceAttrWith(address, "id", func(v string) error {
		*dst = v
		return nil
	})
}

func TestAccBillingMeter_create(t *testing.T) {
	acct := acctest.NewAccount(t)
	resource.ParallelTest(t, resource.TestCase{
		ProtoV6ProviderFactories: tfacc.ProviderFactories,
		Steps: []resource.TestStep{{
			Config: meterConfig(acct, "create_calls", "API calls") + `
data "stripe_billing_meter" "test" {
  id = stripe_billing_meter.test.id
}
`,
			Check: resource.ComposeAggregateTestCheckFunc(
				resource.TestCheckResourceAttrSet(address, "id"),
				resource.TestCheckResourceAttr(address, "display_name", acct.Name("API calls")),
				resource.TestCheckResourceAttr(address, "event_name", acct.Unique("create_calls")),
				resource.TestCheckResourceAttr(address, "default_aggregation.formula", "sum"),
				resource.TestCheckResourceAttr(address, "customer_mapping.type", "by_id"),
				resource.TestCheckResourceAttr(address, "value_settings.event_payload_key", "value"),
				resource.TestCheckResourceAttr(address, "status", "active"),
				resource.TestCheckResourceAttrPair("data.stripe_billing_meter.test", "id", address, "id"),
				resource.TestCheckResourceAttr("data.stripe_billing_meter.test", "event_name", acct.Unique("create_calls")),
				resource.TestCheckResourceAttr("data.stripe_billing_meter.test", "status", "active"),
			),
		}},
	})
}

func TestAccBillingMeter_update(t *testing.T) {
	acct := acctest.NewAccount(t)
	resource.ParallelTest(t, resource.TestCase{
		ProtoV6ProviderFactories: tfacc.ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: meterConfig(acct, "update_calls", "API calls"),
				Check:  resource.TestCheckResourceAttr(address, "display_name", acct.Name("API calls")),
			},
			{
				Config: meterConfig(acct, "update_calls", "API calls v2"),
				ConfigPlanChecks: resource.ConfigPlanChecks{PreApply: []plancheck.PlanCheck{
					plancheck.ExpectResourceAction(address, plancheck.ResourceActionUpdate),
				}},
				Check: resource.TestCheckResourceAttr(address, "display_name", acct.Name("API calls v2")),
			},
		},
	})
}

// event_name cannot be changed on a meter, so changing it replaces the meter.
func TestAccBillingMeter_replace(t *testing.T) {
	acct := acctest.NewAccount(t)
	var first string
	resource.ParallelTest(t, resource.TestCase{
		ProtoV6ProviderFactories: tfacc.ProviderFactories,
		Steps: []resource.TestStep{
			{Config: meterConfig(acct, "replace_calls", "API calls"), Check: idOf(&first)},
			{
				Config: meterConfig(acct, "replace_calls_v2", "API calls"),
				ConfigPlanChecks: resource.ConfigPlanChecks{PreApply: []plancheck.PlanCheck{
					plancheck.ExpectResourceAction(address, plancheck.ResourceActionReplace),
				}},
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr(address, "event_name", acct.Unique("replace_calls_v2")),
					resource.TestCheckResourceAttrWith(address, "id", func(v string) error {
						if v == first {
							return fmt.Errorf("the replacement kept the meter ID %s", v)
						}
						return nil
					}),
				),
			},
		},
	})
}

func TestAccBillingMeter_import(t *testing.T) {
	acct := acctest.NewAccount(t)
	cfg := meterConfig(acct, "import_calls", "API calls")
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

// Stripe cannot delete a meter, so destroy deactivates it and the meter stays in the account.
func TestAccBillingMeter_delete(t *testing.T) {
	acct := acctest.NewAccount(t)
	var id string
	resource.ParallelTest(t, resource.TestCase{
		ProtoV6ProviderFactories: tfacc.ProviderFactories,
		CheckDestroy: func(*terraform.State) error {
			resp, err := tfacc.Client(acct).Do(t.Context(), stripeapi.Request{Method: "GET", Path: "/v1/billing/meters/" + id})
			if err != nil {
				return fmt.Errorf("a deactivated meter still retrieves: %w", err)
			}
			if status, _ := resp.Body.(wire.Object).GetString("status"); status != "inactive" {
				return fmt.Errorf("meter %s has status %q after destroy, want inactive", id, status)
			}
			return nil
		},
		Steps: []resource.TestStep{{
			Config: meterConfig(acct, "delete_calls", "API calls"),
			Check:  idOf(&id),
		}},
	})
}

func TestAccBillingMeter_drift(t *testing.T) {
	acct := acctest.NewAccount(t)
	var id string
	cfg := meterConfig(acct, "drift_calls", "API calls")
	resource.ParallelTest(t, resource.TestCase{
		ProtoV6ProviderFactories: tfacc.ProviderFactories,
		Steps: []resource.TestStep{
			{Config: cfg, Check: idOf(&id)},
			{
				PreConfig: func() {
					tfacc.Post(t, acct, "/v1/billing/meters/"+id, wire.Object{
						{Name: "display_name", Value: wire.String("Renamed")},
					})
				},
				Config: cfg,
				ConfigPlanChecks: resource.ConfigPlanChecks{PreApply: []plancheck.PlanCheck{
					plancheck.ExpectNonEmptyPlan(),
					plancheck.ExpectResourceAction(address, plancheck.ResourceActionUpdate),
				}},
				Check: resource.TestCheckResourceAttr(address, "display_name", acct.Name("API calls")),
			},
		},
	})
}
