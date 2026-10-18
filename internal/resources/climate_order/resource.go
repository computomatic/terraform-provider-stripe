// Package climate_order declares the stripe_climate_order resource and data source.
package climate_order

import (
	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/resource"

	"github.com/computomatic/terraform-provider-stripe/internal/engine"
	"github.com/computomatic/terraform-provider-stripe/internal/generated/climate_order"
	"github.com/computomatic/terraform-provider-stripe/internal/wire"
)

// Declaration is what the spec cannot say about a Climate order. Stripe never deletes one,
// so destroy cancels it (suite: "ClimateOrder", `cancel`) and drops it from state. A
// canceled order stays readable and canceling twice is refused ("cancel again"), so destroy
// reads the status first. Stripe refuses to cancel a delivered order ("Order can not be
// canceled", the fake models it): when the cancel fails and a retrieve shows the order
// delivered, destroy warns, leaves it and drops it from state. Any other failure fails the
// destroy.
var Declaration = engine.Declaration{
	TypeName:    "stripe_climate_order",
	IDPrefix:    "climorder_",
	Description: "A purchase of carbon removal from Stripe Climate. Set exactly one of `metric_tons` (a whole number of tons) and `amount` (the money to spend, in the smallest unit of the order's currency). The order spends the account's available balance in its settlement currency. `terraform destroy` cancels the order and refunds its `amount_subtotal`, but not its `amount_fees`; Stripe cannot delete an order, so a canceled order stays in the account. Stripe cancels only orders that are not yet delivered, so destroy leaves a delivered order as it is, with a warning. Only `beneficiary` and `metadata` change in place: changing any other argument cancels the order and buys a new one, and the fees of the canceled order are not refunded. Stripe does not return `amount`, so an imported order has none in state. Importing an order that was bought by `amount` and configuring `amount` plans a replacement.",
	Paths: engine.Paths{
		Create:   "/v1/climate/orders",
		Retrieve: "/v1/climate/orders/{id}",
		Update:   "/v1/climate/orders/{id}",
		List:     "/v1/climate/orders",
	},
	Delete: engine.Archive,
	Archive: &engine.ArchiveAction{
		Field: "status",
		Value: wire.String("canceled"),
		Path:  "/v1/climate/orders/{id}/cancel",
	},
	KeepWhen: &engine.FieldIs{Field: "status", Value: wire.String("delivered")},
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
