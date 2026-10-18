// Package credit_note declares the stripe_credit_note data source: a reduction of an invoice's amount. There is no resource.
package credit_note

import (
	"github.com/hashicorp/terraform-plugin-framework/datasource"

	"github.com/computomatic/terraform-provider-stripe/internal/engine"
	"github.com/computomatic/terraform-provider-stripe/internal/generated/credit_note"
)

// Declaration is what the spec cannot say about a credit note. A credit note is issued
// against an invoice and cannot be deleted or changed into another thing, only voided, so
// only the data source exists.
var Declaration = engine.Declaration{
	TypeName:    "stripe_credit_note",
	IDPrefix:    "cn_",
	Description: "A reduction of an invoice's amount. Look one up by its `id`.",
	Paths:       engine.Paths{Retrieve: "/v1/credit_notes/{id}"},
	Delete:      engine.NoDelete,
}

// NewDataSource returns the data source constructor the provider registers. It looks a credit note up by `id` only.
func NewDataSource() (func() datasource.DataSource, error) {
	return engine.NewDataSource(Declaration, credit_note.Table)
}
