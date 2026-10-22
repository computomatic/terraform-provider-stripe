// Package issuing_physical_bundle declares the stripe_issuing_physical_bundle data source. There is no resource.
package issuing_physical_bundle

import (
	"github.com/hashicorp/terraform-plugin-framework/datasource"

	"github.com/computomatic/terraform-provider-stripe/internal/engine"
	"github.com/computomatic/terraform-provider-stripe/internal/generated/issuing_physical_bundle"
)

// Declaration is what the spec cannot say about a physical bundle. Stripe offers a catalogue of
// them on a connected account with Issuing and nothing creates one: a POST to the list is the
// unknown-route 404 (suite: "Issuing physical bundles on a US test-mode connected account (first
// run)"). Stripe retrieves one by its ID, and no field of it is unique.
var Declaration = engine.Declaration{
	TypeName:    "stripe_issuing_physical_bundle",
	IDPrefix:    "ics_",
	Description: "The card stock, packaging and carrier letter a physical Issuing card ships in, and which features a personalization design on it takes. Stripe offers the bundles on a connected account with Issuing, so read one through a provider configuration whose `stripe_account` is that account. Look one up by its `id`.",
	Paths:       engine.Paths{Retrieve: "/v1/issuing/physical_bundles/{id}", List: "/v1/issuing/physical_bundles"},
	Delete:      engine.NoDelete,
}

// NewDataSource returns the data source constructor the provider registers. It looks a bundle up by `id` only: Stripe documents no field of a bundle as unique, and two bundles may share a name.
func NewDataSource() (func() datasource.DataSource, error) {
	return engine.NewDataSource(Declaration, issuing_physical_bundle.Table)
}
