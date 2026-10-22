// Package issuing_personalization_design declares the stripe_issuing_personalization_design
// resource and data source: the look of a physical card, with its logo and carrier letter.
package issuing_personalization_design

import (
	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/resource"

	"github.com/computomatic/terraform-provider-stripe/internal/engine"
	"github.com/computomatic/terraform-provider-stripe/internal/generated/issuing_personalization_design"
)

// Declaration is what the spec cannot say about a personalization design. It lives on a
// connected account with Issuing, on one of the physical bundles Stripe offers there, and the
// bundles the suite sees require a card logo: an `issuing_logo` file, which Terraform cannot
// upload, so `card_logo` takes the file's ID (suite: "Issuing personalization designs (first
// run)"). A new design is in `review`; Stripe moves it to `active` or `rejected`, and from
// `active` to `inactive`, and only test helpers do so in a sandbox, so `status` and
// `rejection_reasons` are outputs. An update changes `name` (the empty string clears it),
// `lookup_key` (releasing the old one) and `metadata` (suite: "update"); nothing proves a
// change of the bundle, the logo or the carrier text, so those are refused. Stripe has no
// DELETE route (suite: "delete is not a route"), and only the deactivate test helper, which
// live mode lacks, ends a design, so destroy leaves it. Its `lookup_key` stays taken: a create
// with a key in use is refused (suite: "lookup key in use"), so a replacement that keeps the key
// fails, and the transfer Stripe offers is not proven. Because the key is unique, the data
// source also finds a design by it.
var Declaration = engine.Declaration{
	TypeName: "stripe_issuing_personalization_design",
	IDPrefix: "ipcd_",
	Description: "The look of a physical Issuing card: its physical bundle, card logo and carrier letter. " +
		"Stripe issues cards only for a platform's connected accounts, so manage designs through a provider configuration whose `stripe_account` is a connected account with the `card_issuing` capability active. " +
		"`card_logo` is the ID of a file uploaded with the purpose `issuing_logo`, which a bundle whose `features.card_logo` is `required` needs; Terraform cannot upload files. " +
		"Stripe reviews a new design: `status` reads `review` until Stripe makes it `active` or `rejected`, with its reasons in `rejection_reasons`. " +
		"A plan that changes `physical_bundle`, `card_logo` or `carrier_text` of an existing design fails: create a new design instead.\n\n" +
		"~> Stripe cannot delete a personalization design, so `terraform destroy` leaves it on the connected account, and its `lookup_key` stays taken. A replacement that keeps the `lookup_key` therefore fails: give the new design another key in the same change.",
	DataSourceDescription: "The look of a physical Issuing card, on a connected account: its physical bundle, card logo and carrier letter. Look one up by its `id` or its `lookup_key`.",
	Paths: engine.Paths{
		Create:   "/v1/issuing/personalization_designs",
		Retrieve: "/v1/issuing/personalization_designs/{id}",
		Update:   "/v1/issuing/personalization_designs/{id}",
		List:     "/v1/issuing/personalization_designs",
	},
	Delete:        engine.Retain,
	RetainWarning: "Only Stripe's review ends a personalization design, so it stays, and keeps its `lookup_key`.",
	RefuseChange:  []string{"physical_bundle", "card_logo", "carrier_text"},
	Lookups:       []engine.Lookup{{Field: "lookup_key", Param: "lookup_keys", Array: true}},
}

// NewResource returns the resource constructor the provider registers.
func NewResource() (func() resource.Resource, error) {
	return engine.NewResource(Declaration, issuing_personalization_design.Table)
}

// NewDataSource returns the data source constructor. It looks a design up by `id` or by
// `lookup_key`, which Stripe keeps unique and the list filters by.
func NewDataSource() (func() datasource.DataSource, error) {
	return engine.NewDataSource(Declaration, issuing_personalization_design.Table)
}
