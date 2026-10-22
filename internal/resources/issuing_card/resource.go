// Package issuing_card declares the stripe_issuing_card resource and data source: a virtual
// card issued to a cardholder.
package issuing_card

import (
	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/resource"

	"github.com/computomatic/terraform-provider-stripe/internal/engine"
	"github.com/computomatic/terraform-provider-stripe/internal/generated/issuing_card"
	"github.com/computomatic/terraform-provider-stripe/internal/wire"
)

// Declaration is what the spec cannot say about a card. It is a virtual card on a connected
// account, drawing on a Treasury financial account, which Stripe requires there (suite:
// "Issuing cards on a connected account need a Treasury financial account"). A card is created
// `inactive`; its status moves between `active` and `inactive`, and `canceled` is final: a
// canceled card cannot be activated again (suite: "update status", "cancel", "canceled card
// cannot be reactivated"). Activating needs a cardholder whose review has finished (suite: "New
// Issuing cardholders start `under_review`"). The cardholder, currency, type and financial
// account are create-only (`parameter_unknown` on update). There is no DELETE route, so destroy
// cancels the card. Stripe returns the full `number` and `cvc` only when asked with `expand[]`
// on a single card (suite: "retrieve with number expanded"); the engine asks on every request
// that returns the card, so state holds what Stripe last returned, and both are sensitive.
var Declaration = engine.Declaration{
	TypeName: "stripe_issuing_card",
	IDPrefix: "ic_",
	Description: "A virtual card issued to a cardholder. " +
		"Stripe issues cards only for a platform's connected accounts, so manage cards through a provider configuration whose `stripe_account` is a connected account with the `card_issuing` capability active, and set `financial_account` to a Treasury financial account of that account with its `card_issuing` feature active. " +
		"A new card is `inactive`. Set `status` to `active` once the cardholder's review has finished, and back to `inactive` to pause it. " +
		"`number` and `cvc` hold the full card number and CVC, which Stripe returns on request for a virtual card; they are sensitive.\n\n" +
		"~> Stripe cannot delete a card, so `terraform destroy` cancels it, and the canceled card stays on the connected account. Canceling is final: a card whose `status` is `canceled` cannot be activated again, and Stripe refuses the change. Changing `cardholder`, `currency`, `type`, `financial_account` or any other field Stripe takes only on create replaces the card, which cancels the old one and issues a new number.",
	DataSourceDescription: "A virtual card issued to a cardholder, on a connected account. Look one up by its `id`. `number` and `cvc` hold the full card number and CVC, which are sensitive.",
	Paths: engine.Paths{
		Create:   "/v1/issuing/cards",
		Retrieve: "/v1/issuing/cards/{id}",
		Update:   "/v1/issuing/cards/{id}",
		List:     "/v1/issuing/cards",
	},
	Delete:    engine.Archive,
	Archive:   &engine.ArchiveAction{Field: "status", Value: wire.String("canceled")},
	Expand:    []string{"number", "cvc"},
	Sensitive: []string{"number", "cvc"},
}

// NewResource returns the resource constructor the provider registers.
func NewResource() (func() resource.Resource, error) {
	return engine.NewResource(Declaration, issuing_card.Table)
}

// NewDataSource returns the data source constructor. It looks a card up by `id` only: Stripe
// documents no field of a card as unique.
func NewDataSource() (func() datasource.DataSource, error) {
	return engine.NewDataSource(Declaration, issuing_card.Table)
}
