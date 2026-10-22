// Package quote declares the stripe_quote resource and data source.
package quote

import (
	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/resource"

	"github.com/computomatic/terraform-provider-stripe/internal/engine"
	"github.com/computomatic/terraform-provider-stripe/internal/generated/quote"
	"github.com/computomatic/terraform-provider-stripe/internal/wire"
)

// Declaration is what the spec cannot say about a quote. Stripe never deletes one, so
// destroy cancels it (suite: "Quotes", draft and open quotes cancel) and drops it from
// state. An accepted quote cannot be cancelled (`quote_cancellation_already_accepted`) and
// has become an invoice or subscription, so destroy leaves it as it is.
var Declaration = engine.Declaration{
	TypeName:    "stripe_quote",
	IDPrefix:    "qt_",
	Description: "A proposal of prices to a customer. `terraform destroy` cancels the quote, because Stripe cannot delete one; the cancelled quote stays in the account. A quote that was accepted cannot be cancelled and is left as it is. Finalizing and accepting a quote are not resources: the quote stays a draft here, and `status` reports what happened to it elsewhere. Stripe lets only `metadata` and a few other fields change once the quote is finalized.",
	Paths: engine.Paths{
		Create:   "/v1/quotes",
		Retrieve: "/v1/quotes/{id}",
		Update:   "/v1/quotes/{id}",
		List:     "/v1/quotes",
	},
	// Stripe returns the line items only when asked for.
	Expand: []string{"line_items"},
	// A quote created from another one takes its customer from the source.
	ServerDefaulted: []string{"customer"},
	Delete:          engine.Archive,
	Archive: &engine.ArchiveAction{
		Field: "status",
		Value: wire.String("canceled"),
		Path:  "/v1/quotes/{id}/cancel",
		Ended: []engine.Ended{{Field: "status", Value: wire.String("accepted")}},
	},
}

// NewResource returns the resource constructor the provider registers.
func NewResource() (func() resource.Resource, error) {
	return engine.NewResource(Declaration, quote.Table)
}

// NewDataSource returns the data source constructor. Stripe documents no quote field as unique, so the data source looks up by `id` only.
func NewDataSource() (func() datasource.DataSource, error) {
	return engine.NewDataSource(Declaration, quote.Table)
}
