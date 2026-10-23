package payment_link_line_item_test

import (
	"fmt"
	"os"
	"regexp"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"

	"github.com/computomatic/terraform-provider-stripe/internal/acctest"
	"github.com/computomatic/terraform-provider-stripe/internal/acctest/tfacc"
)

func TestMain(m *testing.M) { os.Exit(acctest.Main(m)) }

const address = "data.stripe_payment_link_line_item.test"

// link is a product named for base with a 2000 and a 1000 price, and a payment link selling
// two of the first and quantity of the second. data lands after it.
func link(c acctest.Config, base, currency string, quantity int, data string) string {
	return c.ProviderBlock() + fmt.Sprintf(`
resource "stripe_product" "test" {
  name = %q
}

resource "stripe_price" "a" {
  product     = stripe_product.test.id
  currency    = %q
  unit_amount = 2000
}

resource "stripe_price" "b" {
  product     = stripe_product.test.id
  currency    = %[2]q
  unit_amount = 1000
}

resource "stripe_payment_link" "test" {
  line_items = [
    { price = stripe_price.a.id, quantity = 2 },
    { price = stripe_price.b.id, quantity = %[3]d, adjustable_quantity = { enabled = true, minimum = 1, maximum = 10 } },
  ]
}

locals {
  item_b = one([for i in stripe_payment_link.test.line_items : i.id if i.price == stripe_price.b.id])
}
%[4]s`, c.Name(base), currency, quantity, data)
}

func dataBlock(link, id string) string {
	return fmt.Sprintf(`
data "stripe_payment_link_line_item" "test" {
  payment_link = %s
  id           = %s
}
`, link, id)
}

// A link's line item is an `item` with its quantity, totals, price and `adjustable_quantity`
// (suite: "Payment links", "update line item quantity" and "adjustable quantity"). Stripe has
// no retrieve for an item, so the data source finds it by its id among the link's items; the
// test reads the second of two, and reads it again after its quantity changes on the link.
func TestAccPaymentLinkLineItem_read(t *testing.T) {
	acct := acctest.NewAccount(t)
	currency := tfacc.AccountCurrency(t, acct)
	read := dataBlock("stripe_payment_link.test.id", "local.item_b")
	resource.ParallelTest(t, resource.TestCase{
		ProtoV6ProviderFactories: tfacc.ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: link(acct, "tfacc-plink-item-read", currency, 1, read),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttrPair(address, "payment_link", "stripe_payment_link.test", "id"),
					resource.TestCheckResourceAttrPair(address, "price.id", "stripe_price.b", "id"),
					resource.TestCheckResourceAttr(address, "currency", currency),
					resource.TestCheckResourceAttr(address, "quantity", "1"),
					resource.TestCheckResourceAttr(address, "amount_subtotal", "1000"),
					resource.TestCheckResourceAttr(address, "amount_total", "1000"),
					resource.TestCheckResourceAttr(address, "adjustable_quantity.enabled", "true"),
					resource.TestCheckResourceAttr(address, "adjustable_quantity.minimum", "1"),
					resource.TestCheckResourceAttr(address, "adjustable_quantity.maximum", "10"),
					resource.TestMatchResourceAttr(address, "id", regexp.MustCompile(`^li_`)),
				),
			},
			{
				Config: link(acct, "tfacc-plink-item-read", currency, 3, read),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr(address, "quantity", "3"),
					resource.TestCheckResourceAttr(address, "amount_total", "3000"),
					resource.TestCheckResourceAttr(address, "adjustable_quantity.maximum", "10"),
				),
			},
		},
	})
}

// An unknown item of a real link is not in its list. A real item under an unknown link is
// Stripe's 404 `resource_missing` for the link (suite: "Payment links", "unknown link line
// items").
func TestAccPaymentLinkLineItem_notFound(t *testing.T) {
	acct := acctest.NewAccount(t)
	currency := tfacc.AccountCurrency(t, acct)
	resource.ParallelTest(t, resource.TestCase{
		ProtoV6ProviderFactories: tfacc.ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: link(acct, "tfacc-plink-item-missing", currency, 1, ""),
			},
			{
				Config:      link(acct, "tfacc-plink-item-missing", currency, 1, dataBlock("stripe_payment_link.test.id", `"li_doesnotexist"`)),
				ExpectError: regexp.MustCompile("no stripe_payment_link_line_item found for\\s+id=li_doesnotexist"),
			},
			{
				Config:      link(acct, "tfacc-plink-item-missing", currency, 1, dataBlock(`"plink_doesnotexist"`, "local.item_b")),
				ExpectError: regexp.MustCompile(`(?s)Stripe API error: resource_missing.*plink_doesnotexist`),
			},
		},
	})
}
