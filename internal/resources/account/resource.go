// Package account declares the stripe_account resource and data source: a connected account
// of a Connect platform.
package account

import (
	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/resource"

	"github.com/computomatic/terraform-provider-stripe/internal/engine"
	"github.com/computomatic/terraform-provider-stripe/internal/generated/account"
)

// Declaration is what the spec cannot say about a connected account. Stripe deletes one with
// DELETE (suite: "TestAccount", "delete"). The platform manages the account with its own
// key and never sends `Stripe-Account`. A missing account and a deleted one are both 403
// `account_invalid`, never 404, so the declaration names that code as the answer for gone.
//
// An account is created with `type` (a custom account) or with `controller` (properties that
// say who collects requirements and pays fees), and the suite proves both; the response of
// the second has the top-level `type` `none`, so `type` and `controller` are server-filled
// as well as create-only. The suite proves `email` and the business profile name on update
// and never clears either, and does not prove an empty string for a plain field, so both are
// server-filled: removing one from the configuration leaves the value. The capabilities an
// account requests are modelled on it, because Stripe's create takes them; the response holds
// their statuses in the same attribute, so the attribute keeps what the configuration says and
// the stripe_account_capability data source reads the statuses. Nothing proves that an account
// update applies a capability request, so a plan that changes `capabilities` is refused.
//
// The onboarding values a US Custom individual account is created with, so that Stripe
// verifies it and activates `card_issuing`, `transfers` and `treasury`, are the suite's
// ("verifiedIndividualForm", "issuingAccountForm"): the business type, the individual, the
// business profile's MCC, URL, product description, annual revenue and worker count, the
// external account token, and the terms of service of the account, of Issuing and of
// Treasury. The suite also creates a company account with `company[name]`. Replacing an
// account deletes a business's account, so a change to any of them whose update the evidence
// does not verify is refused, never a replacement. The business type, the business profile
// values and the acceptances are read back and server-filled, like `email`.
//
// `individual` and `company` are read back as the record shows Stripe returns them. Stripe
// never returns the individual's ID number or SSN, so those two keep what the configuration
// says, as `external_account` does, and are sensitive. It returns a phone number in another
// form (suite: "TestAccountAttributes/leaves/request/PostAccounts/individual.phone" records
// `0000000000` as `+10000000000`), so `individual.phone` keeps the configured form. A
// verification document can name one of Stripe's test tokens, such as
// `file_identity_document_success`, for which the dev sandbox stores a new `file_...` ID, while
// the record's uploaded files return as sent, so the four document references keep the
// configured value too, and both forms converge. Stripe
// returns `company` on every custom account and, once an account has an individual, that
// individual's `address`, `dob` and `verification` with null members where nothing was sent,
// so all of them are server-filled. An account can get its individual outside the provider,
// in Stripe's hosted onboarding, so `individual` is server-filled too. Stripe refuses an empty
// `individual.phone`, so removing it from the configuration keeps the value.
//
// The record proves updates of every member of `individual` and `company` but the ID number
// and SSN, which stay refused, so the rest change in place, one member at a time, because
// Stripe keeps the members a request leaves out (family `v1-connect`, `siblings_kept`) and some
// of them, such as `company.directors_provided`, are attestations it should not be sent back.
// Stripe's refusal of a change it will not take, as of a verified individual, fails the apply.
//
// On an individual account Stripe copies the individual's address and phone into `company`,
// and the copy follows every update of them (the record's `filled_from`, whose accounts have no
// `company` hash sent, because the suite never sends it beside `individual`). So those `company`
// fields are server-filled and a changed source leaves them unknown in the plan. Where a
// configuration sets part of `company.address` beside an individual's address, the dev sandbox
// leaves the rest of the address null, which they then hold; nothing in them is asserted.
//
// Stripe fills the settings' display name, statement descriptor and descriptor prefix in from
// a new account's business name (suite: "create with a business name fills settings"), and
// the sandbox has answered other settings too. The record proves none of this on update: its
// `settings` and `business_profile.name` leaves are unproven and it lists no side effects. On
// the dev sandbox a change of the individual's or the company's name moves the descriptor and
// its prefix, one of `company.name` moves `business_profile.name`, and an account's first
// update can fill the Bacs settings. So a plan that changes the business name, `individual` or
// `company` leaves those unknown where the configuration does not set them, and the apply takes
// what Stripe answers, whether or not it moved them.
var Declaration = engine.Declaration{
	TypeName:              "stripe_account",
	IDPrefix:              "acct_",
	Description:           "A connected account of the platform. `terraform destroy` deletes the account and, with it, its bank accounts. Set exactly one of `type` (a custom account) or `controller` (an account whose controller properties you choose), together with `country`. `type`, `controller` and `country` cannot change on an existing account, so changing one replaces it. Stripe refuses a custom account that requests no capability: one in the US must request at least `transfers`. The resource requests the capabilities it lists on creation only: a plan that changes `capabilities` on an existing account fails, because changing requested capabilities is not supported yet: revert the configuration, ignore the change, or recreate the account deliberately with `terraform taint` (`tofu taint` in OpenTofu). Read their status with the `stripe_account_capability` data source. The onboarding arguments (`business_type`, `individual.id_number`, `individual.ssn_last_4`, `external_account`, `tos_acceptance`, `settings.card_issuing.tos_acceptance`, `settings.treasury.tos_acceptance` and the business profile's `mcc`, `url`, `product_description`, `annual_revenue` and `estimated_worker_count`) are sent on creation only, and a plan that changes one on an existing account fails in the same way. Stripe never returns `individual.id_number`, `individual.ssn_last_4` or `external_account`, so state keeps the values you configured, and the two identity numbers are sensitive. The rest of `individual` and `company` is read back from Stripe and changes in place, so a value changed outside Terraform plans an update back to the configuration, and a member the configuration leaves out but that Stripe holds plans an update that clears it, where Stripe can clear it. Stripe refuses some changes itself, such as of a verified individual, and its refusal fails the apply. `individual.phone` keeps the form you configured, because Stripe stores a phone number in its own form, such as `+10000000000` for `0000000000`, and a verification document's `front` and `back` keep what you configured, because Stripe replaces a test token such as `file_identity_document_success` with a file of its own. Removing `email`, `business_type`, `tos_acceptance`, `individual`, `company`, `individual.phone` or a `business_profile` or `settings` argument from the configuration leaves the value on the account. A refresh that finds the account gone (Stripe answers 403 `account_invalid`) removes it from state.",
	DataSourceDescription: "A connected account of the platform.",
	ImportNote:            "Stripe does not return which `capabilities` were requested, `external_account`, `individual.id_number` or `individual.ssn_last_4`, so an imported account has none of them in state. Add `lifecycle { ignore_changes = [capabilities, external_account, individual.id_number, individual.ssn_last_4] }`, listing those the configuration sets, because a plan that changes one of them fails. An import reads `individual.phone` and the verification documents in Stripe's form, such as `+10000000000` for `0000000000` and a `file_...` ID for a test token, so the first plan updates one configured in another form unless `ignore_changes` lists it too, and Stripe refuses a new document once it has verified the individual.",
	ImportNoteLevel:       engine.NoteWarning,
	Paths: engine.Paths{
		Create:   "/v1/accounts",
		Retrieve: "/v1/accounts/{id}",
		Update:   "/v1/accounts/{id}",
		Delete:   "/v1/accounts/{id}",
		List:     "/v1/accounts",
	},
	Delete:          engine.Delete,
	GoneOnForbidden: "account_invalid",
	ExactlyOneOf:    [][]string{{"type", "controller"}},
	RefuseChange: []string{
		"capabilities", "business_type", "individual.id_number", "individual.ssn_last_4", "external_account", "tos_acceptance",
		"business_profile.mcc", "business_profile.url", "business_profile.product_description",
		"business_profile.annual_revenue", "business_profile.estimated_worker_count",
		"settings.card_issuing.tos_acceptance", "settings.treasury.tos_acceptance",
	},
	Sensitive: []string{"individual.id_number", "individual.ssn_last_4"},
	FilledFrom: map[string][]string{
		"business_profile.name":          {"settings"},
		"company":                        {"settings", "business_profile.name"},
		"individual":                     {"settings"},
		"individual.address.city":        {"company.address.city"},
		"individual.address.line1":       {"company.address.line1"},
		"individual.address.line2":       {"company.address.line2"},
		"individual.address.postal_code": {"company.address.postal_code"},
		"individual.address.state":       {"company.address.state"},
		"individual.phone":               {"company.phone"},
	},
	SendChangedMembers: []string{
		"individual", "individual.address", "individual.registered_address", "individual.relationship",
		"individual.verification", "individual.verification.document", "individual.verification.additional_document",
		"company", "company.address", "company.administrative_address", "company.principal_place_of_business",
	},
	// Neither the record's `families` nor the fake's suite-proven declaration states these.
	SendWholeMembers: []string{"company.directorship_declaration", "company.ownership_declaration"},
	// suite: "TestAccountAttributes/leaves/response/account/company.directors_provided", and
	// `executives_provided` and `owners_provided` beside it: the variants `company`, `individual`
	// and `individual_gb` of the minimal create.
	Defaults: map[string]string{
		"company.directors_provided":  "`false` on a company account, `true` on an individual one",
		"company.executives_provided": "`false` on a company account, `true` on an individual one",
		"company.owners_provided":     "`false` on a company account, `true` on an individual one",
	},
	ServerDefaulted: []string{
		"type", "email", "business_type", "company", "company.address.city", "company.address.line1", "company.address.line2",
		"company.address.postal_code", "company.address.state", "business_profile", "business_profile.name",
		"business_profile.mcc", "business_profile.url", "business_profile.product_description",
		"business_profile.annual_revenue", "business_profile.estimated_worker_count",
		"tos_acceptance", "tos_acceptance.date", "tos_acceptance.ip",
		"settings", "settings.card_issuing", "settings.card_issuing.tos_acceptance",
		"settings.card_issuing.tos_acceptance.date", "settings.card_issuing.tos_acceptance.ip",
		"settings.treasury", "settings.treasury.tos_acceptance",
		"settings.treasury.tos_acceptance.date", "settings.treasury.tos_acceptance.ip",
		"controller", "controller.fees", "controller.fees.payer", "controller.losses", "controller.losses.payments",
		"controller.requirement_collection", "controller.stripe_dashboard", "controller.stripe_dashboard.type",
		"individual", "individual.address", "individual.dob", "individual.phone", "individual.verification", "individual.verification.document",
	},
	PreserveConfig: []string{
		"individual.phone",
		"individual.verification.document.front", "individual.verification.document.back",
		"individual.verification.additional_document.front", "individual.verification.additional_document.back",
	},
}

// NewResource returns the resource constructor the provider registers.
func NewResource() (func() resource.Resource, error) {
	return engine.NewResource(Declaration, account.Table)
}

// NewDataSource returns the data source constructor. Stripe documents no connected account field as unique, so the data source looks up by `id` only.
func NewDataSource() (func() datasource.DataSource, error) {
	return engine.NewDataSource(Declaration, account.Table)
}
