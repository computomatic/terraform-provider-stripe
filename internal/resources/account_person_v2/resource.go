// Package account_person_v2 declares the stripe_account_person_v2 resource and data
// source: a person of an account created through Stripe's /v2 Accounts API
// (v2.core.account_person).
package account_person_v2

import (
	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/resource"

	"github.com/computomatic/terraform-provider-stripe/internal/engine"
	"github.com/computomatic/terraform-provider-stripe/internal/generated/account_person_v2"
)

// Declaration is what the spec cannot say about a v2 person. It is addressed under its
// account, whose path parameter is `account_id`, so its import ID is
// `<account_id>/<person_id>`.
//
// Stripe lists each ID number's type and never its value, so the values stay in state from
// the configuration and are sensitive. One to two minutes after it creates a person with a
// `us_ssn_last_4`, Stripe lists a `us_ssn` before it (suite: "Stripe adds a us_ssn after
// create"), so the list is matched by type and holds only the configured numbers, each with
// its own value. A person created without ID numbers keeps an empty list. The suite sets ID
// numbers on create only, so a change to `id_numbers` is refused, and so is a plan after
// Stripe stopped listing a configured number, which a refresh shows. An update merges `address` and `relationship` (suite: "update
// relationship address and metadata"), and Stripe keeps the members an update of either leaves
// out (family `v2`, `hashes`), so an update sends only the members of `relationship` that changed
// and `address`, which requires its `country`, whole. Their members are server-filled, because
// the engine sent both whole before the record listed them: one removed from the configuration
// keeps its value, though the record shows null unsets most of them. Stripe shows the country in upper case and a whole percent ownership with `.0`,
// so the configured values stay in state. The record proves null unsets `given_name`, `surname`,
// `email` and `legal_gender`, and Stripe refuses it for `phone` and `political_exposure`.
//
// The record of fake `v0.42.0` proves a person's documents, additional addresses and additional
// names on create and on update, each changed whole on a person created with them (probes
// `update/5`, `update/6` and `update/8`), and a maiden name in place of an alias, so an update
// sends each whole as configured, which the record does not list among the hashes whose
// left-out members Stripe keeps.
//
// Suite #149 proves a person token Stripe made for the person's account, sent alone on a create and
// on an update of a minimal person (probes `isolated/person_token_*`), reading nothing back. On the
// dev sandbox (pin 19's smoke run, 2026-11-16) a token sent on an update set the person's names to
// the token's, which the data source then read, as the fake does. Stripe's
// docs say a person made from a token takes later updates only from another token, which the fake
// answers `fake_unproven`, so no test writes such a person without one.
//
// Stripe deletes a person with DELETE, answers a second delete with the same stub, and a
// retrieve of a deleted person with a 404 (suite: "delete").
var Declaration = engine.Declaration{
	TypeName:              "stripe_account_person_v2",
	IDPrefix:              "person_",
	Description:           "A person of an account created through Stripe's `/v2` Accounts API, such as its representative, an executive, an owner or a director. `terraform destroy` deletes the person, and changing `account_id` creates a new one. Stripe never returns the `value` of an ID number, so state keeps the values you configured and treats them as sensitive. State holds only the ID numbers you configured, matched by `type`: a minute or two after it creates a person with a `us_ssn_last_4`, Stripe also lists a `us_ssn`, which the data source shows and the resource leaves out. A plan that changes `id_numbers` on an existing person fails, because changing them is not supported yet: revert the configuration, ignore the change, or recreate the person deliberately with `terraform taint` (`tofu taint` in OpenTofu). Stripe returns `address.country` in upper case and a whole `relationship.percent_ownership` with `.0`, so state keeps the values as you configured them. The kana and kanji `script_addresses` and `script_names` of a Japanese account's person change in place, each sent whole; Stripe fills `address.postal_code` from the script addresses a create sends and moves it when an update changes them, so such a plan leaves it known after apply. `documents`, `additional_addresses` and `additional_names` change in place, each sent whole: the lists are matched by position, so a reorder is a change, and an element the configuration drops is dropped from the person. `person_token` sends a [person token](https://docs.stripe.com/connect/account-tokens) made for the person's account, which holds what a person's create takes: the provider sends it on create and whenever it changes, and Stripe takes each token once, so a replacement, such as one that changing `account_id` plans, sends a used token, which Stripe refuses: set a new token in the same change, and add `lifecycle { create_before_destroy = true }` so that a refused create leaves the old person in place. The conformance suite has proven a token sent alone, on create and on update; Stripe documents refusing some arguments beside a token, and a person made from one takes later changes of what a token holds only from another token, which Stripe decides when you apply. Stripe never returns the token, so state keeps it, and removing it from the configuration sends nothing. While state holds a token, `given_name`, `surname`, `email`, `phone`, `legal_gender`, `date_of_birth` and `address` stay null unless the configuration sets them, because the token sets them, and a plan that changes the token leaves what else a token holds known after apply; the data source reads what Stripe holds. Set `metadata` in the configuration rather than in a token: keys a token adds that the configuration does not set show at the next refresh, and the next apply removes them. Removing `given_name`, `surname`, `email` or `legal_gender` from the configuration clears it. Stripe has no proven way to clear the rest of a person's details, so an apply that removes `phone`, `date_of_birth`, `address` or `relationship` fails; removing `political_exposure`, `nationalities` or a member of `address` or `relationship` leaves its value on the person.",
	DataSourceDescription: "A person of an account created through Stripe's `/v2` Accounts API, such as its representative, an executive, an owner or a director.",
	ImportNote:            "Stripe never returns the `value` of an ID number, so an imported person has none in `id_numbers`, and its `address.country` and `relationship.percent_ownership` are as Stripe returns them. Add `lifecycle { ignore_changes = [id_numbers] }` to a configuration that sets them, because a plan that changes `id_numbers` fails. An import cannot recover `person_token`, and an imported person holds what Stripe has, including what a token set: a configuration that sets the token needs `person_token` in `ignore_changes`, because a plan would send the used token again, which Stripe refuses, and must set the names, contact details, `date_of_birth` and `address` a token filled to the values Stripe holds, or ignore them too, because a plan would otherwise clear them.",
	ImportNoteLevel:       engine.NoteWarning,
	Parent:                "account_id",
	Paths: engine.Paths{
		Create:   "/v2/core/accounts/{account_id}/persons",
		Retrieve: "/v2/core/accounts/{account_id}/persons/{id}",
		Update:   "/v2/core/accounts/{account_id}/persons/{id}",
		Delete:   "/v2/core/accounts/{account_id}/persons/{id}",
		List:     "/v2/core/accounts/{account_id}/persons",
	},
	Delete:         engine.Delete,
	ClearsWithNull: []string{"email", "given_name", "legal_gender", "surname"},
	MatchOnRead:    map[string]string{"id_numbers": "type"},
	RefuseChange:   []string{"id_numbers"},
	Sensitive:      []string{"id_numbers.value"},
	PreserveConfig: []string{"address.country", "relationship.percent_ownership"},
	Defaults: map[string]string{
		// suite: TestV2CoreAccountPersonAttributes, probe `every_field/15`, a create of a JP
		// account's person that sends the kana and kanji script addresses with postal codes
		// `1500001` and `１５００００１` and an address of `country` alone, which Stripe answers
		// with the address's postal code `1500001`.
		"address.postal_code": "none, or the postal code the create sends in `script_addresses` where it sends none here (`1500001` for a kana `1500001` and a kanji `１５００００１`)",
	},
	SendChangedMembers: []string{"relationship"},
	// Stripe moves the address's postal code to the script addresses' when an update changes them
	// (the record's `side_effects`, probe `update/12`).
	// A person token holds what a person's create takes (the spec's
	// `PostV2CoreAccountsAccountIdPersonTokens`), which Stripe writes to the person in its place.
	FilledFrom: map[string][]string{
		"script_addresses": {"address.postal_code"},
		"person_token": {
			"additional_addresses", "additional_names", "additional_terms_of_service", "documents", "nationalities",
			"political_exposure", "relationship", "script_addresses", "script_names",
		},
	},
	// While state holds a token, what it holds that the configuration leaves unset stays null, as a
	// payment intent's `payment_method` does beside `payment_method_data`. `metadata` is not among
	// them: an update sends only the keys that changed, so a null kept in state would hide the
	// token's keys from a later plan that adds one, and Stripe would answer both. A token's keys the
	// configuration does not set show at the next refresh, and the next apply removes them, as keys
	// added outside Terraform are.
	MadeBy: map[string]string{
		"address": "person_token", "date_of_birth": "person_token", "email": "person_token", "given_name": "person_token",
		"legal_gender": "person_token", "phone": "person_token", "surname": "person_token",
	},
	// Stripe returns an additional address's and an additional name's members as sent (the
	// record's `returned`), so a list element holds what the configuration sets.
	NotServerDefaulted: []string{
		"additional_addresses.city", "additional_addresses.line1", "additional_addresses.line2", "additional_addresses.postal_code",
		"additional_addresses.state", "additional_addresses.town", "additional_names.full_name", "additional_names.surname",
	},
	ServerDefaulted: []string{
		"address.city", "address.line1", "address.line2", "address.postal_code", "address.state", "address.town",
		"relationship", "relationship.director", "relationship.executive", "relationship.legal_guardian", "relationship.owner",
		"relationship.percent_ownership", "relationship.representative", "relationship.title",
	},
}

// NewResource returns the resource constructor the provider registers.
func NewResource() (func() resource.Resource, error) {
	return engine.NewResource(Declaration, account_person_v2.Table)
}

// NewDataSource returns the data source constructor. It needs the `account_id` and the `id` of the person. Stripe documents no person field as unique, so there is no other lookup.
func NewDataSource() (func() datasource.DataSource, error) {
	return engine.NewDataSource(Declaration, account_person_v2.Table)
}
