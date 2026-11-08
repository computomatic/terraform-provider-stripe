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
	Description:           "Changes a subscription over time, in phases. Set `status` to `canceled` to cancel the schedule and the subscription it started, or to `released` to end the schedule and keep the subscription billing on its own. Stripe replaces the phases as a whole on update and needs the first phase, and the current one, to keep its `start_date`, which an update sends back from the state where the configuration leaves it unset. Phases, and the items and added invoice items of each, are matched by position, so a reorder updates them in place. A pause schedule keeps its configured `pause_at` and `resume_at`, which Stripe returns as times; the data source reads those. With `collection_method = \"send_invoice\"` the customer needs an email address, or Stripe refuses the create.",
	DataSourceDescription: "Changes a subscription over time, in phases.",
	Paths: engine.Paths{
		Create:   "/v1/subscription_schedules",
		Retrieve: "/v1/subscription_schedules/{id}",
		Update:   "/v1/subscription_schedules/{id}",
		List:     "/v1/subscription_schedules",
	},
	// Stripe fills in tax, trial and quantity settings the request leaves out. It also fills
	// `billing_mode.flexible` (suite: TestSubscriptionScheduleAttributes, probe `enums/1`), the
	// default collection method (`enums/12`), the invoice issuer's account from `on_behalf_of`,
	// in the defaults and in a phase (`every_field/11` and `every_field/14`), an added invoice
	// item's `discountable` and `quantity` (`every_field/21`) and the end of a trial phase
	// (`every_field/6`).
	ServerDefaulted: []string{
		"default_settings.automatic_tax", "phases.automatic_tax", "phases.currency", "phases.items.quantity", "phases.trial",
		"billing_mode.flexible", "billing_mode.flexible.proration_discounts", "default_settings.collection_method",
		"default_settings.invoice_settings.issuer.account", "phases.add_invoice_items.discountable",
		"phases.add_invoice_items.quantity", "phases.invoice_settings", "phases.invoice_settings.issuer",
		"phases.invoice_settings.issuer.account", "phases.trial_end",
	},
	// The phase that has begun keeps its start (suite: "replace phases"); later phases get
	// theirs from Stripe. Stripe needs a phase with a start to anchor the end dates to, so the
	// first phase keeps its start even before it begins: every update probe of the suite sends
	// `phases[0].start_date` as Stripe holds it, on a schedule that starts a day later
	// (TestSubscriptionScheduleAttributes, probes `update/3` and `isolated/phase_duration`), and
	// none leaves it out.
	Carry: []engine.Carry{{List: "phases", Field: "start_date", WhenPast: true, First: true}},
	// How to prorate a change goes with a change to the phases.
	SendWithUpdate:   []string{"proration_behavior"},
	SendWithUpdateOn: []string{"phases"},
	Answers: map[string]string{
		// suite: TestSubscriptionScheduleAttributes, probes `isolated/phases_without_start_date`
		// and `isolated/phases_without_start_date_started`, which send one phase with only its
		// items, on a schedule that has not started and on one that has
		"phases.items.price": "Stripe takes an update of `phases` only where a phase holds a `start_date` to anchor the end dates to, which the update sends for the first phase from the state where the configuration sets none, on a schedule that has started and on one that has not.",
	},
	Defaults: map[string]string{
		// suite: TestSubscriptionScheduleAttributes, probe `every_field/11`, which sends
		// `default_settings.on_behalf_of` beside `default_settings.transfer_data`;
		// `every_field/26` sends the transfer data alone and holds none
		"default_settings.invoice_settings.issuer.account": "none, or the `default_settings.on_behalf_of` account where that is set",
		// suite: TestSubscriptionScheduleAttributes, probe `every_field/14`, which sends the
		// phase's `on_behalf_of` beside its `transfer_data`; `every_field/27` sends the transfer
		// data alone and holds none
		"phases.invoice_settings.issuer.account": "none, or the phase's `on_behalf_of` account where that is set",
		// suite: TestSubscriptionScheduleAttributes, probe `every_field/6`, which sends `trial`
		"phases.trial_end": "none, or a time Stripe sets where `trial` is true",
	},
	Delete:       engine.Refuse,
	RefuseReason: "Stripe cannot delete a subscription schedule; set `status` to `canceled` to cancel one and the subscription it started, or to `released` to release the subscription from it.",
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
