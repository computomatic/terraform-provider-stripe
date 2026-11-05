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
// and disabling are separate action endpoints that the provider does not call yet. Stripe
// returns the webhook endpoint's signing secret only to a create that includes it, so the
// create alone includes it and state keeps it (suite: "create including signing secret";
// ADR adrs/1-pending/2026-10-17-v2-support).
var Declaration = engine.Declaration{
	TypeName:              "stripe_event_destination_v2",
	IDPrefix:              "ed_",
	Description:           "A destination for Stripe's events, delivered as thin or snapshot payloads: a webhook endpoint, an Amazon EventBridge partner event source or an Azure Event Grid partner topic. `terraform destroy` deletes the destination. A webhook endpoint's `signing_secret` is returned once, at creation, and is kept in state as a sensitive value. `status` is read-only: Terraform cannot enable or disable a destination yet.",
	DataSourceDescription: "A destination for Stripe's events, delivered as thin or snapshot payloads: a webhook endpoint, an Amazon EventBridge partner event source or an Azure Event Grid partner topic. Stripe returns a webhook endpoint's `signing_secret` only at creation, so the data source has none.",
	ImportNote:            "An imported destination has no `webhook_endpoint.signing_secret`, because Stripe returns it only at creation. Replacing the destination, as `terraform apply -replace` does, creates one with a new secret in state.",
	Paths: engine.Paths{
		Create:   "/v2/core/event_destinations",
		Retrieve: "/v2/core/event_destinations/{id}",
		Update:   "/v2/core/event_destinations/{id}",
		Delete:   "/v2/core/event_destinations/{id}",
		List:     "/v2/core/event_destinations",
	},
	Delete:          engine.Delete,
	Include:         []string{"webhook_endpoint.url"},
	IncludeOnCreate: []string{"webhook_endpoint.signing_secret"},
	OnceReturned:    []string{"webhook_endpoint.signing_secret"},
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
