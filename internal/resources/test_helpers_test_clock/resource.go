// Package test_helpers_test_clock declares the stripe_test_helpers_test_clock resource and
// data source.
package test_helpers_test_clock

import (
	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/resource"

	"github.com/computomatic/terraform-provider-stripe/internal/engine"
	"github.com/computomatic/terraform-provider-stripe/internal/generated/test_helpers_test_clock"
)

// Declaration is what the spec cannot say about a test clock. Stripe takes `frozen_time` on
// create only and moves a clock with an action endpoint, which answers at once while the
// clock is `advancing` and turns it `ready` a few seconds later, after the renewals of the
// clock's subscriptions (suite: "Advancing a test clock"). A new time is an advance, a
// smaller one is refused by Stripe, and while a clock advances Stripe refuses every
// modification with a 429, which the client retries. Destroy deletes the clock, and Stripe
// deletes the customers on it and cancels their subscriptions (suite: "deleting the clock
// deletes what is attached to it"). A deleted clock is a 404.
var Declaration = engine.Declaration{
	TypeName: "stripe_test_helpers_test_clock",
	IDPrefix: "clock_",
	Description: "A clock that freezes time for the customers and subscriptions attached to it, so a test can move billing forward. " +
		"Create it with a `frozen_time`, then attach a customer by setting the customer's `test_clock` to the clock's `id`. " +
		"Setting a later `frozen_time` advances the clock in place and waits until it is `ready` again, within the update timeout; Stripe renews the subscriptions on the clock as it moves, and refuses to move it more than two billing intervals at a time. Setting an earlier one fails at plan time, because a clock never goes backwards: to start again at an earlier time, recreate the clock deliberately with `terraform taint` or by changing `name`, since `-replace` cannot get past the plan check.\n\n" +
		"~> `terraform destroy` deletes the clock, and Stripe deletes every customer attached to it and cancels their subscriptions. A subscription stays retrievable as `canceled`. Changing `name` replaces the clock, which deletes what is attached to the old one.",
	DataSourceDescription: "A clock that freezes time for the customers and subscriptions attached to it. Look one up by its `id`.",
	Paths: engine.Paths{
		Create:   "/v1/test_helpers/test_clocks",
		Retrieve: "/v1/test_helpers/test_clocks/{id}",
		Delete:   "/v1/test_helpers/test_clocks/{id}",
		List:     "/v1/test_helpers/test_clocks",
	},
	Delete: engine.Delete,
	Advance: &engine.AdvanceAction{
		Field:       "frozen_time",
		Path:        "/v1/test_helpers/test_clocks/{id}/advance",
		StatusField: "status",
		ReadyValue:  "ready",
	},
}

// NewResource returns the resource constructor the provider registers.
func NewResource() (func() resource.Resource, error) {
	return engine.NewResource(Declaration, test_helpers_test_clock.Table)
}

// NewDataSource returns the data source constructor. It looks a clock up by `id` only: a name is not unique.
func NewDataSource() (func() datasource.DataSource, error) {
	return engine.NewDataSource(Declaration, test_helpers_test_clock.Table)
}
