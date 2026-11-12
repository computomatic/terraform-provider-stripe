package issuing_card_test

import (
	"context"
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
	return cardConfigOf(s, holder, "virtual", extra)
}

// cardConfigOf is cardConfig with the card's type.
func cardConfigOf(s scope, holder, cardType, extra string) string {
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
  type              = %q
  financial_account = %q
%s}
`, holder, cardType, s.financialAccount, extra)
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

// design makes a personalization design on the scope's connected account, on a physical bundle
// that takes a logo and carrier text, as the suite's designs are made, and has Stripe approve it
// when active is set. A cleanup deactivates it where Stripe allows, since a design cannot be
// deleted.
func design(t *testing.T, s scope, active bool) string {
	t.Helper()
	resp, err := tfacc.Client(s.conn).Do(t.Context(), stripeapi.Request{Method: "POST", Path: "/v1/issuing/personalization_designs", Params: wire.Object{
		{Name: "physical_bundle", Value: wire.String(tfacc.PhysicalBundle(t, s.conn))},
		{Name: "card_logo", Value: wire.String(tfacc.CardLogo(t, s.conn))},
		{Name: "carrier_text", Value: wire.Object{
			{Name: "header_title", Value: wire.String("Welcome")},
			{Name: "header_body", Value: wire.String("Your new card is enclosed.")},
			{Name: "footer_title", Value: wire.String("Questions")},
			{Name: "footer_body", Value: wire.String("Contact support.")},
		}},
	}})
	if err != nil {
		t.Fatalf("creating a personalization design: %v", err)
	}
	id, _ := resp.Body.(wire.Object).GetString("id")
	if active {
		designHelper(t, s, id, "activate", nil)
	}
	t.Cleanup(func() {
		path := "/v1/test_helpers/issuing/personalization_designs/" + id + "/deactivate"
		if _, err := tfacc.Client(s.conn).Do(context.Background(), stripeapi.Request{Method: "POST", Path: path}); err != nil {
			t.Logf("design %s stays as it is: %v", id, err)
		}
	})
	return id
}

// designHelper moves a design through Stripe's review with a test helper.
func designHelper(t *testing.T, s scope, id, action string, params wire.Object) {
	t.Helper()
	tfacc.Post(t, s.conn, "/v1/test_helpers/issuing/personalization_designs/"+id+"/"+action, params)
}

// seattle is a physical card's shipping and second line as the suite's create sends them, on a
// card of an active design (TestIssuingCardAttributes, probes `every_field/2` and `update/2`).
const seattle = `  second_line = "ACME INC"
  shipping = {
    name = "Sam Doe"
    address = {
      line1       = "1 Pike Pl"
      line2       = "Floor 3"
      city        = "Seattle"
      state       = "WA"
      postal_code = "98101"
      country     = "US"
    }
    business_name     = "Acme Inc"
    customs           = { eori_number = "GB123456789000" }
    phone_number      = "+18005550111"
    require_signature = true
    service           = "express"
    type              = "individual"
  }
`

// newYork is the shipping the suite's update sends whole (TestIssuingCardAttributes, probe
// `update/2`, the record's `side_effects`).
const newYork = `  second_line = "ACME INC"
  shipping = {
    name = "Sam Q Doe"
    address = {
      line1       = "1 Liberty St"
      line2       = "Floor 4"
      city        = "New York"
      state       = "NY"
      postal_code = "10005"
      country     = "US"
    }
    address_validation = { mode = "disabled" }
    business_name      = "Acme Corp"
    customs            = { eori_number = "GB987654321000" }
    phone_number       = "+18005550122"
    require_signature  = false
    service            = "priority"
    type               = "bulk"
  }
`

// A physical card of an active design takes its second line and shipping on create, and Stripe
// answers with the address it normalised, its validation's result, a carrier for the service and
// a pending shipment; an update sends the shipping whole, in place, and Stripe answers again: with
// validation disabled there is no normalised address or result (suite: TestIssuingCardAttributes,
// probe `update/2`). The second line is create-only.
func TestAccIssuingCard_shipping(t *testing.T) {
	s := issuing(t)
	active := design(t, s, true)
	withDesign := fmt.Sprintf("  personalization_design = %q\n", active)
	var first string
	resource.ParallelTest(t, resource.TestCase{
		ProtoV6ProviderFactories: tfacc.ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: cardConfigOf(s, "first", "physical", withDesign+seattle),
				Check: resource.ComposeAggregateTestCheckFunc(
					idOf(&first),
					resource.TestCheckResourceAttr(address, "type", "physical"),
					resource.TestCheckResourceAttr(address, "personalization_design", active),
					resource.TestCheckResourceAttr(address, "second_line", "ACME INC"),
					resource.TestCheckResourceAttr(address, "shipping.name", "Sam Doe"),
					resource.TestCheckResourceAttr(address, "shipping.address.line2", "Floor 3"),
					resource.TestCheckResourceAttr(address, "shipping.business_name", "Acme Inc"),
					resource.TestCheckResourceAttr(address, "shipping.customs.eori_number", "GB123456789000"),
					resource.TestCheckResourceAttr(address, "shipping.phone_number", "+18005550111"),
					resource.TestCheckResourceAttr(address, "shipping.require_signature", "true"),
					resource.TestCheckResourceAttr(address, "shipping.service", "express"),
					resource.TestCheckResourceAttr(address, "shipping.type", "individual"),
					resource.TestCheckResourceAttr(address, "shipping.address_validation.mode", "validation_and_normalization"),
					resource.TestCheckResourceAttr(address, "shipping.address_validation.result", "likely_deliverable"),
					resource.TestCheckResourceAttr(address, "shipping.address_validation.normalized_address.line1", "1 PIKE ST"),
					resource.TestCheckResourceAttr(address, "shipping.address_validation.normalized_address.line2", "FL 3"),
					resource.TestCheckResourceAttr(address, "shipping.address_validation.normalized_address.city", "SEATTLE"),
					resource.TestCheckResourceAttr(address, "shipping.address_validation.normalized_address.postal_code", "98101"),
					resource.TestCheckResourceAttr(address, "shipping.carrier", "fedex"),
					resource.TestCheckResourceAttr(address, "shipping.status", "pending"),
					resource.TestCheckResourceAttrSet(address, "shipping.eta"),
					resource.TestCheckNoResourceAttr(address, "shipping.tracking_number"),
				),
			},
			{
				Config: cardConfigOf(s, "first", "physical", withDesign+newYork),
				ConfigPlanChecks: resource.ConfigPlanChecks{PreApply: []plancheck.PlanCheck{
					plancheck.ExpectResourceAction(address, plancheck.ResourceActionUpdate),
				}},
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttrWith(address, "id", func(v string) error {
						if v != first {
							return fmt.Errorf("the update replaced the card %s with %s", first, v)
						}
						return nil
					}),
					resource.TestCheckResourceAttr(address, "shipping.name", "Sam Q Doe"),
					resource.TestCheckResourceAttr(address, "shipping.address.city", "New York"),
					resource.TestCheckResourceAttr(address, "shipping.business_name", "Acme Corp"),
					resource.TestCheckResourceAttr(address, "shipping.service", "priority"),
					resource.TestCheckResourceAttr(address, "shipping.type", "bulk"),
					resource.TestCheckResourceAttr(address, "shipping.address_validation.mode", "disabled"),
					resource.TestCheckNoResourceAttr(address, "shipping.address_validation.result"),
					resource.TestCheckNoResourceAttr(address, "shipping.address_validation.normalized_address.line1"),
					resource.TestCheckResourceAttr(address, "shipping.carrier", "fedex"),
					resource.TestCheckResourceAttr(address, "shipping.status", "pending"),
				),
			},
			{Config: cardConfigOf(s, "first", "physical", withDesign+newYork), PlanOnly: true},
		},
	})
}

// Stripe moves a card to another design only from a design it rejected, and only to an active
// one: the suite creates the card on a design in review, rejects that design and moves the card
// to an active design (TestIssuingCardAttributes, variant `in_review`, whose update the recipe
// readies with `rejectCardDesign`). A move from a design in force is a state the record does not
// prove, so the test does not make one.
func TestAccIssuingCard_personalizationDesign(t *testing.T) {
	s := issuing(t)
	inReview, other := design(t, s, false), design(t, s, true)
	shipping := `  shipping = {
    name    = "Sam Doe"
    address = { line1 = "1 Pike Pl", city = "Seattle", state = "WA", postal_code = "98101", country = "US" }
  }
`
	resource.ParallelTest(t, resource.TestCase{
		ProtoV6ProviderFactories: tfacc.ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: cardConfigOf(s, "first", "physical", fmt.Sprintf("  personalization_design = %q\n", inReview)+shipping),
				Check:  resource.TestCheckResourceAttr(address, "personalization_design", inReview),
			},
			{
				PreConfig: func() {
					designHelper(t, s, inReview, "reject", wire.Object{{Name: "rejection_reasons", Value: wire.Object{
						{Name: "card_logo", Value: wire.Array{wire.String("other")}},
						{Name: "carrier_text", Value: wire.Array{wire.String("other")}},
					}}})
				},
				Config: cardConfigOf(s, "first", "physical", fmt.Sprintf("  personalization_design = %q\n", other)+shipping),
				ConfigPlanChecks: resource.ConfigPlanChecks{PreApply: []plancheck.PlanCheck{
					plancheck.ExpectResourceAction(address, plancheck.ResourceActionUpdate),
				}},
				Check: resource.TestCheckResourceAttr(address, "personalization_design", other),
			},
		},
	})
}
