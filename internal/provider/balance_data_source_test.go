package provider

import (
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
)

func TestAccBalanceDataSource(t *testing.T) {
	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: providerConfig + testAccBalanceDataSourceConfig(),
				Check: resource.ComposeAggregateTestCheckFunc(
					// Check that we have at least one available balance
					resource.TestCheckResourceAttr("data.stripe_balance.test", "available.#", "1"),
					// Check the structure of the first available balance
					resource.TestCheckResourceAttrSet("data.stripe_balance.test", "available.0.amount"),
					resource.TestCheckResourceAttrSet("data.stripe_balance.test", "available.0.currency"),
					// Check that we have at least one pending balance
					resource.TestCheckResourceAttr("data.stripe_balance.test", "pending.#", "1"),
					// Check the structure of the first pending balance
					resource.TestCheckResourceAttrSet("data.stripe_balance.test", "pending.0.amount"),
					resource.TestCheckResourceAttrSet("data.stripe_balance.test", "pending.0.currency"),
				),
			},
		},
	})
}

func testAccBalanceDataSourceConfig() string {
	return `
data "stripe_balance" "test" {}
`
}
