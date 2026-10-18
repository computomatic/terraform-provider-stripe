package tax_registration_test

import (
	"fmt"
	"os"
	"strconv"
	"testing"
	"time"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/plancheck"
	"github.com/hashicorp/terraform-plugin-testing/terraform"

	"github.com/computomatic/terraform-provider-stripe/internal/acctest"
	"github.com/computomatic/terraform-provider-stripe/internal/acctest/tfacc"
	"github.com/computomatic/terraform-provider-stripe/internal/stripeapi"
	"github.com/computomatic/terraform-provider-stripe/internal/wire"
)

func TestMain(m *testing.M) { os.Exit(acctest.Main(m)) }

const address = "stripe_tax_registration.test"

// withHeadOffice gives the account the head office address Stripe requires before it
// serves any tax registration (suite: tax_registration_test.go, "An account without a head
// office address").
func withHeadOffice(t *testing.T) acctest.Config {
	t.Helper()
	acct := acctest.NewAccount(t)
	tfacc.Post(t, acct, "/v1/tax/settings", wire.Object{
		{Name: "head_office", Value: wire.Object{{Name: "address", Value: wire.Object{
			{Name: "line1", Value: wire.String("354 Oyster Point Blvd")},
			{Name: "city", Value: wire.String("South San Francisco")},
			{Name: "state", Value: wire.String("CA")},
			{Name: "postal_code", Value: wire.String("94080")},
			{Name: "country", Value: wire.String("US")},
		}}}},
	})
	return acct
}

func inHours(h int) string {
	return strconv.FormatInt(time.Now().Add(time.Duration(h)*time.Hour).Unix(), 10)
}

// registrationConfig is a standard registration in a country that takes the `standard`
// type under its lower-case code. extra lands inside the resource block.
func registrationConfig(c acctest.Config, country string, extra string) string {
	return c.ProviderBlock() + fmt.Sprintf(`
resource "stripe_tax_registration" "test" {
  country     = %q
  active_from = "now"
  country_options = {
    %s = { type = "standard" }
  }
%s}
`, country, lower(country), extra)
}

func lower(s string) string {
	b := []byte(s)
	for i, c := range b {
		if c >= 'A' && c <= 'Z' {
			b[i] = c + 'a' - 'A'
		}
	}
	return string(b)
}

func idOf(dst *string) resource.TestCheckFunc {
	return resource.TestCheckResourceAttrWith(address, "id", func(v string) error {
		*dst = v
		return nil
	})
}

func TestAccTaxRegistration_create(t *testing.T) {
	acct := withHeadOffice(t)
	resource.ParallelTest(t, resource.TestCase{
		ProtoV6ProviderFactories: tfacc.ProviderFactories,
		Steps: []resource.TestStep{{
			Config: registrationConfig(acct, "GB", "") + `
data "stripe_tax_registration" "test" {
  id = stripe_tax_registration.test.id
}
`,
			Check: resource.ComposeAggregateTestCheckFunc(
				resource.TestCheckResourceAttrSet(address, "id"),
				resource.TestCheckResourceAttr(address, "country", "GB"),
				resource.TestCheckResourceAttr(address, "active_from", "now"),
				resource.TestCheckResourceAttr(address, "status", "active"),
				resource.TestCheckResourceAttr(address, "country_options.gb.type", "standard"),
				resource.TestCheckResourceAttrPair("data.stripe_tax_registration.test", "id", address, "id"),
				resource.TestCheckResourceAttr("data.stripe_tax_registration.test", "country", "GB"),
				resource.TestCheckResourceAttr("data.stripe_tax_registration.test", "status", "active"),
			),
		}},
	})
}

// A registration that starts later is scheduled, and an end date is accepted on create.
// Destroy starts it first, because it cannot end before it starts.
func TestAccTaxRegistration_scheduled(t *testing.T) {
	acct := withHeadOffice(t)
	start, end := inHours(24), inHours(48)
	cfg := acct.ProviderBlock() + fmt.Sprintf(`
resource "stripe_tax_registration" "test" {
  country     = "GB"
  active_from = %q
  expires_at  = %q
  country_options = {
    gb = { type = "standard" }
  }
}
`, start, end)
	resource.ParallelTest(t, resource.TestCase{
		ProtoV6ProviderFactories: tfacc.ProviderFactories,
		Steps: []resource.TestStep{
			{Config: cfg, Check: resource.ComposeAggregateTestCheckFunc(
				resource.TestCheckResourceAttr(address, "status", "scheduled"),
				resource.TestCheckResourceAttr(address, "active_from", start),
				resource.TestCheckResourceAttr(address, "expires_at", end),
			)},
			{Config: cfg, PlanOnly: true},
		},
	})
}

func TestAccTaxRegistration_update(t *testing.T) {
	acct := withHeadOffice(t)
	var first string
	later := inHours(72)
	resource.ParallelTest(t, resource.TestCase{
		ProtoV6ProviderFactories: tfacc.ProviderFactories,
		Steps: []resource.TestStep{
			{Config: registrationConfig(acct, "GB", ""), Check: idOf(&first)},
			{
				Config: registrationConfig(acct, "GB", fmt.Sprintf("  expires_at = %q\n", inHours(48))),
				ConfigPlanChecks: resource.ConfigPlanChecks{PreApply: []plancheck.PlanCheck{
					plancheck.ExpectResourceAction(address, plancheck.ResourceActionUpdate),
				}},
				Check: resource.TestCheckResourceAttrSet(address, "expires_at"),
			},
			{
				Config: registrationConfig(acct, "GB", fmt.Sprintf("  expires_at = %q\n", later)),
				ConfigPlanChecks: resource.ConfigPlanChecks{PreApply: []plancheck.PlanCheck{
					plancheck.ExpectResourceAction(address, plancheck.ResourceActionUpdate),
				}},
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr(address, "expires_at", later),
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

// country and country_options are accepted on create only, so another country replaces the
// registration. Destroy ends the old one first.
func TestAccTaxRegistration_replace(t *testing.T) {
	acct := withHeadOffice(t)
	var first string
	resource.ParallelTest(t, resource.TestCase{
		ProtoV6ProviderFactories: tfacc.ProviderFactories,
		Steps: []resource.TestStep{
			{Config: registrationConfig(acct, "GB", ""), Check: idOf(&first)},
			{
				Config: registrationConfig(acct, "AU", ""),
				ConfigPlanChecks: resource.ConfigPlanChecks{PreApply: []plancheck.PlanCheck{
					plancheck.ExpectResourceAction(address, plancheck.ResourceActionDestroyBeforeCreate),
				}},
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr(address, "country", "AU"),
					resource.TestCheckResourceAttrWith(address, "id", func(v string) error {
						if v == first {
							return fmt.Errorf("the replacement kept the registration ID %s", v)
						}
						return nil
					}),
				),
			},
		},
	})
}

// Import reads the start time Stripe recorded, so the configuration gives a timestamp where
// it could say `now`. The registration is scheduled, which destroy has to start before it
// can end it.
func TestAccTaxRegistration_import(t *testing.T) {
	acct := withHeadOffice(t)
	cfg := acct.ProviderBlock() + fmt.Sprintf(`
resource "stripe_tax_registration" "test" {
  country     = "GB"
  active_from = %q
  expires_at  = %q
  country_options = {
    gb = { type = "standard" }
  }
}
`, inHours(24), inHours(48))
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

// Stripe cannot delete a registration and refuses `expires_at = now` on a fresh one (suite:
// tax_registration_test.go, "update expires at a few seconds ahead expires the
// registration"), so destroy sets `expires_at` five seconds ahead and waits for the status
// `expired`. The registration stays in the account.
func TestAccTaxRegistration_delete(t *testing.T) {
	acct := withHeadOffice(t)
	var id string
	resource.ParallelTest(t, resource.TestCase{
		ProtoV6ProviderFactories: tfacc.ProviderFactories,
		CheckDestroy: func(*terraform.State) error {
			resp, err := tfacc.Client(acct).Do(t.Context(), stripeapi.Request{Method: "GET", Path: "/v1/tax/registrations/" + id})
			if err != nil {
				return fmt.Errorf("an expired registration still retrieves: %w", err)
			}
			if status, _ := resp.Body.(wire.Object).GetString("status"); status != "expired" {
				return fmt.Errorf("registration %s should be expired after destroy, got %q", id, status)
			}
			return nil
		},
		Steps: []resource.TestStep{{Config: registrationConfig(acct, "GB", ""), Check: idOf(&id)}},
	})
}

func TestAccTaxRegistration_drift(t *testing.T) {
	acct := withHeadOffice(t)
	var id string
	expires := inHours(48)
	cfg := registrationConfig(acct, "GB", fmt.Sprintf("  expires_at = %q\n", expires))
	resource.ParallelTest(t, resource.TestCase{
		ProtoV6ProviderFactories: tfacc.ProviderFactories,
		Steps: []resource.TestStep{
			{Config: cfg, Check: idOf(&id)},
			{
				PreConfig: func() {
					tfacc.Post(t, acct, "/v1/tax/registrations/"+id, wire.Object{
						{Name: "expires_at", Value: wire.String(inHours(96))},
					})
				},
				Config: cfg,
				ConfigPlanChecks: resource.ConfigPlanChecks{PreApply: []plancheck.PlanCheck{
					plancheck.ExpectNonEmptyPlan(),
					plancheck.ExpectResourceAction(address, plancheck.ResourceActionUpdate),
				}},
				Check: resource.TestCheckResourceAttr(address, "expires_at", expires),
			},
		},
	})
}
