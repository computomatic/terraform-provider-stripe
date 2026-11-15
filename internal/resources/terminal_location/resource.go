// Package terminal_location declares the stripe_terminal_location resource and data source.
package terminal_location

import (
	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/resource"

	"github.com/computomatic/terraform-provider-stripe/internal/engine"
	"github.com/computomatic/terraform-provider-stripe/internal/generated/terminal_location"
)

// Declaration is what the spec cannot say about a terminal location. Stripe deletes one with
// DELETE and answers a later GET with a deleted stub, so destroy deletes it. Stripe refuses
// to change a location's country after creation. It answers an omitted `address.line2` with
// `""` (suite: "create minimal") and refuses a US location without `address.state` (suite:
// "create in the US without address state"). The kana and kanji display names and addresses are
// proven only on the suite's JP account, which made its locations with them and no latin address
// (`isolated/jp`, `jp_address`); the provider cannot tell the account's country at plan, so they
// are arguments everywhere and Stripe decides at apply.
// The record's creates without a script address's `line2` (`isolated/jp`, `jp_unset`) do not
// read it, and its default, absent, is the US minimal create's, which sends no script address;
// on the dev sandbox Stripe answered such a create with `""` (pin 18's smoke run), so an unset
// `line2` is server-filled and known after apply, and removing it leaves the value Stripe holds.
// The record shows an update taking `""` and holding it (`isolated/jp_address_unset`), so
// `line2 = ""` clears it.
var Declaration = engine.Declaration{
	TypeName:              "stripe_terminal_location",
	IDPrefix:              "tml_",
	Description:           "A place where you use Stripe Terminal readers. `terraform destroy` deletes the location. Changing the country of `address` creates a new location, because Stripe cannot change it. A location in the US needs `address.state`. The kana and kanji display names and addresses are for a location of a Japanese account, such as the connected account a provider block names in `stripe_account`: the provider sends them wherever you set them, because it does not know the account's country when it plans, and Stripe decides when you apply. Stripe fills a script address's `line2` the configuration leaves unset, so removing it leaves the current value; set it to `\"\"` to clear it.",
	DataSourceDescription: "A place where you use Stripe Terminal readers.",
	Paths: engine.Paths{
		Create:   "/v1/terminal/locations",
		Retrieve: "/v1/terminal/locations/{id}",
		Update:   "/v1/terminal/locations/{id}",
		Delete:   "/v1/terminal/locations/{id}",
		List:     "/v1/terminal/locations",
	},
	Delete:          engine.Delete,
	ReplaceOn:       []string{"address.country"},
	ServerDefaulted: []string{"address.line2", "address_kana.line2", "address_kanji.line2"},
}

// NewResource returns the resource constructor the provider registers.
func NewResource() (func() resource.Resource, error) {
	return engine.NewResource(Declaration, terminal_location.Table)
}

// NewDataSource returns the data source constructor. No location field is documented as unique, so the data source looks up by `id` only.
func NewDataSource() (func() datasource.DataSource, error) {
	return engine.NewDataSource(Declaration, terminal_location.Table)
}
