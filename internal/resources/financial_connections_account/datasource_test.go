package financial_connections_account_test

import (
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

const address = "data.stripe_financial_connections_account.test"

func config(c acctest.Config, id string) string {
	return c.ProviderBlock() + `
data "stripe_financial_connections_account" "test" {
  id = "` + id + `"
}
`
}

// Linking an account needs the browser flow a session starts, so nothing in the API makes one
// (suite: "list by account holder"). The test reads the first account the platform itself has
// linked and skips when there is none, as on a new account.
func TestAccFinancialConnectionsAccount_read(t *testing.T) {
	acct := acctest.NewAccount(t)
	resp, err := tfacc.Client(acct).Do(t.Context(), stripeapi.Request{Method: "GET", Path: "/v1/account"})
	if err != nil {
		t.Fatalf("reading the account: %v", err)
	}
	platform, _ := resp.Body.(wire.Object).GetString("id")
	resp, err = tfacc.Client(acct).Do(t.Context(), stripeapi.Request{Method: "GET", Path: "/v1/financial_connections/accounts", Params: wire.Object{
		{Name: "account_holder", Value: wire.Object{{Name: "account", Value: wire.String(platform)}}},
	}})
	if err != nil {
		t.Fatalf("listing linked accounts: %v", err)
	}
	data, _ := resp.Body.(wire.Object).Get("data")
	items, _ := data.(wire.Array)
	if len(items) == 0 {
		t.Skip("the account has linked no Financial Connections accounts, and linking one needs the browser flow")
	}
	id, _ := items[0].(wire.Object).GetString("id")
	resource.ParallelTest(t, resource.TestCase{
		ProtoV6ProviderFactories: tfacc.ProviderFactories,
		Steps: []resource.TestStep{{
			Config: config(acct, id),
			Check: resource.ComposeAggregateTestCheckFunc(
				resource.TestCheckResourceAttr(address, "id", id),
				resource.TestCheckResourceAttrSet(address, "institution_name"),
				resource.TestCheckResourceAttrSet(address, "status"),
				resource.TestCheckResourceAttrSet(address, "category"),
			),
		}},
	})
}

// An unknown account is a 404 `resource_missing` (suite: "retrieve unknown").
func TestAccFinancialConnectionsAccount_notFound(t *testing.T) {
	acct := acctest.NewAccount(t)
	resource.ParallelTest(t, resource.TestCase{
		ProtoV6ProviderFactories: tfacc.ProviderFactories,
		Steps: []resource.TestStep{{
			Config:      config(acct, "fca_doesnotexist"),
			ExpectError: regexp.MustCompile("no stripe_financial_connections_account found for id=fca_doesnotexist"),
		}},
	})
}
