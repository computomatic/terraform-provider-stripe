// Package event_v2 declares the stripe_event_v2 data source. There is no resource.
package event_v2

import (
	"github.com/hashicorp/terraform-plugin-framework/datasource"

	"github.com/computomatic/terraform-provider-stripe/internal/engine"
	"github.com/computomatic/terraform-provider-stripe/internal/generated/event_v2"
)

// Declaration is what the spec cannot say about this object. Stripe records a `/v2` event when something happens (suite: "v2 events and product catalog imports"), so only the data source exists.
var Declaration = engine.Declaration{
	TypeName:    "stripe_event_v2",
	IDPrefix:    "evt_",
	Description: "A `/v2` event: a record of something that happened in the account, which event destinations deliver. Look one up by its `id`.",
	Paths:       engine.Paths{Retrieve: "/v2/core/events/{id}", List: "/v2/core/events"},
	Delete:      engine.NoDelete,
}

// NewDataSource returns the data source constructor the provider registers. It looks the object up by `id` only: Stripe documents no field of it as unique.
func NewDataSource() (func() datasource.DataSource, error) {
	return engine.NewDataSource(Declaration, event_v2.Table)
}
