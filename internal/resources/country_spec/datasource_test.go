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
					// Each currency a US account can be paid out in maps to the countries of the bank
					// accounts that take it.
					resource.TestCheckResourceAttr("data.stripe_country_spec.test", "supported_bank_account_currencies.usd.0", "US"),
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
