package customer_payment_method_test

import (
	"fmt"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/plancheck"

	"github.com/computomatic/terraform-provider-stripe/internal/acctest"
	"github.com/computomatic/terraform-provider-stripe/internal/acctest/tfacc"
	"github.com/computomatic/terraform-provider-stripe/internal/stripeapi"
	"github.com/computomatic/terraform-provider-stripe/internal/wire"
)

// verifiedBankAccount is the suite's US bank account payment method of probe
// `isolated/us_bank_account_method` (TestCustomerPaymentMethodAttributes,
// `verifiedUSBankAccountPaymentMethod`): an individual's account from Stripe's test routing and
// account numbers, which a setup intent with microdeposits attaches to a new customer once Stripe's
// test amounts verify it, as the suite's microdeposits attempt sends it, in a payment method
// configuration that turns the method on. It returns the customer and the payment method.
func verifiedBankAccount(t *testing.T, acct acctest.Config) (string, string) {
	t.Helper()
	post := func(path string, params wire.Object) wire.Object {
		resp, err := tfacc.Client(acct).Do(t.Context(), stripeapi.Request{Method: "POST", Path: path, Params: params})
		if err != nil {
			t.Fatalf("POST %s: %v", path, err)
		}
		return resp.Body.(wire.Object)
	}
	customer, _ := post("/v1/customers", wire.Object{{Name: "name", Value: wire.String(acct.Name("Bank account customer"))}}).GetString("id")
	config, _ := post("/v1/payment_method_configurations", wire.Object{
		{Name: "name", Value: wire.String(acct.Name("attrs us_bank_account"))},
		{Name: "us_bank_account", Value: wire.Object{{Name: "display_preference", Value: wire.Object{{Name: "preference", Value: wire.String("on")}}}}},
	}).GetString("id")
	intent := post("/v1/setup_intents", wire.Object{
		{Name: "customer", Value: wire.String(customer)},
		{Name: "automatic_payment_methods", Value: wire.Object{{Name: "enabled", Value: wire.Bool(true)}}},
		{Name: "payment_method_configuration", Value: wire.String(config)},
		{Name: "payment_method_data", Value: wire.Object{
			{Name: "type", Value: wire.String("us_bank_account")},
			{Name: "billing_details", Value: wire.Object{{Name: "name", Value: wire.String("Jenny Rosen")}, {Name: "email", Value: wire.String("jenny.rosen@example.com")}}},
			{Name: "us_bank_account", Value: wire.Object{
				{Name: "routing_number", Value: wire.String("110000000")},
				{Name: "account_number", Value: wire.String("000123456789")},
				{Name: "account_holder_type", Value: wire.String("individual")},
			}},
		}},
		{Name: "payment_method_options", Value: wire.Object{{Name: "us_bank_account", Value: wire.Object{{Name: "verification_method", Value: wire.String("microdeposits")}}}}},
		{Name: "mandate_data", Value: wire.Object{{Name: "customer_acceptance", Value: wire.Object{
			{Name: "type", Value: wire.String("online")},
			{Name: "online", Value: wire.Object{{Name: "ip_address", Value: wire.String("8.8.8.8")}, {Name: "user_agent", Value: wire.String("stripe-api-conformance")}}},
		}}}},
		{Name: "confirm", Value: wire.Bool(true)},
		{Name: "return_url", Value: wire.String("https://example.com/return")},
	})
	id, _ := intent.GetString("id")
	verified := post("/v1/setup_intents/"+id+"/verify_microdeposits", wire.Object{{Name: "amounts", Value: wire.Array{wire.Int(32), wire.Int(45)}}})
	pm, _ := verified.GetString("payment_method")
	if status, _ := verified.GetString("status"); status != "succeeded" || pm == "" {
		t.Fatalf("setup intent %s did not verify a bank account: %v", id, verified)
	}
	return customer, pm
}

// A US bank account's holder type and account type change in place, as the suite's probe
// `isolated/us_bank_account_method` changes them on a verified account attached again to its
// customer. It runs on the fake only: each run makes a payment method configuration, which Stripe
// cannot delete.
func TestAccCustomerPaymentMethod_bankAccountUpdate(t *testing.T) {
	acctest.RequireFake(t)
	acct := acctest.NewAccount(t)
	customer, pm := verifiedBankAccount(t, acct)
	cfg := func(extra string) string {
		return acct.ProviderBlock() + fmt.Sprintf(`
resource "stripe_customer_payment_method" "test" {
  customer       = %q
  payment_method = %q
%s}
`, customer, pm, extra)
	}
	resource.ParallelTest(t, resource.TestCase{
		ProtoV6ProviderFactories: tfacc.ProviderFactories,
		Steps: acctest.Forget(t, []resource.TestStep{
			{
				Config: cfg(""),
				Check:  resource.TestCheckResourceAttr(address, "us_bank_account.account_holder_type", "individual"),
			},
			{
				Config: cfg("  us_bank_account = { account_holder_type = \"company\", account_type = \"savings\" }\n"),
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply:             []plancheck.PlanCheck{plancheck.ExpectResourceAction(address, plancheck.ResourceActionUpdate)},
					PostApplyPostRefresh: []plancheck.PlanCheck{plancheck.ExpectEmptyPlan()},
				},
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr(address, "us_bank_account.account_holder_type", "company"),
					resource.TestCheckResourceAttr(address, "us_bank_account.account_type", "savings"),
				),
			},
		}, nil, "stripe_customer_payment_method"),
	})
}
