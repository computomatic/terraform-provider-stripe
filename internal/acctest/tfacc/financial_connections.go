package tfacc

import (
	"testing"

	"github.com/computomatic/terraform-provider-stripe/internal/acctest"
	"github.com/computomatic/terraform-provider-stripe/internal/stripeapi"
	"github.com/computomatic/terraform-provider-stripe/internal/wire"
)

// LinkedAccount seeds an active Financial Connections account, held by a new customer, with the
// `ownership` and `transactions` permissions, and returns its ID. Stripe links an account only
// through its hosted flow, so the test skips in smoke mode.
func LinkedAccount(t *testing.T, c acctest.Config) string {
	t.Helper()
	acctest.RequireFake(t)
	customer := Create(t, c, "/v1/customers", wire.Object{{Name: "name", Value: wire.String(c.Name("tfacc-fc-holder"))}})
	return acctest.Seed(t, c, wire.Object{
		{Name: "object", Value: wire.String("financial_connections.account")},
		{Name: "account_holder", Value: wire.Object{
			{Name: "type", Value: wire.String("customer")},
			{Name: "customer", Value: wire.String(customer)},
		}},
		{Name: "account_numbers", Value: wire.Array{}},
		{Name: "balance", Value: wire.Null{}},
		{Name: "balance_refresh", Value: wire.Null{}},
		{Name: "category", Value: wire.String("cash")},
		{Name: "display_name", Value: wire.String("Success")},
		{Name: "institution_name", Value: wire.String("StripeBank")},
		{Name: "last4", Value: wire.String("6789")},
		{Name: "livemode", Value: wire.Bool(false)},
		{Name: "permissions", Value: wire.Array{wire.String("balances"), wire.String("ownership"), wire.String("payment_method"), wire.String("transactions")}},
		{Name: "status", Value: wire.String("active")},
		{Name: "status_details", Value: wire.Object{}},
		{Name: "subcategory", Value: wire.String("checking")},
		{Name: "supported_payment_method_types", Value: wire.Array{wire.String("link"), wire.String("us_bank_account")}},
	})[0]
}

// RefreshedOwnership refreshes the account's ownership, waits until the refresh has succeeded
// and returns the account's new `ownership`.
func RefreshedOwnership(t *testing.T, c acctest.Config, account string) string {
	t.Helper()
	path := "/v1/financial_connections/accounts/" + account
	Post(t, c, path+"/refresh", wire.Object{{Name: "features", Value: wire.Array{wire.String("ownership")}}})
	awaitRefresh(t, c, path, "ownership_refresh")
	ownership, _ := get(t, c, path).GetString("ownership")
	return ownership
}

// SubscribedTransactions subscribes the account to transactions, which starts a refresh, waits
// until the refresh has succeeded and returns the account's transactions, newest first.
func SubscribedTransactions(t *testing.T, c acctest.Config, account string) []wire.Object {
	t.Helper()
	path := "/v1/financial_connections/accounts/" + account
	Post(t, c, path+"/subscribe", wire.Object{{Name: "features", Value: wire.Array{wire.String("transactions")}}})
	awaitRefresh(t, c, path, "transaction_refresh")
	resp, err := Client(c).Do(t.Context(), stripeapi.Request{Method: "GET", Path: "/v1/financial_connections/transactions", Params: wire.Object{
		{Name: "account", Value: wire.String(account)},
		{Name: "limit", Value: wire.Int(100)},
	}})
	if err != nil {
		t.Fatalf("listing the transactions of %s: %v", account, err)
	}
	data, _ := resp.Body.(wire.Object).Get("data")
	items, _ := data.(wire.Array)
	var out []wire.Object
	for _, item := range items {
		if obj, ok := item.(wire.Object); ok {
			out = append(out, obj)
		}
	}
	if len(out) == 0 {
		t.Fatalf("the transaction refresh of %s surfaced no transactions", account)
	}
	return out
}

// awaitRefresh reads the account until the status of its refresh is `succeeded`, and fails the
// test when the refresh fails or does not finish in time.
func awaitRefresh(t *testing.T, c acctest.Config, path, refresh string) {
	t.Helper()
	status := ""
	ok := poll(func() bool {
		r, _ := get(t, c, path).Get(refresh)
		obj, _ := r.(wire.Object)
		status, _ = obj.GetString("status")
		return status != "pending"
	})
	if !ok {
		t.Fatalf("the %s of %s did not finish within %s", refresh, path, activationLimit)
	}
	if status != "succeeded" {
		t.Fatalf("the %s of %s ended %q", refresh, path, status)
	}
}
