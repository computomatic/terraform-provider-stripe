// Package billing_credit_grant declares the stripe_billing_credit_grant resource and data source.
package billing_credit_grant

import (
	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/resource"

	"github.com/computomatic/terraform-provider-stripe/internal/engine"
	"github.com/computomatic/terraform-provider-stripe/internal/generated/billing_credit_grant"
)

// Declaration is what the spec cannot say about a billing credit grant. Stripe never deletes
// a grant, so destroy voids it (suite: "Billing credit grants", `void`) and drops it from
// state; the voided grant stays in the account. Stripe refuses to void a grant that already
// expired or was voided, so destroy leaves those as they are.
var Declaration = engine.Declaration{
	TypeName:              "stripe_billing_credit_grant",
	IDPrefix:              "credgr_",
	Description:           "Billing credits given to a customer. `terraform destroy` voids the grant, because Stripe cannot delete one: the unused credits can no longer be applied, and the grant stays in the account. Only `expires_at` and `metadata` can change after creation.",
	DataSourceDescription: "Billing credits given to a customer.",
	Paths: engine.Paths{
		Create:   "/v1/billing/credit_grants",
		Retrieve: "/v1/billing/credit_grants/{id}",
		Update:   "/v1/billing/credit_grants/{id}",
		List:     "/v1/billing/credit_grants",
	},
	// Stripe starts a grant at once, with priority 50.
	ServerDefaulted: []string{"effective_at", "priority"},
	Delete:          engine.Archive,
	Archive: &engine.ArchiveAction{
		Field: "voided_at",
		Now:   true,
		Path:  "/v1/billing/credit_grants/{id}/void",
		Ended: []engine.Ended{{Field: "expires_at", Past: true}},
	},
}

// NewResource returns the resource constructor the provider registers.
func NewResource() (func() resource.Resource, error) {
	return engine.NewResource(Declaration, billing_credit_grant.Table)
}

// NewDataSource returns the data source constructor. Stripe documents no credit grant field as unique, so the data source looks up by `id` only.
func NewDataSource() (func() datasource.DataSource, error) {
	return engine.NewDataSource(Declaration, billing_credit_grant.Table)
}
