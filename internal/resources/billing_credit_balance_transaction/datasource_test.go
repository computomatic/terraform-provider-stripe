package billing_credit_balance_transaction_test

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

const address = "data.stripe_billing_credit_balance_transaction.test"

func config(c acctest.Config, id string) string {
	return c.ProviderBlock() + `
data "stripe_billing_credit_balance_transaction" "test" {
  id = "` + id + `"
}
`
}

// granted creates a credit grant of 1000 in the account's currency and returns its ID and the
// ID of the credit Stripe records for it, which it lists a moment later (suite: "Credit balance
// transactions list under the credit grants url").
func granted(t *testing.T, acct acctest.Config) (grant, transaction string) {
	t.Helper()
	customer := tfacc.Create(t, acct, "/v1/customers", nil)
	grant = tfacc.Create(t, acct, "/v1/billing/credit_grants", wire.Object{
		{Name: "customer", Value: wire.String(customer)},
		{Name: "name", Value: wire.String(acct.Name("credit balance"))},
		{Name: "category", Value: wire.String("promotional")},
		{Name: "amount", Value: wire.Object{
			{Name: "type", Value: wire.String("monetary")},
			{Name: "monetary", Value: wire.Object{
				{Name: "currency", Value: wire.String(tfacc.AccountCurrency(t, acct))},
				{Name: "value", Value: wire.Int(1000)},
			}},
		}},
		{Name: "applicability_config", Value: wire.Object{{Name: "scope", Value: wire.Object{{Name: "price_type", Value: wire.String("metered")}}}}},
	})
	items := tfacc.AwaitListed(t, acct, "/v1/billing/credit_balance_transactions", wire.Object{
		{Name: "customer", Value: wire.String(customer)},
		{Name: "credit_grant", Value: wire.String(grant)},
	}, func(items []wire.Object) bool { return len(items) > 0 })
	transaction, _ = items[0].GetString("id")
	return grant, transaction
}

func TestAccBillingCreditBalanceTransaction_read(t *testing.T) {
	acct := acctest.NewAccount(t)
	grant, id := granted(t, acct)
	resource.ParallelTest(t, resource.TestCase{
		ProtoV6ProviderFactories: tfacc.ProviderFactories,
		Steps: []resource.TestStep{{
			Config: config(acct, id),
			Check: resource.ComposeAggregateTestCheckFunc(
				resource.TestCheckResourceAttr(address, "id", id),
				resource.TestCheckResourceAttr(address, "credit_grant", grant),
				resource.TestCheckResourceAttr(address, "type", "credit"),
				resource.TestCheckResourceAttr(address, "credit.type", "credits_granted"),
				resource.TestCheckResourceAttr(address, "credit.amount.type", "monetary"),
				resource.TestCheckResourceAttr(address, "credit.amount.monetary.value", "1000"),
				resource.TestCheckResourceAttr(address, "credit.amount.monetary.currency", tfacc.AccountCurrency(t, acct)),
				resource.TestCheckNoResourceAttr(address, "debit"),
				resource.TestCheckResourceAttrSet(address, "effective_at"),
				resource.TestCheckResourceAttr(address, "livemode", "false"),
			),
		}},
	})
}

// An unknown transaction is a 404 `resource_missing` (suite: "retrieve unknown", an altered
// real ID).
func TestAccBillingCreditBalanceTransaction_notFound(t *testing.T) {
	acct := acctest.NewAccount(t)
	_, id := granted(t, acct)
	missing := tfacc.AlteredID(id)
	resource.ParallelTest(t, resource.TestCase{
		ProtoV6ProviderFactories: tfacc.ProviderFactories,
		Steps: []resource.TestStep{{
			Config:      config(acct, missing),
			ExpectError: regexp.MustCompile(`no stripe_billing_credit_balance_transaction found for\s+id=` + missing),
		}},
	})
}
