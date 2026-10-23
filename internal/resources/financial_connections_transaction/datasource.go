// Package financial_connections_transaction declares the stripe_financial_connections_transaction
// data source. There is no resource.
package financial_connections_transaction

import (
	"github.com/hashicorp/terraform-plugin-framework/datasource"

	"github.com/computomatic/terraform-provider-stripe/internal/engine"
	"github.com/computomatic/terraform-provider-stripe/internal/generated/financial_connections_transaction"
)

// Declaration is what the spec cannot say about this object. Transactions exist only after a subscription to an account's transactions starts a refresh (suite: "Subscribing to Financial Connections transactions starts a refresh"), and nothing in the API creates one. A data source never subscribes or refreshes: both are POSTs that a read must not send. The list by `account` holds every transaction of the account, so it is no lookup key.
var Declaration = engine.Declaration{
	TypeName: "stripe_financial_connections_transaction",
	IDPrefix: "fctxn_",
	Description: "A transaction of a bank account linked through Financial Connections, as the account's institution reported it. Look one up by its `id`. " +
		"An account has transactions only after a transaction refresh, which needs its `transactions` permission, and this data source never starts one: " +
		"subscribe the account to transactions through Stripe's API first (`POST /v1/financial_connections/accounts/{account}/subscribe` with `features[]=transactions`), which starts the first refresh, " +
		"and read its transactions, which `GET /v1/financial_connections/transactions?account={account}` lists, once the account's `transaction_refresh.status` is `succeeded`.",
	Paths:  engine.Paths{Retrieve: "/v1/financial_connections/transactions/{id}", List: "/v1/financial_connections/transactions"},
	Delete: engine.NoDelete,
}

// NewDataSource returns the data source constructor the provider registers. It looks the object up by `id` only: Stripe documents no field of it as unique.
func NewDataSource() (func() datasource.DataSource, error) {
	return engine.NewDataSource(Declaration, financial_connections_transaction.Table)
}
