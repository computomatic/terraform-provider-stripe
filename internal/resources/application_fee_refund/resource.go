// Package application_fee_refund declares the stripe_application_fee_refund resource and data
// source.
package application_fee_refund

import (
	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/resource"

	"github.com/computomatic/terraform-provider-stripe/internal/engine"
	"github.com/computomatic/terraform-provider-stripe/internal/generated/application_fee_refund"
)

// Declaration is what the spec cannot say about an application fee refund (ADR
// 2026-10-22-money-moving-resources, D6.10). It moves money, so the live-mode guard applies.
// It is addressed under its fee, so its import ID is `<fee_id>/<refund_id>`. Without `amount`
// it refunds the rest (suite: "create the rest"); only `metadata` updates (suite: "update",
// "immutable amount"). Stripe cannot delete or undo a refund, so destroy is refused.
var Declaration = engine.Declaration{
	TypeName:              "stripe_application_fee_refund",
	IDPrefix:              "fr_",
	Description:           "A refund of an application fee, which gives some or all of the fee back to the connected account. Without `amount`, it refunds what remains of the fee. Only `metadata` changes after creation.",
	DataSourceDescription: "A refund of an application fee, which gives some or all of the fee back to the connected account. Look one up by the `fee` and the refund's `id`.",
	Parent:                "fee",
	Paths: engine.Paths{
		Create:   "/v1/application_fees/{fee}/refunds",
		Retrieve: "/v1/application_fees/{fee}/refunds/{id}",
		Update:   "/v1/application_fees/{fee}/refunds/{id}",
		List:     "/v1/application_fees/{fee}/refunds",
	},
	MovesMoney:   true,
	Delete:       engine.Refuse,
	RefuseReason: "Stripe cannot delete or undo an application fee refund.",
}

// NewResource returns the resource constructor the provider registers.
func NewResource() (func() resource.Resource, error) {
	return engine.NewResource(Declaration, application_fee_refund.Table)
}

// NewDataSource returns the data source constructor. It needs the `fee` and the `id` of the refund. Stripe documents no refund field as unique.
func NewDataSource() (func() datasource.DataSource, error) {
	return engine.NewDataSource(Declaration, application_fee_refund.Table)
}
