package issuing_physical_bundle_test

import (
	"os"
	"regexp"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"

	"github.com/computomatic/terraform-provider-stripe/internal/acctest"
	"github.com/computomatic/terraform-provider-stripe/internal/acctest/tfacc"
)

func TestMain(m *testing.M) { os.Exit(acctest.Main(m)) }

const address = "data.stripe_issuing_physical_bundle.test"

// config reads the bundle with the given ID through a provider configuration that acts on the
// connected account.
func config(acct, conn acctest.Config, id string) string {
	return acct.ProviderBlock() + conn.ConnectedProviderBlock() + `
data "stripe_issuing_physical_bundle" "test" {
  provider = stripe.connected
  id       = "` + id + `"
}
`
}

// Stripe offers the bundles on a connected account with Issuing: active, with a name, a type and
// the features a design on it takes (suite: "list", "retrieve").
func TestAccIssuingPhysicalBundle_read(t *testing.T) {
	acct := acctest.NewAccount(t)
	conn := tfacc.IssuingAccount(t, acct)
	id := tfacc.PhysicalBundle(t, conn)
	feature := regexp.MustCompile(`^(optional|required|unsupported)$`)
	resource.ParallelTest(t, resource.TestCase{
		ProtoV6ProviderFactories: tfacc.ProviderFactories,
		Steps: []resource.TestStep{{
			Config: config(acct, conn, id),
			Check: resource.ComposeAggregateTestCheckFunc(
				resource.TestCheckResourceAttr(address, "id", id),
				resource.TestCheckResourceAttr(address, "status", "active"),
				resource.TestMatchResourceAttr(address, "type", regexp.MustCompile(`^(custom|standard)$`)),
				resource.TestCheckResourceAttrSet(address, "name"),
				resource.TestCheckResourceAttr(address, "livemode", "false"),
				resource.TestMatchResourceAttr(address, "features.card_logo", feature),
				resource.TestMatchResourceAttr(address, "features.carrier_text", feature),
				resource.TestMatchResourceAttr(address, "features.second_line", feature),
			),
		}},
	})
}

// An unknown bundle is a 404 `resource_missing` on `physical_bundle` (suite: "retrieve
// unknown").
func TestAccIssuingPhysicalBundle_notFound(t *testing.T) {
	acct := acctest.NewAccount(t)
	conn := tfacc.IssuingAccount(t, acct)
	resource.ParallelTest(t, resource.TestCase{
		ProtoV6ProviderFactories: tfacc.ProviderFactories,
		Steps: []resource.TestStep{{
			Config:      config(acct, conn, "ics_doesnotexist"),
			ExpectError: regexp.MustCompile("no stripe_issuing_physical_bundle found for id=ics_doesnotexist"),
		}},
	})
}
