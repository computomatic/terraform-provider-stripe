// Package refund declares the stripe_refund resource and data source.
package refund

import (
	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/resource"

	"github.com/computomatic/terraform-provider-stripe/internal/engine"
	"github.com/computomatic/terraform-provider-stripe/internal/generated/refund"
)

// Declaration is what the spec cannot say about a refund (ADR
// 2026-10-22-money-moving-resources, D6.3). It moves money, so the live-mode guard applies.
// The resource refunds a payment intent or a charge, and Stripe fills in the other (suite:
// "create partial", "create the rest", and the same "by charge"). Only `metadata` changes
// (suite: "update metadata", "immutable amount", "immutable reason"). Stripe cannot delete or
// undo a refund, and a card refund refuses to be canceled (suite: "cancel"), so there is no
// lifecycle attribute and destroy is refused.
var Declaration = engine.Declaration{
	TypeName:              "stripe_refund",
	IDPrefix:              "re_",
	Description:           "A refund of a payment intent or a charge. Set one of `payment_intent` and `charge`; Stripe fills in the other when the charge belongs to a payment intent. Without `amount`, it refunds what remains of the payment. Only `metadata` changes after creation.",
	DataSourceDescription: "A refund of a charge or payment intent.",
	ImportNote:            "Stripe does not return `origin`, `refund_application_fee` or `reverse_transfer`, so an imported refund has them null.",
	Paths: engine.Paths{
		Create:   "/v1/refunds",
		Retrieve: "/v1/refunds/{id}",
		Update:   "/v1/refunds/{id}",
		List:     "/v1/refunds",
	},
	MovesMoney:      true,
	ExactlyOneOf:    [][]string{{"charge", "payment_intent"}},
	ServerDefaulted: []string{"charge", "payment_intent"},
	Delete:          engine.Refuse,
	RefuseReason:    "Stripe cannot delete or undo a refund.",
}

// NewResource returns the resource constructor the provider registers.
func NewResource() (func() resource.Resource, error) {
	return engine.NewResource(Declaration, refund.Table)
}

// NewDataSource returns the data source constructor. It looks the object up by `id` only: Stripe documents no field of it as unique.
func NewDataSource() (func() datasource.DataSource, error) {
	return engine.NewDataSource(Declaration, refund.Table)
}
