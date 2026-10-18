package customer_payment_method_test

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

const address = "stripe_customer_payment_method.test"

var regexpInvalidImportID = regexp.MustCompile(`expected .<customer id>/<id>.`)

// attachmentConfig is a customer and the attachment of the payment method pm to it. extra
// lands inside the attachment.
func attachmentConfig(c acctest.Config, pm string, extra string) string {
	return c.ProviderBlock() + fmt.Sprintf(`
resource "stripe_customer" "test" {}

resource "stripe_customer_payment_method" "test" {
  customer       = stripe_customer.test.id
  payment_method = %q
%s}
`, pm, extra)
}

// paymentMethod creates an unattached card payment method from a test token (suite:
// "PaymentMethod", "create card from token") and returns its ID. Creating one takes card
// data, so the provider has no resource for it.
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

func idOf(dst *string) resource.TestCheckFunc {
	return resource.TestCheckResourceAttrWith(address, "id", func(v string) error {
		*dst = v
		return nil
	})
}

func importID(s *terraform.State) (string, error) {
	rs, ok := s.RootModule().Resources[address]
	if !ok {
		return "", fmt.Errorf("%s is not in the state", address)
	}
	return rs.Primary.Attributes["customer"] + "/" + rs.Primary.ID, nil
}

// customerOf reads a payment method's customer straight from Stripe.
func customerOf(t *testing.T, acct acctest.Config, pm string) (string, error) {
	t.Helper()
	resp, err := tfacc.Client(acct).Do(t.Context(), stripeapi.Request{Method: "GET", Path: "/v1/payment_methods/" + pm})
	if err != nil {
		return "", err
	}
	customer, _ := resp.Body.(wire.Object).GetString("customer")
	return customer, nil
}

func TestAccCustomerPaymentMethod_create(t *testing.T) {
	acct := acctest.NewAccount(t)
	pm := paymentMethod(t, acct)
	resource.ParallelTest(t, resource.TestCase{
		ProtoV6ProviderFactories: tfacc.ProviderFactories,
		Steps: []resource.TestStep{{
			Config: attachmentConfig(acct, pm, "") + `
data "stripe_customer_payment_method" "by_id" {
  customer = stripe_customer.test.id
  id       = stripe_customer_payment_method.test.id
}
`,
			Check: resource.ComposeAggregateTestCheckFunc(
				resource.TestCheckResourceAttr(address, "id", pm),
				resource.TestCheckResourceAttr(address, "payment_method", pm),
				resource.TestCheckResourceAttrPair(address, "customer", "stripe_customer.test", "id"),
				resource.TestCheckResourceAttr(address, "type", "card"),
				resource.TestCheckResourceAttr(address, "card.brand", "visa"),
				resource.TestCheckResourceAttr(address, "card.last4", "4242"),
				resource.TestCheckResourceAttrPair("data.stripe_customer_payment_method.by_id", "id", address, "id"),
				resource.TestCheckResourceAttr("data.stripe_customer_payment_method.by_id", "card.last4", "4242"),
			),
		}},
	})
}

// Attaching a Stripe test payment method returns a new payment method (suite: "attach test
// payment method id"), so the `id` differs from `payment_method` and the plan stays empty.
func TestAccCustomerPaymentMethod_testPaymentMethod(t *testing.T) {
	acct := acctest.NewAccount(t)
	cfg := attachmentConfig(acct, "pm_card_visa", "")
	resource.ParallelTest(t, resource.TestCase{
		ProtoV6ProviderFactories: tfacc.ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: cfg,
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr(address, "payment_method", "pm_card_visa"),
					resource.TestMatchResourceAttr(address, "id", regexp.MustCompile(`^pm_`)),
					resource.TestCheckResourceAttr(address, "card.last4", "4242"),
				),
			},
			{Config: cfg, PlanOnly: true},
		},
	})
}

func TestAccCustomerPaymentMethod_update(t *testing.T) {
	acct := acctest.NewAccount(t)
	pm := paymentMethod(t, acct)
	resource.ParallelTest(t, resource.TestCase{
		ProtoV6ProviderFactories: tfacc.ProviderFactories,
		Steps: []resource.TestStep{
			{Config: attachmentConfig(acct, pm, "  metadata = { keep = \"me\", drop = \"me\" }\n")},
			{
				Config: attachmentConfig(acct, pm, `  metadata = { keep = "me", new = "v" }
  billing_details = {
    name  = "Ada Lovelace"
    email = "ada@example.com"
    address = {
      line1       = "1 Main St"
      city        = "Springfield"
      country     = "US"
      postal_code = "12345"
    }
  }
  card = {
    exp_year = 2039
  }
`),
				ConfigPlanChecks: resource.ConfigPlanChecks{PreApply: []plancheck.PlanCheck{
					plancheck.ExpectResourceAction(address, plancheck.ResourceActionUpdate),
				}},
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr(address, "id", pm),
					resource.TestCheckResourceAttr(address, "billing_details.name", "Ada Lovelace"),
					resource.TestCheckResourceAttr(address, "billing_details.email", "ada@example.com"),
					resource.TestCheckResourceAttr(address, "billing_details.address.city", "Springfield"),
					resource.TestCheckResourceAttr(address, "card.exp_year", "2039"),
					resource.TestCheckResourceAttr(address, "card.last4", "4242"),
					resource.TestCheckResourceAttr(address, "metadata.%", "2"),
					resource.TestCheckResourceAttr(address, "metadata.new", "v"),
				),
			},
		},
	})
}

// A payment method is attached once and never again after a detach (suite: "detached cannot
// be reattached"), so naming another payment method replaces the attachment: the old method
// is detached and the new one attached.
func TestAccCustomerPaymentMethod_replace(t *testing.T) {
	acct := acctest.NewAccount(t)
	first, second := paymentMethod(t, acct), paymentMethod(t, acct)
	var id string
	resource.ParallelTest(t, resource.TestCase{
		ProtoV6ProviderFactories: tfacc.ProviderFactories,
		Steps: []resource.TestStep{
			{Config: attachmentConfig(acct, first, ""), Check: idOf(&id)},
			{
				Config: attachmentConfig(acct, second, ""),
				ConfigPlanChecks: resource.ConfigPlanChecks{PreApply: []plancheck.PlanCheck{
					plancheck.ExpectResourceAction(address, plancheck.ResourceActionReplace),
				}},
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr(address, "id", second),
					func(*terraform.State) error {
						if customer, err := customerOf(t, acct, first); err != nil || customer != "" {
							return fmt.Errorf("the replaced payment method %s is still attached to %q (%v)", first, customer, err)
						}
						return nil
					},
				),
			},
		},
	})
}

func TestAccCustomerPaymentMethod_import(t *testing.T) {
	acct := acctest.NewAccount(t)
	cfg := attachmentConfig(acct, paymentMethod(t, acct), "  metadata = { team = \"a\" }\n")
	resource.ParallelTest(t, resource.TestCase{
		ProtoV6ProviderFactories: tfacc.ProviderFactories,
		Steps: []resource.TestStep{
			{Config: cfg},
			{
				ResourceName:      address,
				ImportState:       true,
				ImportStateIdFunc: importID,
				ImportStateVerify: true,
			},
			{
				ResourceName:      address,
				ImportState:       true,
				ImportStateKind:   resource.ImportBlockWithID,
				ImportStateIdFunc: importID,
				Config:            cfg,
			},
			{
				ResourceName:  address,
				ImportState:   true,
				ImportStateId: "not-a-compound-id",
				ExpectError:   regexpInvalidImportID,
			},
		},
	})
}

// Destroy detaches the payment method (suite: "detach"); the method stays in the account,
// with no customer.
func TestAccCustomerPaymentMethod_delete(t *testing.T) {
	acct := acctest.NewAccount(t)
	pm := paymentMethod(t, acct)
	resource.ParallelTest(t, resource.TestCase{
		ProtoV6ProviderFactories: tfacc.ProviderFactories,
		CheckDestroy: func(*terraform.State) error {
			customer, err := customerOf(t, acct, pm)
			if err != nil {
				return fmt.Errorf("a detached payment method still retrieves: %w", err)
			}
			if customer != "" {
				return fmt.Errorf("payment method %s is still attached to %s after destroy", pm, customer)
			}
			return nil
		},
		Steps: []resource.TestStep{{Config: attachmentConfig(acct, pm, "")}},
	})
}

func TestAccCustomerPaymentMethod_drift(t *testing.T) {
	acct := acctest.NewAccount(t)
	pm := paymentMethod(t, acct)
	cfg := attachmentConfig(acct, pm, "  billing_details = {\n    name = \"Ada Lovelace\"\n  }\n")
	resource.ParallelTest(t, resource.TestCase{
		ProtoV6ProviderFactories: tfacc.ProviderFactories,
		Steps: []resource.TestStep{
			{Config: cfg},
			{
				PreConfig: func() {
					tfacc.Post(t, acct, "/v1/payment_methods/"+pm, wire.Object{{Name: "billing_details", Value: wire.Object{{Name: "name", Value: wire.String("Someone Else")}}}})
				},
				Config: cfg,
				ConfigPlanChecks: resource.ConfigPlanChecks{PreApply: []plancheck.PlanCheck{
					plancheck.ExpectNonEmptyPlan(),
					plancheck.ExpectResourceAction(address, plancheck.ResourceActionUpdate),
				}},
				Check: resource.TestCheckResourceAttr(address, "billing_details.name", "Ada Lovelace"),
			},
			{
				// A payment method detached from outside is gone from the customer, and
				// Stripe never lets it be attached again, so the plan wants a new attachment.
				PreConfig:          func() { tfacc.Post(t, acct, "/v1/payment_methods/"+pm+"/detach", nil) },
				Config:             cfg,
				PlanOnly:           true,
				ExpectNonEmptyPlan: true,
			},
		},
	})
}
