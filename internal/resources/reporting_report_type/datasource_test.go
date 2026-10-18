package reporting_report_type_test

import (
	"os"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"

	"github.com/computomatic/terraform-provider-stripe/internal/acctest"
	"github.com/computomatic/terraform-provider-stripe/internal/acctest/tfacc"
)

func TestMain(m *testing.M) { os.Exit(acctest.Main(m)) }

func TestAccReportingReportType_read(t *testing.T) {
	acct := acctest.NewAccount(t)
	resource.ParallelTest(t, resource.TestCase{
		ProtoV6ProviderFactories: tfacc.ProviderFactories,
		Steps: []resource.TestStep{{
			Config: acct.ProviderBlock() + `
data "stripe_reporting_report_type" "test" {
  id = "balance.summary.1"
}
`,
			Check: resource.ComposeAggregateTestCheckFunc(
				resource.TestCheckResourceAttr("data.stripe_reporting_report_type.test", "id", "balance.summary.1"),
				resource.TestCheckResourceAttrSet("data.stripe_reporting_report_type.test", "name"),
				resource.TestCheckResourceAttrSet("data.stripe_reporting_report_type.test", "default_columns.#"),
			),
		}},
	})
}
