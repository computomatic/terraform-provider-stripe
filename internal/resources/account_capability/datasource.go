// Package account_capability declares the stripe_account_capability data source: a capability
// of a connected account and its status. There is no resource.
package account_capability

import (
	"github.com/hashicorp/terraform-plugin-framework/datasource"

	"github.com/computomatic/terraform-provider-stripe/internal/engine"
	"github.com/computomatic/terraform-provider-stripe/internal/generated/account_capability"
)

// Declaration is what the spec cannot say about a capability. Its `id` is the capability's
// name, such as `card_payments`, and Stripe lists every capability an account could request,
// requested or not. The account requests capabilities when it is created (the account
// resource's `capabilities`), so this is a data source only.
var Declaration = engine.Declaration{
	TypeName:    "stripe_account_capability",
	Description: "A capability of a connected account and its status. Look one up by the `account` and the capability's name as `id`, such as `card_payments`. Stripe answers for every capability an account could request: `requested` says whether this account did.",
	Parent:      "account",
	Paths:       engine.Paths{Retrieve: "/v1/accounts/{account}/capabilities/{id}"},
	Delete:      engine.NoDelete,

	GoneOnForbidden: "account_invalid",
}

// NewDataSource returns the data source constructor. It looks a capability up by `account` and `id` only.
func NewDataSource() (func() datasource.DataSource, error) {
	return engine.NewDataSource(Declaration, account_capability.Table)
}
