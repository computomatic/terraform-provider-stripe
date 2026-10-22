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
// ("submit"); Stripe then decides the dispute, which the suite does not prove. A dispute cannot
// be deleted or withdrawn, so destroy leaves it.
var Declaration = engine.Declaration{
	TypeName: "stripe_issuing_dispute",
	IDPrefix: "idp_",
	Description: "A dispute of an Issuing transaction, with the evidence for it. " +
		"Stripe issues cards only for a platform's connected accounts, so manage disputes through a provider configuration whose `stripe_account` is a connected account with the `card_issuing` capability active. " +
		"A new dispute is `unsubmitted`, for the whole transaction unless `amount` says less, and `amount`, `evidence` and `metadata` change in place until it is submitted. " +
		"Set `status` to `submitted` to submit it to the card network, on create or later. After that Stripe refuses a change to `evidence` or `amount`, and decides the dispute: `status` then reads `won`, `lost` or `expired`, and a configuration that still sets `submitted` plans a change Stripe refuses, so remove `status` from it then.\n\n" +
		"~> Stripe cannot delete or withdraw a dispute, so `terraform destroy` leaves it on the connected account, submitted or not. Changing `transaction` replaces the dispute, which leaves the old one too.",
	DataSourceDescription: "A dispute of an Issuing transaction, on a connected account, with the evidence for it. Look one up by its `id`.",
	Paths: engine.Paths{
		Create:   "/v1/issuing/disputes",
		Retrieve: "/v1/issuing/disputes/{id}",
		Update:   "/v1/issuing/disputes/{id}",
		List:     "/v1/issuing/disputes",
	},
	Delete:          engine.Retain,
	RetainWarning:   "Stripe cannot withdraw a dispute either, so it stays as it is.",
	Transitions:     []engine.Transition{{Field: "status", Value: "submitted", Path: "/v1/issuing/disputes/{id}/submit"}},
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
