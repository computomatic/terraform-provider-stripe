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

func importID(s *terraform.State) (string, error) {
	rs, ok := s.RootModule().Resources[address]
	if !ok {
		return "", fmt.Errorf("%s is not in the state", address)
	}
	return rs.Primary.Attributes["customer"] + "/" + rs.Primary.ID, nil
}

func TestAccCustomerPaymentMethod_create(t *testing.T) {
	acct := acctest.NewAccount(t)
	pm := paymentMethod(t, acct)
	resource.ParallelTest(t, resource.TestCase{
		ProtoV6ProviderFactories: tfacc.ProviderFactories,
		Steps: acctest.Forget(t, []resource.TestStep{{
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
		}}, nil, "stripe_customer_payment_method"),
	})
}

// Attaching a Stripe test payment method returns a new payment method (suite: "attach test
// payment method id"), so the `id` differs from `payment_method` and the plan stays empty.
func TestAccCustomerPaymentMethod_testPaymentMethod(t *testing.T) {
	acct := acctest.NewAccount(t)
	cfg := attachmentConfig(acct, "pm_card_visa", "")
	resource.ParallelTest(t, resource.TestCase{
		ProtoV6ProviderFactories: tfacc.ProviderFactories,
		Steps: acctest.Forget(t, []resource.TestStep{
			{
				Config: cfg,
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr(address, "payment_method", "pm_card_visa"),
					resource.TestMatchResourceAttr(address, "id", regexp.MustCompile(`^pm_`)),
					resource.TestCheckResourceAttr(address, "card.last4", "4242"),
				),
			},
			{Config: cfg, PlanOnly: true},
		}, nil, "stripe_customer_payment_method"),
	})
}

func TestAccCustomerPaymentMethod_update(t *testing.T) {
	acct := acctest.NewAccount(t)
	pm := paymentMethod(t, acct)
	resource.ParallelTest(t, resource.TestCase{
		ProtoV6ProviderFactories: tfacc.ProviderFactories,
		Steps: acctest.Forget(t, []resource.TestStep{
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
		}, nil, "stripe_customer_payment_method"),
	})
}

// Changing the attached payment method would replace the attachment, which Stripe can only
// detach, for good: the plan fails, and so does a taint, at apply.
func TestAccCustomerPaymentMethod_replace(t *testing.T) {
	acct := acctest.NewAccount(t)
	first, second := paymentMethod(t, acct), paymentMethod(t, acct)
	cfg := attachmentConfig(acct, first, "")
	resource.ParallelTest(t, resource.TestCase{
		ProtoV6ProviderFactories: tfacc.ProviderFactories,
		Steps: acctest.Forget(t, []resource.TestStep{
			{Config: cfg},
			{Config: attachmentConfig(acct, second, ""), PlanOnly: true, ExpectError: tfacc.RefusedReplace},
			{Config: cfg, Taint: []string{address}, ExpectError: tfacc.RefusedDestroy},
		}, nil, "stripe_customer_payment_method"),
	})
}

func TestAccCustomerPaymentMethod_import(t *testing.T) {
	acct := acctest.NewAccount(t)
	cfg := attachmentConfig(acct, paymentMethod(t, acct), "  metadata = { team = \"a\" }\n")
	resource.ParallelTest(t, resource.TestCase{
		ProtoV6ProviderFactories: tfacc.ProviderFactories,
		Steps: acctest.Forget(t, []resource.TestStep{
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
		}, nil, "stripe_customer_payment_method"),
	})
}

// Stripe can only detach a payment method, which is final, so dropping the attachment from the
// configuration, and `terraform destroy`, fail at plan time, and the payment method stays
// attached to the customer.
func TestAccCustomerPaymentMethod_delete(t *testing.T) {
	acct := acctest.NewAccount(t)
	pm := paymentMethod(t, acct)
	path := "/v1/payment_methods/" + pm
	var customer string
	var mark int
	cfg := attachmentConfig(acct, pm, "")
	resource.ParallelTest(t, resource.TestCase{
		ProtoV6ProviderFactories: tfacc.ProviderFactories,
		Steps: acctest.Forget(t, []resource.TestStep{
			{Config: cfg, Check: resource.ComposeTestCheckFunc(
				resource.TestCheckResourceAttrWith(address, "customer", func(v string) error { customer = v; return nil }),
				tfacc.LogMark(t, acct, &mark),
			)},
			{Config: acctest.Without(t, cfg, "stripe_customer_payment_method"), PlanOnly: true, ExpectError: tfacc.RefusedDestroy},
			{Config: cfg, Destroy: true, ExpectError: tfacc.RefusedDestroy},
		}, func(s *terraform.State) error {
			return tfacc.Untouched(t, acct, &path, &mark, "customer", wire.String(customer))(s)
		}, "stripe_customer_payment_method"),
	})
}

func TestAccCustomerPaymentMethod_drift(t *testing.T) {
	acct := acctest.NewAccount(t)
	pm := paymentMethod(t, acct)
	cfg := attachmentConfig(acct, pm, "  billing_details = {\n    name = \"Ada Lovelace\"\n  }\n")
	resource.ParallelTest(t, resource.TestCase{
		ProtoV6ProviderFactories: tfacc.ProviderFactories,
		Steps: acctest.Forget(t, []resource.TestStep{
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
		}, nil, "stripe_customer_payment_method"),
	})
}
