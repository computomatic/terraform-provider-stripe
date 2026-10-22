// Package customer_cash_balance_transaction declares the stripe_customer_cash_balance_transaction
// data source. There is no resource.
package customer_cash_balance_transaction

import (
	"github.com/hashicorp/terraform-plugin-framework/datasource"

	"github.com/computomatic/terraform-provider-stripe/internal/engine"
	"github.com/computomatic/terraform-provider-stripe/internal/generated/customer_cash_balance_transaction"
)

// Declaration is what the spec cannot say about this object. Stripe makes a cash balance
// transaction when a customer's cash balance changes, so only the data source exists. It is
// addressed under its customer.
var Declaration = engine.Declaration{
	TypeName:    "stripe_customer_cash_balance_transaction",
	IDPrefix:    "ccsbtxn_",
	Description: "A change to a customer's cash balance, such as a funding by bank transfer. Look one up by the `customer` and its `id`.",
	Parent:      "customer",
	Paths: engine.Paths{
		Retrieve: "/v1/customers/{customer}/cash_balance_transactions/{id}",
		List:     "/v1/customers/{customer}/cash_balance_transactions",
	},
	Delete: engine.NoDelete,
}

// NewDataSource returns the data source constructor the provider registers. It needs the `customer` and the `id` of the transaction. Stripe documents no transaction field as unique.
func NewDataSource() (func() datasource.DataSource, error) {
	return engine.NewDataSource(Declaration, customer_cash_balance_transaction.Table)
}
