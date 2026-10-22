// Package billing_feedback_option declares the stripe_billing_feedback_option resource and data source.
package billing_feedback_option

import (
	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/resource"

	"github.com/computomatic/terraform-provider-stripe/internal/engine"
	"github.com/computomatic/terraform-provider-stripe/internal/generated/billing_feedback_option"
)

// Declaration is what the spec cannot say about a feedback option. Stripe never deletes one,
// and deactivating one is final: Stripe has no reactivate. So destroy is refused, and setting
// `status` to `inactive` deactivates the option through a bare POST to its deactivate
// endpoint (suite: "deactivate").
var Declaration = engine.Declaration{
	TypeName:              "stripe_billing_feedback_option",
	IDPrefix:              "fo_",
	Description:           "A reason a customer can pick when canceling a subscription. Set `status` to `inactive` to deactivate the option, which cannot be undone.",
	DataSourceDescription: "A reason a customer can pick when canceling a subscription.",
	Paths: engine.Paths{
		Create:   "/v1/billing/feedback_options",
		Retrieve: "/v1/billing/feedback_options/{id}",
		Update:   "/v1/billing/feedback_options/{id}",
		List:     "/v1/billing/feedback_options",
	},
	Delete:       engine.Refuse,
	RefuseReason: "Stripe cannot delete a feedback option; set `status` to `inactive` to deactivate one, which cannot be undone.",
	Transitions:  []engine.Transition{{Field: "status", Value: "inactive", Path: "/v1/billing/feedback_options/{id}/deactivate"}},
}

// NewResource returns the resource constructor the provider registers.
func NewResource() (func() resource.Resource, error) {
	return engine.NewResource(Declaration, billing_feedback_option.Table)
}

// NewDataSource returns the data source constructor. No feedback option field is documented as unique, so the data source looks up by `id` only.
func NewDataSource() (func() datasource.DataSource, error) {
	return engine.NewDataSource(Declaration, billing_feedback_option.Table)
}
