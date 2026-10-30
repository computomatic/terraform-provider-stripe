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
// relationship address and metadata"), and the engine sends each whole, so their members are
// server-filled: one removed from the configuration keeps its value, though the record shows
// null unsets most of them. Stripe shows the country in upper case and a whole percent ownership with `.0`,
// so the configured values stay in state. The record proves null unsets `given_name`, `surname`,
// `email` and `legal_gender`, and Stripe refuses it for `phone` and `political_exposure`.
//
// Stripe deletes a person with DELETE, answers a second delete with the same stub, and a
// retrieve of a deleted person with a 404 (suite: "delete").
var Declaration = engine.Declaration{
	TypeName:              "stripe_account_person_v2",
	IDPrefix:              "person_",
	Description:           "A person of an account created through Stripe's `/v2` Accounts API, such as its representative, an executive, an owner or a director. `terraform destroy` deletes the person, and changing `account_id` creates a new one. Stripe never returns the `value` of an ID number, so state keeps the values you configured and treats them as sensitive. State holds only the ID numbers you configured, matched by `type`: a minute or two after it creates a person with a `us_ssn_last_4`, Stripe also lists a `us_ssn`, which the data source shows and the resource leaves out. A plan that changes `id_numbers` on an existing person fails, because changing them is not supported yet: revert the configuration, ignore the change, or recreate the person deliberately with `terraform taint` (`tofu taint` in OpenTofu). Stripe returns `address.country` in upper case and a whole `relationship.percent_ownership` with `.0`, so state keeps the values as you configured them. Removing `given_name`, `surname`, `email` or `legal_gender` from the configuration clears it. Stripe has no proven way to clear the rest of a person's details, so an apply that removes `phone`, `date_of_birth`, `address` or `relationship` fails; removing `political_exposure`, `nationalities` or a member of `address` or `relationship` leaves its value on the person.",
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
