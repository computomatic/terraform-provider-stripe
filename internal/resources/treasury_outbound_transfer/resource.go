// Package treasury_outbound_transfer declares the stripe_treasury_outbound_transfer resource
// and data source.
package treasury_outbound_transfer

import (
	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/resource"

	"github.com/computomatic/terraform-provider-stripe/internal/engine"
	"github.com/computomatic/terraform-provider-stripe/internal/generated/treasury_outbound_transfer"
)

// Declaration is what the spec cannot say about an outbound transfer. It moves money, so the
// live-mode guard applies. It sends money out of a Treasury financial account on a connected
// account to another financial account of that account, with `intra_stripe_flows` active
// (suite: "create to a financial account"), or to a US bank account attached to the account
// itself, with `outbound_transfers.ach` active ("to the account's own bank account"). Stripe
// refuses inline bank account data. Stripe posts a transfer to a financial account at once and
// one to a bank account within about a second ("posts itself"), so `status` reads `posted` and
// a cancel is refused ("cancel is refused"); the engine keeps the status Stripe reports and
// explains the refusal. Stripe has no update, so every argument is create-only, and returns
// neither `destination_payment_method_data` nor `destination_payment_method_options`. Stripe
// cannot delete one, so destroy is refused.
var Declaration = engine.Declaration{
	TypeName: "stripe_treasury_outbound_transfer",
	IDPrefix: "obt_",
	Description: "A transfer of money out of a Treasury financial account to another financial account of the same connected account, given with `destination_payment_method_data`, or to a US bank account attached to the connected account itself, which `destination_payment_method` names. " +
		"Treasury runs on a platform's connected accounts, so manage outbound transfers through a provider configuration whose `stripe_account` is the connected account. " +
		"The financial account needs its `intra_stripe_flows` feature active to send to a financial account, and `outbound_transfers.ach` to send to a bank account. See `stripe_treasury_inbound_transfer` for how a bank account is attached. " +
		"Stripe posts a transfer to a financial account at once, and one to a bank account within about a second, and `status` then reads `posted`. " +
		"Set `status` to `canceled` to cancel a transfer that is still `processing` and that Stripe reports `cancelable`. Stripe refuses to cancel one that has posted: a plan that sets `canceled` then fails, and an apply that finds the transfer posted fails with Stripe's answer. Nothing else changes after creation.",
	DataSourceDescription: "A transfer of money out of a Treasury financial account to another financial account or a bank account the connected account owns. It lives on a connected account, so read it through a provider configuration whose `stripe_account` is that account. Look one up by its `id`.",
	ImportNote:            "Stripe does not return `destination_payment_method_data` or `destination_payment_method_options`, so an imported transfer has neither in state. Add `lifecycle { ignore_changes = [destination_payment_method_data, destination_payment_method_options] }` to a configuration that sets them, because a plan that changes them fails.",
	ImportNoteLevel:       engine.NoteWarning,
	Paths: engine.Paths{
		Create:   "/v1/treasury/outbound_transfers",
		Retrieve: "/v1/treasury/outbound_transfers/{id}",
		List:     "/v1/treasury/outbound_transfers",
	},
	MovesMoney:   true,
	Delete:       engine.Refuse,
	RefuseReason: "Stripe cannot delete an outbound transfer; set `status` to `canceled` to cancel one that is still processing.",
	Transitions: []engine.Transition{
		{Field: "status", Value: "canceled", Path: "/v1/treasury/outbound_transfers/{id}/cancel", From: []string{"processing"}, Final: []string{"posted", "failed", "returned"}},
	},
}

// NewResource returns the resource constructor the provider registers.
func NewResource() (func() resource.Resource, error) {
	return engine.NewResource(Declaration, treasury_outbound_transfer.Table)
}

// NewDataSource returns the data source constructor. It looks the object up by `id` only: Stripe documents no field of it as unique.
func NewDataSource() (func() datasource.DataSource, error) {
	return engine.NewDataSource(Declaration, treasury_outbound_transfer.Table)
}
