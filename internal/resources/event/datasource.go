// Package event declares the stripe_event data source: a record of something that happened in the account. There is no resource.
package event

import (
	"github.com/hashicorp/terraform-plugin-framework/datasource"

	"github.com/computomatic/terraform-provider-stripe/internal/engine"
	"github.com/computomatic/terraform-provider-stripe/internal/generated/event"
)

// Declaration is what the spec cannot say about an event. The event's `data` payload is
// free-form JSON and is not modelled.
var Declaration = engine.Declaration{
	TypeName:    "stripe_event",
	IDPrefix:    "evt_",
	Description: "A record of something that happened in the account. Look one up by its `id`. The event's `data` payload is not available.",
	Paths:       engine.Paths{Retrieve: "/v1/events/{id}"},
	Delete:      engine.NoDelete,
}

// NewDataSource returns the data source constructor the provider registers. It looks an event up by `id` only.
func NewDataSource() (func() datasource.DataSource, error) {
	return engine.NewDataSource(Declaration, event.Table)
}
