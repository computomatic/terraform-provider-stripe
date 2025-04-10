package provider

import (
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
)

func TestAccProductDataSource(t *testing.T) {
	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			// Read testing
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

data "stripe_product" "test" {
  id = stripe_product.test.id
}
`,
				Check: resource.ComposeAggregateTestCheckFunc(
					// Verify product attributes
					resource.TestCheckResourceAttrPair("data.stripe_product.test", "id", "stripe_product.test", "id"),
					resource.TestCheckResourceAttr("data.stripe_product.test", "name", "Test Product"),
					resource.TestCheckResourceAttr("data.stripe_product.test", "description", "Test product for acceptance tests"),
					resource.TestCheckResourceAttr("data.stripe_product.test", "active", "true"),
					resource.TestCheckResourceAttr("data.stripe_product.test", "shippable", "true"),
					resource.TestCheckResourceAttr("data.stripe_product.test", "metadata.test", "value"),
					resource.TestCheckResourceAttrSet("data.stripe_product.test", "created"),
					resource.TestCheckResourceAttrSet("data.stripe_product.test", "updated"),
					// Images should be empty
					resource.TestCheckResourceAttr("data.stripe_product.test", "images.#", "0"),
				),
			},
		},
	})
}
