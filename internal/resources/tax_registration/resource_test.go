package tax_registration_test

import (
	"context"
	"fmt"
	"os"
	"strconv"
	"testing"
	"time"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/plancheck"

	"github.com/computomatic/terraform-provider-stripe/internal/acctest"
	"github.com/computomatic/terraform-provider-stripe/internal/acctest/tfacc"
	"github.com/computomatic/terraform-provider-stripe/internal/stripeapi"
	"github.com/computomatic/terraform-provider-stripe/internal/wire"
)

func TestMain(m *testing.M) { os.Exit(acctest.Main(m)) }

const address = "stripe_tax_registration.test"

// withHeadOffice gives the account the head office address Stripe requires before it
// serves any tax registration (suite: tax_registration_test.go, "An account without a head
// office address"). A country holds one live registration at a time and Stripe cannot
// delete one, so each test registers in a country of its own, which neither another test
// nor the suite (GB, AU and NZ) uses. In smoke mode the shared sandbox may still hold a live
// registration there from an earlier run, so the country is cleared before the test and
// again after it, as the suite does ("clearTaxRegistrations").
func withHeadOffice(t *testing.T, country string) acctest.Config {
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
	if acct.Smoke {
		clearRegistrations(t, acct, country)
		t.Cleanup(func() { clearRegistrations(t, acct, country) })
	}
	return acct
}

// clearRegistrations expires every live registration in country and waits until Stripe
// reports it expired. Stripe refuses an expiry of `now`, so each expires a few seconds
// ahead, and a scheduled one is started first, because it cannot end before it starts.
func clearRegistrations(t *testing.T, c acctest.Config, country string) {
	t.Helper()
	client := tfacc.Client(c)
	var ids []string
	for _, status := range []string{"active", "scheduled"} {
		resp, err := client.Do(context.Background(), stripeapi.Request{Method: "GET", Path: "/v1/tax/registrations", Params: wire.Object{
			{Name: "status", Value: wire.String(status)},
			{Name: "limit", Value: wire.Int(100)},
		}})
		if err != nil {
			t.Fatalf("listing %s tax registrations: %v", status, err)
		}
		data, _ := resp.Body.(wire.Object).Get("data")
		items, _ := data.(wire.Array)
		for _, item := range items {
			obj, _ := item.(wire.Object)
			if cc, _ := obj.GetString("country"); cc == country {
				id, _ := obj.GetString("id")
				ids = append(ids, id)
			}
		}
	}
	for _, id := range ids {
		path := "/v1/tax/registrations/" + id
		expiry := wire.String(strconv.FormatInt(time.Now().Add(5*time.Second).Unix(), 10))
		if _, err := client.Do(context.Background(), stripeapi.Request{Method: "POST", Path: path, Params: wire.Object{{Name: "expires_at", Value: expiry}}}); err != nil {
			tfacc.Post(t, c, path, wire.Object{{Name: "active_from", Value: wire.String("now")}, {Name: "expires_at", Value: expiry}})
		}
	}
	for _, id := range ids {
		deadline := time.Now().Add(time.Minute)
		for {
			resp, err := client.Do(context.Background(), stripeapi.Request{Method: "GET", Path: "/v1/tax/registrations/" + id})
			if err != nil {
				t.Fatalf("reading tax registration %s: %v", id, err)
			}
			if status, _ := resp.Body.(wire.Object).GetString("status"); status == "expired" {
				break
			}
			if time.Now().After(deadline) {
				t.Fatalf("tax registration %s in %s did not expire within a minute", id, country)
			}
			time.Sleep(2 * time.Second)
		}
	}
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

// pathOf records the object's API path.
func pathOf(dst *string) resource.TestCheckFunc {
	return resource.TestCheckResourceAttrWith(address, "id", func(v string) error {
		*dst = "/v1/tax/registrations/" + v
		return nil
	})
}

func TestAccTaxRegistration_create(t *testing.T) {
	acct := withHeadOffice(t, "CH")
	resource.ParallelTest(t, resource.TestCase{
		ProtoV6ProviderFactories: tfacc.ProviderFactories,
		Steps: acctest.Forget(t, []resource.TestStep{{
			Config: registrationConfig(acct, "CH", "") + `
data "stripe_tax_registration" "test" {
  id = stripe_tax_registration.test.id
}
`,
			Check: resource.ComposeAggregateTestCheckFunc(
				resource.TestCheckResourceAttrSet(address, "id"),
				resource.TestCheckResourceAttr(address, "country", "CH"),
				resource.TestCheckResourceAttr(address, "active_from", "now"),
				resource.TestCheckResourceAttr(address, "status", "active"),
				resource.TestCheckResourceAttr(address, "country_options.ch.type", "standard"),
				resource.TestCheckResourceAttrPair("data.stripe_tax_registration.test", "id", address, "id"),
				resource.TestCheckResourceAttr("data.stripe_tax_registration.test", "country", "CH"),
				resource.TestCheckResourceAttr("data.stripe_tax_registration.test", "status", "active"),
			),
		}}, nil, "stripe_tax_registration"),
	})
}

// A registration that starts later is scheduled, and an end date is accepted on create.
// Destroy starts it first, because it cannot end before it starts.
func TestAccTaxRegistration_scheduled(t *testing.T) {
	acct := withHeadOffice(t, "NO")
	start, end := inHours(24), inHours(48)
	cfg := acct.ProviderBlock() + fmt.Sprintf(`
resource "stripe_tax_registration" "test" {
  country     = "NO"
  active_from = %q
  expires_at  = %q
  country_options = {
    no = { type = "standard" }
  }
}
`, start, end)
	resource.ParallelTest(t, resource.TestCase{
		ProtoV6ProviderFactories: tfacc.ProviderFactories,
		Steps: acctest.Forget(t, []resource.TestStep{
			{Config: cfg, Check: resource.ComposeAggregateTestCheckFunc(
				resource.TestCheckResourceAttr(address, "status", "scheduled"),
				resource.TestCheckResourceAttr(address, "active_from", start),
				resource.TestCheckResourceAttr(address, "expires_at", end),
			)},
			{Config: cfg, PlanOnly: true},
		}, nil, "stripe_tax_registration"),
	})
}

func TestAccTaxRegistration_update(t *testing.T) {
	acct := withHeadOffice(t, "IS")
	var first string
	later := inHours(72)
	resource.ParallelTest(t, resource.TestCase{
		ProtoV6ProviderFactories: tfacc.ProviderFactories,
		Steps: acctest.Forget(t, []resource.TestStep{
			{Config: registrationConfig(acct, "IS", ""), Check: idOf(&first)},
			{
				Config: registrationConfig(acct, "IS", fmt.Sprintf("  expires_at = %q\n", inHours(48))),
				ConfigPlanChecks: resource.ConfigPlanChecks{PreApply: []plancheck.PlanCheck{
					plancheck.ExpectResourceAction(address, plancheck.ResourceActionUpdate),
				}},
				Check: resource.TestCheckResourceAttrSet(address, "expires_at"),
			},
			{
				Config: registrationConfig(acct, "IS", fmt.Sprintf("  expires_at = %q\n", later)),
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
		}, nil, "stripe_tax_registration"),
	})
}

// `country` is create-only, so changing it would replace the registration, which Stripe
// cannot delete: the plan fails, and so does a taint, at apply.
func TestAccTaxRegistration_replace(t *testing.T) {
	acct := withHeadOffice(t, "SG")
	cfg := registrationConfig(acct, "SG", "")
	resource.ParallelTest(t, resource.TestCase{
		ProtoV6ProviderFactories: tfacc.ProviderFactories,
		Steps: acctest.Forget(t, []resource.TestStep{
			{Config: cfg},
			{Config: registrationConfig(acct, "AU", ""), PlanOnly: true, ExpectError: tfacc.RefusedReplace},
			{Config: cfg, Taint: []string{address}, ExpectError: tfacc.RefusedDestroy},
		}, nil, "stripe_tax_registration"),
	})
}

// Import reads the start time Stripe recorded, so the configuration gives a timestamp where
// it could say `now`. The registration is scheduled, which destroy has to start before it
// can end it.
func TestAccTaxRegistration_import(t *testing.T) {
	acct := withHeadOffice(t, "ZA")
	cfg := acct.ProviderBlock() + fmt.Sprintf(`
resource "stripe_tax_registration" "test" {
  country     = "ZA"
  active_from = %q
  expires_at  = %q
  country_options = {
    za = { type = "standard" }
  }
}
`, inHours(24), inHours(48))
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
		}, nil, "stripe_tax_registration"),
	})
}

// Stripe cannot delete a tax registration, so dropping it from the configuration, and `terraform
// destroy`, fail at plan time, and the tax registration stays as it was.
func TestAccTaxRegistration_delete(t *testing.T) {
	acct := withHeadOffice(t, "AE")
	var path string
	var mark int
	cfg := registrationConfig(acct, "AE", "")
	resource.ParallelTest(t, resource.TestCase{
		ProtoV6ProviderFactories: tfacc.ProviderFactories,
		Steps: acctest.Forget(t, []resource.TestStep{
			{Config: cfg, Check: resource.ComposeTestCheckFunc(pathOf(&path), tfacc.LogMark(t, acct, &mark))},
			{Config: acctest.Without(t, cfg, "stripe_tax_registration"), PlanOnly: true, ExpectError: tfacc.RefusedDestroy},
			{Config: cfg, Destroy: true, ExpectError: tfacc.RefusedDestroy},
		}, tfacc.Untouched(t, acct, &path, &mark, "status", wire.String("active")), "stripe_tax_registration"),
	})
}

func TestAccTaxRegistration_drift(t *testing.T) {
	acct := withHeadOffice(t, "JP")
	var id string
	expires := inHours(48)
	cfg := registrationConfig(acct, "JP", fmt.Sprintf("  expires_at = %q\n", expires))
	resource.ParallelTest(t, resource.TestCase{
		ProtoV6ProviderFactories: tfacc.ProviderFactories,
		Steps: acctest.Forget(t, []resource.TestStep{
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
		}, nil, "stripe_tax_registration"),
	})
}
