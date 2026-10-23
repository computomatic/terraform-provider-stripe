// Package credit_note declares the stripe_credit_note resource and data source.
package credit_note

import (
	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/resource"

	"github.com/computomatic/terraform-provider-stripe/internal/engine"
	"github.com/computomatic/terraform-provider-stripe/internal/generated/credit_note"
)

// Declaration is what the spec cannot say about a credit note (ADR
// 2026-10-22-money-moving-resources, D6.5). Whether a note refunds money depends on its
// amounts, so the live-mode guard applies to every one. Stripe cannot delete a note, so
// destroy is refused. `memo` and `metadata` update, on a void note too (suite: "update",
// "update a voided note"); the spec's update takes nothing else. `status` voids an issued note
// (suite: "void", "already void").
var Declaration = engine.Declaration{
	TypeName:              "stripe_credit_note",
	IDPrefix:              "cn_",
	Description:           "A reduction of a finalized invoice's amount, which can credit the customer's balance or refund a payment. Stripe refuses a credit note on a draft invoice. Set `status` to `void` to void an issued credit note, which gives the invoice back the amount it credited; an invoice cannot be voided while an issued credit note remains. Only `memo` and `metadata` change after creation.",
	DataSourceDescription: "A reduction of an invoice's amount.",
	Paths: engine.Paths{
		Create:   "/v1/credit_notes",
		Retrieve: "/v1/credit_notes/{id}",
		Update:   "/v1/credit_notes/{id}",
		List:     "/v1/credit_notes",
	},
	MovesMoney: true,
	// Stripe fills a line's decimal amount in from `unit_amount` (suite: "setup note").
	ServerDefaulted: []string{"lines.unit_amount_decimal"},
	Delete:          engine.Refuse,
	RefuseReason:    "Stripe cannot delete a credit note; set `status` to `void` to void one.",
	Transitions: []engine.Transition{
		{Field: "status", Value: "void", Path: "/v1/credit_notes/{id}/void"},
	},
}

// NewResource returns the resource constructor the provider registers.
func NewResource() (func() resource.Resource, error) {
	return engine.NewResource(Declaration, credit_note.Table)
}

// NewDataSource returns the data source constructor. It looks a credit note up by `id` only.
func NewDataSource() (func() datasource.DataSource, error) {
	return engine.NewDataSource(Declaration, credit_note.Table)
}
