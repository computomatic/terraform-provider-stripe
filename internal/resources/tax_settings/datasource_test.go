package tax_settings_test

import (
	"os"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"

	"github.com/computomatic/terraform-provider-stripe/internal/acctest"
	"github.com/computomatic/terraform-provider-stripe/internal/acctest/tfacc"
	"github.com/computomatic/terraform-provider-stripe/internal/wire"
)

func TestMain(m *testing.M) { os.Exit(acctest.Main(m)) }

func TestAccTaxSettings_read(t *testing.T) {
	acct := acctest.NewAccount(t)
	tfacc.Post(t, acct, "/v1/tax/settings", wire.Object{
		{Name: "defaults", Value: wire.Object{{Name: "tax_behavior", Value: wire.String("exclusive")}}},
		{Name: "head_office", Value: wire.Object{{Name: "address", Value: wire.Object{
			{Name: "line1", Value: wire.String("354 Oyster Point Blvd")},
			{Name: "city", Value: wire.String("South San Francisco")},
			{Name: "state", Value: wire.String("CA")},
			{Name: "postal_code", Value: wire.String("94080")},
			{Name: "country", Value: wire.String("US")},
		}}}},
	})
	resource.ParallelTest(t, resource.TestCase{
		ProtoV6ProviderFactories: tfacc.ProviderFactories,
		Steps: []resource.TestStep{{
			Config: acct.ProviderBlock() + `
data "stripe_tax_settings" "test" {}
`,
			Check: resource.ComposeAggregateTestCheckFunc(
				resource.TestCheckResourceAttr("data.stripe_tax_settings.test", "defaults.tax_behavior", "exclusive"),
				resource.TestCheckResourceAttr("data.stripe_tax_settings.test", "head_office.address.country", "US"),
				resource.TestCheckResourceAttr("data.stripe_tax_settings.test", "status", "active"),
			),
		}},
	})
}
