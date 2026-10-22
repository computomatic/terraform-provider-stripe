// Package customer_balance_transaction declares the stripe_customer_balance_transaction
// resource and data source: an adjustment to a customer's credit balance.
package customer_balance_transaction

import (
	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/resource"

	"github.com/computomatic/terraform-provider-stripe/internal/engine"
	"github.com/computomatic/terraform-provider-stripe/internal/generated/customer_balance_transaction"
)

// Declaration is what the spec cannot say about a customer balance transaction. It is
// addressed under its customer, so its import ID is `<customer_id>/<transaction_id>`. Stripe
// updates only `description` and `metadata` (suite: "amount is not updatable") and has no
// delete (suite: "delete is not served"), so destroy is refused.
var Declaration = engine.Declaration{
	TypeName:              "stripe_customer_balance_transaction",
	IDPrefix:              "cbtxn_",
	Description:           "An adjustment to a customer's credit balance: a negative `amount` credits the customer, a positive one debits them. Only `description` and `metadata` change after creation.",
	DataSourceDescription: "An adjustment to a customer's credit balance.",
	Parent:                "customer",
	Paths: engine.Paths{
		Create:   "/v1/customers/{customer}/balance_transactions",
		Retrieve: "/v1/customers/{customer}/balance_transactions/{id}",
		Update:   "/v1/customers/{customer}/balance_transactions/{id}",
		List:     "/v1/customers/{customer}/balance_transactions",
	},
	Delete:       engine.Refuse,
	RefuseReason: "Stripe cannot delete or reverse a customer balance transaction; it stays on the customer's balance.",
}

// NewResource returns the resource constructor the provider registers.
func NewResource() (func() resource.Resource, error) {
	return engine.NewResource(Declaration, customer_balance_transaction.Table)
}

// NewDataSource returns the data source constructor. It needs the `customer` and the `id` of the transaction. Stripe documents no transaction field as unique.
func NewDataSource() (func() datasource.DataSource, error) {
	return engine.NewDataSource(Declaration, customer_balance_transaction.Table)
}
