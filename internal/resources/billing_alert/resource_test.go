package billing_alert_test

import (
	"fmt"
	"os"
	"regexp"
	"testing"

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

const address = "stripe_billing_alert.test"

// alertConfig is a meter and a usage threshold alert on it. extra lands inside the alert.
func alertConfig(c acctest.Config, title string, gte int, extra string) string {
	return c.ProviderBlock() + fmt.Sprintf(`
resource "stripe_billing_meter" "test" {
  display_name = %q
  event_name   = %q

  default_aggregation = {
    formula = "sum"
  }
}

resource "stripe_billing_alert" "test" {
  alert_type = "usage_threshold"
  title      = %q

  usage_threshold = {
    gte        = %d
    meter      = stripe_billing_meter.test.id
    recurrence = "one_time"
  }
%s}
`, c.Name("Alert meter"), c.Unique("alert_events"), c.Name(title), gte, extra)
}

// idOf records the alert's ID.
func idOf(id *string) resource.TestCheckFunc {
	return resource.TestCheckResourceAttrWith(address, "id", func(v string) error { *id = v; return nil })
}

func alertStatus(t *testing.T, c acctest.Config, id string) (string, error) {
	t.Helper()
	resp, err := tfacc.Client(c).Do(t.Context(), stripeapi.Request{Method: "GET", Path: "/v1/billing/alerts/" + id})
	if err != nil {
		return "", err
	}
	status, _ := resp.Body.(wire.Object).GetString("status")
	return status, nil
}

func TestAccBillingAlert_create(t *testing.T) {
	acct := acctest.NewAccount(t)
	resource.ParallelTest(t, resource.TestCase{
		ProtoV6ProviderFactories: tfacc.ProviderFactories,
		Steps: []resource.TestStep{{
			Config: alertConfig(acct, "Heavy usage", 100, "") + `
data "stripe_billing_alert" "test" {
  id = stripe_billing_alert.test.id
}

resource "stripe_customer" "test" {
  name = "Alert customer"
}

resource "stripe_billing_alert" "filtered" {
  alert_type = "usage_threshold"
  title      = "Filtered"

  usage_threshold = {
    gte        = 5
    meter      = stripe_billing_meter.test.id
    recurrence = "one_time"
    filters = [{
      type     = "customer"
      customer = stripe_customer.test.id
    }]
  }
  status = "inactive"
}
`,
			Check: resource.ComposeAggregateTestCheckFunc(
				resource.TestCheckResourceAttrSet(address, "id"),
				resource.TestCheckResourceAttr(address, "alert_type", "usage_threshold"),
				resource.TestCheckResourceAttr(address, "status", "active"),
				resource.TestCheckResourceAttr(address, "usage_threshold.gte", "100"),
				resource.TestCheckResourceAttrPair(address, "usage_threshold.meter", "stripe_billing_meter.test", "id"),
				resource.TestCheckResourceAttr(address, "usage_threshold.recurrence", "one_time"),
				resource.TestCheckResourceAttrPair("data.stripe_billing_alert.test", "id", address, "id"),
				resource.TestCheckResourceAttr("data.stripe_billing_alert.test", "status", "active"),
				resource.TestCheckResourceAttrPair("data.stripe_billing_alert.test", "usage_threshold.meter", "stripe_billing_meter.test", "id"),
				// Created inactive: the alert is created active and then deactivated.
				resource.TestCheckResourceAttr("stripe_billing_alert.filtered", "status", "inactive"),
				resource.TestCheckResourceAttrPair("stripe_billing_alert.filtered", "usage_threshold.filters.0.customer", "stripe_customer.test", "id"),
			),
		}},
	})
}

// Status moves between active and inactive in place, by Stripe's action endpoints (suite:
// "deactivate and activate"); the alert keeps its ID. TestAccBillingAlert_archive covers
// `archived`.
func TestAccBillingAlert_update(t *testing.T) {
	acct := acctest.NewAccount(t)
	var first string
	inPlace := resource.ConfigPlanChecks{PreApply: []plancheck.PlanCheck{
		plancheck.ExpectResourceAction(address, plancheck.ResourceActionUpdate),
	}}
	sameID := resource.TestCheckResourceAttrWith(address, "id", func(v string) error {
		if v != first {
			return fmt.Errorf("the in-place update changed the alert from %s to %s", first, v)
		}
		return nil
	})
	resource.ParallelTest(t, resource.TestCase{
		ProtoV6ProviderFactories: tfacc.ProviderFactories,
		Steps: []resource.TestStep{
			{Config: alertConfig(acct, "Heavy usage", 100, ""), Check: idOf(&first)},
			{
				Config:           alertConfig(acct, "Heavy usage", 100, "  status = \"inactive\"\n"),
				ConfigPlanChecks: inPlace,
				Check:            resource.ComposeAggregateTestCheckFunc(resource.TestCheckResourceAttr(address, "status", "inactive"), sameID),
			},
			// Inactive again changes nothing.
			{
				Config:   alertConfig(acct, "Heavy usage", 100, "  status = \"inactive\"\n"),
				PlanOnly: true,
			},
			{
				Config:           alertConfig(acct, "Heavy usage", 100, "  status = \"active\"\n"),
				ConfigPlanChecks: inPlace,
				Check:            resource.ComposeAggregateTestCheckFunc(resource.TestCheckResourceAttr(address, "status", "active"), sameID),
			},
			// Leaving status out keeps the value.
			{Config: alertConfig(acct, "Heavy usage", 100, ""), PlanOnly: true},
		},
	})
}

// Setting `status` to `archived` archives the alert (suite: "archive"), which is final (suite:
// "archived alert cannot be reactivated"): destroy and replacement of an archived alert then
// fail at plan time. A value no action sets is refused.
func TestAccBillingAlert_archive(t *testing.T) {
	acct := acctest.NewAccount(t)
	var id string
	path := func() string { return "/v1/billing/alerts/" + id }
	archived := alertConfig(acct, "Heavy usage", 100, "  status = \"archived\"\n")
	resource.ParallelTest(t, resource.TestCase{
		ProtoV6ProviderFactories: tfacc.ProviderFactories,
		Steps: acctest.Forget(t, []resource.TestStep{
			{Config: alertConfig(acct, "Heavy usage", 100, "  status = \"paused\"\n"), PlanOnly: true, ExpectError: regexp.MustCompile("can be set to `active` or `inactive` or `archived`")},
			{Config: alertConfig(acct, "Heavy usage", 100, ""), Check: idOf(&id)},
			{
				Config: archived,
				Check: resource.ComposeTestCheckFunc(
					resource.TestCheckResourceAttr(address, "status", "archived"),
					func(s *terraform.State) error {
						p := path()
						return tfacc.Holds(t, acct, &p, "status", wire.String("archived"))(s)
					},
				),
			},
			{Config: alertConfig(acct, "Heavy usage", 100, "  status = \"active\"\n"), PlanOnly: true, ExpectError: regexp.MustCompile("Cannot change `status` from `archived`")},
			{Config: acctest.Without(t, archived, "stripe_billing_alert"), PlanOnly: true, ExpectError: tfacc.RefusedDestroy},
			{Config: alertConfig(acct, "Heavier usage", 100, "  status = \"archived\"\n"), PlanOnly: true, ExpectError: tfacc.RefusedReplace},
		}, nil, "stripe_billing_alert"),
	})
}

// Stripe does not update an alert, so a new title or threshold replaces it.
func TestAccBillingAlert_replace(t *testing.T) {
	acct := acctest.NewAccount(t)
	var first string
	replaced := resource.ConfigPlanChecks{PreApply: []plancheck.PlanCheck{
		plancheck.ExpectResourceAction(address, plancheck.ResourceActionReplace),
	}}
	resource.ParallelTest(t, resource.TestCase{
		ProtoV6ProviderFactories: tfacc.ProviderFactories,
		Steps: []resource.TestStep{
			{Config: alertConfig(acct, "Heavy usage", 100, ""), Check: idOf(&first)},
			{
				Config:           alertConfig(acct, "Heavier usage", 100, ""),
				ConfigPlanChecks: replaced,
				Check: resource.TestCheckResourceAttrWith(address, "id", func(v string) error {
					if v == first {
						return fmt.Errorf("the replacement kept the alert %s", v)
					}
					return nil
				}),
			},
			{
				Config:           alertConfig(acct, "Heavier usage", 200, ""),
				ConfigPlanChecks: replaced,
				Check:            resource.TestCheckResourceAttr(address, "usage_threshold.gte", "200"),
			},
		},
	})
}

func TestAccBillingAlert_import(t *testing.T) {
	acct := acctest.NewAccount(t)
	cfg := alertConfig(acct, "Heavy usage", 100, "")
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
func TestAccBillingAlert_importIdentity(t *testing.T) {
	acct := acctest.NewAccount(t)
	cfg := alertConfig(acct, "Heavy usage", 100, "")
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

// Stripe cannot delete an alert, so destroy deactivates it, which `status` can undo; the
// alert stays in the account.
func TestAccBillingAlert_delete(t *testing.T) {
	acct := acctest.NewAccount(t)
	var id string
	resource.ParallelTest(t, resource.TestCase{
		ProtoV6ProviderFactories: tfacc.ProviderFactories,
		CheckDestroy: func(*terraform.State) error {
			status, err := alertStatus(t, acct, id)
			if err != nil {
				return fmt.Errorf("a deactivated alert should still retrieve: %w", err)
			}
			if status != "inactive" {
				return fmt.Errorf("the alert should be inactive after destroy, got %q", status)
			}
			return nil
		},
		Steps: []resource.TestStep{{Config: alertConfig(acct, "Heavy usage", 100, ""), Check: resource.ComposeTestCheckFunc(
			idOf(&id), resource.TestCheckResourceAttr(address, "status", "active"))}},
	})
}

// An alert deactivated out of band shows as drift on status, and the apply activates it.
func TestAccBillingAlert_drift(t *testing.T) {
	acct := acctest.NewAccount(t)
	var id string
	cfg := alertConfig(acct, "Heavy usage", 100, "  status = \"active\"\n")
	resource.ParallelTest(t, resource.TestCase{
		ProtoV6ProviderFactories: tfacc.ProviderFactories,
		Steps: []resource.TestStep{
			{Config: cfg, Check: idOf(&id)},
			{
				PreConfig: func() {
					if _, err := tfacc.Client(acct).Do(t.Context(), stripeapi.Request{Method: "POST", Path: "/v1/billing/alerts/" + id + "/deactivate"}); err != nil {
						t.Fatal(err)
					}
				},
				Config: cfg,
				ConfigPlanChecks: resource.ConfigPlanChecks{PreApply: []plancheck.PlanCheck{
					plancheck.ExpectNonEmptyPlan(),
					plancheck.ExpectResourceAction(address, plancheck.ResourceActionUpdate),
				}},
				Check: resource.TestCheckResourceAttr(address, "status", "active"),
			},
		},
	})
}
