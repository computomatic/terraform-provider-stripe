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
// with each requested feature's status, and only when expanded (TestTreasuryFinancialAccountAttributes),
// so the engine expands it on every request. Stripe takes an update of `features` and ignores it
// (the record's update leaves are `accepted_ignored`), so a change to `features` is refused.
// Stripe takes `forwarding_settings` only on a closed account, and refuses it on an open one
// (probe `isolated/forward_while_open`), and never returns it. Stripe has a close endpoint, but
// the provider does not close an account, and Stripe cannot delete one, so destroy is refused.
// The suite reads `supported_currencies` as a set of currencies. Stripe fills in
// `platform_restrictions`, unrestricted, when the request leaves it out
// ("TestTreasuryFinancialAccount", "list").
var Declaration = engine.Declaration{
	TypeName: "stripe_treasury_financial_account",
	IDPrefix: "fa_",
	Description: "A Treasury financial account that holds money on a connected account and that Issuing cards draw on. " +
		"Treasury runs on a platform's connected accounts, so manage financial accounts through a provider configuration whose `stripe_account` is a connected account with the `treasury` capability active. " +
		"`features` requests features when the account is created, and reads back each requested feature's `status`; `active_features`, `pending_features` and `restricted_features` list them too. A plan that changes `features` afterwards fails. " +
		"Stripe takes `forwarding_settings`, where the money of a closed account goes, only once the account is closed, which the provider does not do: close it with Stripe's API or Dashboard first.",
	DataSourceDescription: "A Treasury financial account that holds money on a connected account. Look one up by its `id`.",
	Paths: engine.Paths{
		Create:   "/v1/treasury/financial_accounts",
		Retrieve: "/v1/treasury/financial_accounts/{id}",
		Update:   "/v1/treasury/financial_accounts/{id}",
		List:     "/v1/treasury/financial_accounts",
	},
	Delete:       engine.Refuse,
	RefuseReason: "Stripe cannot delete a financial account, and the provider does not close one.",
	// Stripe returns `features` only when asked to expand it (suite:
	// TestTreasuryFinancialAccountAttributes, which expands it on every request).
	Expand:          []string{"features"},
	RefuseChange:    []string{"features"},
	ServerDefaulted: []string{"platform_restrictions"},
	Sets:            []string{"supported_currencies"},
	Sensitive:       []string{"financial_addresses.aba.account_number"},
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
