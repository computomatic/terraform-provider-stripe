// Package billing_meter_event_summary declares the stripe_billing_meter_event_summary data source. There is no resource.
package billing_meter_event_summary

import (
	"github.com/hashicorp/terraform-plugin-framework/datasource"

	"github.com/computomatic/terraform-provider-stripe/internal/engine"
	"github.com/computomatic/terraform-provider-stripe/internal/generated/billing_meter_event_summary"
)

// Declaration is what the spec cannot say about this object. Stripe has no retrieve for a summary, only the list under its meter, and that list needs a customer and a window (suite: "Meter event summaries"), so a summary's ID cannot find it on its own. The data source reads the one summary of the whole window: without a grouping window Stripe lists exactly one, whose times echo the request. A summary is made by Stripe as events arrive, so only the data source exists.
var Declaration = engine.Declaration{
	TypeName:    "stripe_billing_meter_event_summary",
	Description: "The aggregated usage a billing meter recorded for one customer over a time window. Look one up by the `meter`, the `customer` and the window's `start_time` and `end_time`, which Stripe requires to fall on minute boundaries. Stripe aggregates events about a minute after they are sent, and a window with no events may have no summary, which is an error.",
	Parent:      "meter",
	Paths:       engine.Paths{List: "/v1/billing/meters/{meter}/event_summaries"},
	FindBy: []engine.FindParam{
		{Name: "customer", Description: "The ID of the customer whose usage the summary aggregates."},
		{Name: "start_time"},
		{Name: "end_time"},
	},
	Delete: engine.NoDelete,
}

// NewDataSource returns the data source constructor the provider registers. It reads the summary by the meter, the customer and the window, because Stripe lists summaries only by those.
func NewDataSource() (func() datasource.DataSource, error) {
	return engine.NewDataSource(Declaration, billing_meter_event_summary.Table)
}
