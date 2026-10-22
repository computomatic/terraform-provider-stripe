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
var Declaration = engine.Declaration{
	TypeName:              "stripe_account",
	IDPrefix:              "acct_",
	Description:           "A connected account of the platform. `terraform destroy` deletes the account and, with it, its bank accounts. Set exactly one of `type` (a custom account) or `controller` (an account whose controller properties you choose), together with `country`. `type`, `controller` and `country` cannot change on an existing account, so changing one replaces it. The resource requests the capabilities it lists on creation only: a plan that changes `capabilities` on an existing account fails, because changing requested capabilities is not supported yet: revert the configuration, ignore the change, or recreate the account deliberately with `terraform taint` (`tofu taint` in OpenTofu). Read their status with the `stripe_account_capability` data source. Removing `email` or `business_profile.name` from the configuration leaves the value on the account. A refresh that finds the account gone (Stripe answers 403 `account_invalid`) removes it from state.",
	DataSourceDescription: "A connected account of the platform.",
	ImportNote:            "Stripe does not return which capabilities were requested, so an imported account has no `capabilities` in state. Add `lifecycle { ignore_changes = [capabilities] }` to a configuration that sets it, because a plan that changes `capabilities` fails.",
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
	RefuseChange:    []string{"capabilities"},
	ServerDefaulted: []string{
		"type", "email", "business_profile", "business_profile.name",
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
