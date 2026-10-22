// Package issuing_authorization declares the stripe_issuing_authorization data source. There is no resource.
package issuing_authorization

import (
	"github.com/hashicorp/terraform-plugin-framework/datasource"

	"github.com/computomatic/terraform-provider-stripe/internal/engine"
	"github.com/computomatic/terraform-provider-stripe/internal/generated/issuing_authorization"
)

// Declaration is what the spec cannot say about an authorization. A card network asks for it
// when an Issuing card is used, or Stripe's test helper makes one (suite: "Issuing
// authorizations from the test helper (first run)"). It embeds the card whole; Stripe never
// returns the card's number and CVC there, but they are sensitive should it do so. Stripe
// retrieves it by its ID, and no field of it is unique.
var Declaration = engine.Declaration{
	TypeName:    "stripe_issuing_authorization",
	IDPrefix:    "iauth_",
	Description: "A request to spend with an Issuing card, its approval and the transactions that settled it. It lives on a connected account, so read it through a provider configuration whose `stripe_account` is that account. Look one up by its `id`. The embedded `card` never carries the card's `number` or `cvc`.",
	Paths:       engine.Paths{Retrieve: "/v1/issuing/authorizations/{id}", List: "/v1/issuing/authorizations"},
	Delete:      engine.NoDelete,
	Sensitive:   []string{"card.number", "card.cvc"},
}

// NewDataSource returns the data source constructor the provider registers. It looks an authorization up by `id` only: Stripe documents no field of it as unique.
func NewDataSource() (func() datasource.DataSource, error) {
	return engine.NewDataSource(Declaration, issuing_authorization.Table)
}
