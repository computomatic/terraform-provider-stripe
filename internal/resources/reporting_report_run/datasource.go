// Package reporting_report_run declares the stripe_reporting_report_run data source. There is no resource.
package reporting_report_run

import (
	"github.com/hashicorp/terraform-plugin-framework/datasource"

	"github.com/computomatic/terraform-provider-stripe/internal/engine"
	"github.com/computomatic/terraform-provider-stripe/internal/generated/reporting_report_run"
)

// Declaration is what the spec cannot say about this object. A run is a request for a report that Stripe runs in the background and cannot delete (suite: "Platform account and report runs"), so only the data source exists.
var Declaration = engine.Declaration{
	TypeName:    "stripe_reporting_report_run",
	IDPrefix:    "frr_",
	Description: "A run of a financial report, and the file it produced once it succeeded. Look one up by its `id`.",
	Paths:       engine.Paths{Retrieve: "/v1/reporting/report_runs/{id}", List: "/v1/reporting/report_runs"},
	Delete:      engine.NoDelete,
}

// NewDataSource returns the data source constructor the provider registers. It looks the object up by `id` only: Stripe documents no field of it as unique.
func NewDataSource() (func() datasource.DataSource, error) {
	return engine.NewDataSource(Declaration, reporting_report_run.Table)
}
