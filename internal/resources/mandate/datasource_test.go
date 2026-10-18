package mandate_test

import (
	"os"
	"strings"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"

	"github.com/computomatic/terraform-provider-stripe/internal/acctest"
	"github.com/computomatic/terraform-provider-stripe/internal/acctest/tfacc"
	"github.com/computomatic/terraform-provider-stripe/internal/stripeapi"
	"github.com/computomatic/terraform-provider-stripe/internal/wire"
)

func TestMain(m *testing.M) { os.Exit(acctest.Main(m)) }

// Confirming an offline accepted SEPA debit setup intent makes a mandate (suite: "Mandates"):
// the default payment method configuration allows no SEPA debit, so the intent uses a
// configuration with it on. Nothing else creates a mandate.
func TestAccMandate_read(t *testing.T) {
	acct := acctest.NewAccount(t)
	config := tfacc.Create(t, acct, "/v1/payment_method_configurations", wire.Object{
		{Name: "name", Value: wire.String("sepa")},
		{Name: "sepa_debit", Value: wire.Object{{Name: "display_preference", Value: wire.Object{{Name: "preference", Value: wire.String("on")}}}}},
	})
	customer := tfacc.Create(t, acct, "/v1/customers", nil)
	resp, err := tfacc.Client(acct).Do(t.Context(), stripeapi.Request{Method: "POST", Path: "/v1/setup_intents", Params: wire.Object{
		{Name: "customer", Value: wire.String(customer)},
		{Name: "confirm", Value: wire.Bool(true)},
		{Name: "payment_method_configuration", Value: wire.String(config)},
		{Name: "payment_method_data", Value: wire.Object{
			{Name: "type", Value: wire.String("sepa_debit")},
			{Name: "sepa_debit", Value: wire.Object{{Name: "iban", Value: wire.String("DE89370400440532013000")}}},
			{Name: "billing_details", Value: wire.Object{
				{Name: "name", Value: wire.String("Conformance")},
				{Name: "email", Value: wire.String("conformance@example.com")},
			}},
		}},
		{Name: "mandate_data", Value: wire.Object{{Name: "customer_acceptance", Value: wire.Object{{Name: "type", Value: wire.String("offline")}}}}},
		{Name: "automatic_payment_methods", Value: wire.Object{
			{Name: "enabled", Value: wire.Bool(true)},
			{Name: "allow_redirects", Value: wire.String("never")},
		}},
	}})
	if err != nil {
		if strings.Contains(err.Error(), "fake_") {
			t.Skipf("the fake does not serve a SEPA debit mandate yet: %v", err)
		}
		t.Fatalf("confirming a SEPA debit setup intent: %v", err)
	}
	body := resp.Body.(wire.Object)
	mandate, _ := body.GetString("mandate")
	pm, _ := body.GetString("payment_method")
	resource.ParallelTest(t, resource.TestCase{
		ProtoV6ProviderFactories: tfacc.ProviderFactories,
		Steps: []resource.TestStep{{
			Config: acct.ProviderBlock() + `
data "stripe_mandate" "test" {
  id = "` + mandate + `"
}
`,
			Check: resource.ComposeAggregateTestCheckFunc(
				resource.TestCheckResourceAttr("data.stripe_mandate.test", "id", mandate),
				resource.TestCheckResourceAttr("data.stripe_mandate.test", "status", "active"),
				resource.TestCheckResourceAttr("data.stripe_mandate.test", "type", "multi_use"),
				resource.TestCheckResourceAttr("data.stripe_mandate.test", "payment_method", pm),
				resource.TestCheckResourceAttr("data.stripe_mandate.test", "customer_acceptance.type", "offline"),
				resource.TestCheckResourceAttr("data.stripe_mandate.test", "payment_method_details.type", "sepa_debit"),
			),
		}},
	})
}
