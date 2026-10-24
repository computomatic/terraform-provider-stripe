// Package subscription declares the stripe_subscription resource and data source.
package subscription

import (
	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/resource"

	"github.com/computomatic/terraform-provider-stripe/internal/engine"
	"github.com/computomatic/terraform-provider-stripe/internal/generated/subscription"
)

// Declaration is what the spec cannot say about a subscription. Stripe's DELETE cancels it
// at once (suite: "Subscriptions", `cancel`), and a cancelled subscription stays readable
// with the status `canceled`, so destroy deletes. Cancelling twice is a 404, which the
// engine counts as done.
var Declaration = engine.Declaration{
	TypeName:              "stripe_subscription",
	IDPrefix:              "sub_",
	Description:           "A customer's recurring purchase of one or more prices. `terraform destroy` cancels the subscription immediately, because Stripe cannot delete one: billing stops and the cancelled subscription stays in the account. `items` are matched by `price`: an item whose price stays keeps its ID and changes in place, a new price adds an item, a dropped price deletes it, and a reorder changes nothing. A price may appear once. An item added from outside, such as one that `stripe_subscription_item` created, is not part of `items` and is never changed or deleted by this resource. An update that changes `items` also takes `proration_behavior` and `payment_behavior`. An item that sets `price_data` has no price of its own to match by, so it is matched by its `price_data`: changing its quantity, metadata or other settings updates the item in place, and changing its `price_data` replaces the item, which gets a new item ID. Two items may not carry identical `price_data`. With `collection_method = \"send_invoice\"` the customer needs an email address, or Stripe refuses the create.",
	DataSourceDescription: "A customer's recurring purchase of one or more prices.",
	ImportNote:            "Importing a subscription adopts every item it has, so a configuration that lists fewer items deletes the rest on the next apply.",
	ImportNoteLevel:       engine.NoteWarning,
	Paths: engine.Paths{
		Create:   "/v1/subscriptions",
		Retrieve: "/v1/subscriptions/{id}",
		Update:   "/v1/subscriptions/{id}",
		Delete:   "/v1/subscriptions/{id}",
		List:     "/v1/subscriptions",
	},
	// Stripe starts an item at quantity 1 and gives it empty metadata, and sets `cancel_at` to
	// the end of the period when `cancel_at_period_end` is true. It fills in `payment_settings`
	// and `trial_settings` when the request leaves them out, even with no trial (suite:
	// "create/invoiced subscription").
	ServerDefaulted: []string{"cancel_at", "items.quantity", "items.metadata", "payment_settings", "trial_settings"},
	// Items are matched by price: an item whose price stays keeps its ID and changes in
	// place, a new price adds an item and a dropped one deletes it (suite: "swap item price"
	// names an item by ID).
	MatchBy: map[string]string{"items": "price"},
	// An item that sets `price_data` is the same item while the data stays; other changes
	// to it apply in place.
	MatchData: map[string]string{"items": "price_data"},
	// How to prorate and pay for a change goes with a change to the items, the one kind of
	// update the suite proves them with.
	SendWithUpdate:   []string{"payment_behavior", "proration_behavior"},
	SendWithUpdateOn: []string{"items"},
	Delete:           engine.Delete,
}

// NewResource returns the resource constructor the provider registers.
func NewResource() (func() resource.Resource, error) {
	return engine.NewResource(Declaration, subscription.Table)
}

// NewDataSource returns the data source constructor. Stripe documents no subscription field as unique, so the data source looks up by `id` only.
func NewDataSource() (func() datasource.DataSource, error) {
	return engine.NewDataSource(Declaration, subscription.Table)
}
