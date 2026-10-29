// Package treasury_debit_reversal declares the stripe_treasury_debit_reversal resource and data
// source.
package treasury_debit_reversal

import (
	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/resource"

	"github.com/computomatic/terraform-provider-stripe/internal/engine"
	"github.com/computomatic/terraform-provider-stripe/internal/generated/treasury_debit_reversal"
)

// Declaration is what the spec cannot say about a debit reversal. It moves money, so the
// live-mode guard applies. It reverses a received debit of a Treasury financial account on a
// connected account, once (suite: "Credit and debit reversals"). Stripe moves a new reversal
// to `succeeded` by itself about two seconds later, and only then gives it a `transaction`.
// Stripe has no update and no cancel for a reversal, so every argument is create-only and there
// is no `status` to set, and it cannot delete one, so destroy is refused.
var Declaration = engine.Declaration{
	TypeName: "stripe_treasury_debit_reversal",
	IDPrefix: "debrev_",
	Description: "The reversal of a received debit, which returns the money to the Treasury financial account. " +
		"Treasury runs on a platform's connected accounts, so manage debit reversals through a provider configuration whose `stripe_account` is the connected account. " +
		"Stripe reverses a received debit only once, and only while its `reversal_details.restricted_reason` is null. " +
		"Stripe moves a new reversal from `processing` to `succeeded` by itself within seconds, and only then sets its `transaction`. A reversal cannot be canceled, and nothing changes after creation.",
	DataSourceDescription: "The reversal of a received debit, which returns the money to the financial account. It lives on a connected account, so read it through a provider configuration whose `stripe_account` is that account. Look one up by its `id`.",
	Paths: engine.Paths{
		Create:   "/v1/treasury/debit_reversals",
		Retrieve: "/v1/treasury/debit_reversals/{id}",
		List:     "/v1/treasury/debit_reversals",
	},
	MovesMoney:   true,
	Delete:       engine.Refuse,
	RefuseReason: "Stripe cannot delete or cancel a debit reversal.",
}

// NewResource returns the resource constructor the provider registers.
func NewResource() (func() resource.Resource, error) {
	return engine.NewResource(Declaration, treasury_debit_reversal.Table)
}

// NewDataSource returns the data source constructor. It looks the object up by `id` only: Stripe documents no field of it as unique.
func NewDataSource() (func() datasource.DataSource, error) {
	return engine.NewDataSource(Declaration, treasury_debit_reversal.Table)
}
