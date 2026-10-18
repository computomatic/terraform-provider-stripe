package balance_transaction_test

import (
	"os"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"

	"github.com/computomatic/terraform-provider-stripe/internal/acctest"
	"github.com/computomatic/terraform-provider-stripe/internal/acctest/tfacc"
	"github.com/computomatic/terraform-provider-stripe/internal/wire"
)

func TestMain(m *testing.M) { os.Exit(acctest.Main(m)) }

// Balance transactions come from charges and payouts, so the test seeds one.
func TestAccBalanceTransaction_read(t *testing.T) {
	acct := acctest.NewAccount(t)
	id := acctest.Seed(t, acct, wire.Object{
		{Name: "object", Value: wire.String("balance_transaction")},
		{Name: "amount", Value: wire.Int(1000)},
		{Name: "balance_type", Value: wire.String("payments")},
		{Name: "available_on", Value: wire.Int(1893456000)},
		{Name: "currency", Value: wire.String("usd")},
		{Name: "description", Value: wire.String("Seeded charge")},
		{Name: "exchange_rate", Value: wire.Null{}},
		{Name: "fee", Value: wire.Int(59)},
		{Name: "fee_details", Value: wire.Array{wire.Object{
			{Name: "amount", Value: wire.Int(59)},
			{Name: "application", Value: wire.Null{}},
			{Name: "currency", Value: wire.String("usd")},
			{Name: "description", Value: wire.String("Stripe processing fees")},
			{Name: "type", Value: wire.String("stripe_fee")},
		}}},
		{Name: "net", Value: wire.Int(941)},
		{Name: "reporting_category", Value: wire.String("charge")},
		{Name: "source", Value: wire.Null{}},
		{Name: "status", Value: wire.String("available")},
		{Name: "type", Value: wire.String("charge")},
	})[0]
	resource.ParallelTest(t, resource.TestCase{
		ProtoV6ProviderFactories: tfacc.ProviderFactories,
		Steps: []resource.TestStep{{
			Config: acct.ProviderBlock() + `
data "stripe_balance_transaction" "test" {
  id = "` + id + `"
}
`,
			Check: resource.ComposeAggregateTestCheckFunc(
				resource.TestCheckResourceAttr("data.stripe_balance_transaction.test", "amount", "1000"),
				resource.TestCheckResourceAttr("data.stripe_balance_transaction.test", "net", "941"),
				resource.TestCheckResourceAttr("data.stripe_balance_transaction.test", "type", "charge"),
				resource.TestCheckResourceAttr("data.stripe_balance_transaction.test", "fee_details.0.type", "stripe_fee"),
			),
		}},
	})
}
