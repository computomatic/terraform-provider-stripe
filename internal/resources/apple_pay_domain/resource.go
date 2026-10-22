// Package apple_pay_domain declares the stripe_apple_pay_domain resource and data source.
package apple_pay_domain

import (
	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/resource"

	"github.com/computomatic/terraform-provider-stripe/internal/engine"
	"github.com/computomatic/terraform-provider-stripe/internal/generated/apple_pay_domain"
)

// Declaration is what the spec cannot say about an Apple Pay domain. Stripe deletes one with
// DELETE and answers 404 afterwards, so destroy deletes it. A domain name can be registered
// more than once, so `domain_name` is a lookup key that fails on an ambiguous match.
var Declaration = engine.Declaration{
	TypeName:              "stripe_apple_pay_domain",
	IDPrefix:              "apwc_",
	Description:           "A domain registered for Apple Pay. `terraform destroy` deletes the domain. Stripe accepts the same domain name more than once, so changing `domain_name` creates a new registration.",
	DataSourceDescription: "A domain registered for Apple Pay.",
	Paths: engine.Paths{
		Create:   "/v1/apple_pay/domains",
		Retrieve: "/v1/apple_pay/domains/{id}",
		Delete:   "/v1/apple_pay/domains/{id}",
		List:     "/v1/apple_pay/domains",
	},
	Delete:  engine.Delete,
	Lookups: []engine.Lookup{{Field: "domain_name"}},
}

// NewResource returns the resource constructor the provider registers.
func NewResource() (func() resource.Resource, error) {
	return engine.NewResource(Declaration, apple_pay_domain.Table)
}

// NewDataSource returns the data source constructor. It finds a domain by `id` or by `domain_name`; two registrations of one name make the lookup ambiguous.
func NewDataSource() (func() datasource.DataSource, error) {
	return engine.NewDataSource(Declaration, apple_pay_domain.Table)
}
