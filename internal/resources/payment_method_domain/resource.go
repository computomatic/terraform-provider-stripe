// Package payment_method_domain declares the stripe_payment_method_domain resource and data source.
package payment_method_domain

import (
	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/resource"

	"github.com/computomatic/terraform-provider-stripe/internal/engine"
	"github.com/computomatic/terraform-provider-stripe/internal/generated/payment_method_domain"
	"github.com/computomatic/terraform-provider-stripe/internal/wire"
)

// Declaration is what the spec cannot say about a payment method domain. Stripe never
// deletes one (DELETE answers with an error), so destroy disables it (`enabled = false`) and
// drops it from state.
var Declaration = engine.Declaration{
	TypeName:    "stripe_payment_method_domain",
	IDPrefix:    "pmd_",
	Description: "A domain on which payment methods such as Apple Pay and Google Pay are offered. `terraform destroy` disables the domain, because Stripe cannot delete one; the domain stays in the account.",
	Paths: engine.Paths{
		Create:   "/v1/payment_method_domains",
		Retrieve: "/v1/payment_method_domains/{id}",
		Update:   "/v1/payment_method_domains/{id}",
		List:     "/v1/payment_method_domains",
	},
	Delete:  engine.Archive,
	Archive: &engine.ArchiveAction{Field: "enabled", Value: wire.Bool(false)},
}

// NewResource returns the resource constructor the provider registers.
func NewResource() (func() resource.Resource, error) {
	return engine.NewResource(Declaration, payment_method_domain.Table)
}

// NewDataSource returns the data source constructor. Stripe accepts one domain name more than once, so the data source looks up by `id` only.
func NewDataSource() (func() datasource.DataSource, error) {
	return engine.NewDataSource(Declaration, payment_method_domain.Table)
}
