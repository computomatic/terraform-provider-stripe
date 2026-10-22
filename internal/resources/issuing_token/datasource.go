// Package issuing_token declares the stripe_issuing_token data source. There is no resource.
package issuing_token

import (
	"github.com/hashicorp/terraform-plugin-framework/datasource"

	"github.com/computomatic/terraform-provider-stripe/internal/engine"
	"github.com/computomatic/terraform-provider-stripe/internal/generated/issuing_token"
)

// Declaration is what the spec cannot say about an Issuing token: a card number a digital wallet
// or merchant holds in place of the card's own. A wallet makes one when it provisions the card,
// which no test helper does, so a new test-mode card has none (suite: "Issuing tokens on a new
// test-mode card (first run)"). Stripe retrieves one by its ID and lists them only by card, and
// no field of it is unique.
var Declaration = engine.Declaration{
	TypeName:    "stripe_issuing_token",
	IDPrefix:    "intok_",
	Description: "A network token for an Issuing card, which a digital wallet or merchant holds in place of the card number. It lives on a connected account, so read it through a provider configuration whose `stripe_account` is that account. Look one up by its `id`.",
	Paths:       engine.Paths{Retrieve: "/v1/issuing/tokens/{id}", List: "/v1/issuing/tokens"},
	Delete:      engine.NoDelete,
}

// NewDataSource returns the data source constructor the provider registers. It looks a token up by `id` only: Stripe documents no field of it as unique.
func NewDataSource() (func() datasource.DataSource, error) {
	return engine.NewDataSource(Declaration, issuing_token.Table)
}
