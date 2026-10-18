package setup_intent_test

import (
	"fmt"
	"os"
	"regexp"
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

const address = "stripe_setup_intent.test"

// intentConfig is a customer and a setup intent for it. extra lands inside the intent.
func intentConfig(c acctest.Config, extra string) string {
	return c.ProviderBlock() + fmt.Sprintf(`
resource "stripe_customer" "test" {}

resource "stripe_setup_intent" "test" {
  automatic_payment_methods = {
    enabled         = true
    allow_redirects = "never"
  }
%s}
`, extra)
}

func idOf(dst *string) resource.TestCheckFunc {
	return resource.TestCheckResourceAttrWith(address, "id", func(v string) error {
		*dst = v
		return nil
	})
}

// paymentMethod creates a card payment method from a test token, as the suite does
// ("PaymentMethod", "create card from token"), and returns its ID.
func paymentMethod(t *testing.T, acct acctest.Config) string {
	t.Helper()
	resp, err := tfacc.Client(acct).Do(t.Context(), stripeapi.Request{Method: "POST", Path: "/v1/payment_methods", Params: wire.Object{
		{Name: "type", Value: wire.String("card")},
		{Name: "card", Value: wire.Object{{Name: "token", Value: wire.String("tok_visa")}}},
	}})
	if err != nil {
		t.Fatalf("creating a payment method: %v", err)
	}
	id, _ := resp.Body.(wire.Object).GetString("id")
	return id
}

func TestAccSetupIntent_create(t *testing.T) {
	acct := acctest.NewAccount(t)
	resource.ParallelTest(t, resource.TestCase{
		ProtoV6ProviderFactories: tfacc.ProviderFactories,
		Steps: []resource.TestStep{{
			Config: intentConfig(acct, "  customer = stripe_customer.test.id\n  description = \"Card on file\"\n  usage = \"on_session\"\n  metadata = { env = \"test\" }\n") + `
data "stripe_setup_intent" "by_id" {
  id = stripe_setup_intent.test.id
}
`,
			Check: resource.ComposeAggregateTestCheckFunc(
				resource.TestCheckResourceAttrSet(address, "id"),
				resource.TestMatchResourceAttr(address, "client_secret", regexp.MustCompile(`^seti_.+_secret_`)),
				resource.TestCheckResourceAttr(address, "status", "requires_payment_method"),
				resource.TestCheckResourceAttrPair(address, "customer", "stripe_customer.test", "id"),
				resource.TestCheckResourceAttr(address, "description", "Card on file"),
				resource.TestCheckResourceAttr(address, "usage", "on_session"),
				resource.TestCheckResourceAttr(address, "metadata.env", "test"),
				resource.TestCheckResourceAttr(address, "automatic_payment_methods.enabled", "true"),
				resource.TestCheckResourceAttr(address, "automatic_payment_methods.allow_redirects", "never"),
				resource.TestCheckResourceAttrPair("data.stripe_setup_intent.by_id", "id", address, "id"),
				resource.TestCheckResourceAttr("data.stripe_setup_intent.by_id", "status", "requires_payment_method"),
			),
		}},
	})
}

// A payment method moves the intent to `requires_confirmation`, and removing it moves the
// intent back (suite: "update payment method moves status").
func TestAccSetupIntent_update(t *testing.T) {
	acct := acctest.NewAccount(t)
	pm := paymentMethod(t, acct)
	var id, secret string
	resource.ParallelTest(t, resource.TestCase{
		ProtoV6ProviderFactories: tfacc.ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: intentConfig(acct, "  description = \"one\"\n  metadata = { keep = \"me\", drop = \"me\" }\n"),
				Check: resource.ComposeAggregateTestCheckFunc(idOf(&id),
					resource.TestCheckResourceAttrWith(address, "client_secret", func(v string) error { secret = v; return nil })),
			},
			{
				Config: intentConfig(acct, fmt.Sprintf("  description = \"two\"\n  customer = stripe_customer.test.id\n  payment_method = %q\n  metadata = { keep = \"me\", new = \"v\" }\n", pm)),
				ConfigPlanChecks: resource.ConfigPlanChecks{PreApply: []plancheck.PlanCheck{
					plancheck.ExpectResourceAction(address, plancheck.ResourceActionUpdate),
				}},
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttrWith(address, "id", func(v string) error {
						if v != id {
							return fmt.Errorf("the update replaced the setup intent %s with %s", id, v)
						}
						return nil
					}),
					resource.TestCheckResourceAttr(address, "description", "two"),
					resource.TestCheckResourceAttrPair(address, "customer", "stripe_customer.test", "id"),
					resource.TestCheckResourceAttr(address, "payment_method", pm),
					resource.TestCheckResourceAttr(address, "status", "requires_confirmation"),
					resource.TestCheckResourceAttr(address, "metadata.%", "2"),
					resource.TestCheckResourceAttr(address, "metadata.new", "v"),
					resource.TestCheckResourceAttrWith(address, "client_secret", func(v string) error {
						if v != secret {
							return fmt.Errorf("the client secret changed on update")
						}
						return nil
					}),
				),
			},
			{
				Config: intentConfig(acct, "  description = \"two\"\n  customer = stripe_customer.test.id\n  metadata = { keep = \"me\", new = \"v\" }\n"),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckNoResourceAttr(address, "payment_method"),
					resource.TestCheckResourceAttr(address, "status", "requires_payment_method"),
				),
			},
		},
	})
}

// `usage` is accepted on create only (suite: "update immutable fields rejected"), so changing
// it replaces the intent.
func TestAccSetupIntent_replace(t *testing.T) {
	acct := acctest.NewAccount(t)
	var first string
	resource.ParallelTest(t, resource.TestCase{
		ProtoV6ProviderFactories: tfacc.ProviderFactories,
		Steps: []resource.TestStep{
			{Config: intentConfig(acct, "  usage = \"on_session\"\n"), Check: idOf(&first)},
			{
				Config: intentConfig(acct, "  usage = \"off_session\"\n"),
				ConfigPlanChecks: resource.ConfigPlanChecks{PreApply: []plancheck.PlanCheck{
					plancheck.ExpectResourceAction(address, plancheck.ResourceActionReplace),
				}},
				Check: resource.TestCheckResourceAttrWith(address, "id", func(v string) error {
					if v == first {
						return fmt.Errorf("the replacement kept the setup intent %s", v)
					}
					return nil
				}),
			},
		},
	})
}

func TestAccSetupIntent_import(t *testing.T) {
	acct := acctest.NewAccount(t)
	cfg := intentConfig(acct, "  customer = stripe_customer.test.id\n  description = \"Card on file\"\n  metadata = { team = \"a\" }\n")
	resource.ParallelTest(t, resource.TestCase{
		ProtoV6ProviderFactories: tfacc.ProviderFactories,
		Steps: []resource.TestStep{
			{Config: cfg},
			{
				ResourceName:            address,
				ImportState:             true,
				ImportStateVerify:       true,
				ImportStateVerifyIgnore: []string{"client_secret"},
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

func status(t *testing.T, acct acctest.Config, id string) (string, error) {
	t.Helper()
	resp, err := tfacc.Client(acct).Do(t.Context(), stripeapi.Request{Method: "GET", Path: "/v1/setup_intents/" + id})
	if err != nil {
		return "", err
	}
	s, _ := resp.Body.(wire.Object).GetString("status")
	return s, nil
}

// Stripe cannot delete a setup intent; one that is not finished cancels (suite: "cancel"), so
// destroy cancels it and the intent stays in the account.
func TestAccSetupIntent_delete(t *testing.T) {
	acct := acctest.NewAccount(t)
	var id string
	resource.ParallelTest(t, resource.TestCase{
		ProtoV6ProviderFactories: tfacc.ProviderFactories,
		CheckDestroy: func(*terraform.State) error {
			got, err := status(t, acct, id)
			if err != nil {
				return fmt.Errorf("a canceled setup intent still retrieves: %w", err)
			}
			if got != "canceled" {
				return fmt.Errorf("setup intent %s is %q after destroy, want canceled", id, got)
			}
			return nil
		},
		Steps: []resource.TestStep{{Config: intentConfig(acct, ""), Check: idOf(&id)}},
	})
}

// A succeeded intent cannot be canceled (suite: "cancel errors", "already succeeded"), so
// destroy leaves it as it is.
func TestAccSetupIntent_deleteAfterSuccess(t *testing.T) {
	acct := acctest.NewAccount(t)
	pm := paymentMethod(t, acct)
	var id string
	cfg := intentConfig(acct, fmt.Sprintf("  payment_method = %q\n", pm))
	resource.ParallelTest(t, resource.TestCase{
		ProtoV6ProviderFactories: tfacc.ProviderFactories,
		CheckDestroy: func(*terraform.State) error {
			if got, err := status(t, acct, id); err != nil || got != "succeeded" {
				return fmt.Errorf("setup intent %s is %q (%v) after destroy, want succeeded", id, got, err)
			}
			return nil
		},
		Steps: []resource.TestStep{
			{Config: cfg, Check: idOf(&id)},
			{
				PreConfig: func() {
					tfacc.Post(t, acct, "/v1/setup_intents/"+id+"/confirm", nil)
				},
				Config: cfg,
				Check:  resource.TestCheckResourceAttr(address, "status", "succeeded"),
			},
		},
	})
}

// An intent canceled out of band reads as canceled, and destroy does not cancel it again
// (suite: "cancel errors", "already canceled").
func TestAccSetupIntent_deleteAfterCancel(t *testing.T) {
	acct := acctest.NewAccount(t)
	var id string
	cfg := intentConfig(acct, "")
	resource.ParallelTest(t, resource.TestCase{
		ProtoV6ProviderFactories: tfacc.ProviderFactories,
		Steps: []resource.TestStep{
			{Config: cfg, Check: idOf(&id)},
			{
				PreConfig: func() { tfacc.Post(t, acct, "/v1/setup_intents/"+id+"/cancel", nil) },
				Config:    cfg,
				Check:     resource.TestCheckResourceAttr(address, "status", "canceled"),
			},
		},
	})
}

func TestAccSetupIntent_drift(t *testing.T) {
	acct := acctest.NewAccount(t)
	var id string
	cfg := intentConfig(acct, "  description = \"Card on file\"\n")
	resource.ParallelTest(t, resource.TestCase{
		ProtoV6ProviderFactories: tfacc.ProviderFactories,
		Steps: []resource.TestStep{
			{Config: cfg, Check: idOf(&id)},
			{
				PreConfig: func() {
					tfacc.Post(t, acct, "/v1/setup_intents/"+id, wire.Object{{Name: "description", Value: wire.String("changed")}})
				},
				Config: cfg,
				ConfigPlanChecks: resource.ConfigPlanChecks{PreApply: []plancheck.PlanCheck{
					plancheck.ExpectNonEmptyPlan(),
					plancheck.ExpectResourceAction(address, plancheck.ResourceActionUpdate),
				}},
				Check: resource.TestCheckResourceAttr(address, "description", "Card on file"),
			},
		},
	})
}
