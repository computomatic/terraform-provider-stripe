// Package webhook_endpoint declares the stripe_webhook_endpoint resource and data source.
package webhook_endpoint

import (
	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/resource"

	"github.com/computomatic/terraform-provider-stripe/internal/engine"
	"github.com/computomatic/terraform-provider-stripe/internal/generated/webhook_endpoint"
)

// Declaration is what the spec cannot say about a webhook endpoint. Stripe returns the
// signing `secret` once, when it creates the endpoint, so state keeps it from then on.
var Declaration = engine.Declaration{
	TypeName:    "stripe_webhook_endpoint",
	IDPrefix:    "we_",
	Description: "An endpoint Stripe sends events to. The signing `secret` is returned once, at creation, and is kept in state as a sensitive value. An imported endpoint has no `secret`, and neither has the data source, because Stripe returns it only at creation. `terraform destroy` deletes the endpoint.",
	Paths: engine.Paths{
		Create:   "/v1/webhook_endpoints",
		Retrieve: "/v1/webhook_endpoints/{id}",
		Update:   "/v1/webhook_endpoints/{id}",
		Delete:   "/v1/webhook_endpoints/{id}",
		List:     "/v1/webhook_endpoints",
	},
	Delete:       engine.Delete,
	OnceReturned: []string{"secret"},
}

// NewResource returns the resource constructor the provider registers.
func NewResource() (func() resource.Resource, error) {
	return engine.NewResource(Declaration, webhook_endpoint.Table)
}

// NewDataSource returns the data source constructor. Stripe documents no webhook endpoint field as unique (several endpoints may share a URL), so the data source looks up by `id` only. It returns the `secret` only when the retrieve response carries it, which Stripe does not do after creation.
func NewDataSource() (func() datasource.DataSource, error) {
	return engine.NewDataSource(Declaration, webhook_endpoint.Table)
}
