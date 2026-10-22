// Package treasury_financial_account declares the stripe_treasury_financial_account resource
// and data source: a Treasury account that holds money on a connected account.
package treasury_financial_account

import (
	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/resource"

	"github.com/computomatic/terraform-provider-stripe/internal/engine"
	"github.com/computomatic/terraform-provider-stripe/internal/generated/treasury_financial_account"
)

// Declaration is what the spec cannot say about a financial account. Treasury runs on a
// connected account with the `treasury` capability active (suite: "Treasury reads on a US
// connected account"). The suite creates one with `supported_currencies` `usd` and the
// `card_issuing` feature requested, which is active within a second, and reads it back `open`
// ("Issuing cards on a connected account need a Treasury financial account",
// "TestTreasuryFinancialAccount"). `features` is a hash of `requested` flags that Stripe answers
// with feature statuses, and only when expanded, so it is request-only, as an account's
// `capabilities` is, and the statuses are read from `active_features`, `pending_features` and
// `restricted_features`. Nothing proves that an update requests or drops a feature, so a change
// to `features` is refused. Stripe has a close endpoint, but nothing proves closing, and Stripe
// cannot delete a financial account, so destroy is refused. The suite reads `supported_currencies` as a set of currencies.
var Declaration = engine.Declaration{
	TypeName: "stripe_treasury_financial_account",
	IDPrefix: "fa_",
	Description: "A Treasury financial account that holds money on a connected account and that Issuing cards draw on. " +
		"Treasury runs on a platform's connected accounts, so manage financial accounts through a provider configuration whose `stripe_account` is a connected account with the `treasury` capability active. " +
		"`features` requests features when the account is created; `active_features`, `pending_features` and `restricted_features` say how Stripe answered. A plan that changes `features` afterwards fails.",
	DataSourceDescription: "A Treasury financial account that holds money on a connected account. Look one up by its `id`.",
	ImportNote:            "Stripe does not return which features were requested, so an imported financial account has no `features` in state. Add `lifecycle { ignore_changes = [features] }` to a configuration that sets it, because a plan that changes `features` fails.",
	ImportNoteLevel:       engine.NoteWarning,
	Paths: engine.Paths{
		Create:   "/v1/treasury/financial_accounts",
		Retrieve: "/v1/treasury/financial_accounts/{id}",
		Update:   "/v1/treasury/financial_accounts/{id}",
		List:     "/v1/treasury/financial_accounts",
	},
	Delete:       engine.Refuse,
	RefuseReason: "Stripe cannot delete a financial account, and the provider does not close one.",
	RefuseChange: []string{"features"},
	Sets:         []string{"supported_currencies"},
	Sensitive:    []string{"financial_addresses.aba.account_number"},
}

// NewResource returns the resource constructor the provider registers.
func NewResource() (func() resource.Resource, error) {
	return engine.NewResource(Declaration, treasury_financial_account.Table)
}

// NewDataSource returns the data source constructor. It looks a financial account up by `id`
// only: a nickname is not unique.
func NewDataSource() (func() datasource.DataSource, error) {
	return engine.NewDataSource(Declaration, treasury_financial_account.Table)
}
