package provider

import (
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
)

func TestAccProductResource(t *testing.T) {
	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			// Create and Read testing
			{
				Config: providerConfig + `
resource "stripe_product" "test" {
  name = "Test Product"
  description = "Test product for acceptance tests"
  active = true
  shippable = true
  metadata = {
    test = "value"
  }
}
`,
				Check: resource.ComposeAggregateTestCheckFunc(
					// Verify product attributes
					resource.TestCheckResourceAttr("stripe_product.test", "name", "Test Product"),
					resource.TestCheckResourceAttr("stripe_product.test", "description", "Test product for acceptance tests"),
					resource.TestCheckResourceAttr("stripe_product.test", "active", "true"),
					resource.TestCheckResourceAttr("stripe_product.test", "shippable", "true"),
					resource.TestCheckResourceAttr("stripe_product.test", "metadata.test", "value"),
					resource.TestCheckResourceAttrSet("stripe_product.test", "id"),
					resource.TestCheckResourceAttrSet("stripe_product.test", "created"),
					resource.TestCheckResourceAttrSet("stripe_product.test", "updated"),
					// Images should be empty
					resource.TestCheckResourceAttr("stripe_product.test", "images.#", "0"),
				),
			},
			// ImportState testing
			{
				ResourceName:      "stripe_product.test",
				ImportState:       true,
				ImportStateVerify: true,
			},
			// Update and Read testing
			{
				Config: providerConfig + `
resource "stripe_product" "test" {
  name = "Updated Test Product"
  description = "Updated test product for acceptance tests"
  active = false
  shippable = false
  metadata = {
    test = "updated"
    new = "value"
  }
  statement_descriptor = "TEST PRODUCT"
  unit_label = "item"
}
`,
				Check: resource.ComposeAggregateTestCheckFunc(
					// Verify updated product attributes
					resource.TestCheckResourceAttr("stripe_product.test", "name", "Updated Test Product"),
					resource.TestCheckResourceAttr("stripe_product.test", "description", "Updated test product for acceptance tests"),
					resource.TestCheckResourceAttr("stripe_product.test", "active", "false"),
					resource.TestCheckResourceAttr("stripe_product.test", "shippable", "false"),
					resource.TestCheckResourceAttr("stripe_product.test", "metadata.test", "updated"),
					resource.TestCheckResourceAttr("stripe_product.test", "metadata.new", "value"),
					resource.TestCheckResourceAttr("stripe_product.test", "statement_descriptor", "TEST PRODUCT"),
					resource.TestCheckResourceAttr("stripe_product.test", "unit_label", "item"),
					resource.TestCheckResourceAttrSet("stripe_product.test", "updated"),
				),
			},
			// Delete testing automatically occurs in TestCase
		},
	})
}
