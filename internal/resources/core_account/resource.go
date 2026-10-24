// Package core_account declares the stripe_core_account resource and data source: a connected
// account created through Stripe's /v2 Accounts API (v2.core.account).
package core_account

import (
	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/resource"

	"github.com/computomatic/terraform-provider-stripe/internal/engine"
	"github.com/computomatic/terraform-provider-stripe/internal/generated/core_account"
)

// Declaration is what the spec cannot say about a v2 account. Stripe returns every includable
// field, but null unless the request includes it, and `configuration.<name>` fills only that
// configuration (suite: "retrieve with include"), so the engine includes all of them on every
// request that returns the account and state holds every configuration the account applies.
//
// The suite updates the display name, the contact email, the identity's registered name and
// the metadata, so the identity's country and entity type are create-only, and a change to
// the configurations, the dashboard or the defaults is refused: nothing proves Stripe applies
// one. Stripe does not return the capabilities a configuration requested, only their status,
// so the `requested` flags stay in state from the configuration. Stripe shows the country in
// upper case, so the configured one stays in state. An update merges the identity (suite:
// "update"), so its members are server-filled: one removed from the configuration keeps its
// value.
//
// Stripe refuses `dashboard` on an account that is only a customer (suite: "dashboard on a
// customer only account"). Its message names a wider rule, about merchants and recipients
// with the `stripe_transfers` capability, that the suite does not prove, so the refusal is
// left to Stripe at apply rather than checked at plan time.
//
// Stripe has no DELETE for a v2 account, so destroy is refused. Closing is a state change,
// proven by the suite ("close"): it must name exactly the account's applied configurations
// (`configs_must_match_to_close` otherwise), so the action is sent the account's own
// `applied_configurations` as it stands. Closing cannot be undone, and nothing proves a write
// to a closed account.
var Declaration = engine.Declaration{
	TypeName:              "stripe_core_account",
	IDPrefix:              "acct_",
	Description:           "A connected account created through Stripe's `/v2` Accounts API, whose configurations say how the platform uses it: as a customer, a merchant or a recipient. Each configuration requests one capability: `automatic_indirect_tax` for a customer, `card_payments` for a merchant and `stripe_balance.stripe_transfers` for a recipient; state holds every configuration the account has applied, with the status of each capability. Stripe refuses `dashboard` on an account that is only a customer. The account is also a v1 connected account with the same ID, which the `stripe_account` data source reads. Set `closed` to `true` to close the account, which cannot be undone; Stripe no longer returns a closed account's `identity`, so state keeps the values it had. Changing `identity.country` or `identity.entity_type` needs a new account. A plan that changes `configuration`, `dashboard` or `defaults` on an existing account fails, because changing them is not supported yet: revert the configuration, ignore the change, or remove the account from the state and apply again to create a new one. Stripe returns `identity.country` in upper case, so state keeps the country as you configured it. Stripe has no proven way to clear `display_name` or `contact_email`, so an apply that removes one from the configuration fails; removing the registered name leaves it on the account.",
	DataSourceDescription: "A connected account created through Stripe's `/v2` Accounts API, whose configurations say how the platform uses it: as a customer, a merchant or a recipient.",
	ImportNote:            "Stripe does not return the capabilities a configuration requested, so an imported account has no `requested` flags in `configuration`, and its `identity.country` is in upper case. Add `lifecycle { ignore_changes = [configuration] }` to a configuration that sets it, because a plan that changes `configuration` fails, and add `identity.country` to the list if the configuration writes the country in lower case, because a plan that changes it fails too.",
	ImportNoteLevel:       engine.NoteWarning,
	Paths: engine.Paths{
		Create:   "/v2/core/accounts",
		Retrieve: "/v2/core/accounts/{id}",
		Update:   "/v2/core/accounts/{id}",
		List:     "/v2/core/accounts",
	},
	Delete:       engine.Refuse,
	RefuseReason: "Stripe cannot delete an account created through `/v2`; set `closed` to `true` to close it.",
	Transitions:  []engine.Transition{{Field: "closed", Value: "true", Path: "/v2/core/accounts/{id}/close", Send: []string{"applied_configurations"}}},
	Include: []string{
		"configuration.customer", "configuration.merchant", "configuration.recipient",
		"defaults", "future_requirements", "identity", "requirements",
	},
	RefuseChange:   []string{"configuration", "dashboard", "defaults"},
	PreserveConfig: []string{"identity.country"},
	// A closed account's identity has no country or entity type (suite: "close"); the sandbox
	// sweep showed the whole identity null, which the suite's check cannot tell apart.
	KeepOmitted: []string{"identity", "identity.country", "identity.entity_type"},
	ServerDefaulted: []string{
		"defaults", "defaults.currency",
		"identity", "identity.business_details", "identity.business_details.registered_name",
	},
}

// NewResource returns the resource constructor the provider registers.
func NewResource() (func() resource.Resource, error) {
	return engine.NewResource(Declaration, core_account.Table)
}

// NewDataSource returns the data source constructor. Stripe documents no account field as unique, so the data source looks up by `id` only.
func NewDataSource() (func() datasource.DataSource, error) {
	return engine.NewDataSource(Declaration, core_account.Table)
}
