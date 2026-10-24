package apps_secret_test

import (
	"context"
	"os"
	"regexp"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"

	"github.com/computomatic/terraform-provider-stripe/internal/acctest"
	"github.com/computomatic/terraform-provider-stripe/internal/acctest/tfacc"
	"github.com/computomatic/terraform-provider-stripe/internal/stripeapi"
	"github.com/computomatic/terraform-provider-stripe/internal/wire"
)

func TestMain(m *testing.M) { os.Exit(acctest.Main(m)) }

const address = "data.stripe_apps_secret.test"

var accountScope = wire.Object{{Name: "type", Value: wire.String("account")}}

func config(c acctest.Config, name string) string {
	return c.ProviderBlock() + `
data "stripe_apps_secret" "test" {
  name = "` + name + `"
  scope = {
    type = "account"
  }
}
`
}

// A secret set with a secret key in the account scope is found by its name, and its payload
// is returned when expanded (suite: "The Secret Store"). The secret is deleted when the test
// ends.
func TestAccAppsSecret_read(t *testing.T) {
	acct := acctest.NewAccount(t)
	name := acct.Unique("tfacc_secret_read")
	id := tfacc.Create(t, acct, "/v1/apps/secrets", wire.Object{
		{Name: "name", Value: wire.String(name)},
		{Name: "payload", Value: wire.String("tfacc-payload")},
		{Name: "scope", Value: accountScope},
	})
	t.Cleanup(func() {
		// The test's context is done by the time cleanups run.
		_, _ = tfacc.Client(acct).Do(context.Background(), stripeapi.Request{Method: "POST", Path: "/v1/apps/secrets/delete", Params: wire.Object{
			{Name: "name", Value: wire.String(name)},
			{Name: "scope", Value: accountScope},
		}})
	})
	resource.ParallelTest(t, resource.TestCase{
		ProtoV6ProviderFactories: tfacc.ProviderFactories,
		Steps: []resource.TestStep{{
			Config: config(acct, name),
			Check: resource.ComposeAggregateTestCheckFunc(
				resource.TestCheckResourceAttr(address, "id", id),
				resource.TestCheckResourceAttr(address, "name", name),
				resource.TestCheckResourceAttr(address, "scope.type", "account"),
				resource.TestCheckResourceAttr(address, "payload", "tfacc-payload"),
				resource.TestCheckResourceAttrSet(address, "created"),
				resource.TestCheckResourceAttr(address, "livemode", "false"),
			),
		}},
	})
}

// An unknown name is a 404 `resource_missing` with no param (suite: "find unknown").
func TestAccAppsSecret_notFound(t *testing.T) {
	acct := acctest.NewAccount(t)
	name := acct.Unique("tfacc_secret_missing")
	resource.ParallelTest(t, resource.TestCase{
		ProtoV6ProviderFactories: tfacc.ProviderFactories,
		Steps: []resource.TestStep{{
			Config:      config(acct, name),
			ExpectError: regexp.MustCompile(`no stripe_apps_secret found for\s+name=` + name + `,\s+scope.type=account`),
		}},
	})
}
