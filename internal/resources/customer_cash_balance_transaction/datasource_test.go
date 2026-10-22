package customer_cash_balance_transaction_test

import (
	"errors"
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
	// Stripe funds a cash balance by bank transfer in usd, eur, gbp, jpy and mxn, and an
	// account takes the currencies of its country (suite: "fund_cash_balance result"), so the
	// test funds it in the account's own currency: usd on a US account.
	currency := tfacc.AccountCurrency(t, acct)
	resp, err := tfacc.Client(acct).Do(t.Context(), stripeapi.Request{Method: "POST", Path: "/v1/test_helpers/customers/" + customer + "/fund_cash_balance", Params: wire.Object{
		{Name: "amount", Value: wire.Int(1500)},
		{Name: "currency", Value: wire.String(currency)},
		{Name: "reference", Value: wire.String("acceptance")},
	}})
	var apiErr *stripeapi.APIError
	if errors.As(err, &apiErr) && strings.HasPrefix(apiErr.Code, "fake_") {
		t.Skipf("the fake does not serve funding a cash balance in %s yet: %v", currency, err)
	}
	if err != nil {
		t.Fatalf("funding the cash balance in %s: %v", currency, err)
	}
	id, _ := resp.Body.(wire.Object).GetString("id")
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
