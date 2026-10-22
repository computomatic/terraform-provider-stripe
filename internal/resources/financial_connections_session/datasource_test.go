package financial_connections_session_test

import (
	"os"
	"regexp"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"

	"github.com/computomatic/terraform-provider-stripe/internal/acctest"
	"github.com/computomatic/terraform-provider-stripe/internal/acctest/tfacc"
	"github.com/computomatic/terraform-provider-stripe/internal/wire"
)

func TestMain(m *testing.M) { os.Exit(acctest.Main(m)) }

const address = "data.stripe_financial_connections_session.test"

func config(c acctest.Config, id string) string {
	return c.ProviderBlock() + `
data "stripe_financial_connections_session" "test" {
  id = "` + id + `"
}
`
}

// A session for a customer that collects balances, as the suite creates one (suite: "create and
// retrieve"). A session cannot be deleted; an unused one expires.
func TestAccFinancialConnectionsSession_read(t *testing.T) {
	acct := acctest.NewAccount(t)
	customer := tfacc.Create(t, acct, "/v1/customers", wire.Object{{Name: "name", Value: wire.String(acct.Name("Financial Connections"))}})
	id := tfacc.Create(t, acct, "/v1/financial_connections/sessions", wire.Object{
		{Name: "account_holder", Value: wire.Object{
			{Name: "type", Value: wire.String("customer")},
			{Name: "customer", Value: wire.String(customer)},
		}},
		{Name: "permissions", Value: wire.Array{wire.String("balances")}},
	})
	resource.ParallelTest(t, resource.TestCase{
		ProtoV6ProviderFactories: tfacc.ProviderFactories,
		Steps: []resource.TestStep{{
			Config: config(acct, id),
			Check: resource.ComposeAggregateTestCheckFunc(
				resource.TestCheckResourceAttr(address, "id", id),
				resource.TestCheckResourceAttr(address, "account_holder.type", "customer"),
				resource.TestCheckResourceAttr(address, "account_holder.customer", customer),
				resource.TestCheckResourceAttr(address, "permissions.#", "1"),
				resource.TestCheckResourceAttr(address, "permissions.0", "balances"),
				resource.TestCheckResourceAttrSet(address, "client_secret"),
				resource.TestCheckResourceAttr(address, "livemode", "false"),
			),
		}},
	})
}

// An unknown session is a 404 `resource_missing` (suite: "retrieve unknown").
func TestAccFinancialConnectionsSession_notFound(t *testing.T) {
	acct := acctest.NewAccount(t)
	resource.ParallelTest(t, resource.TestCase{
		ProtoV6ProviderFactories: tfacc.ProviderFactories,
		Steps: []resource.TestStep{{
			Config:      config(acct, "fcsess_doesnotexist"),
			ExpectError: regexp.MustCompile("no stripe_financial_connections_session found for id=fcsess_doesnotexist"),
		}},
	})
}
