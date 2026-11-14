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
// Stripe deletes a person with DELETE, answers a second delete with the same stub, and a
// retrieve of a deleted person with a 404 (suite: "delete").
var Declaration = engine.Declaration{
	TypeName:              "stripe_account_person_v2",
	IDPrefix:              "person_",
	Description:           "A person of an account created through Stripe's `/v2` Accounts API, such as its representative, an executive, an owner or a director. `terraform destroy` deletes the person, and changing `account_id` creates a new one. Stripe never returns the `value` of an ID number, so state keeps the values you configured and treats them as sensitive. State holds only the ID numbers you configured, matched by `type`: a minute or two after it creates a person with a `us_ssn_last_4`, Stripe also lists a `us_ssn`, which the data source shows and the resource leaves out. A plan that changes `id_numbers` on an existing person fails, because changing them is not supported yet: revert the configuration, ignore the change, or recreate the person deliberately with `terraform taint` (`tofu taint` in OpenTofu). Stripe returns `address.country` in upper case and a whole `relationship.percent_ownership` with `.0`, so state keeps the values as you configured them. The kana and kanji `script_addresses` and `script_names` of a Japanese account's person change in place, each sent whole; Stripe fills `address.postal_code` from the script addresses a create sends and moves it when an update changes them, so such a plan leaves it known after apply. `documents`, `additional_addresses` and `additional_names` change in place, each sent whole: the lists are matched by position, so a reorder is a change, and an element the configuration drops is dropped from the person. Removing `given_name`, `surname`, `email` or `legal_gender` from the configuration clears it. Stripe has no proven way to clear the rest of a person's details, so an apply that removes `phone`, `date_of_birth`, `address` or `relationship` fails; removing `political_exposure`, `nationalities` or a member of `address` or `relationship` leaves its value on the person.",
	DataSourceDescription: "A person of an account created through Stripe's `/v2` Accounts API, such as its representative, an executive, an owner or a director.",
	ImportNote:            "Stripe never returns the `value` of an ID number, so an imported person has none in `id_numbers`, and its `address.country` and `relationship.percent_ownership` are as Stripe returns them. Add `lifecycle { ignore_changes = [id_numbers] }` to a configuration that sets them, because a plan that changes `id_numbers` fails.",
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
	FilledFrom: map[string][]string{"script_addresses": {"address.postal_code"}},
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
