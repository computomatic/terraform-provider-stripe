// Package core_event declares the stripe_core_event data source. There is no resource.
package core_event

import (
	"github.com/hashicorp/terraform-plugin-framework/datasource"

	"github.com/computomatic/terraform-provider-stripe/internal/engine"
	"github.com/computomatic/terraform-provider-stripe/internal/generated/core_event"
)

// Declaration is what the spec cannot say about this object. Stripe records a `/v2` event when something happens (suite: "v2 events and product catalog imports"), so only the data source exists.
var Declaration = engine.Declaration{
	TypeName:    "stripe_core_event",
	IDPrefix:    "evt_",
	Description: "A `/v2` event: a record of something that happened in the account, which event destinations deliver. Look one up by its `id`.",
	Paths:       engine.Paths{Retrieve: "/v2/core/events/{id}", List: "/v2/core/events"},
	Delete:      engine.NoDelete,
}

// NewDataSource returns the data source constructor the provider registers. It looks the object up by `id` only: Stripe documents no field of it as unique.
func NewDataSource() (func() datasource.DataSource, error) {
	return engine.NewDataSource(Declaration, core_event.Table)
}
