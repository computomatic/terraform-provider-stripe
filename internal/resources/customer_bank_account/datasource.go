// Package customer_bank_account declares the stripe_customer_bank_account data source. There
// is no resource.
package customer_bank_account

import (
	"github.com/hashicorp/terraform-plugin-framework/datasource"

	"github.com/computomatic/terraform-provider-stripe/internal/engine"
	"github.com/computomatic/terraform-provider-stripe/internal/generated/customer_bank_account"
)

// Declaration is what the spec cannot say about a customer's bank account: a legacy payment
// source for ACH debits, attached from a bank account token or hash. Stripe serves a
// customer's cards and bank accounts as one collection under `/sources`, and its bank
// accounts alone under `/bank_accounts`, with the same body for the same ID (suite: "Legacy
// customer sources"). The data source reads `/bank_accounts`, so its table is the bank
// account's. A missing bank account there is a 404 with no error code, which is still not
// found. It is read-only for now.
var Declaration = engine.Declaration{
	TypeName:    "stripe_customer_bank_account",
	IDPrefix:    "ba_",
	Description: "A bank account saved to a customer as a legacy payment source. `status` says whether it is verified. Look one up by the `customer` and the bank account's `id`.",
	Parent:      "customer",
	Paths: engine.Paths{
		Retrieve: "/v1/customers/{customer}/bank_accounts/{id}",
		List:     "/v1/customers/{customer}/bank_accounts",
	},
	Delete: engine.NoDelete,
}

// NewDataSource returns the data source constructor the provider registers. It needs the `customer` and the `id` of the bank account. Stripe documents no bank account field as unique.
func NewDataSource() (func() datasource.DataSource, error) {
	return engine.NewDataSource(Declaration, customer_bank_account.Table)
}
