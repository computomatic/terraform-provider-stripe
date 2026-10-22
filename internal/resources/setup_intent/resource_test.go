package setup_intent_test

import (
	"fmt"
	"os"
	"regexp"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/plancheck"

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

// pathOf records the setup intent's API path.
func pathOf(dst *string) resource.TestCheckFunc {
	return resource.TestCheckResourceAttrWith(address, "id", func(v string) error {
		*dst = "/v1/setup_intents/" + v
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
		Steps: acctest.Forget(t, []resource.TestStep{{
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
		}}, nil, "stripe_setup_intent"),
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
		Steps: acctest.Forget(t, []resource.TestStep{
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
		}, nil, "stripe_setup_intent"),
	})
}

// `usage` is create-only, so changing it would replace the intent, which Stripe cannot
// delete: the plan fails, and so does a taint, at apply.
func TestAccSetupIntent_replace(t *testing.T) {
	acct := acctest.NewAccount(t)
	cfg := intentConfig(acct, "  usage = \"on_session\"\n")
	resource.ParallelTest(t, resource.TestCase{
		ProtoV6ProviderFactories: tfacc.ProviderFactories,
		Steps: acctest.Forget(t, []resource.TestStep{
			{Config: cfg},
			{Config: intentConfig(acct, "  usage = \"off_session\"\n"), PlanOnly: true, ExpectError: tfacc.RefusedReplace},
			{Config: cfg, Taint: []string{address}, ExpectError: tfacc.RefusedDestroy},
		}, nil, "stripe_setup_intent"),
	})
}

func TestAccSetupIntent_import(t *testing.T) {
	acct := acctest.NewAccount(t)
	cfg := intentConfig(acct, "  customer = stripe_customer.test.id\n  description = \"Card on file\"\n  metadata = { team = \"a\" }\n")
	resource.ParallelTest(t, resource.TestCase{
		ProtoV6ProviderFactories: tfacc.ProviderFactories,
		Steps: acctest.Forget(t, []resource.TestStep{
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
		}, nil, "stripe_setup_intent"),
	})
}

// Stripe cannot delete a setup intent, so dropping it from the configuration, and `terraform
// destroy`, fail at plan time, and the intent stays as it was.
func TestAccSetupIntent_delete(t *testing.T) {
	acct := acctest.NewAccount(t)
	var path string
	var mark int
	cfg := intentConfig(acct, "")
	resource.ParallelTest(t, resource.TestCase{
		ProtoV6ProviderFactories: tfacc.ProviderFactories,
		Steps: acctest.Forget(t, []resource.TestStep{
			{Config: cfg, Check: resource.ComposeTestCheckFunc(pathOf(&path), tfacc.LogMark(t, acct, &mark))},
			{Config: acctest.Without(t, cfg, "stripe_setup_intent"), PlanOnly: true, ExpectError: tfacc.RefusedDestroy},
			{Config: cfg, Destroy: true, ExpectError: tfacc.RefusedDestroy},
		}, tfacc.Untouched(t, acct, &path, &mark, "status", wire.String("requires_payment_method")), "stripe_setup_intent"),
	})
}

// Setting `status` to `canceled` cancels the intent through its action endpoint (suite:
// "cancel"), after which it reads back canceled.
func TestAccSetupIntent_cancel(t *testing.T) {
	acct := acctest.NewAccount(t)
	var path string
	resource.ParallelTest(t, resource.TestCase{
		ProtoV6ProviderFactories: tfacc.ProviderFactories,
		Steps: acctest.Forget(t, []resource.TestStep{
			{Config: intentConfig(acct, ""), Check: pathOf(&path)},
			{
				Config: intentConfig(acct, "  status = \"canceled\"\n"),
				ConfigPlanChecks: resource.ConfigPlanChecks{PreApply: []plancheck.PlanCheck{
					plancheck.ExpectResourceAction(address, plancheck.ResourceActionUpdate),
				}},
				Check: resource.ComposeTestCheckFunc(
					resource.TestCheckResourceAttr(address, "status", "canceled"),
					tfacc.Holds(t, acct, &path, "status", wire.String("canceled")),
				),
			},
		}, nil, "stripe_setup_intent"),
	})
}

func TestAccSetupIntent_drift(t *testing.T) {
	acct := acctest.NewAccount(t)
	var id string
	cfg := intentConfig(acct, "  description = \"Card on file\"\n")
	resource.ParallelTest(t, resource.TestCase{
		ProtoV6ProviderFactories: tfacc.ProviderFactories,
		Steps: acctest.Forget(t, []resource.TestStep{
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
		}, nil, "stripe_setup_intent"),
	})
}

// A succeeded intent cannot be cancelled (suite: "cancel errors", "already succeeded"), so a
// configuration that sets `status` to `canceled` fails at plan time.
func TestAccSetupIntent_cancelSucceeded(t *testing.T) {
	acct := acctest.NewAccount(t)
	var id string
	pm := fmt.Sprintf("  payment_method = %q\n", paymentMethod(t, acct))
	cfg := intentConfig(acct, pm)
	resource.ParallelTest(t, resource.TestCase{
		ProtoV6ProviderFactories: tfacc.ProviderFactories,
		Steps: acctest.Forget(t, []resource.TestStep{
			{Config: cfg, Check: idOf(&id)},
			{
				PreConfig: func() { tfacc.Post(t, acct, "/v1/setup_intents/"+id+"/confirm", nil) },
				Config:    cfg,
				Check:     resource.TestCheckResourceAttr(address, "status", "succeeded"),
			},
			{
				Config:      intentConfig(acct, pm+"  status = \"canceled\"\n"),
				PlanOnly:    true,
				ExpectError: regexp.MustCompile("Cannot change `status` from `succeeded`"),
			},
		}, nil, "stripe_setup_intent"),
	})
}
