// Package financial_connections_account_owner declares the
// stripe_financial_connections_account_owner data source. There is no resource.
package financial_connections_account_owner

import (
	"github.com/hashicorp/terraform-plugin-framework/datasource"

	"github.com/computomatic/terraform-provider-stripe/internal/engine"
	"github.com/computomatic/terraform-provider-stripe/internal/generated/financial_connections_account_owner"
)

// Declaration is what the spec cannot say about this object. Stripe has no retrieve for an owner, only the list under the account, which requires the `ownership` the owners belong to (suite: "Financial Connections ownership refresh and owners"), so the data source finds the owner by `id` in that list. Each ownership refresh makes a new ownership whose owners have new IDs, and the account's `ownership` names the latest, which the data source reads when the configuration leaves `ownership` unset. A data source never starts a refresh: it is a POST that a read must not send.
var Declaration = engine.Declaration{
	TypeName: "stripe_financial_connections_account_owner",
	IDPrefix: "fcaown_",
	Description: "An owner of a bank account linked through Financial Connections, as the account's institution reported it. " +
		"Look one up by the `account` and the owner's `id`. " +
		"An account has owners only after an ownership refresh, which needs its `ownership` permission, and this data source never starts one: " +
		"refresh the account's ownership through Stripe's API first (`POST /v1/financial_connections/accounts/{account}/refresh` with `features[]=ownership`) and read the owner once the account's `ownership_refresh.status` is `succeeded`. " +
		"Each refresh makes a new ownership whose owners have new IDs, and `ownership` defaults to the account's latest, so set it to keep reading an owner after a later refresh.",
	Parent:     "account",
	FindInList: true,
	Paths:      engine.Paths{List: "/v1/financial_connections/accounts/{account}/owners"},
	ListParams: []engine.ListParam{{
		Name:        "ownership",
		ParentPath:  "/v1/financial_connections/accounts/{account}",
		Description: "The ID of the ownership the owner belongs to. Defaults to the account's latest `ownership`.",
		Unset:       "refresh the account's ownership first, and read the owner once the refresh has succeeded.",
	}},
	Delete: engine.NoDelete,
}

// NewDataSource returns the data source constructor the provider registers. It needs the `account` and the `id` of the owner, which it finds in the list of the ownership. Stripe documents no owner field as unique, and the list filters by none.
func NewDataSource() (func() datasource.DataSource, error) {
	return engine.NewDataSource(Declaration, financial_connections_account_owner.Table)
}
