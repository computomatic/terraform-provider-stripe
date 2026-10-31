// Package payment_intent declares the stripe_payment_intent resource and data source.
package payment_intent

import (
	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/resource"

	"github.com/computomatic/terraform-provider-stripe/internal/engine"
	"github.com/computomatic/terraform-provider-stripe/internal/generated/payment_intent"
)

// Declaration is what the spec cannot say about a payment intent (ADR
// 2026-10-22-money-moving-resources, D6.1). It moves money, so the live-mode guard applies.
// Stripe never deletes one, so destroy is refused. `status` confirms and captures it (suite:
// "confirm", "confirm after attaching payment method", "capture the full amount") or cancels it
// (suite: "cancel", "cancel an authorized intent releases the hold"); a succeeded intent cannot
// be canceled and a canceled one cannot succeed (suite: "already succeeded").
var Declaration = engine.Declaration{
	TypeName:              "stripe_payment_intent",
	IDPrefix:              "pi_",
	Description:           "A customer payment. Set `status` to `succeeded` to confirm the intent with its `payment_method` and, when `capture_method` is `manual`, to capture it in the same apply; set it to `canceled` to cancel one that has not succeeded. Confirming with a card that needs a further action, or that is declined, leaves the intent where Stripe put it and fails the apply. Setting `payment_method` moves the intent to `requires_confirmation`; a test payment method such as `pm_card_visa`, which Stripe turns into a new one, stays in the state as configured. Stripe changes `amount`, `currency` and `setup_future_usage` only before the intent is confirmed (`requires_payment_method`, `requires_confirmation` or `requires_action`), `statement_descriptor_suffix` also while it is `requires_capture`, and `capture_method` only while it is `requires_payment_method` or `requires_confirmation`; it refuses such a change in any other status, and the apply fails. `description`, `metadata` and `receipt_email` still change once it has succeeded. `transfer_group` is sent on creation only: Stripe refuses to change a transfer group once set, so a plan that sets or changes it on an existing payment intent fails, and removing it from the configuration keeps the current value. The `client_secret` is sensitive.",
	DataSourceDescription: "A customer payment, such as the one behind an invoice. The nested objects of its last payment error are not available.",
	Paths: engine.Paths{
		Create:   "/v1/payment_intents",
		Retrieve: "/v1/payment_intents/{id}",
		Update:   "/v1/payment_intents/{id}",
		List:     "/v1/payment_intents",
	},
	MovesMoney: true,
	// The client secret lets a front end finish the payment.
	Sensitive: []string{"client_secret"},
	// Stripe answers a test payment method such as `pm_card_visa` with a new payment method's
	// ID, so the configured value stays in state.
	PreserveConfig: []string{"payment_method"},
	// Stripe fills in `capture_method` and `confirmation_method` when the request leaves them
	// out (suite: "Payment intents", `automatic_async` and `automatic`). Stripe refuses to unset
	// `transfer_group` (suite: "transfer_group cannot be unset"), so it is computed too: removing
	// it from the configuration keeps the current value and sends nothing. Stripe also turns on
	// `automatic_payment_methods` on a create with no payment method types (the record's default
	// of `enabled`, true).
	ServerDefaulted: []string{"automatic_payment_methods", "capture_method", "confirmation_method", "transfer_group"},
	// Stripe answers `payment_method_data` with the method it made (the record's `returned`,
	// `inline_object`), which `payment_method` then leaves to it.
	MadeBy: map[string]string{"payment_method": "payment_method_data"},
	// Stripe refuses to change `transfer_group` once set (suite: "update transfer_group once
	// set"), and a payment intent cannot be replaced, so a change fails the plan. It is sent on
	// create only.
	RefuseChange: []string{"transfer_group"},
	// `customer_account` is verified only by Stripe's refusal of every value, on create and on
	// update, so a change stays refused.
	ReplaceOn:    []string{"customer_account"},
	Delete:       engine.Refuse,
	RefuseReason: "Stripe cannot delete a payment intent; set `status` to `canceled` to cancel one that has not succeeded.",
	Transitions: []engine.Transition{
		{Field: "status", Value: "succeeded", Path: "/v1/payment_intents/{id}/confirm", From: []string{"requires_confirmation"}, Final: []string{"canceled"}},
		{Field: "status", Value: "succeeded", Path: "/v1/payment_intents/{id}/capture", From: []string{"requires_capture"}, Final: []string{"canceled"}},
		{Field: "status", Value: "canceled", Path: "/v1/payment_intents/{id}/cancel", From: []string{"requires_payment_method", "requires_confirmation", "requires_action", "requires_capture"}, Final: []string{"succeeded"}},
	},
}

// NewResource returns the resource constructor the provider registers.
func NewResource() (func() resource.Resource, error) {
	return engine.NewResource(Declaration, payment_intent.Table)
}

// NewDataSource returns the data source constructor. It looks the object up by `id` only: Stripe documents no field of it as unique.
func NewDataSource() (func() datasource.DataSource, error) {
	return engine.NewDataSource(Declaration, payment_intent.Table)
}
