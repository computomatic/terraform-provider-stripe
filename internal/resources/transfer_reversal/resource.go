// Package transfer_reversal declares the stripe_transfer_reversal resource and data source.
package transfer_reversal

import (
	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/resource"

	"github.com/computomatic/terraform-provider-stripe/internal/engine"
	"github.com/computomatic/terraform-provider-stripe/internal/generated/transfer_reversal"
)

// Declaration is what the spec cannot say about a transfer reversal: money moved back from a
// connected account to the platform (ADR 2026-10-22-money-moving-resources, D6.11). It moves
// money, so the live-mode guard applies. It is addressed under its transfer, so its import ID
// is `<transfer_id>/<reversal_id>`. Without `amount` it reverses the rest (suite: "create the
// rest"); only `metadata` updates (suite: "update", "immutable amount"). Stripe cannot delete
// or undo a reversal, so destroy is refused.
var Declaration = engine.Declaration{
	TypeName:              "stripe_transfer_reversal",
	IDPrefix:              "trr_",
	Description:           "A reversal of a transfer, which moves some or all of the transfer's amount back from the connected account to the platform. Without `amount`, it reverses what remains of the transfer. Only `metadata` changes after creation.",
	DataSourceDescription: "A reversal of a transfer, which moves some or all of the transfer's amount back from the connected account to the platform. Look one up by the `transfer` and the reversal's `id`.",
	ImportNote:            "Stripe does not return `description` or `refund_application_fee`, so an imported reversal has them null.",
	Parent:                "transfer",
	Paths: engine.Paths{
		Create:   "/v1/transfers/{transfer}/reversals",
		Retrieve: "/v1/transfers/{transfer}/reversals/{id}",
		Update:   "/v1/transfers/{transfer}/reversals/{id}",
		List:     "/v1/transfers/{transfer}/reversals",
	},
	MovesMoney: true,
	// suite: "create the rest". The record's literal for "TestTransferReversalAttributes/leaves/
	// request/PostTransfersIdReversals/amount" is what its transfer held.
	Defaults:     map[string]string{"amount": "what remains of the transfer"},
	Delete:       engine.Refuse,
	RefuseReason: "Stripe cannot delete or undo a transfer reversal.",
}

// NewResource returns the resource constructor the provider registers.
func NewResource() (func() resource.Resource, error) {
	return engine.NewResource(Declaration, transfer_reversal.Table)
}

// NewDataSource returns the data source constructor. It needs the `transfer` and the `id` of the reversal. Stripe documents no reversal field as unique.
func NewDataSource() (func() datasource.DataSource, error) {
	return engine.NewDataSource(Declaration, transfer_reversal.Table)
}
