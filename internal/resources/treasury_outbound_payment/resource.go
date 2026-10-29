// Package treasury_outbound_payment declares the stripe_treasury_outbound_payment resource and
// data source.
package treasury_outbound_payment

import (
	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/resource"

	"github.com/computomatic/terraform-provider-stripe/internal/engine"
	"github.com/computomatic/terraform-provider-stripe/internal/generated/treasury_outbound_payment"
)

// Declaration is what the spec cannot say about an outbound payment. It moves money, so the
// live-mode guard applies. It pays out of a Treasury financial account on a connected account,
// with `outbound_payments.ach` active, to a third party's US bank account given inline, which
// needs no customer (suite: "create to inline us_bank_account data"); Stripe refuses a financial
// account of the same connected account ("create to inline financial_account data of the same
// account"). Stripe posts a new payment by itself within about a second, after which it refuses
// a cancel, and a cancel sent at once races the posting (suite: "cancel"); the engine keeps the
// status Stripe reports and explains the refusal. Stripe fills in `end_user_details` when the
// request leaves it out. Stripe has no update, so every argument is create-only, and returns
// neither `destination_payment_method_data`, whose account number is sensitive, nor
// `destination_payment_method_options`. Stripe cannot delete one, so destroy is refused.
var Declaration = engine.Declaration{
	TypeName: "stripe_treasury_outbound_payment",
	IDPrefix: "obp_",
	Description: "A payment of money out of a Treasury financial account to a third party, such as a US bank account given with `destination_payment_method_data`. " +
		"Treasury runs on a platform's connected accounts, so manage outbound payments through a provider configuration whose `stripe_account` is the connected account. " +
		"The financial account needs its `outbound_payments.ach` feature active. Stripe refuses a payment to a financial account of the same connected account; use `stripe_treasury_outbound_transfer` for that. " +
		"Stripe posts a new payment by itself within about a second, and `status` then reads `posted`. " +
		"Set `status` to `canceled` to cancel a payment that is still `processing`. Stripe refuses to cancel one that has posted: a plan that sets `canceled` then fails, and an apply that finds the payment posted fails with Stripe's answer. Nothing else changes after creation.",
	DataSourceDescription: "A payment of money out of a Treasury financial account to a third party. It lives on a connected account, so read it through a provider configuration whose `stripe_account` is that account. Look one up by its `id`.",
	ImportNote:            "Stripe does not return `destination_payment_method_data` or `destination_payment_method_options`, so an imported payment has neither in state. Add `lifecycle { ignore_changes = [destination_payment_method_data, destination_payment_method_options] }` to a configuration that sets them, because a plan that changes them fails.",
	ImportNoteLevel:       engine.NoteWarning,
	Paths: engine.Paths{
		Create:   "/v1/treasury/outbound_payments",
		Retrieve: "/v1/treasury/outbound_payments/{id}",
		List:     "/v1/treasury/outbound_payments",
	},
	MovesMoney:      true,
	Delete:          engine.Refuse,
	RefuseReason:    "Stripe cannot delete an outbound payment; set `status` to `canceled` to cancel one that is still processing.",
	ServerDefaulted: []string{"end_user_details"},
	Sensitive:       []string{"destination_payment_method_data.us_bank_account.account_number"},
	Transitions: []engine.Transition{
		{Field: "status", Value: "canceled", Path: "/v1/treasury/outbound_payments/{id}/cancel", From: []string{"processing"}, Final: []string{"posted", "failed", "returned"}},
	},
}

// NewResource returns the resource constructor the provider registers.
func NewResource() (func() resource.Resource, error) {
	return engine.NewResource(Declaration, treasury_outbound_payment.Table)
}

// NewDataSource returns the data source constructor. It looks the object up by `id` only: Stripe documents no field of it as unique.
func NewDataSource() (func() datasource.DataSource, error) {
	return engine.NewDataSource(Declaration, treasury_outbound_payment.Table)
}
