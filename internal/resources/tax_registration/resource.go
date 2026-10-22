// Package tax_registration declares the stripe_tax_registration resource and data source.
package tax_registration

import (
	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/resource"

	"github.com/computomatic/terraform-provider-stripe/internal/engine"
	"github.com/computomatic/terraform-provider-stripe/internal/generated/tax_registration"
)

// Declaration is what the spec cannot say about a tax registration. Stripe never deletes one;
// it ends at `expires_at`, which is final, so destroy is refused. `active_from` takes `now`,
// which Stripe answers with a timestamp, so the configured value is kept.
var Declaration = engine.Declaration{
	TypeName:              "stripe_tax_registration",
	IDPrefix:              "taxreg_",
	Description:           "A place where you are registered to collect tax. Set `expires_at` to end the registration. A country keeps one live registration, so a new one can only be created once the old one has expired.",
	DataSourceDescription: "A place where you are registered to collect tax.",
	Paths: engine.Paths{
		Create:   "/v1/tax/registrations",
		Retrieve: "/v1/tax/registrations/{id}",
		Update:   "/v1/tax/registrations/{id}",
		List:     "/v1/tax/registrations",
	},
	Delete:         engine.Refuse,
	RefuseReason:   "Stripe cannot delete a tax registration, and it keeps collecting tax until `expires_at`; set `expires_at` to end one.",
	PreserveConfig: []string{"active_from"},
}

// NewResource returns the resource constructor the provider registers.
func NewResource() (func() resource.Resource, error) {
	return engine.NewResource(Declaration, tax_registration.Table)
}

// NewDataSource returns the data source constructor. The list endpoint filters by `status` only, so the data source looks up by `id` only.
func NewDataSource() (func() datasource.DataSource, error) {
	return engine.NewDataSource(Declaration, tax_registration.Table)
}
