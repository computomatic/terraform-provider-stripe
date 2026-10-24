package checkout_session_test

import (
	"os"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"

	"github.com/computomatic/terraform-provider-stripe/internal/acctest"
	"github.com/computomatic/terraform-provider-stripe/internal/acctest/tfacc"
	"github.com/computomatic/terraform-provider-stripe/internal/wire"
)

func TestMain(m *testing.M) { os.Exit(acctest.Main(m)) }

// A payment mode session with one price is open and unpaid, and carries its line items when
// they are expanded (suite: "Checkout sessions"). Sessions are made by the code that starts a
// checkout, so the test makes one behind the provider's back.
func TestAccCheckoutSession_read(t *testing.T) {
	acct := acctest.NewAccount(t)
	product := tfacc.Create(t, acct, "/v1/products", wire.Object{{Name: "name", Value: wire.String(acct.Name("Course"))}})
	price := tfacc.Create(t, acct, "/v1/prices", wire.Object{
		{Name: "product", Value: wire.String(product)},
		{Name: "currency", Value: wire.String("usd")},
		{Name: "unit_amount", Value: wire.Int(2000)},
	})
	session := tfacc.Create(t, acct, "/v1/checkout/sessions", wire.Object{
		{Name: "mode", Value: wire.String("payment")},
		{Name: "success_url", Value: wire.String("https://example.com/success")},
		{Name: "line_items", Value: wire.Array{wire.Object{
			{Name: "price", Value: wire.String(price)},
			{Name: "quantity", Value: wire.Int(2)},
		}}},
	})
	resource.ParallelTest(t, resource.TestCase{
		ProtoV6ProviderFactories: tfacc.ProviderFactories,
		Steps: []resource.TestStep{{
			Config: acct.ProviderBlock() + `
data "stripe_checkout_session" "test" {
  id = "` + session + `"
}
`,
			Check: resource.ComposeAggregateTestCheckFunc(
				resource.TestCheckResourceAttr("data.stripe_checkout_session.test", "id", session),
				resource.TestCheckResourceAttr("data.stripe_checkout_session.test", "status", "open"),
				resource.TestCheckResourceAttr("data.stripe_checkout_session.test", "payment_status", "unpaid"),
				resource.TestCheckResourceAttr("data.stripe_checkout_session.test", "mode", "payment"),
				resource.TestCheckResourceAttr("data.stripe_checkout_session.test", "amount_total", "4000"),
				resource.TestCheckResourceAttr("data.stripe_checkout_session.test", "line_items.#", "1"),
				resource.TestCheckResourceAttr("data.stripe_checkout_session.test", "line_items.0.quantity", "2"),
			),
		}},
	})
}
