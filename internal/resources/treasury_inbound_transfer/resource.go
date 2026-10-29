// Package treasury_inbound_transfer declares the stripe_treasury_inbound_transfer resource and
// data source.
package treasury_inbound_transfer

import (
	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/resource"

	"github.com/computomatic/terraform-provider-stripe/internal/engine"
	"github.com/computomatic/terraform-provider-stripe/internal/generated/treasury_inbound_transfer"
)

// Declaration is what the spec cannot say about an inbound transfer. It moves money, so the
// live-mode guard applies. It lives in a Treasury financial account on a connected account and
// pulls from a US bank account payment method attached to that account itself by a setup intent
// with `attach_to_self`, with the financial account's `inbound_transfers.ach` feature active
// (suite: "from the account's own bank account"). The provider has no resource for that attach,
// and Stripe refuses the setup intent on a Custom connected account to platforms it considers
// new (suite: "Direct charges on a Custom connected account differ between sandboxes"). Stripe
// has no update, so every argument is create-only. `status` cancels a processing transfer
// (suite: "cancel"); Stripe refuses a cancel once the transfer has succeeded ("cancel after
// succeed"), and does not move one by itself. Stripe cannot delete one, so destroy is refused.
var Declaration = engine.Declaration{
	TypeName: "stripe_treasury_inbound_transfer",
	IDPrefix: "ibt_",
	Description: "A transfer of money into a Treasury financial account from a US bank account attached to the connected account itself, which `origin_payment_method` names. " +
		"Treasury runs on a platform's connected accounts, so manage inbound transfers through a provider configuration whose `stripe_account` is the connected account. " +
		"The financial account needs its `inbound_transfers.ach` feature active. " +
		"The provider does not attach the bank account: attach it in the Dashboard, or with a setup intent on the connected account that sets `attach_to_self` and `flow_directions = [\"inbound\"]`. " +
		"Stripe refuses that setup intent on a Custom or Express connected account to platforms it considers new (\"Creating direct charges with type=express or type=custom is not supported for new platforms\"), and links migration and exemption instructions in the platform's Dashboard, so such a platform may be unable to make an inbound transfer. " +
		"Set `status` to `canceled` to cancel a transfer that is still `processing`. Stripe refuses to cancel one that has succeeded or failed, so a plan that sets `canceled` then fails. Nothing else changes after creation.",
	DataSourceDescription: "A transfer of money into a Treasury financial account from a bank account the connected account owns. It lives on a connected account, so read it through a provider configuration whose `stripe_account` is that account. Look one up by its `id`.",
	Paths: engine.Paths{
		Create:   "/v1/treasury/inbound_transfers",
		Retrieve: "/v1/treasury/inbound_transfers/{id}",
		List:     "/v1/treasury/inbound_transfers",
	},
	MovesMoney:   true,
	Delete:       engine.Refuse,
	RefuseReason: "Stripe cannot delete an inbound transfer; set `status` to `canceled` to cancel one that is still processing.",
	Transitions: []engine.Transition{
		{Field: "status", Value: "canceled", Path: "/v1/treasury/inbound_transfers/{id}/cancel", From: []string{"processing"}, Final: []string{"succeeded", "failed"}},
	},
}

// NewResource returns the resource constructor the provider registers.
func NewResource() (func() resource.Resource, error) {
	return engine.NewResource(Declaration, treasury_inbound_transfer.Table)
}

// NewDataSource returns the data source constructor. It looks the object up by `id` only: Stripe documents no field of it as unique.
func NewDataSource() (func() datasource.DataSource, error) {
	return engine.NewDataSource(Declaration, treasury_inbound_transfer.Table)
}
