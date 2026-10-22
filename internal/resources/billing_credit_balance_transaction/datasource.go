// Package billing_credit_balance_transaction declares the stripe_billing_credit_balance_transaction data source. There is no resource.
package billing_credit_balance_transaction

import (
	"github.com/hashicorp/terraform-plugin-framework/datasource"

	"github.com/computomatic/terraform-provider-stripe/internal/engine"
	"github.com/computomatic/terraform-provider-stripe/internal/generated/billing_credit_balance_transaction"
)

// Declaration is what the spec cannot say about this object. Stripe records a transaction when a credit grant is created, voided or applied (suite: "Credit balance transactions list under the credit grants url"), so only the data source exists.
var Declaration = engine.Declaration{
	TypeName:    "stripe_billing_credit_balance_transaction",
	IDPrefix:    "cbtxn_",
	Description: "A movement of a customer's billing credit balance: credit granted, or credit applied to an invoice or voided. Look one up by its `id`.",
	Paths:       engine.Paths{Retrieve: "/v1/billing/credit_balance_transactions/{id}", List: "/v1/billing/credit_balance_transactions"},
	Delete:      engine.NoDelete,
}

// NewDataSource returns the data source constructor the provider registers. It looks the object up by `id` only: Stripe documents no field of it as unique.
func NewDataSource() (func() datasource.DataSource, error) {
	return engine.NewDataSource(Declaration, billing_credit_balance_transaction.Table)
}
