// Package subscription_item declares the stripe_subscription_item resource and data source.
package subscription_item

import (
	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/resource"

	"github.com/computomatic/terraform-provider-stripe/internal/engine"
	"github.com/computomatic/terraform-provider-stripe/internal/generated/subscription_item"
)

// Declaration is what the spec cannot say about a subscription item. Stripe deletes an item
// with DELETE (suite: "Subscription items", `delete`), except the last one of a subscription,
// which is a 400 without a code: that refusal is shown as it is, because a bare 400 does not
// say why.
var Declaration = engine.Declaration{
	TypeName:              "stripe_subscription_item",
	IDPrefix:              "si_",
	Description:           "A price on a subscription, with a quantity. `terraform destroy` removes the item from the subscription. Stripe refuses to remove a subscription's last item, so cancel the subscription instead. An item added here shows up in the subscription's `items`, so give a `stripe_subscription` that sets `items` `lifecycle { ignore_changes = [items] }`.",
	DataSourceDescription: "A price on a subscription, with a quantity.",
	Paths: engine.Paths{
		Create:   "/v1/subscription_items",
		Retrieve: "/v1/subscription_items/{id}",
		Update:   "/v1/subscription_items/{id}",
		Delete:   "/v1/subscription_items/{id}",
		List:     "/v1/subscription_items",
	},
	// Stripe starts an item at quantity 1.
	ServerDefaulted: []string{"quantity"},
	// How to prorate and pay for a change goes with a change to what the item bills.
	SendWithUpdate:   []string{"payment_behavior", "proration_behavior"},
	SendWithUpdateOn: []string{"price", "price_data", "quantity", "tax_rates"},
	Delete:           engine.Delete,
}

// NewResource returns the resource constructor the provider registers.
func NewResource() (func() resource.Resource, error) {
	return engine.NewResource(Declaration, subscription_item.Table)
}

// NewDataSource returns the data source constructor. Stripe documents no subscription item field as unique, so the data source looks up by `id` only.
func NewDataSource() (func() datasource.DataSource, error) {
	return engine.NewDataSource(Declaration, subscription_item.Table)
}
