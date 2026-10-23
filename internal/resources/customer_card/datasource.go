// Package customer_card declares the stripe_customer_card data source. There is no resource.
package customer_card

import (
	"github.com/hashicorp/terraform-plugin-framework/datasource"

	"github.com/computomatic/terraform-provider-stripe/internal/engine"
	"github.com/computomatic/terraform-provider-stripe/internal/generated/customer_card"
)

// Declaration is what the spec cannot say about a customer's card: a legacy payment source
// attached from a card token. Stripe serves a customer's cards and bank accounts as one
// collection under `/sources`, and its cards alone under `/cards`, with the same body for the
// same ID (suite: "Legacy customer sources"). The data source reads `/cards`, so its table is
// the card's. What `/cards` answers for a bank account's ID is unproven. It is read-only for
// now.
var Declaration = engine.Declaration{
	TypeName:    "stripe_customer_card",
	IDPrefix:    "card_",
	Description: "A card saved to a customer as a legacy payment source, from a card token. Look one up by the `customer` and the card's `id`.",
	Parent:      "customer",
	Paths: engine.Paths{
		Retrieve: "/v1/customers/{customer}/cards/{id}",
		List:     "/v1/customers/{customer}/cards",
	},
	Delete: engine.NoDelete,
}

// NewDataSource returns the data source constructor the provider registers. It needs the `customer` and the `id` of the card. Stripe documents no card field as unique.
func NewDataSource() (func() datasource.DataSource, error) {
	return engine.NewDataSource(Declaration, customer_card.Table)
}
