// Package issuing_cardholder declares the stripe_issuing_cardholder resource and data source:
// a person or business that Issuing cards are issued to.
package issuing_cardholder

import (
	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/resource"

	"github.com/computomatic/terraform-provider-stripe/internal/engine"
	"github.com/computomatic/terraform-provider-stripe/internal/generated/issuing_cardholder"
	"github.com/computomatic/terraform-provider-stripe/internal/wire"
)

// Declaration is what the spec cannot say about a cardholder. Stripe issues cards only for a
// platform's connected accounts, so the cardholder lives on one, reached through the
// provider's `stripe_account` (suite: "Issuing is for connected accounts on this platform").
// Stripe takes `name` and `type` on create only (they are `parameter_unknown` on update), and
// an update merges `individual` and replaces the billing address (suite: "update"). There is no
// DELETE route (suite: "delete is not a route"), so destroy deactivates the cardholder with
// `status=inactive`, which the suite proves and reverses ("update status"). Stripe answers a
// cardholder created without spending controls with an empty `spending_controls` object, so it
// is server-filled. Stripe never returns a company's `tax_id`, only `tax_id_provided`, so state
// keeps the configured value.
var Declaration = engine.Declaration{
	TypeName: "stripe_issuing_cardholder",
	IDPrefix: "ich_",
	Description: "A person or business that Issuing cards are issued to. " +
		"Stripe issues cards only for a platform's connected accounts, so manage cardholders through a provider configuration whose `stripe_account` is a connected account with the `card_issuing` capability active. " +
		"Stripe reviews a new cardholder before its cards can be activated: `requirements.disabled_reason` reads `under_review` until then.\n\n" +
		"~> Stripe cannot delete a cardholder, so `terraform destroy` sets its `status` to `inactive` and leaves it on the connected account. Changing `name` or `type` replaces the cardholder, which leaves the old one inactive.",
	DataSourceDescription: "A person or business that Issuing cards are issued to, on a connected account. Look one up by its `id`.",
	ImportNote:            "Stripe never returns a company's `tax_id`, so an imported cardholder has none in state, and a configuration that sets `company.tax_id` plans an update that sends it again.",
	Paths: engine.Paths{
		Create:   "/v1/issuing/cardholders",
		Retrieve: "/v1/issuing/cardholders/{id}",
		Update:   "/v1/issuing/cardholders/{id}",
		List:     "/v1/issuing/cardholders",
	},
	Delete:  engine.Archive,
	Archive: &engine.ArchiveAction{Field: "status", Value: wire.String("inactive")},
	// Stripe fills in an individual's `verification` with no document, and the limits'
	// currency from the account's when limits are set without one (suite: "TestIssuingCardholder").
	ServerDefaulted: []string{"individual.verification", "spending_controls", "spending_controls.spending_limits_currency"},
	Sensitive:       []string{"company.tax_id"},
}

// NewResource returns the resource constructor the provider registers.
func NewResource() (func() resource.Resource, error) {
	return engine.NewResource(Declaration, issuing_cardholder.Table)
}

// NewDataSource returns the data source constructor. It looks a cardholder up by `id` only: the
// list filters by email, which Stripe does not keep unique.
func NewDataSource() (func() datasource.DataSource, error) {
	return engine.NewDataSource(Declaration, issuing_cardholder.Table)
}
