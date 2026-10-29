// Package treasury_credit_reversal declares the stripe_treasury_credit_reversal resource and
// data source.
package treasury_credit_reversal

import (
	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/resource"

	"github.com/computomatic/terraform-provider-stripe/internal/engine"
	"github.com/computomatic/terraform-provider-stripe/internal/generated/treasury_credit_reversal"
)

// Declaration is what the spec cannot say about a credit reversal. It moves money, so the
// live-mode guard applies. It reverses a received credit of a Treasury financial account on a
// connected account: Stripe reverses an ACH received credit once, before its
// `reversal_details.deadline`, and refuses a wire one (suite: "Credit and debit reversals").
// Stripe posts a new reversal by itself about six seconds later. Stripe has no update and no
// cancel for a reversal, so every argument is create-only and there is no `status` to set, and
// it cannot delete one, so destroy is refused.
var Declaration = engine.Declaration{
	TypeName: "stripe_treasury_credit_reversal",
	IDPrefix: "credrev_",
	Description: "The return of a received credit to its sender, which takes the money back out of the Treasury financial account. " +
		"Treasury runs on a platform's connected accounts, so manage credit reversals through a provider configuration whose `stripe_account` is the connected account. " +
		"Stripe reverses a received credit only once, and only while its `reversal_details.restricted_reason` is null: it refuses one sent by wire, or reversed already. " +
		"Stripe moves a new reversal from `processing` to `posted` by itself within seconds. A reversal cannot be canceled, and nothing changes after creation.",
	DataSourceDescription: "The return of a received credit to its sender. It lives on a connected account, so read it through a provider configuration whose `stripe_account` is that account. Look one up by its `id`.",
	Paths: engine.Paths{
		Create:   "/v1/treasury/credit_reversals",
		Retrieve: "/v1/treasury/credit_reversals/{id}",
		List:     "/v1/treasury/credit_reversals",
	},
	MovesMoney:   true,
	Delete:       engine.Refuse,
	RefuseReason: "Stripe cannot delete or cancel a credit reversal.",
}

// NewResource returns the resource constructor the provider registers.
func NewResource() (func() resource.Resource, error) {
	return engine.NewResource(Declaration, treasury_credit_reversal.Table)
}

// NewDataSource returns the data source constructor. It looks the object up by `id` only: Stripe documents no field of it as unique.
func NewDataSource() (func() datasource.DataSource, error) {
	return engine.NewDataSource(Declaration, treasury_credit_reversal.Table)
}
