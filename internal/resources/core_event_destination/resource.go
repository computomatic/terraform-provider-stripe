// Package core_event_destination declares the stripe_core_event_destination resource and
// data source, the first object in Stripe's /v2 namespace (v2.core.event_destination).
package core_event_destination

import (
	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/resource"

	"github.com/computomatic/terraform-provider-stripe/internal/engine"
	"github.com/computomatic/terraform-provider-stripe/internal/generated/core_event_destination"
)

// Declaration is what the spec cannot say about an event destination. Stripe redacts
// `webhook_endpoint.url` unless a request includes it, so the engine includes it on every
// request and the URL is read back like any other field. `status` is read-only: enabling
// and disabling are separate action endpoints that the provider does not call yet. The
// signing secret is left out of the table until the conformance suite and the fake prove
// it (ADR adrs/1-pending/2026-10-17-v2-support).
var Declaration = engine.Declaration{
	TypeName:    "stripe_core_event_destination",
	IDPrefix:    "ed_",
	Description: "A destination for Stripe's events, delivered as thin or snapshot payloads: a webhook endpoint, an Amazon EventBridge partner event source or an Azure Event Grid partner topic. `terraform destroy` deletes the destination. The destination's signing secret is not available yet. `status` is read-only: Terraform cannot enable or disable a destination yet.",
	Paths: engine.Paths{
		Create:   "/v2/core/event_destinations",
		Retrieve: "/v2/core/event_destinations/{id}",
		Update:   "/v2/core/event_destinations/{id}",
		Delete:   "/v2/core/event_destinations/{id}",
		List:     "/v2/core/event_destinations",
	},
	Delete:  engine.Delete,
	Include: []string{"webhook_endpoint.url"},
}

// NewResource returns the resource constructor the provider registers.
func NewResource() (func() resource.Resource, error) {
	return engine.NewResource(Declaration, core_event_destination.Table)
}

// NewDataSource returns the data source constructor. Stripe's list has no filter and
// documents no unique field, so the data source looks up by `id` only.
func NewDataSource() (func() datasource.DataSource, error) {
	return engine.NewDataSource(Declaration, core_event_destination.Table)
}
