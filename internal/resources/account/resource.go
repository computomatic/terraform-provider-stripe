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
// Treasury. The suite also creates a company account with `company[name]`. All are proven on
// create only, and replacing an account deletes a business's account, so a change to any of
// them is refused, never a replacement. Stripe never returns the individual's ID number or
// SSN, and returns the individual and company as far larger objects than the request, which
// the fake does not show, so `individual` and `company` keep what the configuration says,
// as `external_account` does, and the two numbers are sensitive. The business type, the
// business profile values and the acceptances are read back and server-filled, like `email`.
var Declaration = engine.Declaration{
	TypeName:              "stripe_account",
	IDPrefix:              "acct_",
	Description:           "A connected account of the platform. `terraform destroy` deletes the account and, with it, its bank accounts. Set exactly one of `type` (a custom account) or `controller` (an account whose controller properties you choose), together with `country`. `type`, `controller` and `country` cannot change on an existing account, so changing one replaces it. The resource requests the capabilities it lists on creation only: a plan that changes `capabilities` on an existing account fails, because changing requested capabilities is not supported yet: revert the configuration, ignore the change, or recreate the account deliberately with `terraform taint` (`tofu taint` in OpenTofu). Read their status with the `stripe_account_capability` data source. The onboarding arguments (`business_type`, `individual`, `company`, `external_account`, `tos_acceptance`, `settings.card_issuing.tos_acceptance`, `settings.treasury.tos_acceptance` and the business profile's `mcc`, `url`, `product_description`, `annual_revenue` and `estimated_worker_count`) are sent on creation only, and a plan that changes one on an existing account fails in the same way. Stripe never returns `individual.id_number` or `individual.ssn_last_4`, and the provider does not read `individual`, `company` or `external_account` back, so state keeps the values you configured, and the two identity numbers are sensitive. Removing `email`, `business_type`, `tos_acceptance` or a `business_profile` or `settings` argument from the configuration leaves the value on the account. A refresh that finds the account gone (Stripe answers 403 `account_invalid`) removes it from state.",
	DataSourceDescription: "A connected account of the platform.",
	ImportNote:            "Stripe does not return which capabilities were requested, and the provider does not read back `individual`, `company` or `external_account`, so an imported account has none of them in state. Add `lifecycle { ignore_changes = [capabilities, individual, company, external_account] }`, listing those the configuration sets, because a plan that changes one of them fails.",
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
		"capabilities", "business_type", "individual", "company", "external_account", "tos_acceptance",
		"business_profile.mcc", "business_profile.url", "business_profile.product_description",
		"business_profile.annual_revenue", "business_profile.estimated_worker_count",
		"settings.card_issuing.tos_acceptance", "settings.treasury.tos_acceptance",
	},
	Sensitive: []string{"individual.id_number", "individual.ssn_last_4"},
	ServerDefaulted: []string{
		"type", "email", "business_type", "business_profile", "business_profile.name",
		"business_profile.mcc", "business_profile.url", "business_profile.product_description",
		"business_profile.annual_revenue", "business_profile.estimated_worker_count",
		"tos_acceptance", "tos_acceptance.date", "tos_acceptance.ip",
		"settings", "settings.card_issuing", "settings.card_issuing.tos_acceptance",
		"settings.card_issuing.tos_acceptance.date", "settings.card_issuing.tos_acceptance.ip",
		"settings.treasury", "settings.treasury.tos_acceptance",
		"settings.treasury.tos_acceptance.date", "settings.treasury.tos_acceptance.ip",
		"controller", "controller.fees", "controller.fees.payer", "controller.losses", "controller.losses.payments",
		"controller.requirement_collection", "controller.stripe_dashboard", "controller.stripe_dashboard.type",
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
