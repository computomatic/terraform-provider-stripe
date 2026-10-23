package checkout_session_line_item_test

import (
	"fmt"
	"os"
	"regexp"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"

	"github.com/computomatic/terraform-provider-stripe/internal/acctest"
	"github.com/computomatic/terraform-provider-stripe/internal/acctest/tfacc"
	"github.com/computomatic/terraform-provider-stripe/internal/stripeapi"
	"github.com/computomatic/terraform-provider-stripe/internal/wire"
)

func TestMain(m *testing.M) { os.Exit(acctest.Main(m)) }

const address = "data.stripe_checkout_session_line_item.test"

func config(c acctest.Config, session, id string) string {
	return c.ProviderBlock() + fmt.Sprintf(`
data "stripe_checkout_session_line_item" "test" {
  checkout_session = %q
  id               = %q
}
`, session, id)
}

// session creates a payment mode session for a product named for base, with two of a 2000
// and one of a 1000 price, and returns it and the cheaper price.
func session(t *testing.T, c acctest.Config, base, currency string) (session, cheaper string) {
	t.Helper()
	product := tfacc.Create(t, c, "/v1/products", wire.Object{{Name: "name", Value: wire.String(c.Name(base))}})
	price := func(amount int64) string {
		return tfacc.Create(t, c, "/v1/prices", wire.Object{
			{Name: "product", Value: wire.String(product)},
			{Name: "currency", Value: wire.String(currency)},
			{Name: "unit_amount", Value: wire.Int(amount)},
		})
	}
	dearer, cheaper := price(2000), price(1000)
	session = tfacc.Create(t, c, "/v1/checkout/sessions", wire.Object{
		{Name: "mode", Value: wire.String("payment")},
		{Name: "success_url", Value: wire.String("https://example.com/success")},
		{Name: "line_items", Value: wire.Array{
			wire.Object{{Name: "price", Value: wire.String(dearer)}, {Name: "quantity", Value: wire.Int(2)}},
			wire.Object{{Name: "price", Value: wire.String(cheaper)}, {Name: "quantity", Value: wire.Int(1)}},
		}},
	})
	return session, cheaper
}

// itemOf returns the ID of the session's line item for price.
func itemOf(t *testing.T, c acctest.Config, session, price string) string {
	t.Helper()
	path := "/v1/checkout/sessions/" + session + "/line_items"
	resp, err := tfacc.Client(c).Do(t.Context(), stripeapi.Request{Method: "GET", Path: path})
	if err != nil {
		t.Fatalf("GET %s: %v", path, err)
	}
	data, _ := resp.Body.(wire.Object).Get("data")
	items, _ := data.(wire.Array)
	for _, item := range items {
		obj, _ := item.(wire.Object)
		p, _ := obj.Get("price")
		if id, _ := p.(wire.Object).GetString("id"); id == price {
			got, _ := obj.GetString("id")
			return got
		}
	}
	t.Fatalf("GET %s lists no item for %s", path, price)
	return ""
}

// A session's line item is an `item` with its quantity, totals and price (suite: "Checkout
// sessions", "line items"). Stripe has no retrieve for an item, so the data source finds it
// by its id among the session's items; the test reads the second of two.
func TestAccCheckoutSessionLineItem_read(t *testing.T) {
	acct := acctest.NewAccount(t)
	currency := tfacc.AccountCurrency(t, acct)
	cs, price := session(t, acct, "tfacc-cs-item-read", currency)
	item := itemOf(t, acct, cs, price)
	resource.ParallelTest(t, resource.TestCase{
		ProtoV6ProviderFactories: tfacc.ProviderFactories,
		Steps: []resource.TestStep{{
			Config: config(acct, cs, item),
			Check: resource.ComposeAggregateTestCheckFunc(
				resource.TestCheckResourceAttr(address, "id", item),
				resource.TestCheckResourceAttr(address, "checkout_session", cs),
				resource.TestCheckResourceAttr(address, "currency", currency),
				resource.TestCheckResourceAttr(address, "quantity", "1"),
				resource.TestCheckResourceAttr(address, "amount_subtotal", "1000"),
				resource.TestCheckResourceAttr(address, "amount_total", "1000"),
				resource.TestCheckResourceAttr(address, "price.id", price),
				resource.TestCheckResourceAttr(address, "price.unit_amount", "1000"),
			),
		}},
	})
}

// An unknown item of a real session is not in its list. A real item under an unknown session
// is Stripe's 404 `resource_missing` for the session (suite: "line items of unknown
// session").
func TestAccCheckoutSessionLineItem_notFound(t *testing.T) {
	acct := acctest.NewAccount(t)
	cs, price := session(t, acct, "tfacc-cs-item-missing", tfacc.AccountCurrency(t, acct))
	item := itemOf(t, acct, cs, price)
	resource.ParallelTest(t, resource.TestCase{
		ProtoV6ProviderFactories: tfacc.ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config:      config(acct, cs, "li_doesnotexist"),
				ExpectError: regexp.MustCompile("no stripe_checkout_session_line_item found for\\s+id=li_doesnotexist"),
			},
			{
				Config:      config(acct, "cs_test_doesnotexist", item),
				ExpectError: regexp.MustCompile(`(?s)Stripe API error: resource_missing.*cs_test_doesnotexist`),
			},
		},
	})
}
