package issuing_card_test

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

const address = "stripe_issuing_card.test"

// scope is a test's account, its connected account with Issuing active and the financial
// account cards draw on there.
type scope struct {
	acct, conn       acctest.Config
	financialAccount string
}

func issuing(t *testing.T) scope {
	t.Helper()
	acct := acctest.NewAccount(t)
	conn := tfacc.IssuingAccount(t, acct)
	return scope{acct: acct, conn: conn, financialAccount: tfacc.IssuingFinancialAccount(t, conn)}
}

// cardConfig is a virtual `usd` card issued to the cardholder named by holder, one of two
// cardholders with the details Stripe needs (suite: "cardholderForm"). extra lands inside the
// card.
func cardConfig(s scope, holder, extra string) string {
	cfg := s.acct.ProviderBlock() + s.conn.ConnectedProviderBlock()
	for _, name := range []string{"first", "second"} {
		cfg += fmt.Sprintf(`
resource "stripe_issuing_cardholder" %q {
  provider     = stripe.connected
  type         = "individual"
  name         = "Jenny Rosen"
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
}
`, name, s.acct.Name("jenny-"+name)+"@example.com")
	}
	return cfg + fmt.Sprintf(`
resource "stripe_issuing_card" "test" {
  provider          = stripe.connected
  cardholder        = stripe_issuing_cardholder.%s.id
  currency          = "usd"
  type              = "virtual"
  financial_account = %q
%s}
`, holder, s.financialAccount, extra)
}

func idOf(dst *string) resource.TestCheckFunc {
	return resource.TestCheckResourceAttrWith(address, "id", func(v string) error {
		*dst = v
		return nil
	})
}

func status(t *testing.T, conn acctest.Config, id string) string {
	t.Helper()
	resp, err := tfacc.Client(conn).Do(t.Context(), stripeapi.Request{Method: "GET", Path: "/v1/issuing/cards/" + id})
	if err != nil {
		t.Fatalf("reading card %s: %v", id, err)
	}
	s, _ := resp.Body.(wire.Object).GetString("status")
	return s
}

// A new card is inactive, and its full number and CVC are read back on request (suite:
// "create virtual", "retrieve with number expanded").
func TestAccIssuingCard_create(t *testing.T) {
	s := issuing(t)
	resource.ParallelTest(t, resource.TestCase{
		ProtoV6ProviderFactories: tfacc.ProviderFactories,
		Steps: []resource.TestStep{{
			Config: cardConfig(s, "first", `  metadata = { env = "test" }
  spending_controls = {
    spending_limits = [{ amount = 5000, interval = "per_authorization" }]
  }
`) + `
data "stripe_issuing_card" "test" {
  provider = stripe.connected
  id       = stripe_issuing_card.test.id
}
`,
			Check: resource.ComposeAggregateTestCheckFunc(
				resource.TestMatchResourceAttr(address, "id", regexp.MustCompile(`^ic_`)),
				resource.TestCheckResourceAttrPair(address, "cardholder", "stripe_issuing_cardholder.first", "id"),
				resource.TestCheckResourceAttr(address, "financial_account", s.financialAccount),
				resource.TestCheckResourceAttr(address, "currency", "usd"),
				resource.TestCheckResourceAttr(address, "type", "virtual"),
				resource.TestCheckResourceAttr(address, "status", "inactive"),
				resource.TestCheckResourceAttr(address, "brand", "Visa"),
				resource.TestMatchResourceAttr(address, "last4", regexp.MustCompile(`^\d{4}$`)),
				resource.TestMatchResourceAttr(address, "number", regexp.MustCompile(`^\d{16}$`)),
				resource.TestMatchResourceAttr(address, "cvc", regexp.MustCompile(`^\d{3}$`)),
				resource.TestCheckResourceAttrSet(address, "exp_month"),
				resource.TestCheckResourceAttrSet(address, "exp_year"),
				resource.TestCheckResourceAttr(address, "metadata.env", "test"),
				resource.TestCheckResourceAttr(address, "spending_controls.spending_limits.0.amount", "5000"),
				resource.TestCheckResourceAttr(address, "spending_controls.spending_limits.0.interval", "per_authorization"),
				resource.TestCheckNoResourceAttr(address, "cancellation_reason"),
				resource.TestCheckResourceAttrPair("data.stripe_issuing_card.test", "id", address, "id"),
				resource.TestCheckResourceAttrPair("data.stripe_issuing_card.test", "number", address, "number"),
				resource.TestCheckResourceAttrPair("data.stripe_issuing_card.test", "last4", address, "last4"),
				resource.TestCheckResourceAttrPair("data.stripe_issuing_card.test", "cardholder", "stripe_issuing_cardholder.first", "id"),
			),
		}},
	})
}

// Metadata, spending controls and status change in place: the card is activated once its
// cardholder's review has finished, paused, and canceled with a reason (suite: "update",
// "update status", "cancel with reason"). Stripe refuses `cardholder`, `currency` and `type` on
// update (suite: "update errors"), so each update passing shows the provider left them out.
func TestAccIssuingCard_update(t *testing.T) {
	s := issuing(t)
	var first, cardholder string
	inPlace := resource.ConfigPlanChecks{PreApply: []plancheck.PlanCheck{
		plancheck.ExpectResourceAction(address, plancheck.ResourceActionUpdate),
	}}
	sameCard := resource.TestCheckResourceAttrWith(address, "id", func(v string) error {
		if v != first {
			return fmt.Errorf("the update replaced the card %s with %s", first, v)
		}
		return nil
	})
	resource.ParallelTest(t, resource.TestCase{
		ProtoV6ProviderFactories: tfacc.ProviderFactories,
		Steps: acctest.Forget(t, []resource.TestStep{
			{
				Config: cardConfig(s, "first", "  metadata = { a = \"1\" }\n"),
				Check: resource.ComposeTestCheckFunc(
					idOf(&first),
					resource.TestCheckResourceAttrWith("stripe_issuing_cardholder.first", "id", func(v string) error { cardholder = v; return nil }),
				),
			},
			{
				PreConfig: func() { tfacc.AwaitCardholderReview(t, s.conn, cardholder) },
				Config: cardConfig(s, "first", `  status   = "active"
  metadata = { b = "2" }
  spending_controls = {
    spending_limits = [{ amount = 2500, interval = "weekly" }]
  }
`),
				ConfigPlanChecks: inPlace,
				Check: resource.ComposeAggregateTestCheckFunc(
					sameCard,
					resource.TestCheckResourceAttr(address, "status", "active"),
					resource.TestCheckNoResourceAttr(address, "metadata.a"),
					resource.TestCheckResourceAttr(address, "metadata.b", "2"),
					resource.TestCheckResourceAttr(address, "spending_controls.spending_limits.0.amount", "2500"),
					resource.TestCheckResourceAttr(address, "spending_controls.spending_limits.0.interval", "weekly"),
				),
			},
			{
				Config:           cardConfig(s, "first", "  status = \"inactive\"\n"),
				ConfigPlanChecks: inPlace,
				Check:            resource.ComposeAggregateTestCheckFunc(sameCard, resource.TestCheckResourceAttr(address, "status", "inactive")),
			},
			{
				Config:           cardConfig(s, "first", "  status              = \"canceled\"\n  cancellation_reason = \"lost\"\n"),
				ConfigPlanChecks: inPlace,
				Check: resource.ComposeAggregateTestCheckFunc(
					sameCard,
					resource.TestCheckResourceAttr(address, "status", "canceled"),
					resource.TestCheckResourceAttr(address, "cancellation_reason", "lost"),
				),
			},
			// Stripe cannot deactivate a canceled card, so its destroy fails at plan time, and so
			// does a change that would replace it.
			{Config: acctest.Without(t, cardConfig(s, "first", ""), "stripe_issuing_card"), PlanOnly: true, ExpectError: tfacc.RefusedDestroy},
			{
				Config:      cardConfig(s, "second", "  status              = \"canceled\"\n  cancellation_reason = \"lost\"\n"),
				PlanOnly:    true,
				ExpectError: tfacc.RefusedReplace,
			},
			// Canceling is final: Stripe refuses to activate the card again (suite: "canceled
			// card cannot be reactivated").
			{
				Config:      cardConfig(s, "first", "  status              = \"active\"\n  cancellation_reason = \"lost\"\n"),
				ExpectError: regexp.MustCompile(`Stripe API error: invalid_request_error`),
			},
		}, nil, "stripe_issuing_card"),
	})
}

// Stripe takes the cardholder on create only, so another cardholder replaces the card, and
// the old card is canceled.
func TestAccIssuingCard_replace(t *testing.T) {
	s := issuing(t)
	var first string
	resource.ParallelTest(t, resource.TestCase{
		ProtoV6ProviderFactories: tfacc.ProviderFactories,
		Steps: []resource.TestStep{
			{Config: cardConfig(s, "first", ""), Check: idOf(&first)},
			{
				Config: cardConfig(s, "second", ""),
				ConfigPlanChecks: resource.ConfigPlanChecks{PreApply: []plancheck.PlanCheck{
					plancheck.ExpectResourceAction(address, plancheck.ResourceActionReplace),
				}},
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttrPair(address, "cardholder", "stripe_issuing_cardholder.second", "id"),
					resource.TestCheckResourceAttrWith(address, "id", func(v string) error {
						if v == first {
							return fmt.Errorf("the replacement kept the card %s", v)
						}
						if got := status(t, s.conn, first); got != "inactive" {
							return fmt.Errorf("the replaced card %s is %s, want inactive", first, got)
						}
						return nil
					}),
				),
			},
		},
	})
}

func TestAccIssuingCard_import(t *testing.T) {
	s := issuing(t)
	cfg := cardConfig(s, "first", "  metadata = { env = \"test\" }\n")
	resource.ParallelTest(t, resource.TestCase{
		ProtoV6ProviderFactories: tfacc.ProviderFactories,
		Steps: []resource.TestStep{
			{Config: cfg},
			{
				Config:            cfg,
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
func TestAccIssuingCard_importIdentity(t *testing.T) {
	s := issuing(t)
	cfg := cardConfig(s, "first", "")
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

// Stripe cannot delete a card, so destroy deactivates it, which `status` can undo; the card
// stays on the connected account.
func TestAccIssuingCard_delete(t *testing.T) {
	s := issuing(t)
	var id, cardholder string
	resource.ParallelTest(t, resource.TestCase{
		ProtoV6ProviderFactories: tfacc.ProviderFactories,
		CheckDestroy: func(*terraform.State) error {
			if got := status(t, s.conn, id); got != "inactive" {
				return fmt.Errorf("card %s is %s after destroy, want inactive", id, got)
			}
			return nil
		},
		Steps: []resource.TestStep{
			{
				Config: cardConfig(s, "first", ""),
				Check: resource.ComposeTestCheckFunc(idOf(&id),
					resource.TestCheckResourceAttrWith("stripe_issuing_cardholder.first", "id", func(v string) error { cardholder = v; return nil })),
			},
			{
				PreConfig: func() { tfacc.AwaitCardholderReview(t, s.conn, cardholder) },
				Config:    cardConfig(s, "first", "  status = \"active\"\n"),
				Check:     resource.TestCheckResourceAttr(address, "status", "active"),
			},
		},
	})
}

// A card whose metadata and spending limits were changed out of band shows as a change, and the
// apply restores both.
func TestAccIssuingCard_drift(t *testing.T) {
	s := issuing(t)
	var id string
	cfg := cardConfig(s, "first", `  metadata = { env = "test" }
  spending_controls = {
    spending_limits = [{ amount = 5000, interval = "daily" }]
  }
`)
	resource.ParallelTest(t, resource.TestCase{
		ProtoV6ProviderFactories: tfacc.ProviderFactories,
		Steps: []resource.TestStep{
			{Config: cfg, Check: idOf(&id)},
			{
				PreConfig: func() {
					tfacc.Post(t, s.conn, "/v1/issuing/cards/"+id, wire.Object{
						{Name: "metadata", Value: wire.Object{{Name: "env", Value: wire.String("drifted")}}},
						{Name: "spending_controls", Value: wire.Object{{Name: "spending_limits", Value: wire.Array{wire.Object{
							{Name: "amount", Value: wire.Int(100)},
							{Name: "interval", Value: wire.String("weekly")},
						}}}}},
					})
				},
				Config: cfg,
				ConfigPlanChecks: resource.ConfigPlanChecks{PreApply: []plancheck.PlanCheck{
					plancheck.ExpectNonEmptyPlan(),
					plancheck.ExpectResourceAction(address, plancheck.ResourceActionUpdate),
				}},
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr(address, "metadata.env", "test"),
					resource.TestCheckResourceAttr(address, "spending_controls.spending_limits.0.amount", "5000"),
					resource.TestCheckResourceAttr(address, "spending_controls.spending_limits.0.interval", "daily"),
				),
			},
		},
	})
}
