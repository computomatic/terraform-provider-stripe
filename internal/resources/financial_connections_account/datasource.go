// Package financial_connections_account declares the stripe_financial_connections_account data source. There is no resource.
package financial_connections_account

import (
	"github.com/hashicorp/terraform-plugin-framework/datasource"

	"github.com/computomatic/terraform-provider-stripe/internal/engine"
	"github.com/computomatic/terraform-provider-stripe/internal/generated/financial_connections_account"
)

// Declaration is what the spec cannot say about this object. Linking an account needs the browser flow a session starts, so nothing in the API creates one (suite: "Financial Connections account list needs no filter") and only the data source exists. It is read by `id`: no field of it is unique.
var Declaration = engine.Declaration{
	TypeName:    "stripe_financial_connections_account",
	IDPrefix:    "fca_",
	Description: "A bank account that a customer, or the account itself, linked through Financial Connections. Look one up by its `id`.",
	Paths:       engine.Paths{Retrieve: "/v1/financial_connections/accounts/{id}", List: "/v1/financial_connections/accounts"},
	Delete:      engine.NoDelete,
}

// NewDataSource returns the data source constructor the provider registers. It looks the object up by `id` only: Stripe documents no field of it as unique.
func NewDataSource() (func() datasource.DataSource, error) {
	return engine.NewDataSource(Declaration, financial_connections_account.Table)
}
