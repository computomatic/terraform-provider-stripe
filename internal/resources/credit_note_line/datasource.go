// Package credit_note_line declares the stripe_credit_note_line data source. There is no resource.
package credit_note_line

import (
	"github.com/hashicorp/terraform-plugin-framework/datasource"

	"github.com/computomatic/terraform-provider-stripe/internal/engine"
	"github.com/computomatic/terraform-provider-stripe/internal/generated/credit_note_line"
)

// Declaration is what the spec cannot say about this object. Stripe has no retrieve for a line, only the list under its credit note, so the data source finds the line by `id` in that list. Lines are made by the credit note.
var Declaration = engine.Declaration{
	TypeName:    "stripe_credit_note_line",
	IDPrefix:    "cnli_",
	Description: "A line of a credit note. Look one up by the `credit_note` and the line's `id`.",
	Parent:      "credit_note",
	FindInList:  true,
	Paths:       engine.Paths{List: "/v1/credit_notes/{credit_note}/lines"},
	Delete:      engine.NoDelete,
}

// NewDataSource returns the data source constructor the provider registers. It needs the `credit_note` and the `id` of the line, which it finds in the credit note's list.
func NewDataSource() (func() datasource.DataSource, error) {
	return engine.NewDataSource(Declaration, credit_note_line.Table)
}
