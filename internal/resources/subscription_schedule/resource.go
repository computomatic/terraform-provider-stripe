// Package subscription_schedule declares the stripe_subscription_schedule resource and data source.
package subscription_schedule

import (
	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/resource"

	"github.com/computomatic/terraform-provider-stripe/internal/engine"
	"github.com/computomatic/terraform-provider-stripe/internal/generated/subscription_schedule"
)

// Declaration is what the spec cannot say about a subscription schedule. Stripe never
// deletes one, and cancelling or releasing one is final, so destroy is refused. Setting
// `status` to `canceled` cancels the schedule and the subscription it started, and `released`
// releases the subscription from it (suite: "Subscription schedules", "cancel", "release").
var Declaration = engine.Declaration{
	TypeName:              "stripe_subscription_schedule",
	IDPrefix:              "sub_sched_",
	Description:           "Changes a subscription over time, in phases. Set `status` to `canceled` to cancel the schedule and the subscription it started, or to `released` to end the schedule and keep the subscription billing on its own. Stripe replaces the phases as a whole on update and needs the current phase to keep its `start_date`, which an update sends back from the state.",
	DataSourceDescription: "Changes a subscription over time, in phases.",
	Paths: engine.Paths{
		Create:   "/v1/subscription_schedules",
		Retrieve: "/v1/subscription_schedules/{id}",
		Update:   "/v1/subscription_schedules/{id}",
		List:     "/v1/subscription_schedules",
	},
	// Stripe fills in tax, trial and quantity settings the request leaves out.
	ServerDefaulted: []string{"default_settings.automatic_tax", "phases.automatic_tax", "phases.currency", "phases.items.quantity", "phases.trial"},
	// The phase that has begun keeps its start (suite: "replace phases"); later phases get
	// theirs from Stripe.
	Carry: []engine.Carry{{List: "phases", Field: "start_date", WhenPast: true}},
	// How to prorate a change goes with a change to the phases.
	SendWithUpdate:   []string{"proration_behavior"},
	SendWithUpdateOn: []string{"phases"},
	Delete:           engine.Refuse,
	RefuseReason:     "Stripe cannot delete a subscription schedule; set `status` to `canceled` to cancel one and the subscription it started, or to `released` to release the subscription from it.",
	// A released, canceled or completed schedule cannot be released or canceled (suite: "Release
	// and cancel of a schedule not in `not_started` or `active` are 400").
	Transitions: []engine.Transition{
		{Field: "status", Value: "canceled", Path: "/v1/subscription_schedules/{id}/cancel", Final: []string{"released", "completed"}},
		{Field: "status", Value: "released", Path: "/v1/subscription_schedules/{id}/release", Final: []string{"canceled", "completed"}},
	},
}

// NewResource returns the resource constructor the provider registers.
func NewResource() (func() resource.Resource, error) {
	return engine.NewResource(Declaration, subscription_schedule.Table)
}

// NewDataSource returns the data source constructor. Stripe documents no schedule field as unique, so the data source looks up by `id` only.
func NewDataSource() (func() datasource.DataSource, error) {
	return engine.NewDataSource(Declaration, subscription_schedule.Table)
}
