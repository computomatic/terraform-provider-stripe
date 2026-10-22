// Package subscription_schedule declares the stripe_subscription_schedule resource and data source.
package subscription_schedule

import (
	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/resource"

	"github.com/computomatic/terraform-provider-stripe/internal/engine"
	"github.com/computomatic/terraform-provider-stripe/internal/generated/subscription_schedule"
	"github.com/computomatic/terraform-provider-stripe/internal/wire"
)

// Declaration is what the spec cannot say about a subscription schedule. Stripe never
// deletes one, so destroy cancels it (suite: "Subscription schedules", `cancel`, which also
// cancels the subscription the schedule started) and drops it from state. A schedule that
// was already released or completed cannot be cancelled, and has nothing left to cancel,
// so destroy leaves it as it is. Release would keep the subscription billing after
// destroy, so it is not used.
var Declaration = engine.Declaration{
	TypeName:              "stripe_subscription_schedule",
	IDPrefix:              "sub_sched_",
	Description:           "Changes a subscription over time, in phases. `terraform destroy` cancels the schedule, which also cancels the subscription it started, because Stripe cannot delete a schedule; the cancelled schedule stays in the account. Stripe replaces the phases as a whole on update and needs the current phase to keep its `start_date`, which an update sends back from the state.",
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
	Delete:           engine.Archive,
	Archive: &engine.ArchiveAction{
		Field: "status",
		Value: wire.String("canceled"),
		Path:  "/v1/subscription_schedules/{id}/cancel",
		Ended: []engine.Ended{
			{Field: "status", Value: wire.String("released")},
			{Field: "status", Value: wire.String("completed")},
		},
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
