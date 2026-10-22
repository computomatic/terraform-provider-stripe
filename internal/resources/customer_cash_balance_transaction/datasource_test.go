package customer_cash_balance_transaction_test

import (
	"os"
	"strings"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"

	"github.com/computomatic/terraform-provider-stripe/internal/acctest"
	"github.com/computomatic/terraform-provider-stripe/internal/acctest/tfacc"
	"github.com/computomatic/terraform-provider-stripe/internal/stripeapi"
	"github.com/computomatic/terraform-provider-stripe/internal/wire"
)

func TestMain(m *testing.M) { os.Exit(acctest.Main(m)) }

// Funding a customer's cash balance with Stripe's test helper makes a cash balance
// transaction (suite: "fund_cash_balance result"); nothing else here does.
func TestAccCustomerCashBalanceTransaction_read(t *testing.T) {
	acctest.RequireFake(t)
	acct := acctest.NewAccount(t)
	customer := tfacc.Create(t, acct, "/v1/customers", nil)
	// The account takes only some currencies and refuses the others with `Invalid currency`
	// (suite: "fund_cash_balance result"), so each is tried in turn.
	var id, currency string
	for _, c := range []string{"usd", "eur", "gbp", "jpy", "mxn"} {
		resp, err := tfacc.Client(acct).Do(t.Context(), stripeapi.Request{Method: "POST", Path: "/v1/test_helpers/customers/" + customer + "/fund_cash_balance", Params: wire.Object{
			{Name: "amount", Value: wire.Int(1500)},
			{Name: "currency", Value: wire.String(c)},
			{Name: "reference", Value: wire.String("acceptance")},
		}})
		if err != nil {
			if strings.Contains(err.Error(), "Invalid currency") {
				continue
			}
			if strings.Contains(err.Error(), "fake_") {
				t.Skipf("the fake does not serve funding a cash balance in %s yet: %v", c, err)
			}
			t.Fatalf("funding the cash balance in %s: %v", c, err)
		}
		id, _ = resp.Body.(wire.Object).GetString("id")
		currency = c
		break
	}
	if id == "" {
		t.Skip("the fake takes none of the currencies for funding a cash balance")
	}
	resource.ParallelTest(t, resource.TestCase{
		ProtoV6ProviderFactories: tfacc.ProviderFactories,
		Steps: []resource.TestStep{{
			Config: acct.ProviderBlock() + `
data "stripe_customer_cash_balance_transaction" "test" {
  customer = "` + customer + `"
  id       = "` + id + `"
}
`,
			Check: resource.ComposeAggregateTestCheckFunc(
				resource.TestCheckResourceAttr("data.stripe_customer_cash_balance_transaction.test", "id", id),
				resource.TestCheckResourceAttr("data.stripe_customer_cash_balance_transaction.test", "customer", customer),
				resource.TestCheckResourceAttr("data.stripe_customer_cash_balance_transaction.test", "type", "funded"),
				resource.TestCheckResourceAttr("data.stripe_customer_cash_balance_transaction.test", "net_amount", "1500"),
				resource.TestCheckResourceAttr("data.stripe_customer_cash_balance_transaction.test", "currency", currency),
				resource.TestCheckResourceAttrSet("data.stripe_customer_cash_balance_transaction.test", "funded.bank_transfer.type"),
			),
		}},
	})
}
