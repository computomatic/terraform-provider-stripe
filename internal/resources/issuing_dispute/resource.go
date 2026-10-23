// Package issuing_dispute declares the stripe_issuing_dispute resource and data source: a
// cardholder's dispute of an Issuing transaction.
package issuing_dispute

import (
	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/resource"

	"github.com/computomatic/terraform-provider-stripe/internal/engine"
	"github.com/computomatic/terraform-provider-stripe/internal/generated/issuing_dispute"
)

// Declaration is what the spec cannot say about an Issuing dispute. It disputes a transaction
// on a connected account with Issuing, for the whole transaction unless `amount` says less, and
// the transaction names it at once (suite: "Issuing disputes (first run)", "create"). Stripe
// requires `evidence` and its `reason`, takes `transaction` on create only, and changes
// `amount`, `evidence` and `metadata` in place while the dispute is `unsubmitted` ("update").
// Stripe answers with the evidence hash of the reason filled in with nulls when the request
// leaves it out ("submit with a reason and no explanation", proven for `fraudulent`, the one
// reason the fake takes), and with the hashes of other reasons null, so each reason's hash is
// server-filled, and a change of reason leaves the hashes the configuration does not set to
// Stripe's answer instead of the state. Submitting is an action endpoint that moves `status` from
// `unsubmitted` to `submitted`, after which evidence is fixed and metadata still changes
// ("submit"). Stripe then decides the dispute, in test mode about two seconds later by the
// fraudulent explanation `winning_evidence` or `losing_evidence` ("Decided Issuing disputes in
// test mode"), and `status` reads `won` or `lost`, which satisfies a configured `submitted`, so
// the plan keeps it and the submit, which Stripe would refuse, is not sent again. Stripe's
// dispute lifecycle also moves a dispute left unsubmitted for 110 days, or rejected, to
// `expired` (docs.stripe.com/issuing/purchases/disputes, unproven by the suite), so `expired` is
// final rather than reached: it may never have been submitted. A dispute cannot be deleted or
// withdrawn, so destroy is refused.
var Declaration = engine.Declaration{
	TypeName: "stripe_issuing_dispute",
	IDPrefix: "idp_",
	Description: "A dispute of an Issuing transaction, with the evidence for it. " +
		"Stripe issues cards only for a platform's connected accounts, so manage disputes through a provider configuration whose `stripe_account` is a connected account with the `card_issuing` capability active. " +
		"A new dispute is `unsubmitted`, for the whole transaction unless `amount` says less, and `amount`, `evidence` and `metadata` change in place until it is submitted. " +
		"Set `status` to `submitted` to submit it to the card network, on create or later. After that Stripe refuses a change to `evidence` or `amount`, and decides the dispute: `status` then keeps reading its outcome, `won` or `lost`, which satisfies a configured `submitted`, so the plan shows no change. " +
		"A dispute that Stripe expires, such as one left unsubmitted too long, reads `expired` and cannot be submitted: a configuration that sets `submitted` then fails the plan, so remove `status` from it.",
	DataSourceDescription: "A dispute of an Issuing transaction, on a connected account, with the evidence for it. Look one up by its `id`.",
	Paths: engine.Paths{
		Create:   "/v1/issuing/disputes",
		Retrieve: "/v1/issuing/disputes/{id}",
		Update:   "/v1/issuing/disputes/{id}",
		List:     "/v1/issuing/disputes",
	},
	Delete:          engine.Refuse,
	RefuseReason:    "Stripe cannot delete or withdraw a dispute.",
	Transitions:     []engine.Transition{{Field: "status", Value: "submitted", Path: "/v1/issuing/disputes/{id}/submit", Final: []string{"expired"}, Reached: []string{"won", "lost"}}},
	ServerDefaulted: evidenceHashes,
	FilledFrom:      map[string][]string{"evidence.reason": evidenceHashes},
}

// evidenceHashes are the evidence hashes, one per dispute reason.
var evidenceHashes = []string{
	"evidence.canceled", "evidence.duplicate", "evidence.fraudulent", "evidence.merchandise_not_as_described",
	"evidence.no_valid_authorization", "evidence.not_received", "evidence.other", "evidence.service_not_as_described",
}

// NewResource returns the resource constructor the provider registers.
func NewResource() (func() resource.Resource, error) {
	return engine.NewResource(Declaration, issuing_dispute.Table)
}

// NewDataSource returns the data source constructor. It looks a dispute up by `id` only: Stripe
// documents no field of a dispute as unique.
func NewDataSource() (func() datasource.DataSource, error) {
	return engine.NewDataSource(Declaration, issuing_dispute.Table)
}
