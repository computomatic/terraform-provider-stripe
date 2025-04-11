package provider

import (
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
)

func TestAccPaymentLinkResource(t *testing.T) {
	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: providerConfig + `
resource "stripe_product" "test" {
  name = "Test Product"
	active = true
}

resource "stripe_price" "test" {
  product     = stripe_product.test.id
  unit_amount = 1000
  currency    = "usd"
	active      = true
}

resource "stripe_payment_link" "test" {
  line_items = [{
    price    = stripe_price.test.id
    quantity = 1
  }]
}`,
				Check: resource.ComposeTestCheckFunc(
					resource.TestCheckResourceAttrSet("stripe_payment_link.test", "id"),
					resource.TestCheckResourceAttr("stripe_payment_link.test", "line_items.#", "1"),
					resource.TestCheckResourceAttr("stripe_payment_link.test", "line_items.0.price", "stripe_price.test.id"),
					resource.TestCheckResourceAttr("stripe_payment_link.test", "line_items.0.quantity", "1"),
				),
			},
			{
				Config: providerConfig + `
resource "stripe_product" "test" {
  name = "Test Product"
	active = true
}

resource "stripe_price" "test" {
  product     = stripe_product.test.id
  unit_amount = 1000
  currency    = "usd"
	active      = true
}

resource "stripe_payment_link" "test" {
  line_items = [{
    price    = stripe_price.test.id
    quantity = 1
    adjustable_quantity = {
      enabled = true
      minimum = 1
      maximum = 10
    }
  }]
  after_completion = {
    type = "redirect"
    redirect_url = "https://example.com/success"
  }
}`,
				Check: resource.ComposeTestCheckFunc(
					resource.TestCheckResourceAttrSet("stripe_payment_link.test", "id"),
					resource.TestCheckResourceAttr("stripe_payment_link.test", "line_items.#", "1"),
					resource.TestCheckResourceAttr("stripe_payment_link.test", "line_items.0.price", "stripe_price.test.id"),
					resource.TestCheckResourceAttr("stripe_payment_link.test", "line_items.0.quantity", "1"),
					resource.TestCheckResourceAttr("stripe_payment_link.test", "line_items.0.adjustable_quantity.enabled", "true"),
					resource.TestCheckResourceAttr("stripe_payment_link.test", "line_items.0.adjustable_quantity.minimum", "1"),
					resource.TestCheckResourceAttr("stripe_payment_link.test", "line_items.0.adjustable_quantity.maximum", "10"),
					resource.TestCheckResourceAttr("stripe_payment_link.test", "after_completion.type", "redirect"),
					resource.TestCheckResourceAttr("stripe_payment_link.test", "after_completion.redirect_url", "https://example.com/success"),
				),
			},
			{
				ResourceName:      "stripe_payment_link.test",
				ImportState:       true,
				ImportStateVerify: true,
			},
		},
	})
}

func TestAccPaymentLinkResource_WithAdjustableQuantity(t *testing.T) {
	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			// Create and Read testing
			{
				Config: providerConfig + `
resource "stripe_product" "test" {
  name = "Test Product for Payment Link with Adjustable Quantity"
  description = "Test product for payment link with adjustable quantity acceptance tests"
  active = true
}

resource "stripe_price" "test" {
  product = stripe_product.test.id
  currency = "usd"
  unit_amount = 1000
  active = true
}

resource "stripe_payment_link" "test" {
  line_items = [{
    price = stripe_price.test.id
    quantity = 1
    adjustable_quantity = {
      enabled = true
      minimum = 1
      maximum = 10
    }
  }]
  active = true
  currency = "usd"
}
`,
				Check: resource.ComposeAggregateTestCheckFunc(
					// Verify payment link attributes
					resource.TestCheckResourceAttr("stripe_payment_link.test", "active", "true"),
					resource.TestCheckResourceAttr("stripe_payment_link.test", "currency", "usd"),
					resource.TestCheckResourceAttr("stripe_payment_link.test", "line_items.#", "1"),
					resource.TestCheckResourceAttr("stripe_payment_link.test", "line_items.0.quantity", "1"),
					resource.TestCheckResourceAttr("stripe_payment_link.test", "line_items.0.adjustable_quantity.enabled", "true"),
					resource.TestCheckResourceAttr("stripe_payment_link.test", "line_items.0.adjustable_quantity.minimum", "1"),
					resource.TestCheckResourceAttr("stripe_payment_link.test", "line_items.0.adjustable_quantity.maximum", "10"),
					resource.TestCheckResourceAttrPair("stripe_payment_link.test", "line_items.0.price", "stripe_price.test", "id"),
					resource.TestCheckResourceAttrSet("stripe_payment_link.test", "id"),
					resource.TestCheckResourceAttrSet("stripe_payment_link.test", "url"),
				),
			},
			// ImportState testing
			{
				ResourceName:      "stripe_payment_link.test",
				ImportState:       true,
				ImportStateVerify: true,
			},
			// Update and Read testing
			{
				Config: providerConfig + `
resource "stripe_product" "test" {
  name = "Test Product for Payment Link with Adjustable Quantity"
  description = "Test product for payment link with adjustable quantity acceptance tests"
  active = true
}

resource "stripe_price" "test" {
  product = stripe_product.test.id
  currency = "usd"
  unit_amount = 1000
  active = true
}

resource "stripe_payment_link" "test" {
  line_items = [{
    price = stripe_price.test.id
    quantity = 1
    adjustable_quantity = {
      enabled = true
      minimum = 5
      maximum = 20
    }
  }]
  active = false
  currency = "usd"
}
`,
				Check: resource.ComposeAggregateTestCheckFunc(
					// Verify updated payment link attributes
					resource.TestCheckResourceAttr("stripe_payment_link.test", "active", "false"),
					resource.TestCheckResourceAttr("stripe_payment_link.test", "currency", "usd"),
					resource.TestCheckResourceAttr("stripe_payment_link.test", "line_items.#", "1"),
					resource.TestCheckResourceAttr("stripe_payment_link.test", "line_items.0.quantity", "1"),
					resource.TestCheckResourceAttr("stripe_payment_link.test", "line_items.0.adjustable_quantity.enabled", "true"),
					resource.TestCheckResourceAttr("stripe_payment_link.test", "line_items.0.adjustable_quantity.minimum", "5"),
					resource.TestCheckResourceAttr("stripe_payment_link.test", "line_items.0.adjustable_quantity.maximum", "20"),
					resource.TestCheckResourceAttrPair("stripe_payment_link.test", "line_items.0.price", "stripe_price.test", "id"),
					resource.TestCheckResourceAttrSet("stripe_payment_link.test", "id"),
					resource.TestCheckResourceAttrSet("stripe_payment_link.test", "url"),
				),
			},
			// Delete testing automatically occurs in TestCase
		},
	})
}

func TestAccPaymentLinkResource_WithAfterCompletion(t *testing.T) {
	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			// Create and Read testing
			{
				Config: providerConfig + `
resource "stripe_product" "test" {
  name = "Test Product for Payment Link with After Completion"
  description = "Test product for payment link with after completion acceptance tests"
  active = true
}

resource "stripe_price" "test" {
  product = stripe_product.test.id
  currency = "usd"
  unit_amount = 1000
  active = true
}

resource "stripe_payment_link" "test" {
  line_items = [{
    price = stripe_price.test.id
    quantity = 1
  }]
  active = true
  currency = "usd"
  after_completion = {
    type = "redirect"
    redirect_url = "https://example.com/success"
  }
}
`,
				Check: resource.ComposeAggregateTestCheckFunc(
					// Verify payment link attributes
					resource.TestCheckResourceAttr("stripe_payment_link.test", "active", "true"),
					resource.TestCheckResourceAttr("stripe_payment_link.test", "currency", "usd"),
					resource.TestCheckResourceAttr("stripe_payment_link.test", "line_items.#", "1"),
					resource.TestCheckResourceAttr("stripe_payment_link.test", "line_items.0.quantity", "1"),
					resource.TestCheckResourceAttr("stripe_payment_link.test", "after_completion.type", "redirect"),
					resource.TestCheckResourceAttr("stripe_payment_link.test", "after_completion.redirect_url", "https://example.com/success"),
					resource.TestCheckResourceAttrPair("stripe_payment_link.test", "line_items.0.price", "stripe_price.test", "id"),
					resource.TestCheckResourceAttrSet("stripe_payment_link.test", "id"),
					resource.TestCheckResourceAttrSet("stripe_payment_link.test", "url"),
				),
			},
			// ImportState testing
			{
				ResourceName:      "stripe_payment_link.test",
				ImportState:       true,
				ImportStateVerify: true,
			},
			// Update and Read testing
			{
				Config: providerConfig + `
resource "stripe_product" "test" {
  name = "Test Product for Payment Link with After Completion"
  description = "Test product for payment link with after completion acceptance tests"
  active = true
}

resource "stripe_price" "test" {
  product = stripe_product.test.id
  currency = "usd"
  unit_amount = 1000
  active = true
}

resource "stripe_payment_link" "test" {
  line_items = [{
    price = stripe_price.test.id
    quantity = 1
  }]
  active = false
  currency = "usd"
  after_completion = {
    type = "hosted_confirmation"
    hosted_confirmation_custom_message = "Thank you for your purchase!"
  }
}
`,
				Check: resource.ComposeAggregateTestCheckFunc(
					// Verify updated payment link attributes
					resource.TestCheckResourceAttr("stripe_payment_link.test", "active", "false"),
					resource.TestCheckResourceAttr("stripe_payment_link.test", "currency", "usd"),
					resource.TestCheckResourceAttr("stripe_payment_link.test", "line_items.#", "1"),
					resource.TestCheckResourceAttr("stripe_payment_link.test", "line_items.0.quantity", "1"),
					resource.TestCheckResourceAttr("stripe_payment_link.test", "after_completion.type", "hosted_confirmation"),
					resource.TestCheckResourceAttr("stripe_payment_link.test", "after_completion.hosted_confirmation_custom_message", "Thank you for your purchase!"),
					resource.TestCheckResourceAttrPair("stripe_payment_link.test", "line_items.0.price", "stripe_price.test", "id"),
					resource.TestCheckResourceAttrSet("stripe_payment_link.test", "id"),
					resource.TestCheckResourceAttrSet("stripe_payment_link.test", "url"),
				),
			},
			// Delete testing automatically occurs in TestCase
		},
	})
}

func TestAccPaymentLinkResource_WithAutomaticTax(t *testing.T) {
	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			// Create and Read testing
			{
				Config: providerConfig + `
resource "stripe_product" "test" {
  name = "Test Product for Payment Link with Automatic Tax"
  description = "Test product for payment link with automatic tax acceptance tests"
  active = true
}

resource "stripe_price" "test" {
  product = stripe_product.test.id
  currency = "usd"
  unit_amount = 1000
  active = true
}

resource "stripe_payment_link" "test" {
  line_items = [{
    price = stripe_price.test.id
    quantity = 1
  }]
  active = true
  currency = "usd"
  automatic_tax = {
    enabled = true
  }
}
`,
				Check: resource.ComposeAggregateTestCheckFunc(
					// Verify payment link attributes
					resource.TestCheckResourceAttr("stripe_payment_link.test", "active", "true"),
					resource.TestCheckResourceAttr("stripe_payment_link.test", "currency", "usd"),
					resource.TestCheckResourceAttr("stripe_payment_link.test", "line_items.#", "1"),
					resource.TestCheckResourceAttr("stripe_payment_link.test", "line_items.0.quantity", "1"),
					resource.TestCheckResourceAttr("stripe_payment_link.test", "automatic_tax.enabled", "true"),
					resource.TestCheckResourceAttrPair("stripe_payment_link.test", "line_items.0.price", "stripe_price.test", "id"),
					resource.TestCheckResourceAttrSet("stripe_payment_link.test", "id"),
					resource.TestCheckResourceAttrSet("stripe_payment_link.test", "url"),
				),
			},
			// ImportState testing
			{
				ResourceName:      "stripe_payment_link.test",
				ImportState:       true,
				ImportStateVerify: true,
			},
			// Update and Read testing
			{
				Config: providerConfig + `
resource "stripe_product" "test" {
  name = "Test Product for Payment Link with Automatic Tax"
  description = "Test product for payment link with automatic tax acceptance tests"
  active = true
}

resource "stripe_price" "test" {
  product = stripe_product.test.id
  currency = "usd"
  unit_amount = 1000
  active = true
}

resource "stripe_payment_link" "test" {
  line_items = [{
    price = stripe_price.test.id
    quantity = 1
  }]
  active = false
  currency = "usd"
  automatic_tax = {
    enabled = false
  }
}
`,
				Check: resource.ComposeAggregateTestCheckFunc(
					// Verify updated payment link attributes
					resource.TestCheckResourceAttr("stripe_payment_link.test", "active", "false"),
					resource.TestCheckResourceAttr("stripe_payment_link.test", "currency", "usd"),
					resource.TestCheckResourceAttr("stripe_payment_link.test", "line_items.#", "1"),
					resource.TestCheckResourceAttr("stripe_payment_link.test", "line_items.0.quantity", "1"),
					resource.TestCheckResourceAttr("stripe_payment_link.test", "automatic_tax.enabled", "false"),
					resource.TestCheckResourceAttrPair("stripe_payment_link.test", "line_items.0.price", "stripe_price.test", "id"),
					resource.TestCheckResourceAttrSet("stripe_payment_link.test", "id"),
					resource.TestCheckResourceAttrSet("stripe_payment_link.test", "url"),
				),
			},
			// Delete testing automatically occurs in TestCase
		},
	})
}
