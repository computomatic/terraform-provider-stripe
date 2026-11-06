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
// A draft updates every field the suite proves (TestInvoiceAttributes, probes `update/1`,
// `enums/35` to `enums/43`); once finalized only `metadata` is proven ("update an open invoice's
// metadata"), and Stripe refuses the rest ("update refusals once finalized"), which the apply
// reports.
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
	Description:           "A bill for a customer. With `pending_invoice_items_behavior = \"include\"` it takes the customer's pending invoice items when it is created. `terraform destroy` deletes a draft invoice, and Stripe deletes the invoice items on it too. Set `status` to `open` to finalize a draft, to `paid` to finalize it if need be and charge its `default_payment_method`, to `void` to void an open or uncollectible invoice, or to `uncollectible` to mark an open one uncollectible. With `auto_advance`, Stripe finalizes a draft and collects payment by itself. Once an invoice is finalized Stripe refuses changes to most arguments, but `metadata` still changes. Finalizing turns a `rendering.pdf.page_size` of `auto` into the page size Stripe picks, such as `letter`, so set `a4` or `letter` on an invoice you finalize, or leave `rendering` unset. A change that Stripe cannot make in place, such as removing `shipping_details`, replaces a draft and fails the plan of a finalized invoice.",
	DataSourceDescription: "A bill sent to a customer, such as the `latest_invoice` of a subscription.",
	ImportNote:            "Stripe does not return `days_until_due`, `pending_invoice_items_behavior` or `subscription`, so an imported invoice has them null. Leave them out of its configuration: setting one is a change, which Stripe refuses once the invoice is finalized. An imported invoice names each of its discounts by its ID in `discount`, so a configuration that names one by `coupon` or `promotion_code` plans an update that applies it again, which Stripe refuses once the invoice is finalized. Name each discount of an imported invoice by `discount`.",
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
	// invoice item"). A card's options that leave out `installments.enabled` and
	// `request_three_d_secure` get `false` and `automatic` (suite: TestInvoiceAttributes, probes
	// `enums/12` and `nested_minimal/6`), and `shipping_rate_data` makes the shipping rate that
	// `shipping_cost.shipping_rate` names (probe `enums/13`). A revision copies the revised
	// invoice's `metadata` (minimal create `revision`), which the default says, but the engine
	// keeps a top-level `metadata` Optional alone, so that removing it clears it.
	ServerDefaulted: []string{
		"automatically_finalizes_at", "due_date", "effective_at", "number", "rendering",
		"payment_settings.payment_method_options.card.installments.enabled",
		"payment_settings.payment_method_options.card.request_three_d_secure",
		"shipping_cost.shipping_rate",
	},
	Answers: map[string]string{
		// suite: TestInvoiceAttributes, probes `isolated/coupon_expired` and `isolated/coupon_beside_held`
		"discounts.coupon": "Stripe takes a coupon that has not expired, and on an update not in the same element as a `discount`.",
		// suite: TestInvoiceAttributes, probe `isolated/promotion_code_inactive`
		"discounts.promotion_code": "Stripe takes an active promotion code.",
	},
	Defaults: map[string]string{
		// suite: TestInvoiceAttributes, minimal create `revision`
		"metadata": "none, or on a revision the metadata of the invoice `from_invoice` revises, such as `{\"conformance\":\"invoice\"}` in the suite",
		// suite: TestInvoiceAttributes, probe `enums/12`
		"payment_settings.payment_method_options.card.installments.enabled": "none, or `false` where `installments` is sent without it",
		// suite: TestInvoiceAttributes, probe `nested_minimal/6`
		"payment_settings.payment_method_options.card.request_three_d_secure": "none, or `automatic` where `card` is sent without it",
		// suite: TestInvoiceAttributes, probe `enums/13`
		"shipping_cost.shipping_rate": "none, or the shipping rate Stripe makes from `shipping_rate_data`",
	},
	// Finalizing resolves a page size of `auto` to a size of its own, numbers the invoice, dates
	// it and sets when it is due from `days_until_due` (suite: "finalize", "finalize resolves a
	// page size set to auto"), and a change of `days_until_due` moves the due date (suite:
	// TestInvoiceAttributes, probe `update/15`), so a change of either leaves those unknown,
	// which E5 would otherwise keep from the state.
	FilledFrom: map[string][]string{
		"status":         {"rendering", "number", "effective_at", "due_date"},
		"days_until_due": {"due_date"},
	},
	// Stripe sorts the lists of a US bank account's Financial Connections (the record's
	// `normalised` form `sorted`: a create's `permissions` and an update's
	// `account_subcategories` and `prefetch`, TestInvoiceAttributes, probe `update/11`), so the
	// configured order stays in state, as a subscription's does.
	PreserveConfig: []string{
		"payment_settings.payment_method_options.us_bank_account.financial_connections.filters.account_subcategories",
		"payment_settings.payment_method_options.us_bank_account.financial_connections.permissions",
		"payment_settings.payment_method_options.us_bank_account.financial_connections.prefetch",
	},
	// Stripe answers an empty string for these with `cannot be unset`, or the record shows no
	// unset of them and the fake refuses one as unproven (TestInvoiceAttributes, probes `unset/*`),
	// so a removal replaces a draft and fails the plan of any other invoice.
	ReplaceOnRemoval: []string{
		"account_tax_ids", "shipping_cost", "shipping_details",
		"payment_settings.payment_method_options", "payment_settings.payment_method_types",
		"payment_settings.payment_method_options.acss_debit", "payment_settings.payment_method_options.acss_debit.mandate_options",
		"payment_settings.payment_method_options.bancontact", "payment_settings.payment_method_options.billie",
		"payment_settings.payment_method_options.billie.company_details",
		"payment_settings.payment_method_options.billie.company_details.registered_address",
		"payment_settings.payment_method_options.card", "payment_settings.payment_method_options.card.installments",
		"payment_settings.payment_method_options.customer_balance",
		"payment_settings.payment_method_options.customer_balance.bank_transfer",
		"payment_settings.payment_method_options.customer_balance.bank_transfer.eu_bank_transfer",
		"payment_settings.payment_method_options.payto", "payment_settings.payment_method_options.payto.mandate_options",
		"payment_settings.payment_method_options.pix", "payment_settings.payment_method_options.upi",
		"payment_settings.payment_method_options.upi.mandate_options", "payment_settings.payment_method_options.us_bank_account",
		"payment_settings.payment_method_options.us_bank_account.financial_connections",
		"payment_settings.payment_method_options.us_bank_account.financial_connections.filters",
		"payment_settings.payment_method_options.us_bank_account.financial_connections.filters.account_subcategories",
		"payment_settings.payment_method_options.us_bank_account.financial_connections.permissions",
		"payment_settings.payment_method_options.us_bank_account.financial_connections.prefetch",
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
