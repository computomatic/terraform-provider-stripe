// Package account_person declares the stripe_account_person resource and data source: a
// person, such as a representative or an owner, of a connected account's business.
package account_person

import (
	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/resource"

	"github.com/computomatic/terraform-provider-stripe/internal/engine"
	"github.com/computomatic/terraform-provider-stripe/internal/generated/account_person"
)

// Declaration is what the spec cannot say about a person. The universe entries
// `accounts.persons` and `accounts.people` are one object, and Stripe routes only the first
// (suite: "people alias is not routed"), so there is one resource, named for it. It is
// addressed under its account, so its import ID is `<account_id>/<person_id>`.
//
// Stripe never returns `id_number` or `ssn_last_4`, only whether each was provided, so both
// stay in state from the configuration and are sensitive. The suite sets them on create only,
// so a change to either is refused. It proves an update that merges `relationship`, `address`
// and `metadata`. The fields exposed before the record proved them are server-filled: removing
// one from the configuration leaves the value. Of those it proves since, `gender`,
// `maiden_name`, `registered_address` and the verification documents are unset by an empty
// string, so removing one clears it. Stripe shows a ten digit phone number with `+1`, so the
// configured phone stays in state. A verification document keeps what the configuration says:
// the record's uploaded files return as sent, but on the dev sandbox `stripe_account`'s
// `individual`, the same object, had a test token such as `file_identity_document_success`
// replaced with a new `file_...` ID. An update sends only the members of `address`,
// `registered_address`, `relationship` and `verification` that changed, because Stripe keeps the
// others (family `v1-connect`, `hashes`) and refuses a new document once it has verified the
// person.
//
// The kana and kanji names and addresses are proven only on the suite's JP company account, and
// `id_number_secondary` only on its TH company account (batch C1, `isolated/jp`, `jp_unset` and
// `th_id_number_secondary`). The provider cannot tell a person's country at plan, so they are
// arguments everywhere, and Stripe decides at apply. An update sends each script address whole
// (`SendWholeMembers`), as the probe changed it, and a name or address member removed goes as
// `""`, which Stripe holds as null (`isolated/jp_unset`), but an address's `country`, whose unset
// Stripe refuses, keeps its value. Stripe never returns `id_number_secondary`, which the probe
// changes in place, so state keeps it.
// The record proves the latin `first_name` and `last_name` only on US persons. On the dev
// sandbox (pin 18's smoke run) Stripe filled a JP person's unset latin names from the kanji names
// on create and moved them when an update changed the kanji names, which the record does not
// show: the update plan keeps the state's value (E5), so the description tells a user to set the
// latin names, as the suite's JP onboarding does (internal/hardcase's PersonForm).
// Stripe deletes a person with DELETE and answers it, and any person it does not know, with a
// 404 (suite: "delete"). A person under a deleted account is the 403 `account_invalid`, which
// means gone.
var Declaration = engine.Declaration{
	TypeName:              "stripe_account_person",
	IDPrefix:              "person_",
	Description:           "A person of a connected account's business, such as its representative, an executive, an owner or a director. `terraform destroy` deletes the person, and changing `account` creates a new one. Stripe never returns `id_number` or `ssn_last_4`, so state keeps the values you configured and treats them as sensitive; `id_number_provided` and `ssn_last_4_provided` say whether Stripe has them. A plan that changes either on an existing person fails, because changing them is not supported yet: revert the configuration, ignore the change, or recreate the person deliberately with `terraform taint` (`tofu taint` in OpenTofu). Stripe returns a ten digit `phone` with `+1` in front, so state keeps the phone number as you configured it, and a verification document's `front` and `back` keep what you configured, because Stripe replaces a test token such as `file_identity_document_success` with a file of its own, so a change of these made outside Terraform is not seen. Stripe refuses a new document once it has verified the person, and its refusal fails the apply. The kana and kanji names and addresses are for a person of a Japanese account, and `id_number_secondary` for one of a Thai account: the provider sends them wherever you set them, because it does not know the account's country when it plans, and Stripe decides when you apply. Set a Japanese person's `first_name` and `last_name` beside the kana and kanji names: Stripe can fill them from the kanji names, which the provider does not plan for. Stripe never returns `id_number_secondary`, so state keeps the value you configured and treats it as sensitive; a change to it is sent in place. Removing `gender`, `maiden_name`, a kana or kanji name, a member of `registered_address`, a member of `address_kana` or `address_kanji` other than `country`, or a verification document from the configuration clears it, removing a `metadata` key removes that key, and removing any other argument leaves its value on the person. A refresh that finds the account gone (Stripe answers 403 `account_invalid`) removes the person from state.",
	DataSourceDescription: "A person of a connected account's business, such as its representative, an executive, an owner or a director.",
	ImportNote:            "Stripe never returns `id_number` or `ssn_last_4`, so an imported person has neither in state, and its `phone` and verification documents are in Stripe's form. Add `lifecycle { ignore_changes = [id_number, ssn_last_4] }` to a configuration that sets them, because a plan that changes them fails, and add `phone` to the list if the configuration writes it without the `+1`, and the verification documents if it names a test token.",
	ImportNoteLevel:       engine.NoteWarning,
	Parent:                "account",
	Paths: engine.Paths{
		Create:   "/v1/accounts/{account}/persons",
		Retrieve: "/v1/accounts/{account}/persons/{id}",
		Update:   "/v1/accounts/{account}/persons/{id}",
		Delete:   "/v1/accounts/{account}/persons/{id}",
		List:     "/v1/accounts/{account}/persons",
	},
	Delete:          engine.Delete,
	GoneOnForbidden: "account_invalid",
	Answers: map[string]string{
		// suite: TestAccountPersonAttributes, probes `isolated/person_token_create` and
		// `isolated/person_token_update`
		"person_token": "Stripe refused a token it could not find, on create and on update; the conformance suite has recorded no person token it takes.",
		// suite: TestAccountPersonAttributes, probes `isolated/authorizer_create` and
		// `isolated/authorizer_update`, on a US company account
		"relationship.authorizer": "Stripe refused `true` on create and on update of a person of a US company account, and takes the role only for a government entity or a public corporation.",
	},
	RefuseChange:       []string{"id_number", "ssn_last_4"},
	Sensitive:          []string{"id_number", "ssn_last_4"},
	SendChangedMembers: []string{"address", "registered_address", "relationship", "verification", "verification.document", "verification.additional_document"},
	SendWholeMembers:   []string{"address_kana", "address_kanji"},
	PreserveConfig: []string{
		"phone",
		"verification.document.front", "verification.document.back",
		"verification.additional_document.front", "verification.additional_document.back",
	},
	ServerDefaulted: []string{
		"address", "address.city", "address.country", "address.line1", "address.line2", "address.postal_code", "address.state",
		"dob", "email", "first_name", "last_name", "phone",
		"relationship", "relationship.director", "relationship.executive", "relationship.owner", "relationship.percent_ownership",
		"relationship.representative", "relationship.title",
	},
}

// NewResource returns the resource constructor the provider registers.
func NewResource() (func() resource.Resource, error) {
	return engine.NewResource(Declaration, account_person.Table)
}

// NewDataSource returns the data source constructor. It needs the `account` and the `id` of the person. Stripe documents no person field as unique, so there is no other lookup.
func NewDataSource() (func() datasource.DataSource, error) {
	return engine.NewDataSource(Declaration, account_person.Table)
}
