// Package shipping_rate declares the stripe_shipping_rate resource and data source.
package shipping_rate

import (
	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/resource"

	"github.com/computomatic/terraform-provider-stripe/internal/engine"
	"github.com/computomatic/terraform-provider-stripe/internal/generated/shipping_rate"
	"github.com/computomatic/terraform-provider-stripe/internal/wire"
)

// Declaration is what the spec cannot say about a shipping rate. Stripe never deletes a
// shipping rate (DELETE answers with an error), so destroy archives it (`active = false`)
// and drops it from state; the shipping rate stays in the account.
var Declaration = engine.Declaration{
	TypeName:              "stripe_shipping_rate",
	IDPrefix:              "shr_",
	Description:           "What Checkout charges for shipping. `terraform destroy` archives the shipping rate, because Stripe cannot delete one.",
	DataSourceDescription: "What Checkout charges for shipping.",
	Paths: engine.Paths{
		Create:   "/v1/shipping_rates",
		Retrieve: "/v1/shipping_rates/{id}",
		Update:   "/v1/shipping_rates/{id}",
		List:     "/v1/shipping_rates",
	},
	// Stripe reports an unset `tax_behavior` as `unspecified`.
	ServerDefaulted: []string{"tax_behavior"},
	// Stripe refuses to change a `tax_behavior` once it is set (suite: "update tax behavior once
	// specified"). A shipping rate is archived on destroy, so a change replaces it, as a change
	// to its amount does.
	FixedOnceSet: map[string]string{"tax_behavior": "unspecified"},
	// Stripe returns the currency options only when asked for, as the suite's requests ask
	// (TestShippingRateAttributes).
	Expand: []string{"fixed_amount.currency_options"},
	// Stripe returns the rate's own currency among the currency options, with its amount and tax
	// behavior (the record's normalised forms `with_own_currency` and `with_own_tax_behavior`:
	// TestShippingRateAttributes, `fixed_amount.currency_options.amount` and `.tax_behavior`), so a
	// read leaves it out.
	OwnCurrency: map[string]string{"fixed_amount.currency_options": "fixed_amount.currency"},
	Delete:      engine.Archive,
	Archive:     &engine.ArchiveAction{Field: "active", Value: wire.Bool(false)},
}

// NewResource returns the resource constructor the provider registers.
func NewResource() (func() resource.Resource, error) {
	return engine.NewResource(Declaration, shipping_rate.Table)
}

// NewDataSource returns the data source constructor. No shipping rate field is documented as unique, so the data source looks up by `id` only.
func NewDataSource() (func() datasource.DataSource, error) {
	return engine.NewDataSource(Declaration, shipping_rate.Table)
}
