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

// Declaration is what the spec cannot say about a card. It is a card on a connected account,
// drawing on a Treasury financial account, which Stripe requires there (suite:
// "Issuing cards on a connected account need a Treasury financial account"). A card is created
// `inactive`; its status moves between `active` and `inactive`, and `canceled` is final: a
// canceled card cannot be activated again (suite: "update status", "cancel", "canceled card
// cannot be reactivated"). Activating needs a cardholder whose review has finished (suite: "New
// Issuing cardholders start `under_review`"). The cardholder, currency, type and financial
// account are create-only (`parameter_unknown` on update). There is no DELETE route, so destroy
// deactivates the card, which is reversible, and refuses a canceled one. Stripe returns the full `number` and `cvc` only when asked with `expand[]`
// on a single card's retrieve (suite: "retrieve with number expanded"), and refuses the expand
// on a create or an update (suite: "number and cvc expand only on retrieve"), so the engine
// asks on every retrieve and retrieves the card after each create and update. State holds what
// Stripe last returned, and both are sensitive. A physical card takes a `personalization_design`,
// its `shipping` and a `second_line` (suite: TestIssuingCardAttributes, variant `physical`).
// Stripe answers the shipping with the address it normalised and a carrier for the service, and
// an update sends the shipping whole, after which Stripe answers again (probe `update/2`). It
// moves a card to another design only from one it rejected, and to an active one (variant
// `in_review`), and refuses an empty value for the design or any shipping member, each of which
// a removal therefore leaves as it is.
var Declaration = engine.Declaration{
	TypeName: "stripe_issuing_card",
	IDPrefix: "ic_",
	Description: "A virtual or physical card issued to a cardholder. " +
		"Stripe issues cards only for a platform's connected accounts, so manage cards through a provider configuration whose `stripe_account` is a connected account with the `card_issuing` capability active, and set `financial_account` to a Treasury financial account of that account with its `card_issuing` feature active. " +
		"A new card is `inactive`. Set `status` to `active` once the cardholder's review has finished, back to `inactive` to pause it, or to `canceled` to cancel it, which is final: a canceled card cannot be activated again. " +
		"`number` and `cvc` hold the full card number and CVC, which Stripe returns on request for a virtual card; they are sensitive. " +
		"A physical card takes a `personalization_design`, a `second_line` and its `shipping`, which Stripe answers with the address it normalised, the carrier and the shipment's `status`; `shipping` changes in place before the card ships, and Stripe moves a card to another active design only from a design it rejected.\n\n" +
		"~> Stripe cannot delete a card, so `terraform destroy` deactivates it (`status = inactive`), and the card stays on the connected account. Changing `cardholder`, `currency`, `type`, `financial_account` or any other field Stripe takes only on create replaces the card, which deactivates the old one and issues a new number.",
	DataSourceDescription: "A virtual or physical card issued to a cardholder, on a connected account. Look one up by its `id`. `number` and `cvc` hold the full card number and CVC, which are sensitive.",
	Paths: engine.Paths{
		Create:   "/v1/issuing/cards",
		Retrieve: "/v1/issuing/cards/{id}",
		Update:   "/v1/issuing/cards/{id}",
		List:     "/v1/issuing/cards",
	},
	Delete:  engine.Archive,
	Archive: &engine.ArchiveAction{Field: "status", Value: wire.String("inactive")},
	RefuseWhen: &engine.RefuseWhen{
		Field:  "status",
		Value:  wire.String("canceled"),
		Reason: "Stripe cannot deactivate a canceled card, because canceling one is final.",
	},
	ExpandOnRetrieve: []string{"number", "cvc"},
	Sensitive:        []string{"number", "cvc"},
	// Stripe keeps the members of `spending_controls` an update leaves out (family
	// `v1-issuing-treasury`, `hashes`), so an update sends only those that changed.
	SendChangedMembers: []string{"spending_controls"},
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
