// Package invoice declares the stripe_invoice resource and data source.
package invoice

import (
	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/resource"

	"github.com/computomatic/terraform-provider-stripe/internal/engine"
	"github.com/computomatic/terraform-provider-stripe/internal/generated/invoice"
	"github.com/computomatic/terraform-provider-stripe/internal/wire"
)

// Declaration is what the spec cannot say about an invoice (ADR
// 2026-10-22-money-moving-resources, D6.4). Paying one moves money, so the live-mode guard
// applies. Stripe deletes a draft, and the items it swept up with it, and refuses to delete a
// finalized one (suite: "delete a draft", "delete a draft with items", "delete errors"), so
// destroy deletes a draft and is refused for any other invoice.
//
// A draft updates the fields the suite proves ("update a draft", "update a draft's collection
// method"); once finalized only `metadata` is proven ("update an open invoice's metadata"),
// and Stripe refuses the rest ("update refusals once finalized"), which the apply reports. The
// spec's other update fields are not proven, so they replace a draft and fail the plan of any
// other invoice.
//
// `status` finalizes, pays, voids or marks uncollectible (suite: "finalize", "pay with no
// parameters", "void", "void an uncollectible invoice", "mark uncollectible", and their
// errors). Paying a draft finalizes it first; Stripe would do both in one call ("pay a draft
// finalizes it"), which the suite proves only out of band, so the resource finalizes and then
// pays. A pay sends no parameters, so it charges the invoice's `default_payment_method`.
// Sending an invoice leaves its status as it is, so it is not a status.
var Declaration = engine.Declaration{
	TypeName:              "stripe_invoice",
	IDPrefix:              "in_",
	Description:           "A bill for a customer. With `pending_invoice_items_behavior = \"include\"` it takes the customer's pending invoice items when it is created. `terraform destroy` deletes a draft invoice, and Stripe deletes the invoice items on it too. Set `status` to `open` to finalize a draft, to `paid` to finalize it if need be and charge its `default_payment_method`, to `void` to void an open or uncollectible invoice, or to `uncollectible` to mark an open one uncollectible. With `auto_advance`, Stripe finalizes a draft and collects payment by itself. Once an invoice is finalized Stripe refuses changes to most arguments, but `metadata` still changes. The invoice's `last_finalization_error` is not available.",
	DataSourceDescription: "A bill sent to a customer, such as the `latest_invoice` of a subscription. The invoice's `last_finalization_error` is not available.",
	ImportNote:            "Stripe does not return `days_until_due`, `pending_invoice_items_behavior`, `application_fee_amount`, `subscription` or `transfer_data`, so an imported invoice has them null. Leave them out of its configuration: setting one is a change, which Stripe refuses once the invoice is finalized.",
	Paths: engine.Paths{
		Create:   "/v1/invoices",
		Retrieve: "/v1/invoices/{id}",
		Update:   "/v1/invoices/{id}",
		Delete:   "/v1/invoices/{id}",
		List:     "/v1/invoices",
	},
	MovesMoney: true,
	// The confirmation secret is the client secret of the invoice's payment.
	Sensitive: []string{"confirmation_secret.client_secret"},
	// Stripe numbers an invoice, dates it and sets when it is due when it finalizes it
	// (suite: "finalize"), and schedules a draft's finalization when it auto-advances. It
	// fills in `rendering` when the request leaves it out (suite: "draft produced by a pending
	// invoice item").
	ServerDefaulted: []string{"automatically_finalizes_at", "due_date", "effective_at", "number", "rendering"},
	// The spec's update takes these, which the suite has not proven.
	ReplaceOn: []string{
		"account_tax_ids", "application_fee_amount", "automatic_tax", "default_source", "default_tax_rates", "issuer",
		"on_behalf_of", "payment_settings", "rendering", "shipping_cost", "shipping_details", "transfer_data",
	},
	// The spec's update takes these too, unproven, but Stripe sets them when it finalizes the
	// invoice, which an apply can do along with another change, so they cannot keep their
	// prior value as a field that replaces does.
	RefuseChange: []string{"automatically_finalizes_at", "effective_at", "number"},
	Delete:       engine.Delete,
	RefuseWhen: &engine.RefuseWhen{
		Field:  "status",
		Value:  wire.String("draft"),
		Except: true,
		Reason: "Stripe deletes only a draft invoice; a finalized one stays in the account.",
	},
	Transitions: []engine.Transition{
		{Field: "status", Value: "open", Path: "/v1/invoices/{id}/finalize", From: []string{"draft"}, Final: []string{"paid", "void", "uncollectible"}},
		{Field: "status", Value: "paid", Path: "/v1/invoices/{id}/finalize", From: []string{"draft"}, Final: []string{"void"}},
		{Field: "status", Value: "paid", Path: "/v1/invoices/{id}/pay", From: []string{"open"}, Final: []string{"void"}},
		{Field: "status", Value: "void", Path: "/v1/invoices/{id}/void", From: []string{"open", "uncollectible"}, Final: []string{"paid"}},
		{Field: "status", Value: "uncollectible", Path: "/v1/invoices/{id}/mark_uncollectible", From: []string{"open"}, Final: []string{"paid", "void"}},
	},
}

// NewResource returns the resource constructor the provider registers.
func NewResource() (func() resource.Resource, error) {
	return engine.NewResource(Declaration, invoice.Table)
}

// NewDataSource returns the data source constructor. It looks an invoice up by `id` only: the list endpoint does not filter by the invoice `number`.
func NewDataSource() (func() datasource.DataSource, error) {
	return engine.NewDataSource(Declaration, invoice.Table)
}
