// Package billing_credit_grant declares the stripe_billing_credit_grant resource and data source.
package billing_credit_grant

import (
	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/resource"

	"github.com/computomatic/terraform-provider-stripe/internal/engine"
	"github.com/computomatic/terraform-provider-stripe/internal/generated/billing_credit_grant"
)

// Declaration is what the spec cannot say about a billing credit grant. Stripe never deletes
// a grant, and voiding or expiring one is final (suite: "Billing credit grants", "void again",
// "expire again"), so destroy is refused. `expires_at` ends a grant at a time.
var Declaration = engine.Declaration{
	TypeName:              "stripe_billing_credit_grant",
	IDPrefix:              "credgr_",
	Description:           "Billing credits given to a customer. Only `expires_at` and `metadata` can change after creation.",
	DataSourceDescription: "Billing credits given to a customer.",
	Paths: engine.Paths{
		Create:   "/v1/billing/credit_grants",
		Retrieve: "/v1/billing/credit_grants/{id}",
		Update:   "/v1/billing/credit_grants/{id}",
		List:     "/v1/billing/credit_grants",
	},
	// Stripe starts a grant at once, with priority 50.
	ServerDefaulted: []string{"effective_at", "priority"},
	Delete:          engine.Refuse,
	RefuseReason:    "Stripe cannot delete a credit grant; set `expires_at` to end one.",
}

// NewResource returns the resource constructor the provider registers.
func NewResource() (func() resource.Resource, error) {
	return engine.NewResource(Declaration, billing_credit_grant.Table)
}

// NewDataSource returns the data source constructor. Stripe documents no credit grant field as unique, so the data source looks up by `id` only.
func NewDataSource() (func() datasource.DataSource, error) {
	return engine.NewDataSource(Declaration, billing_credit_grant.Table)
}
