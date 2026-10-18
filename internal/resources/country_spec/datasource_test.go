package country_spec_test

import (
	"os"
	"regexp"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"

	"github.com/computomatic/terraform-provider-stripe/internal/acctest"
	"github.com/computomatic/terraform-provider-stripe/internal/acctest/tfacc"
)

func TestMain(m *testing.M) { os.Exit(acctest.Main(m)) }

func TestAccCountrySpec_read(t *testing.T) {
	acct := acctest.NewAccount(t)
	resource.ParallelTest(t, resource.TestCase{
		ProtoV6ProviderFactories: tfacc.ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: acct.ProviderBlock() + `
data "stripe_country_spec" "test" {
  id = "US"
}
`,
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("data.stripe_country_spec.test", "id", "US"),
					resource.TestCheckResourceAttr("data.stripe_country_spec.test", "default_currency", "usd"),
					resource.TestCheckResourceAttrSet("data.stripe_country_spec.test", "supported_payment_currencies.#"),
					resource.TestCheckResourceAttrSet("data.stripe_country_spec.test", "verification_fields.individual.minimum.#"),
				),
			},
			{
				Config: acct.ProviderBlock() + `
data "stripe_country_spec" "test" {
  id = "ZZ"
}
`,
				ExpectError: regexp.MustCompile(`no stripe_country_spec found for id=ZZ`),
			},
		},
	})
}
