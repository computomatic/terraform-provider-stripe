// Package quote declares the stripe_quote resource and data source.
package quote

import (
	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/resource"

	"github.com/computomatic/terraform-provider-stripe/internal/engine"
	"github.com/computomatic/terraform-provider-stripe/internal/generated/quote"
)

// Declaration is what the spec cannot say about a quote. Stripe never deletes one, and
// cancelling one is final (suite: "Quotes", "already canceled"), so destroy is refused.
// Setting `status` to `canceled` cancels a draft or open quote (suite: "cancel"); an accepted
// quote cannot be cancelled (`quote_cancellation_already_accepted`).
var Declaration = engine.Declaration{
	TypeName:              "stripe_quote",
	IDPrefix:              "qt_",
	Description:           "A proposal of prices to a customer. Set `status` to `canceled` to cancel the quote; a quote that was accepted cannot be cancelled. Finalizing and accepting a quote are not resources: the quote stays a draft here, and `status` reports what happened to it elsewhere. Stripe lets only `metadata` and a few other fields change once the quote is finalized. With `collection_method = \"send_invoice\"` the customer needs an email address, or Stripe refuses the create.",
	DataSourceDescription: "A proposal of prices to a customer.",
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
	Delete:          engine.Refuse,
	RefuseReason:    "Stripe cannot delete a quote; set `status` to `canceled` to cancel one.",
	// An accepted quote cannot be cancelled (suite: "cancel once accepted").
	Transitions: []engine.Transition{{Field: "status", Value: "canceled", Path: "/v1/quotes/{id}/cancel", Final: []string{"accepted"}}},
}

// NewResource returns the resource constructor the provider registers.
func NewResource() (func() resource.Resource, error) {
	return engine.NewResource(Declaration, quote.Table)
}

// NewDataSource returns the data source constructor. Stripe documents no quote field as unique, so the data source looks up by `id` only.
func NewDataSource() (func() datasource.DataSource, error) {
	return engine.NewDataSource(Declaration, quote.Table)
}
