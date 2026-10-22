// Package climate_order declares the stripe_climate_order resource and data source.
package climate_order

import (
	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/resource"

	"github.com/computomatic/terraform-provider-stripe/internal/engine"
	"github.com/computomatic/terraform-provider-stripe/internal/generated/climate_order"
)

// Declaration is what the spec cannot say about a Climate order. Stripe never deletes one,
// and canceling one is final ("cancel again"), so destroy is refused. Setting `status` to
// `canceled` cancels it (suite: "ClimateOrder", `cancel`); Stripe refuses to cancel a
// delivered order ("Order can not be canceled", the fake models it).
var Declaration = engine.Declaration{
	TypeName:              "stripe_climate_order",
	IDPrefix:              "climorder_",
	Description:           "A purchase of carbon removal from Stripe Climate. Set exactly one of `metric_tons` (a whole number of tons) and `amount` (the money to spend, in the smallest unit of the order's currency). The order spends the account's available balance in its settlement currency. Set `status` to `canceled` to cancel an order that is not yet delivered, which refunds its `amount_subtotal` but not its `amount_fees`. Only `beneficiary` and `metadata` change in place: changing any other argument fails the plan.",
	DataSourceDescription: "A purchase of carbon removal from Stripe Climate.",
	ImportNote:            "Stripe does not return `amount`, so an imported order has none in state. Importing an order that was bought by `amount` and configuring `amount` plans a replacement.",
	ImportNoteLevel:       engine.NoteWarning,
	Paths: engine.Paths{
		Create:   "/v1/climate/orders",
		Retrieve: "/v1/climate/orders/{id}",
		Update:   "/v1/climate/orders/{id}",
		List:     "/v1/climate/orders",
	},
	Delete:       engine.Refuse,
	RefuseReason: "Stripe cannot delete a Climate order; set `status` to `canceled` to cancel one that is not yet delivered.",
	Transitions:  []engine.Transition{{Field: "status", Value: "canceled", Path: "/v1/climate/orders/{id}/cancel"}},
	// An order without a beneficiary is attributed to the account (suite: "A climate order
	// without a beneficiary is attributed to the account").
	ServerDefaulted: []string{"beneficiary"},
	// An order bought by `amount` has Stripe's computed tons, and Stripe spells the tons it
	// returns as it chooses, so the configured value stays.
	PreserveConfig: []string{"metric_tons"},
	ExactlyOneOf:   [][]string{{"amount", "metric_tons"}},
}

// NewResource returns the resource constructor the provider registers.
func NewResource() (func() resource.Resource, error) {
	return engine.NewResource(Declaration, climate_order.Table)
}

// NewDataSource returns the data source constructor. Stripe documents no order field as unique, so the data source looks up by `id` only.
func NewDataSource() (func() datasource.DataSource, error) {
	return engine.NewDataSource(Declaration, climate_order.Table)
}
