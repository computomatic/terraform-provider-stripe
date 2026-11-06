// Package account_v2 declares the stripe_account_v2 resource and data source: a connected
// account created through Stripe's /v2 Accounts API (v2.core.account).
package account_v2

import (
	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/resource"

	"github.com/computomatic/terraform-provider-stripe/internal/engine"
	"github.com/computomatic/terraform-provider-stripe/internal/generated/account_v2"
)

// Declaration is what the spec cannot say about a v2 account. Stripe returns every includable
// field, but null unless the request includes it, and `configuration.<name>` fills only that
// configuration (suite: "retrieve with include"), so the engine includes all of them on every
// request that returns the account and state holds every configuration the account applies.
//
// The record's update leaves prove less than a change for some members, so those stay refused,
// because a change is what a plan would send. `dashboard` and the collectors: Stripe answers
// anything but the value they hold with `You cannot change ... via API once it has been set`, in
// the leaves' own refusals, and takes them on an account that has none, beside the recipient's
// `stripe_transfers` request, which it refuses without them (the record's answers to probes
// `isolated/change_adds_recipient` and `isolated/change_adds_recipient_with_collectors`), so a
// plan sets them while the state holds none and fails on any other change (SetOnce).
// `configuration.customer` and `configuration.merchant`: nothing proves an update of either.
// What a change does prove stays in place: the identity's country and entity type, the defaults'
// currency, locales and profile, the recipient configuration's `applied` and `stripe_transfers`
// request, and the contact phone. The country and the currency moved in place with the record of
// fake `v0.31.0`, whose probes change each on an account that held another (`isolated/change_*`):
// Stripe takes a country on an account with none and fills the currency from it, and it refused
// `GB` on a recipient that requests `stripe_transfers` (probe
// `isolated/change_country_on_recipient`). Stripe changes `applied_configurations` with the
// recipient's `applied` and its request (the record's `side_effects`); the record shows the
// request's effect only with `dashboard` `none` and both collectors `application` carried beside
// it (`carried` of probe `isolated/change_adds_recipient_with_collectors`), which is all
// `FilledFrom` needs, as it only leaves `applied_configurations` unknown. An update of
// `configuration`, `defaults`, `defaults.profile` or `identity` sends only the members that
// changed, because Stripe keeps those a request leaves out (family `v2`, `hashes`);
// `configuration.recipient` and `identity.business_details`, which the record does not list, go
// whole. Stripe refuses `defaults.responsibilities` in part (`hashes`, `refused`:
// `losses_collector: Required request field missing`), and it is only ever sent whole, with both
// collectors it requires. Stripe does not return the capabilities a configuration requested, only
// their status, so the `requested` flags stay in state from the configuration, and Stripe drops
// the recipient's `stripe_transfers` from its answer once it is no longer requested (the record's
// `side_effects`, probe `update/2`), so that flag is kept even then. Stripe answers an account
// with no configuration with a `configuration` of three nulls (the record's probe `update/1`), so
// `configuration` is server-filled. Stripe shows the country in upper case, so the configured one
// stays in state. Stripe refuses a null `identity.country` and ignores a null `dashboard` (the
// record's `unset`), so removing either keeps its value, as removing another member of the
// identity does. The record proves null unsets `contact_phone`,
// `identity.entity_type` and the members of `defaults.profile`.
//
// Stripe refuses `dashboard` on an account that is only a customer (suite: "dashboard on a
// customer only account"). Its message names a wider rule, about merchants and recipients
// with the `stripe_transfers` capability, that the suite does not prove, so the refusal is
// left to Stripe at apply rather than checked at plan time.
//
// Stripe has no DELETE for a v2 account, so destroy is refused. Closing is a state change,
// proven by the suite ("close"): it must name exactly the account's applied configurations
// (`configs_must_match_to_close` otherwise), so the action is sent the account's own
// `applied_configurations` as it stands. Closing cannot be undone, and Stripe refuses every
// update of a closed account with a 403 `forbidden` that names only a permission (suite:
// "update a closed account"), so the engine refuses at plan time any change to an account the
// state shows closed (KeepWhenClosed). An import of a closed account has none of what Stripe
// nulls, so a configuration that sets those fields meets that refusal too. The close answers `identity`, `configuration`, `defaults`,
// `requirements` and `future_requirements` null, and every read after it `dashboard`,
// `contact_email`, `display_name` and `metadata` too (suite: "close"), so a closed account
// keeps the state's value of every field Stripe nulls. What Stripe still returns, such as
// `closed` and `applied_configurations`, is read as usual, and so is an open account, where a
// null is drift. The close itself keeps the included fields its response nulls (Transition).
var Declaration = engine.Declaration{
	TypeName:              "stripe_account_v2",
	IDPrefix:              "acct_",
	Description:           "A connected account created through Stripe's `/v2` Accounts API, whose configurations say how the platform uses it: as a customer, a merchant or a recipient. Each configuration requests one capability: `automatic_indirect_tax` for a customer, `card_payments` for a merchant and `stripe_balance.stripe_transfers` for a recipient; state holds every configuration the account has applied, with the status of each capability. Stripe refuses `dashboard` on an account that is only a customer. The account is also a v1 connected account with the same ID, which the `stripe_account` data source reads. Set `closed` to `true` to close the account, which cannot be undone; Stripe returns most fields of a closed account as null, so state keeps the values they had before the close. Stripe refuses any change to a closed account, so once it is closed a plan that changes it fails. `identity.country`, `identity.entity_type`, `identity.business_details.registered_name`, `defaults.currency`, `defaults.locales`, `defaults.profile` and the recipient configuration's `applied` and `capabilities.stripe_balance.stripe_transfers.requested` change in place; changing the country can fill `defaults.currency`, and changing the recipient configuration can move `applied_configurations` and the status of the recipient's capabilities. An account without a dashboard takes `dashboard` and the collectors in `defaults.responsibilities` on update, as adding the recipient configuration to it needs, but Stripe refuses a change of them once they are set, so a plan that changes one the account holds fails, as does one that changes `configuration.customer` or `configuration.merchant`, because changing them is not supported yet: revert the configuration, ignore the change, or remove the account from the state and apply again to create a new one. Stripe returns `identity.country` in upper case, so state keeps the country as you configured it. Stripe has no proven way to clear `display_name` or `contact_email`, so an apply that removes one from the configuration fails; removing `dashboard`, `identity.country` or the registered name leaves it on the account, and removing `contact_phone`, `identity.entity_type` or a member of `defaults.profile` clears it.",
	DataSourceDescription: "A connected account created through Stripe's `/v2` Accounts API, whose configurations say how the platform uses it: as a customer, a merchant or a recipient.",
	ImportNote:            "Stripe does not return the capabilities a configuration requested, so an imported account has no `requested` flags in `configuration`, and its `identity.country` is in upper case. Add `lifecycle { ignore_changes = [configuration] }` to a configuration that sets it, because a plan that changes `configuration.customer` or `configuration.merchant` fails, and add `identity.country` to the list if the configuration writes the country in lower case, because a plan would otherwise send the country again. An imported closed account has no values for the fields Stripe returns as null, and a plan that changes a closed account fails, so its configuration needs `lifecycle { ignore_changes = all }`.",
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
	RefuseChange: []string{"configuration.customer", "configuration.merchant", "dashboard", "defaults.responsibilities"},
	SetOnce:      []string{"dashboard", "defaults.responsibilities"},
	FilledFrom: map[string][]string{
		"identity.country":                {"defaults.currency"},
		"configuration.recipient.applied": {"applied_configurations"},
		"configuration.recipient.capabilities.stripe_balance.stripe_transfers.requested": {"applied_configurations"},
	},
	Answers: map[string]string{
		// suite: TestV2CoreAccountAttributes, probe `isolated/hash_responsibilities`
		"defaults.responsibilities.fees_collector": "Stripe takes it on an update only beside `losses_collector`, which an update sends with it.",
		// suite: TestV2CoreAccountAttributes, probes `isolated/change_adds_recipient` and
		// `isolated/change_adds_recipient_with_collectors`
		"configuration.recipient.capabilities.stripe_balance.stripe_transfers.requested": "Stripe takes the request on an update of an account without a dashboard only beside `dashboard` and both collectors of `defaults.responsibilities`.",
		// suite: TestV2CoreAccountAttributes, probes `isolated/url_*`
		"defaults.profile.business_url": "Stripe judges the URL by its value: an update took `https://accessible.stripe.com`, `https://www.iana.org` and `https://no-such-host.invalid` and refused `https://stripe.com`, `https://docs.stripe.com`, `https://example.com` and `https://example.com/acme`, and a create took `https://stripe.com` and `https://example.com/acme` on an account without the recipient configuration and refused `https://stripe.com` on one with it.",
		// suite: TestV2CoreAccountAttributes, probes `isolated/change_country`,
		// `isolated/change_country_from_none` and `isolated/change_country_on_recipient`
		"identity.country": "Stripe took `GB` on an update of an account in `US`, and `US` on an account that had no country, and refused `GB` on an account whose recipient configuration requests `stripe_transfers`.",
	},
	// Stripe keeps the members of `configuration.recipient` an update leaves out (probe
	// `isolated/hash_recipient`), and takes `applied` and the transfers request each alone
	// (`isolated/change_applied_to_false`, `isolated/transfers_requested_false`) and together
	// (`isolated/recipient_unapplied_whole`, `isolated/recipient_pair_changed`,
	// `isolated/recipient_pair_added`), so an update sends the members that changed. The
	// recipient's capabilities go whole, as every probe sent them; nothing proves a member of
	// `identity.business_details` alone.
	SendChangedMembers: []string{"configuration", "configuration.recipient", "defaults", "defaults.profile", "identity"},
	SendWholeMembers:   []string{"configuration.recipient.capabilities", "identity.business_details"},
	ClearsWithNull:     []string{"contact_phone"},
	PreserveConfig:     []string{"identity.country", "configuration.recipient.capabilities.stripe_balance.stripe_transfers.requested"},
	KeepWhenClosed:     "closed",
	ServerDefaulted: []string{
		"configuration", "dashboard", "defaults", "defaults.currency",
		"identity", "identity.business_details", "identity.business_details.registered_name", "identity.country",
	},
}

// NewResource returns the resource constructor the provider registers.
func NewResource() (func() resource.Resource, error) {
	return engine.NewResource(Declaration, account_v2.Table)
}

// NewDataSource returns the data source constructor. Stripe documents no account field as unique, so the data source looks up by `id` only.
func NewDataSource() (func() datasource.DataSource, error) {
	return engine.NewDataSource(Declaration, account_v2.Table)
}
