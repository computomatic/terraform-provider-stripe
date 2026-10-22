package payment_intent_amount_details_line_item_test

import (
	"os"
	"regexp"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"

	"github.com/computomatic/terraform-provider-stripe/internal/acctest"
	"github.com/computomatic/terraform-provider-stripe/internal/acctest/tfacc"
	"github.com/computomatic/terraform-provider-stripe/internal/wire"
)

func TestMain(m *testing.M) { os.Exit(acctest.Main(m)) }

const address = "data.stripe_payment_intent_amount_details_line_item.test"

func config(c acctest.Config, intent, id string) string {
	return c.ProviderBlock() + `
data "stripe_payment_intent_amount_details_line_item" "test" {
  payment_intent = "` + intent + `"
  id             = "` + id + `"
}
`
}

// withLineItems creates a 2000 payment intent whose two line items add up to the amount, as
// Stripe requires (suite: "amountDetailsForm"), with the automatic payment methods the suite
// creates it with, and returns its ID.
func withLineItems(t *testing.T, acct acctest.Config) string {
	t.Helper()
	item := func(name string, cost, quantity int64) wire.Object {
		return wire.Object{
			{Name: "product_name", Value: wire.String(name)},
			{Name: "unit_cost", Value: wire.Int(cost)},
			{Name: "quantity", Value: wire.Int(quantity)},
		}
	}
	return tfacc.Create(t, acct, "/v1/payment_intents", wire.Object{
		{Name: "amount", Value: wire.Int(2000)},
		{Name: "currency", Value: wire.String(tfacc.AccountCurrency(t, acct))},
		{Name: "amount_details", Value: wire.Object{{Name: "line_items", Value: wire.Array{item("Widget", 600, 2), item("Gadget", 800, 1)}}}},
		{Name: "automatic_payment_methods", Value: wire.Object{
			{Name: "enabled", Value: wire.Bool(true)},
			{Name: "allow_redirects", Value: wire.String("never")},
		}},
	})
}

// Stripe has no retrieve for a line item (suite: "Payment intent line items"), so the data
// source finds the item by its id among the intent's line items, which list in the order given.
func TestAccPaymentIntentAmountDetailsLineItem_read(t *testing.T) {
	acct := acctest.NewAccount(t)
	intent := withLineItems(t, acct)
	id := tfacc.FirstListed(t, acct, "/v1/payment_intents/"+intent+"/amount_details_line_items")
	resource.ParallelTest(t, resource.TestCase{
		ProtoV6ProviderFactories: tfacc.ProviderFactories,
		Steps: []resource.TestStep{{
			Config: config(acct, intent, id),
			Check: resource.ComposeAggregateTestCheckFunc(
				resource.TestCheckResourceAttr(address, "id", id),
				resource.TestCheckResourceAttr(address, "payment_intent", intent),
				resource.TestCheckResourceAttr(address, "product_name", "Widget"),
				resource.TestCheckResourceAttr(address, "unit_cost", "600"),
				resource.TestCheckResourceAttr(address, "quantity", "2"),
				resource.TestCheckNoResourceAttr(address, "discount_amount"),
				resource.TestCheckNoResourceAttr(address, "tax"),
			),
		}},
	})
}

// A line item the intent does not list is not found. The provider never sends its id, so
// Stripe's answer to an unknown cursor does not matter.
func TestAccPaymentIntentAmountDetailsLineItem_notFound(t *testing.T) {
	acct := acctest.NewAccount(t)
	intent := withLineItems(t, acct)
	resource.ParallelTest(t, resource.TestCase{
		ProtoV6ProviderFactories: tfacc.ProviderFactories,
		Steps: []resource.TestStep{{
			Config:      config(acct, intent, "uli_doesnotexist"),
			ExpectError: regexp.MustCompile(`no stripe_payment_intent_amount_details_line_item found\s+for\s+id=uli_doesnotexist`),
		}},
	})
}
