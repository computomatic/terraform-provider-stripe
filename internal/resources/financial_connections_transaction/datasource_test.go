package financial_connections_transaction_test

import (
	"os"
	"regexp"
	"strconv"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"

	"github.com/computomatic/terraform-provider-stripe/internal/acctest"
	"github.com/computomatic/terraform-provider-stripe/internal/acctest/tfacc"
	"github.com/computomatic/terraform-provider-stripe/internal/wire"
)

func TestMain(m *testing.M) { os.Exit(acctest.Main(m)) }

const address = "data.stripe_financial_connections_transaction.test"

func config(c acctest.Config, id string) string {
	return c.ProviderBlock() + `
data "stripe_financial_connections_transaction" "test" {
  id = "` + id + `"
}
`
}

// Subscribing an account to transactions starts the refresh that surfaces them; the newest at
// the test institution is a posted `usd` 10000 (suite: "Financial Connections transactions").
// Retrieve returns what the list does.
func TestAccFinancialConnectionsTransaction_read(t *testing.T) {
	acct := acctest.NewAccount(t)
	account := tfacc.LinkedAccount(t, acct)
	newest := tfacc.SubscribedTransactions(t, acct, account)[0]
	id, _ := newest.GetString("id")
	refresh, _ := newest.GetString("transaction_refresh")
	at, _ := newest.Get("transacted_at")
	transacted := strconv.FormatInt(int64(at.(wire.Int)), 10)
	resource.ParallelTest(t, resource.TestCase{
		ProtoV6ProviderFactories: tfacc.ProviderFactories,
		Steps: []resource.TestStep{{
			Config: config(acct, id),
			Check: resource.ComposeAggregateTestCheckFunc(
				resource.TestCheckResourceAttr(address, "id", id),
				resource.TestCheckResourceAttr(address, "account", account),
				resource.TestCheckResourceAttr(address, "amount", "10000"),
				resource.TestCheckResourceAttr(address, "currency", "usd"),
				resource.TestCheckResourceAttr(address, "description", "Typographic"),
				resource.TestCheckResourceAttr(address, "status", "posted"),
				resource.TestCheckResourceAttr(address, "transaction_refresh", refresh),
				resource.TestCheckResourceAttr(address, "transacted_at", transacted),
				resource.TestCheckResourceAttr(address, "status_transitions.posted_at", transacted),
				resource.TestCheckNoResourceAttr(address, "status_transitions.void_at"),
				resource.TestCheckResourceAttrSet(address, "updated"),
			),
		}},
	})
}

// An unknown transaction is a 404 `resource_missing` (suite: "Financial Connections
// transactions").
func TestAccFinancialConnectionsTransaction_notFound(t *testing.T) {
	acct := acctest.NewAccount(t)
	resource.ParallelTest(t, resource.TestCase{
		ProtoV6ProviderFactories: tfacc.ProviderFactories,
		Steps: []resource.TestStep{{
			Config:      config(acct, "fctxn_doesnotexist"),
			ExpectError: regexp.MustCompile("no stripe_financial_connections_transaction found for\\s+id=fctxn_doesnotexist"),
		}},
	})
}
