// Package charge declares the stripe_charge resource and data source.
package charge

import (
	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/resource"

	"github.com/computomatic/terraform-provider-stripe/internal/engine"
	"github.com/computomatic/terraform-provider-stripe/internal/generated/charge"
)

// Declaration is what the spec cannot say about a charge (ADR 2026-10-22-money-moving-resources,
// D6.2). The resource makes a direct charge of a source, which the spec deprecates but Stripe
// serves (suite: "create", "create uncaptured"). It moves money, so the live-mode guard
// applies. `captured` captures an uncaptured charge in full (suite: "capture the full amount",
// "capturing twice"). `description`, `metadata`, `receipt_email`, `shipping` and
// `fraud_details.user_report` are proven to update (TestChargeAttributes); Stripe refuses `amount`
// ("immutable amount"). Stripe cannot delete a charge, so destroy is refused.
var Declaration = engine.Declaration{
	TypeName:              "stripe_charge",
	IDPrefix:              "ch_",
	Description:           "A direct charge of a payment source, such as Stripe's test token `tok_visa`. Stripe's API deprecates direct charges in favour of payment intents, and a `stripe_payment_intent` makes a charge too. With `capture = false` the charge only authorizes the payment; set `captured = true` to capture it in full. Stripe releases an authorization that is not captured within 7 days. Only `description`, `metadata`, `receipt_email`, `shipping` and `fraud_details.user_report` change after creation.",
	DataSourceDescription: "A single attempt to move money from a customer.",
	ImportNote:            "Stripe does not return `source` as it was sent, or `capture`, so an imported charge has them null. Leave them out of its configuration: changing either needs a new charge.",
	Paths: engine.Paths{
		Create:   "/v1/charges",
		Retrieve: "/v1/charges/{id}",
		Update:   "/v1/charges/{id}",
		List:     "/v1/charges",
	},
	MovesMoney: true,
	// Stripe copies `statement_descriptor` into the suffix a charge leaves out (suite: the rule of
	// `statement_descriptor_suffix`, variant `descriptor`), and names a transfer group for a charge
	// with `transfer_data` (variants `destination` and `transfer_amount`).
	ServerDefaulted: []string{"statement_descriptor_suffix", "transfer_group"},
	Defaults: map[string]string{
		// suite: TestChargeAttributes, variant `descriptor`
		"statement_descriptor_suffix": "none, or the `statement_descriptor` the charge sends",
		// suite: TestChargeAttributes, variants `destination` and `transfer_amount`
		"transfer_group": "none, or a `group_` ID Stripe makes for a charge with `transfer_data`",
	},
	// Stripe refuses a change of these once they are set (suite:
	// TestChargeAttributes/leaves/request/PostChargesCharge/customer and transfer_group).
	ReplaceOn:    []string{"customer", "transfer_group"},
	Delete:       engine.Refuse,
	RefuseReason: "Stripe cannot delete a charge; refund it with a `stripe_refund`.",
	Transitions: []engine.Transition{
		{Field: "captured", Value: "true", Path: "/v1/charges/{id}/capture"},
	},
}

// NewResource returns the resource constructor the provider registers.
func NewResource() (func() resource.Resource, error) {
	return engine.NewResource(Declaration, charge.Table)
}

// NewDataSource returns the data source constructor. It looks the object up by `id` only: Stripe documents no field of it as unique.
func NewDataSource() (func() datasource.DataSource, error) {
	return engine.NewDataSource(Declaration, charge.Table)
}
