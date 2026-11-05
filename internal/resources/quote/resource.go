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
	ImportNote:            "An imported quote names each of its discounts, and each of its lines' discounts, by its ID in `discount`, so a configuration that names one by `coupon` or `promotion_code` plans an update that applies it again. Name each discount of an imported quote by `discount`.",
	Paths: engine.Paths{
		Create:   "/v1/quotes",
		Retrieve: "/v1/quotes/{id}",
		Update:   "/v1/quotes/{id}",
		List:     "/v1/quotes",
	},
	// Stripe returns the line items only when asked for. The engine also expands the quote's
	// `discounts` and its lines' (`line_items.data.discounts`), each of which a line holds whole,
	// and maps them back by what each names.
	Expand: []string{"line_items"},
	// A quote created from another one takes its customer from the source.
	ServerDefaulted: []string{"customer"},
	// Stripe keeps the members of `invoice_settings` an update leaves out (family `v1-billing`,
	// `hashes`), so an update sends only those that changed; `issuer` requires its `type` and
	// goes whole.
	SendChangedMembers: []string{"invoice_settings"},
	Answers: map[string]string{
		// suite: TestQuoteAttributes, probes `isolated/coupon_expired` and `isolated/coupon_beside_held`
		"discounts.coupon": "Stripe takes a coupon that has not expired, and on an update not in the same element as a `discount`.",
		// suite: TestQuoteAttributes, probe `isolated/promotion_code_inactive`
		"discounts.promotion_code": "Stripe takes an active promotion code.",
		// suite: TestQuoteAttributes, probes `isolated/line_coupon_expired` and `isolated/line_coupon_beside_held`
		"line_items.discounts.coupon": "Stripe takes a coupon that has not expired, and on an update not in the same element as a `discount`.",
		// suite: TestQuoteAttributes, probes `isolated/line_discount_held` and `isolated/line_discount_of_quote`
		"line_items.discounts.discount": "On an update Stripe takes a discount the line already holds, not one the quote holds.",
		// suite: TestQuoteAttributes, probe `isolated/line_promotion_code_inactive`
		"line_items.discounts.promotion_code": "Stripe takes an active promotion code.",
	},
	Delete:       engine.Refuse,
	RefuseReason: "Stripe cannot delete a quote; set `status` to `canceled` to cancel one.",
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
