package payment_link_test

import (
	"fmt"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/plancheck"

	"github.com/computomatic/terraform-provider-stripe/internal/acctest"
	"github.com/computomatic/terraform-provider-stripe/internal/acctest/tfacc"
	"github.com/computomatic/terraform-provider-stripe/internal/wire"
)

// An inline product's tax details take a performance location, beside the tax code of an event's
// admission, as the suite's probe `isolated/performance_location` sends them on a performance
// location at the suite's head office address (`suiteTaxLocation`), which Stripe cannot delete.
// Stripe never returns the product data, so state keeps them from configuration.
func TestAccPaymentLink_performanceLocation(t *testing.T) {
	acct := acctest.NewAccount(t)
	location := tfacc.Create(t, acct, "/v1/tax/locations", wire.Object{
		{Name: "type", Value: wire.String("performance")},
		{Name: "description", Value: wire.String(acct.Name("Payment link venue"))},
		{Name: "address", Value: wire.Object{
			{Name: "line1", Value: wire.String("354 Oyster Point Blvd")},
			{Name: "city", Value: wire.String("South San Francisco")},
			{Name: "state", Value: wire.String("CA")},
			{Name: "postal_code", Value: wire.String("94080")},
			{Name: "country", Value: wire.String("US")},
		}},
	})
	items := fmt.Sprintf(`[{
    quantity = 2
    price_data = {
      currency    = "usd"
      unit_amount = 5000
      product_data = {
        name        = %q
        tax_details = { performance_location = %q, tax_code = "txcd_20030000" }
      }
    }
  }]`, acct.Name("Admission"), location)
	resource.ParallelTest(t, resource.TestCase{
		ProtoV6ProviderFactories: tfacc.ProviderFactories,
		Steps: []resource.TestStep{{
			Config: linkConfig(acct, items, ""),
			Check: resource.ComposeAggregateTestCheckFunc(
				resource.TestCheckResourceAttr(address, "line_items.0.price_data.product_data.tax_details.performance_location", location),
				resource.TestCheckResourceAttr(address, "line_items.0.amount_total", "10000"),
			),
			ConfigPlanChecks: resource.ConfigPlanChecks{PostApplyPostRefresh: []plancheck.PlanCheck{plancheck.ExpectEmptyPlan()}},
		}},
	})
}
