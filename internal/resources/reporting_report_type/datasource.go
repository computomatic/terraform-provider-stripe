// Package reporting_report_type declares the stripe_reporting_report_type data source: a type of report Stripe can run. There is no resource.
package reporting_report_type

import (
	"github.com/hashicorp/terraform-plugin-framework/datasource"

	"github.com/computomatic/terraform-provider-stripe/internal/engine"
	"github.com/computomatic/terraform-provider-stripe/internal/generated/reporting_report_type"
)

// Declaration is what the spec cannot say about a report type. Its ID is the report's name,
// such as `balance.summary.1`.
var Declaration = engine.Declaration{
	TypeName:    "stripe_reporting_report_type",
	Description: "A type of report Stripe can run, such as `balance.summary.1`. Look one up by its `id`.",
	Paths:       engine.Paths{Retrieve: "/v1/reporting/report_types/{id}"},
	Delete:      engine.NoDelete,
}

// NewDataSource returns the data source constructor the provider registers. It looks a report type up by `id` only.
func NewDataSource() (func() datasource.DataSource, error) {
	return engine.NewDataSource(Declaration, reporting_report_type.Table)
}
