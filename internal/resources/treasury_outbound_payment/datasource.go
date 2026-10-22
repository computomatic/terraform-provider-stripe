// Package treasury_outbound_payment declares the stripe_treasury_outbound_payment data source. There is no resource.
package treasury_outbound_payment

import (
	"github.com/hashicorp/terraform-plugin-framework/datasource"

	"github.com/computomatic/terraform-provider-stripe/internal/engine"
	"github.com/computomatic/terraform-provider-stripe/internal/generated/treasury_outbound_payment"
)

// Declaration is what the spec cannot say about this object. It is a money movement in a Treasury financial account on a connected account (suite: "Treasury reads on a US connected account"). Stripe retrieves it by its ID alone and lists it by financial account, and no field of it is unique, so the data source reads it by `id` and returns its `financial_account`. Making one needs a customer with a US bank account payment method on the connected account, which no test helper creates (suite: "outbound payments"), so only the data source exists.
var Declaration = engine.Declaration{
	TypeName:    "stripe_treasury_outbound_payment",
	IDPrefix:    "obp_",
	Description: "A payment of money out of a Treasury financial account to a third party. It lives on a connected account, so read it through a provider configuration whose `stripe_account` is that account. Look one up by its `id`.",
	Paths:       engine.Paths{Retrieve: "/v1/treasury/outbound_payments/{id}", List: "/v1/treasury/outbound_payments"},
	Delete:      engine.NoDelete,
}

// NewDataSource returns the data source constructor the provider registers. It looks the object up by `id` only: Stripe documents no field of it as unique.
func NewDataSource() (func() datasource.DataSource, error) {
	return engine.NewDataSource(Declaration, treasury_outbound_payment.Table)
}
