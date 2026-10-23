package apps_install_test

import (
	"os"
	"regexp"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"

	"github.com/computomatic/terraform-provider-stripe/internal/acctest"
	"github.com/computomatic/terraform-provider-stripe/internal/acctest/tfacc"
)

func TestMain(m *testing.M) { os.Exit(acctest.Main(m)) }

// No suite test can install an app (suite: "App installs": the sandbox has none, and an
// unknown app is a 404), so nothing proves an install's fields and the fake serves none. The
// test reads an install Stripe does not know, which is a 404 `resource_missing` (suite:
// "retrieve missing"), and so not found.
func TestAccAppsInstall_read(t *testing.T) {
	acct := acctest.NewAccount(t)
	tfacc.SkipUnlessFakeAnswers(t, acct, "/v1/apps/installs/appinst_1DoesNotExist0000")
	resource.ParallelTest(t, resource.TestCase{
		ProtoV6ProviderFactories: tfacc.ProviderFactories,
		Steps: []resource.TestStep{{
			Config: acct.ProviderBlock() + `
data "stripe_apps_install" "test" {
  id = "appinst_1DoesNotExist0000"
}
`,
			ExpectError: regexp.MustCompile("no stripe_apps_install found for id=appinst_1DoesNotExist0000"),
		}},
	})
}
