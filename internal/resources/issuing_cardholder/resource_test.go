package issuing_cardholder_test

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

const address = "stripe_issuing_cardholder.test"

// issuing returns the test's account and the configuration for its connected account with
// Issuing active, where cardholders live.
func issuing(t *testing.T) (acct, conn acctest.Config) {
	t.Helper()
	acct = acctest.NewAccount(t)
	return acct, tfacc.IssuingAccount(t, acct)
}

// cardholderConfig is an individual cardholder with the details Stripe needs before it issues
// cards (suite: "cardholderForm"), on the connected account. Stripe limits `name` to 24
// characters (suite: "name over 24 characters"), so the run's prefix goes into the email
// instead. extra lands inside the cardholder.
func cardholderConfig(acct, conn acctest.Config, name, extra string) string {
	return acct.ProviderBlock() + conn.ConnectedProviderBlock() + fmt.Sprintf(`
resource "stripe_issuing_cardholder" "test" {
  provider     = stripe.connected
  type         = "individual"
  name         = %q
  email        = %q
  phone_number = "+18008675309"
  individual = {
    first_name   = "Jenny"
    last_name    = "Rosen"
    dob          = { day = 1, month = 1, year = 1990 }
    card_issuing = { user_terms_acceptance = { date = 1609459200, ip = "8.8.8.8" } }
  }
  billing = {
    address = {
      line1       = "354 Oyster Point Blvd"
      city        = "South San Francisco"
      state       = "CA"
      postal_code = "94080"
      country     = "US"
    }
  }
%s}
`, name, acct.Name("jenny")+"@example.com", extra)
}

func idOf(dst *string) resource.TestCheckFunc {
	return resource.TestCheckResourceAttrWith(address, "id", func(v string) error {
		*dst = v
		return nil
	})
}

func status(t *testing.T, conn acctest.Config, id string) string {
	t.Helper()
	resp, err := tfacc.Client(conn).Do(t.Context(), stripeapi.Request{Method: "GET", Path: "/v1/issuing/cardholders/" + id})
	if err != nil {
		t.Fatalf("reading cardholder %s: %v", id, err)
	}
	s, _ := resp.Body.(wire.Object).GetString("status")
	return s
}

func TestAccIssuingCardholder_create(t *testing.T) {
	acct, conn := issuing(t)
	resource.ParallelTest(t, resource.TestCase{
		ProtoV6ProviderFactories: tfacc.ProviderFactories,
		Steps: []resource.TestStep{{
			Config: cardholderConfig(acct, conn, "Jenny Rosen", `  preferred_locales = ["en"]
  metadata          = { env = "test" }
  spending_controls = {
    spending_limits    = [{ amount = 10000, interval = "daily" }]
    blocked_categories = ["veterinary_services"]
  }
`) + `
data "stripe_issuing_cardholder" "test" {
  provider = stripe.connected
  id       = stripe_issuing_cardholder.test.id
}
`,
			Check: resource.ComposeAggregateTestCheckFunc(
				resource.TestMatchResourceAttr(address, "id", regexp.MustCompile(`^ich_`)),
				resource.TestCheckResourceAttr(address, "name", "Jenny Rosen"),
				resource.TestCheckResourceAttr(address, "type", "individual"),
				resource.TestCheckResourceAttr(address, "status", "active"),
				resource.TestCheckResourceAttr(address, "email", acct.Name("jenny")+"@example.com"),
				resource.TestCheckResourceAttr(address, "phone_number", "+18008675309"),
				resource.TestCheckResourceAttr(address, "billing.address.country", "US"),
				resource.TestCheckResourceAttr(address, "individual.dob.year", "1990"),
				resource.TestCheckResourceAttr(address, "preferred_locales.0", "en"),
				resource.TestCheckResourceAttr(address, "metadata.env", "test"),
				resource.TestCheckResourceAttr(address, "spending_controls.spending_limits.0.amount", "10000"),
				resource.TestCheckResourceAttr(address, "spending_controls.spending_limits.0.interval", "daily"),
				resource.TestCheckResourceAttr(address, "spending_controls.blocked_categories.0", "veterinary_services"),
				resource.TestCheckNoResourceAttr(address, "company"),
				resource.TestCheckResourceAttr(address, "livemode", "false"),
				resource.TestCheckResourceAttrPair("data.stripe_issuing_cardholder.test", "id", address, "id"),
				resource.TestCheckResourceAttr("data.stripe_issuing_cardholder.test", "name", "Jenny Rosen"),
				resource.TestCheckResourceAttr("data.stripe_issuing_cardholder.test", "billing.address.line1", "354 Oyster Point Blvd"),
				resource.TestCheckResourceAttr("data.stripe_issuing_cardholder.test", "spending_controls.spending_limits.0.amount", "10000"),
			),
		}},
	})
}

// The contact details, the spending controls, the locales, the metadata and the status change
// in place (suite: "update", "update status"). Stripe refuses `name` and `type` on update
// (suite: "update errors"), so each update passing shows the provider left them out. The fake's
// request log cannot show it: it keeps the requests made through `Stripe-Account` with the
// connected account, which its admin API does not reach.
func TestAccIssuingCardholder_update(t *testing.T) {
	acct, conn := issuing(t)
	var first string
	inPlace := resource.ConfigPlanChecks{PreApply: []plancheck.PlanCheck{
		plancheck.ExpectResourceAction(address, plancheck.ResourceActionUpdate),
	}}
	resource.ParallelTest(t, resource.TestCase{
		ProtoV6ProviderFactories: tfacc.ProviderFactories,
		Steps: []resource.TestStep{
			{Config: cardholderConfig(acct, conn, "Jenny Rosen", "  metadata = { a = \"1\" }\n"), Check: idOf(&first)},
			{
				Config: cardholderConfig(acct, conn, "Jenny Rosen", `  status            = "inactive"
  preferred_locales = ["fr"]
  metadata          = { b = "2" }
  spending_controls = {
    spending_limits = [{ amount = 2500, interval = "weekly" }]
  }
`),
				ConfigPlanChecks: inPlace,
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr(address, "status", "inactive"),
					resource.TestCheckResourceAttr(address, "preferred_locales.0", "fr"),
					resource.TestCheckNoResourceAttr(address, "metadata.a"),
					resource.TestCheckResourceAttr(address, "metadata.b", "2"),
					resource.TestCheckResourceAttr(address, "spending_controls.spending_limits.0.amount", "2500"),
					resource.TestCheckResourceAttr(address, "spending_controls.spending_limits.0.interval", "weekly"),
					resource.TestCheckResourceAttrWith(address, "id", func(v string) error {
						if v != first {
							return fmt.Errorf("the update replaced the cardholder %s with %s", first, v)
						}
						return nil
					}),
				),
			},
			{
				Config:           cardholderConfig(acct, conn, "Jenny Rosen", "  status            = \"active\"\n  preferred_locales = [\"fr\"]\n"),
				ConfigPlanChecks: inPlace,
				Check:            resource.TestCheckResourceAttr(address, "status", "active"),
			},
		},
	})
}

// Stripe takes `name` on create only, so a new name replaces the cardholder, and the old one is
// left inactive.
func TestAccIssuingCardholder_replace(t *testing.T) {
	acct, conn := issuing(t)
	var first string
	resource.ParallelTest(t, resource.TestCase{
		ProtoV6ProviderFactories: tfacc.ProviderFactories,
		Steps: []resource.TestStep{
			{Config: cardholderConfig(acct, conn, "Jenny Rosen", ""), Check: idOf(&first)},
			{
				Config: cardholderConfig(acct, conn, "Jane Rosen", ""),
				ConfigPlanChecks: resource.ConfigPlanChecks{PreApply: []plancheck.PlanCheck{
					plancheck.ExpectResourceAction(address, plancheck.ResourceActionReplace),
				}},
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr(address, "name", "Jane Rosen"),
					resource.TestCheckResourceAttrWith(address, "id", func(v string) error {
						if v == first {
							return fmt.Errorf("the replacement kept the cardholder %s", v)
						}
						if got := status(t, conn, first); got != "inactive" {
							return fmt.Errorf("the replaced cardholder %s is %s, want inactive", first, got)
						}
						return nil
					}),
				),
			},
		},
	})
}

func TestAccIssuingCardholder_import(t *testing.T) {
	acct, conn := issuing(t)
	cfg := cardholderConfig(acct, conn, "Jenny Rosen", "  metadata = { env = \"test\" }\n")
	resource.ParallelTest(t, resource.TestCase{
		ProtoV6ProviderFactories: tfacc.ProviderFactories,
		Steps: []resource.TestStep{
			{Config: cfg},
			{
				Config:            cfg,
				ResourceName:      address,
				ImportState:       true,
				ImportStateVerify: true,
				// The create response says the cardholder is `under_review`, and Stripe clears
				// that once it has reviewed the cardholder (suite: "New Issuing cardholders start
				// `under_review`").
				ImportStateVerifyIgnore: []string{"requirements.disabled_reason"},
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
func TestAccIssuingCardholder_importIdentity(t *testing.T) {
	acct, conn := issuing(t)
	cfg := cardholderConfig(acct, conn, "Jenny Rosen", "")
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

// Stripe cannot delete a cardholder (suite: "delete is not a route"), so destroy deactivates it
// and it stays retrievable.
func TestAccIssuingCardholder_delete(t *testing.T) {
	acct, conn := issuing(t)
	var id string
	resource.ParallelTest(t, resource.TestCase{
		ProtoV6ProviderFactories: tfacc.ProviderFactories,
		CheckDestroy: func(*terraform.State) error {
			if got := status(t, conn, id); got != "inactive" {
				return fmt.Errorf("cardholder %s is %s after destroy, want inactive", id, got)
			}
			return nil
		},
		Steps: []resource.TestStep{{Config: cardholderConfig(acct, conn, "Jenny Rosen", ""), Check: idOf(&id)}},
	})
}

// A cardholder given another email and first name out of band shows as a change, and the
// apply restores both.
func TestAccIssuingCardholder_drift(t *testing.T) {
	acct, conn := issuing(t)
	var id string
	cfg := cardholderConfig(acct, conn, "Jenny Rosen", "")
	resource.ParallelTest(t, resource.TestCase{
		ProtoV6ProviderFactories: tfacc.ProviderFactories,
		Steps: []resource.TestStep{
			{Config: cfg, Check: idOf(&id)},
			{
				PreConfig: func() {
					tfacc.Post(t, conn, "/v1/issuing/cardholders/"+id, wire.Object{
						{Name: "email", Value: wire.String(acct.Name("drifted") + "@example.com")},
						{Name: "individual", Value: wire.Object{{Name: "first_name", Value: wire.String("Jane")}}},
					})
				},
				Config: cfg,
				ConfigPlanChecks: resource.ConfigPlanChecks{PreApply: []plancheck.PlanCheck{
					plancheck.ExpectNonEmptyPlan(),
					plancheck.ExpectResourceAction(address, plancheck.ResourceActionUpdate),
				}},
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr(address, "email", acct.Name("jenny")+"@example.com"),
					resource.TestCheckResourceAttr(address, "individual.first_name", "Jenny"),
				),
			},
		},
	})
}
