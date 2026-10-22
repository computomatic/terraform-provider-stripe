package quote_line_item_test

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

// A draft quote with a one-time and a monthly price has an item for each (suite: "Credit note
// lines and quote line items"). Stripe has no retrieve for an item, so the data source finds
// it by its id among the quote's items.
func TestAccQuoteLineItem_read(t *testing.T) {
	acct := acctest.NewAccount(t)
	currency := tfacc.AccountCurrency(t, acct)
	product := tfacc.Create(t, acct, "/v1/products", wire.Object{{Name: "name", Value: wire.String(acct.Name("Quoted"))}})
	oneTime := tfacc.Create(t, acct, "/v1/prices", wire.Object{
		{Name: "product", Value: wire.String(product)},
		{Name: "currency", Value: wire.String(currency)},
		{Name: "unit_amount", Value: wire.Int(2500)},
	})
	monthly := tfacc.Create(t, acct, "/v1/prices", wire.Object{
		{Name: "product", Value: wire.String(product)},
		{Name: "currency", Value: wire.String(currency)},
		{Name: "unit_amount", Value: wire.Int(1000)},
		{Name: "recurring", Value: wire.Object{{Name: "interval", Value: wire.String("month")}}},
	})
	quote := tfacc.Create(t, acct, "/v1/quotes", wire.Object{
		{Name: "customer", Value: wire.String(tfacc.Create(t, acct, "/v1/customers", nil))},
		{Name: "line_items", Value: wire.Array{
			wire.Object{{Name: "price", Value: wire.String(oneTime)}, {Name: "quantity", Value: wire.Int(2)}},
			wire.Object{{Name: "price", Value: wire.String(monthly)}, {Name: "quantity", Value: wire.Int(1)}},
		}},
	})
	item := tfacc.FirstListed(t, acct, "/v1/quotes/"+quote+"/line_items")
	resource.ParallelTest(t, resource.TestCase{
		ProtoV6ProviderFactories: tfacc.ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: acct.ProviderBlock() + `
data "stripe_quote_line_item" "test" {
  quote = "` + quote + `"
  id    = "` + item + `"
}
`,
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("data.stripe_quote_line_item.test", "id", item),
					resource.TestCheckResourceAttr("data.stripe_quote_line_item.test", "quote", quote),
					resource.TestCheckResourceAttr("data.stripe_quote_line_item.test", "currency", currency),
					resource.TestCheckResourceAttrSet("data.stripe_quote_line_item.test", "amount_total"),
					resource.TestCheckResourceAttrSet("data.stripe_quote_line_item.test", "quantity"),
					resource.TestCheckResourceAttrSet("data.stripe_quote_line_item.test", "price.id"),
				),
			},
			{
				Config: acct.ProviderBlock() + `
data "stripe_quote_line_item" "test" {
  quote = "` + quote + `"
  id    = "li_doesnotexist"
}
`,
				ExpectError: regexp.MustCompile("no stripe_quote_line_item found for id=li_doesnotexist"),
			},
		},
	})
}
