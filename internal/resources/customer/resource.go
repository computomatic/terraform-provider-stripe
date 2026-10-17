// Package customer declares the stripe_customer resource and data source.
package customer

import (
	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/resource"

	"github.com/computomatic/terraform-provider-stripe/internal/engine"
	"github.com/computomatic/terraform-provider-stripe/internal/generated/customer"
)

// Declaration is what the spec cannot say about a customer.
var Declaration = engine.Declaration{
	TypeName:    "stripe_customer",
	IDPrefix:    "cus_",
	Description: "A person or business that pays you. `terraform destroy` deletes the customer, which also cancels their subscriptions.",
	Paths: engine.Paths{
		Create:   "/v1/customers",
		Retrieve: "/v1/customers/{id}",
		Update:   "/v1/customers/{id}",
		Delete:   "/v1/customers/{id}",
		List:     "/v1/customers",
	},
	// Stripe fills these in on every customer although the response schema is nullable.
	ServerDefaulted: []string{"balance", "invoice_prefix", "invoice_settings", "next_invoice_sequence", "tax_exempt"},
	Delete:          engine.Delete,
}

// NewResource returns the resource constructor the provider registers.
func NewResource() (func() resource.Resource, error) {
	return engine.NewResource(Declaration, customer.Table)
}

// NewDataSource returns the data source constructor. Stripe documents no customer field as unique (`email` is not), so the data source looks up by `id` only.
func NewDataSource() (func() datasource.DataSource, error) {
	return engine.NewDataSource(Declaration, customer.Table)
}
