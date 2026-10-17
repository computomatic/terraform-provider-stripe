// Package billing_meter declares the stripe_billing_meter resource and data source.
package billing_meter

import (
	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/resource"

	"github.com/computomatic/terraform-provider-stripe/internal/engine"
	"github.com/computomatic/terraform-provider-stripe/internal/generated/billing_meter"
	"github.com/computomatic/terraform-provider-stripe/internal/wire"
)

// Declaration is what the spec cannot say about a billing meter. Stripe never deletes a
// meter, so destroy deactivates it and drops it from state; the meter stays in the account.
var Declaration = engine.Declaration{
	TypeName:    "stripe_billing_meter",
	IDPrefix:    "mtr_",
	Description: "Defines how usage events are aggregated into a billable quantity. `terraform destroy` deactivates the meter, because Stripe cannot delete one.",
	Paths: engine.Paths{
		Create:   "/v1/billing/meters",
		Retrieve: "/v1/billing/meters/{id}",
		Update:   "/v1/billing/meters/{id}",
		List:     "/v1/billing/meters",
	},
	Delete: engine.Archive,
	Archive: &engine.ArchiveAction{
		Field: "status",
		Value: wire.String("inactive"),
		Path:  "/v1/billing/meters/{id}/deactivate",
	},
}

// NewResource returns the resource constructor the provider registers.
func NewResource() (func() resource.Resource, error) {
	return engine.NewResource(Declaration, billing_meter.Table)
}

// NewDataSource returns the data source constructor. Stripe documents no meter field as unique, so the data source looks up by `id` only.
func NewDataSource() (func() datasource.DataSource, error) {
	return engine.NewDataSource(Declaration, billing_meter.Table)
}
