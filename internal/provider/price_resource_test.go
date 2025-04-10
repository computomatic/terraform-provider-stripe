package provider

import (
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
)

func TestAccPriceResource(t *testing.T) {
	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			// Create and Read testing
			{
				Config: providerConfig + `
resource "stripe_product" "test" {
  name = "Test Product for Price"
  description = "Test product for price acceptance tests"
  active = true
  shippable = false
}

resource "stripe_price" "test" {
  product = stripe_product.test.id
  currency = "usd"
  unit_amount = 1000
  active = true
  metadata = {
    test = "value"
  }
  nickname = "Test Price"
}
`,
				Check: resource.ComposeAggregateTestCheckFunc(
					// Verify price attributes
					resource.TestCheckResourceAttr("stripe_price.test", "currency", "usd"),
					resource.TestCheckResourceAttr("stripe_price.test", "unit_amount", "1000"),
					resource.TestCheckResourceAttr("stripe_price.test", "active", "true"),
					resource.TestCheckResourceAttr("stripe_price.test", "metadata.test", "value"),
					resource.TestCheckResourceAttr("stripe_price.test", "nickname", "Test Price"),
					resource.TestCheckResourceAttr("stripe_price.test", "type", "one_time"),
					resource.TestCheckResourceAttrSet("stripe_price.test", "id"),
					resource.TestCheckResourceAttrSet("stripe_price.test", "created"),
					resource.TestCheckResourceAttrSet("stripe_price.test", "unit_amount_decimal"),
					resource.TestCheckResourceAttrPair("stripe_price.test", "product", "stripe_product.test", "id"),
				),
			},
			// ImportState testing
			{
				ResourceName:      "stripe_price.test",
				ImportState:       true,
				ImportStateVerify: true,
			},
			// Update and Read testing
			{
				Config: providerConfig + `
resource "stripe_product" "test" {
  name = "Test Product for Price"
  description = "Test product for price acceptance tests"
  active = true
  shippable = false
}

resource "stripe_price" "test" {
  product = stripe_product.test.id
  currency = "usd"
  unit_amount = 2000
  active = false
  metadata = {
    test = "updated"
    new = "value"
  }
  nickname = "Updated Test Price"
}
`,
				Check: resource.ComposeAggregateTestCheckFunc(
					// Verify updated price attributes
					resource.TestCheckResourceAttr("stripe_price.test", "currency", "usd"),
					resource.TestCheckResourceAttr("stripe_price.test", "unit_amount", "2000"),
					resource.TestCheckResourceAttr("stripe_price.test", "active", "false"),
					resource.TestCheckResourceAttr("stripe_price.test", "metadata.test", "updated"),
					resource.TestCheckResourceAttr("stripe_price.test", "metadata.new", "value"),
					resource.TestCheckResourceAttr("stripe_price.test", "nickname", "Updated Test Price"),
					resource.TestCheckResourceAttr("stripe_price.test", "type", "one_time"),
					resource.TestCheckResourceAttrSet("stripe_price.test", "id"),
					resource.TestCheckResourceAttrSet("stripe_price.test", "created"),
					resource.TestCheckResourceAttrSet("stripe_price.test", "unit_amount_decimal"),
					resource.TestCheckResourceAttrPair("stripe_price.test", "product", "stripe_product.test", "id"),
				),
			},
			// Delete testing automatically occurs in TestCase
		},
	})
}

func TestAccPriceResource_Recurring(t *testing.T) {
	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			// Create and Read testing
			{
				Config: providerConfig + `
resource "stripe_product" "test" {
  name = "Test Product for Recurring Price"
  description = "Test product for recurring price acceptance tests"
  active = true
  shippable = false
}

resource "stripe_price" "test" {
  product = stripe_product.test.id
  currency = "usd"
  unit_amount = 1000
  active = true
  recurring = {
    interval = "month"
    interval_count = 1
    usage_type = "licensed"
  }
  metadata = {
    test = "value"
  }
  nickname = "Test Recurring Price"
}
`,
				Check: resource.ComposeAggregateTestCheckFunc(
					// Verify price attributes
					resource.TestCheckResourceAttr("stripe_price.test", "currency", "usd"),
					resource.TestCheckResourceAttr("stripe_price.test", "unit_amount", "1000"),
					resource.TestCheckResourceAttr("stripe_price.test", "active", "true"),
					resource.TestCheckResourceAttr("stripe_price.test", "metadata.test", "value"),
					resource.TestCheckResourceAttr("stripe_price.test", "nickname", "Test Recurring Price"),
					resource.TestCheckResourceAttr("stripe_price.test", "type", "recurring"),
					resource.TestCheckResourceAttr("stripe_price.test", "recurring.interval", "month"),
					resource.TestCheckResourceAttr("stripe_price.test", "recurring.interval_count", "1"),
					resource.TestCheckResourceAttr("stripe_price.test", "recurring.usage_type", "licensed"),
					resource.TestCheckResourceAttrSet("stripe_price.test", "id"),
					resource.TestCheckResourceAttrSet("stripe_price.test", "created"),
					resource.TestCheckResourceAttrSet("stripe_price.test", "unit_amount_decimal"),
					resource.TestCheckResourceAttrPair("stripe_price.test", "product", "stripe_product.test", "id"),
				),
			},
			// ImportState testing
			{
				ResourceName:      "stripe_price.test",
				ImportState:       true,
				ImportStateVerify: true,
			},
			// Update and Read testing
			{
				Config: providerConfig + `
resource "stripe_product" "test" {
  name = "Test Product for Recurring Price"
  description = "Test product for recurring price acceptance tests"
  active = true
  shippable = false
}

resource "stripe_price" "test" {
  product = stripe_product.test.id
  currency = "usd"
  unit_amount = 2000
  active = false
  recurring = {
    interval = "year"
    interval_count = 1
    usage_type = "metered"
  }
  metadata = {
    test = "updated"
    new = "value"
  }
  nickname = "Updated Test Recurring Price"
}
`,
				Check: resource.ComposeAggregateTestCheckFunc(
					// Verify updated price attributes
					resource.TestCheckResourceAttr("stripe_price.test", "currency", "usd"),
					resource.TestCheckResourceAttr("stripe_price.test", "unit_amount", "2000"),
					resource.TestCheckResourceAttr("stripe_price.test", "active", "false"),
					resource.TestCheckResourceAttr("stripe_price.test", "metadata.test", "updated"),
					resource.TestCheckResourceAttr("stripe_price.test", "metadata.new", "value"),
					resource.TestCheckResourceAttr("stripe_price.test", "nickname", "Updated Test Recurring Price"),
					resource.TestCheckResourceAttr("stripe_price.test", "type", "recurring"),
					resource.TestCheckResourceAttr("stripe_price.test", "recurring.interval", "year"),
					resource.TestCheckResourceAttr("stripe_price.test", "recurring.interval_count", "1"),
					resource.TestCheckResourceAttr("stripe_price.test", "recurring.usage_type", "metered"),
					resource.TestCheckResourceAttrSet("stripe_price.test", "id"),
					resource.TestCheckResourceAttrSet("stripe_price.test", "created"),
					resource.TestCheckResourceAttrSet("stripe_price.test", "unit_amount_decimal"),
					resource.TestCheckResourceAttrPair("stripe_price.test", "product", "stripe_product.test", "id"),
				),
			},
			// Delete testing automatically occurs in TestCase
		},
	})
}
