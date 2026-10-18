package tax_code_test

import (
	"os"
	"regexp"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"

	"github.com/computomatic/terraform-provider-stripe/internal/acctest"
	"github.com/computomatic/terraform-provider-stripe/internal/acctest/tfacc"
)

func TestMain(m *testing.M) { os.Exit(acctest.Main(m)) }

func TestAccTaxCode_read(t *testing.T) {
	acct := acctest.NewAccount(t)
	resource.ParallelTest(t, resource.TestCase{
		ProtoV6ProviderFactories: tfacc.ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: acct.ProviderBlock() + `
data "stripe_tax_code" "test" {
  id = "txcd_10000000"
}
`,
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("data.stripe_tax_code.test", "id", "txcd_10000000"),
					resource.TestCheckResourceAttrSet("data.stripe_tax_code.test", "name"),
					resource.TestCheckResourceAttrSet("data.stripe_tax_code.test", "description"),
				),
			},
			{
				// Stripe answers an unknown tax code with a 400, not a 404.
				Config: acct.ProviderBlock() + `
data "stripe_tax_code" "test" {
  id = "txcd_not_a_tax_code"
}
`,
				ExpectError: regexp.MustCompile(`Invalid tax code`),
			},
		},
	})
}
