// Package application_fee declares the stripe_application_fee data source. There is no
// resource.
package application_fee

import (
	"github.com/hashicorp/terraform-plugin-framework/datasource"

	"github.com/computomatic/terraform-provider-stripe/internal/engine"
	"github.com/computomatic/terraform-provider-stripe/internal/generated/application_fee"
)

// Declaration is what the spec cannot say about an application fee: what a platform earns from
// a charge it makes for a connected account. A destination charge makes it, and nothing
// creates one through the API (suite: "Application fees from destination charges"). Its
// `charge` is the connected account's payment, not the platform's charge, which is
// `originating_transaction`. Stripe retrieves it by its ID, and its list filters by `charge`
// and `created`, neither of which Stripe documents as unique.
var Declaration = engine.Declaration{
	TypeName:    "stripe_application_fee",
	IDPrefix:    "fee_",
	Description: "The fee a platform collects from a charge it makes on behalf of a connected account. `charge` is the payment on the connected account and `originating_transaction` the platform's charge. Look one up by its `id`. Read one of its refunds with the `stripe_application_fee_refund` data source.",
	Paths:       engine.Paths{Retrieve: "/v1/application_fees/{id}", List: "/v1/application_fees"},
	Delete:      engine.NoDelete,
}

// NewDataSource returns the data source constructor the provider registers. It looks a fee up by `id` only.
func NewDataSource() (func() datasource.DataSource, error) {
	return engine.NewDataSource(Declaration, application_fee.Table)
}
