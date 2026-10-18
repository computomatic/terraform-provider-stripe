// Package price declares the stripe_price resource and data source.
package price

import (
	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/resource"

	"github.com/computomatic/terraform-provider-stripe/internal/engine"
	"github.com/computomatic/terraform-provider-stripe/internal/generated/price"
	"github.com/computomatic/terraform-provider-stripe/internal/wire"
)

// Declaration is what the spec cannot say about a price. Stripe never deletes a price, so
// destroy archives it (`active = false`) and drops it from state; the price stays in the
// account.
var Declaration = engine.Declaration{
	TypeName:    "stripe_price",
	IDPrefix:    "price_",
	Description: "What a product costs and how often it is charged. `terraform destroy` archives the price, because Stripe cannot delete one.",
	Paths: engine.Paths{
		Create:   "/v1/prices",
		Retrieve: "/v1/prices/{id}",
		Update:   "/v1/prices/{id}",
		List:     "/v1/prices",
	},
	// Stripe returns tiers and currency options only when asked for.
	Expand: []string{"currency_options", "tiers"},
	// The last tier of a graduated or volume price is unbounded: Stripe returns its `up_to`
	// as null, and the configuration says `inf`.
	NullAs: map[string]string{"tiers.up_to": "inf", "currency_options.tiers.up_to": "inf"},
	// Stripe derives each amount from the other, in tiers and currency options too, and
	// reports an unset `tax_behavior` as `unspecified`.
	ServerDefaulted: []string{
		"unit_amount", "unit_amount_decimal", "tax_behavior",
		"tiers.unit_amount", "tiers.unit_amount_decimal", "tiers.flat_amount", "tiers.flat_amount_decimal",
		"currency_options.unit_amount", "currency_options.unit_amount_decimal", "currency_options.tax_behavior",
		"currency_options.tiers.unit_amount", "currency_options.tiers.unit_amount_decimal",
		"currency_options.tiers.flat_amount", "currency_options.tiers.flat_amount_decimal",
	},
	// Stripe returns the price's own currency among the options and refuses it as one.
	CurrencyOptions: "currency_options",
	Delete:          engine.Archive,
	Archive:         &engine.ArchiveAction{Field: "active", Value: wire.Bool(false)},
	Lookups:         []engine.Lookup{{Field: "lookup_key", Param: "lookup_keys", Array: true}},
}

// NewResource returns the resource constructor the provider registers.
func NewResource() (func() resource.Resource, error) {
	return engine.NewResource(Declaration, price.Table)
}

// NewDataSource returns the data source constructor. It looks a price up by `id` or by `lookup_key`, which Stripe keeps unique per account.
func NewDataSource() (func() datasource.DataSource, error) {
	return engine.NewDataSource(Declaration, price.Table)
}
