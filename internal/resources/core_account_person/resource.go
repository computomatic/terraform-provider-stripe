// Package core_account_person declares the stripe_core_account_person resource and data
// source: a person of an account created through Stripe's /v2 Accounts API
// (v2.core.account_person).
package core_account_person

import (
	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/resource"

	"github.com/computomatic/terraform-provider-stripe/internal/engine"
	"github.com/computomatic/terraform-provider-stripe/internal/generated/core_account_person"
)

// Declaration is what the spec cannot say about a v2 person. It is addressed under its
// account, whose path parameter is `account_id`, so its import ID is
// `<account_id>/<person_id>`.
//
// Stripe lists each ID number's type and never its value, so the values stay in state from
// the configuration and are sensitive. A few minutes after it verifies a `us_ssn_last_4`,
// Stripe lists a `us_ssn` before it (the validation tool's dev sandbox cycle, 2026-10-28), so
// the list is matched by type and holds only the configured numbers, each with its own value.
// The suite sets ID numbers on create only, so a change to `id_numbers` is refused, and so is
// a plan after Stripe stopped listing a configured number, which a refresh shows. The suite
// does not pin the `us_ssn` yet. An update merges `address` and `relationship` (suite: "update
// relationship address and metadata"), and the engine sends each whole, so their members are
// server-filled: one removed from the configuration keeps its value. Stripe shows the country in upper case and a whole percent ownership with `.0`,
// so the configured values stay in state.
//
// Stripe deletes a person with DELETE, answers a second delete with the same stub, and a
// retrieve of a deleted person with a 404 (suite: "delete").
var Declaration = engine.Declaration{
	TypeName:              "stripe_core_account_person",
	IDPrefix:              "person_",
	Description:           "A person of an account created through Stripe's `/v2` Accounts API, such as its representative, an executive, an owner or a director. `terraform destroy` deletes the person, and changing `account_id` creates a new one. Stripe never returns the `value` of an ID number, so state keeps the values you configured and treats them as sensitive. State holds only the ID numbers you configured, matched by `type`: once Stripe verifies a `us_ssn_last_4` it also lists a `us_ssn`, which the data source shows and the resource leaves out. A plan that changes `id_numbers` on an existing person fails, because changing them is not supported yet: revert the configuration, ignore the change, or recreate the person deliberately with `terraform taint` (`tofu taint` in OpenTofu). Stripe returns `address.country` in upper case and a whole `relationship.percent_ownership` with `.0`, so state keeps the values as you configured them. Stripe has no proven way to clear a person's details, so an apply that removes `given_name`, `surname`, `email`, `phone`, `date_of_birth`, `address` or `relationship` from the configuration fails; removing a member of `address` or `relationship` leaves its value on the person.",
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
	MatchOnRead:    map[string]string{"id_numbers": "type"},
	RefuseChange:   []string{"id_numbers"},
	Sensitive:      []string{"id_numbers.value"},
	PreserveConfig: []string{"address.country", "relationship.percent_ownership"},
	ServerDefaulted: []string{
		"address.city", "address.line1", "address.line2", "address.postal_code", "address.state",
		"relationship", "relationship.director", "relationship.executive", "relationship.owner", "relationship.percent_ownership",
		"relationship.representative", "relationship.title",
	},
}

// NewResource returns the resource constructor the provider registers.
func NewResource() (func() resource.Resource, error) {
	return engine.NewResource(Declaration, core_account_person.Table)
}

// NewDataSource returns the data source constructor. It needs the `account_id` and the `id` of the person. Stripe documents no person field as unique, so there is no other lookup.
func NewDataSource() (func() datasource.DataSource, error) {
	return engine.NewDataSource(Declaration, core_account_person.Table)
}
