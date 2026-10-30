// Package event_destination_v2 declares the stripe_event_destination_v2 resource and
// data source, the first object in Stripe's /v2 namespace (v2.core.event_destination).
package event_destination_v2

import (
	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/resource"

	"github.com/computomatic/terraform-provider-stripe/internal/engine"
	"github.com/computomatic/terraform-provider-stripe/internal/generated/event_destination_v2"
)

// Declaration is what the spec cannot say about an event destination. Stripe redacts
// `webhook_endpoint.url` unless a request includes it, so the engine includes it on every
// request and the URL is read back like any other field. `status` is read-only: enabling
// and disabling are separate action endpoints that the provider does not call yet. The
// signing secret is in the exclusion register until the engine can send `include` on create
// alone to read it once (ADR adrs/1-pending/2026-10-17-v2-support).
var Declaration = engine.Declaration{
	TypeName:              "stripe_event_destination_v2",
	IDPrefix:              "ed_",
	Description:           "A destination for Stripe's events, delivered as thin or snapshot payloads: a webhook endpoint, an Amazon EventBridge partner event source or an Azure Event Grid partner topic. `terraform destroy` deletes the destination. The destination's signing secret is not available yet. `status` is read-only: Terraform cannot enable or disable a destination yet.",
	DataSourceDescription: "A destination for Stripe's events, delivered as thin or snapshot payloads: a webhook endpoint, an Amazon EventBridge partner event source or an Azure Event Grid partner topic.",
	Paths: engine.Paths{
		Create:   "/v2/core/event_destinations",
		Retrieve: "/v2/core/event_destinations/{id}",
		Update:   "/v2/core/event_destinations/{id}",
		Delete:   "/v2/core/event_destinations/{id}",
		List:     "/v2/core/event_destinations",
	},
	Delete:  engine.Delete,
	Include: []string{"webhook_endpoint.url"},
	// Stripe defaults `events_from` to `["@self"]` when a request omits it.
	ServerDefaulted: []string{"events_from"},
}

// NewResource returns the resource constructor the provider registers.
func NewResource() (func() resource.Resource, error) {
	return engine.NewResource(Declaration, event_destination_v2.Table)
}

// NewDataSource returns the data source constructor. Stripe's list has no filter and
// documents no unique field, so the data source looks up by `id` only.
func NewDataSource() (func() datasource.DataSource, error) {
	return engine.NewDataSource(Declaration, event_destination_v2.Table)
}
