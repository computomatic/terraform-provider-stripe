package provider

import (
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
)

func TestAccPriceDataSource(t *testing.T) {
	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			// Read testing
			{
				Config: providerConfig + testAccPriceDataSourceConfig(),
				Check: resource.ComposeAggregateTestCheckFunc(
					// Check that we have a price ID
					resource.TestCheckResourceAttrSet("data.stripe_price.test", "id"),
					// Check basic price attributes
					resource.TestCheckResourceAttrSet("data.stripe_price.test", "active"),
					resource.TestCheckResourceAttrSet("data.stripe_price.test", "currency"),
					resource.TestCheckResourceAttrSet("data.stripe_price.test", "type"),
					resource.TestCheckResourceAttrSet("data.stripe_price.test", "unit_amount"),
					resource.TestCheckResourceAttrSet("data.stripe_price.test", "unit_amount_decimal"),
					// Check product reference
					resource.TestCheckResourceAttrSet("data.stripe_price.test", "product"),
					// Check recurring attributes not set
					resource.TestCheckResourceAttr("data.stripe_price.test", "recurring.interval", ""),
					resource.TestCheckResourceAttr("data.stripe_price.test", "recurring.interval_count", "0"),
					resource.TestCheckResourceAttr("data.stripe_price.test", "recurring.usage_type", ""),
				),
			},
		},
	})
}

func testAccPriceDataSourceConfig() string {
	return `
resource "stripe_product" "test" {
  name = "Test Product for Price Data Source"
  description = "Test product for price data source acceptance tests"
  active = true
}

resource "stripe_price" "test_price" {
  product = stripe_product.test.id
  currency = "usd"
  unit_amount = 1000
  active = true
  metadata = {}
}

data "stripe_price" "test" {
  id = stripe_price.test_price.id
}
`
}
