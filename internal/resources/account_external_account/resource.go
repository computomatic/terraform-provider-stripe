// Package account_external_account declares the stripe_account_external_account resource and
// data source: a bank account that a connected account is paid out to.
package account_external_account

import (
	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/resource"

	"github.com/computomatic/terraform-provider-stripe/internal/engine"
	"github.com/computomatic/terraform-provider-stripe/internal/generated/account_external_account"
	"github.com/computomatic/terraform-provider-stripe/internal/wire"
)

// Declaration is what the spec cannot say about an external account. The universe entries
// `accounts.external_accounts` and `accounts.bank_accounts` are one object: Stripe serves the
// same collection under both paths and the same ids, so there is one resource, named for the
// first. It is addressed under its account, so its import ID is `<account_id>/<ba_id>`.
//
// Create sends the bank account as the hash `external_account[...]`, next to
// `default_for_currency` and `metadata`. Stripe never returns the account number, so it stays
// in state from the configuration and is sensitive. Routing number, country, currency and
// account number are create-only (the update form has no such parameters). The suite proves
// updating the holder name and type, the metadata and the default for the currency. Stripe
// refuses to delete the default bank account of a currency (400 `invalid_request_error`), so
// when it does and a retrieve shows the bank account is the default, destroy warns, leaves it
// and removes it from state: deleting the account removes it. `default_for_currency` is
// server-filled, because the first bank account of a currency becomes the default.
//
// A deleted bank account is a 404 on retrieve, and one under a deleted account is the 403
// `account_invalid`, which also means gone.
var Declaration = engine.Declaration{
	TypeName:              "stripe_account_external_account",
	IDPrefix:              "ba_",
	Description:           "A bank account that a connected account is paid out to. `terraform destroy` deletes it. Stripe refuses to delete the default bank account of a currency, so destroy leaves that one on the account with a warning; make another bank account the default first to delete it, or delete the account. The first bank account added in a currency is its default: bank accounts created in one apply are added in no fixed order, so order them with `depends_on` or set `default_for_currency`. The bank account is also served as `bank_accounts` in Stripe's API: it is the same object. Stripe never returns the `account_number`, so state keeps the value you configured and treats it as sensitive. Only `account_holder_name`, `account_holder_type`, `default_for_currency` and `metadata` change in place; changing anything else creates a new bank account.",
	DataSourceDescription: "A bank account that a connected account is paid out to.",
	ImportNote:            "Stripe never returns the account number, so an imported bank account has none in state: a configuration that sets `account_number` plans a replacement unless it ignores changes to it with `lifecycle { ignore_changes = [account_number] }`.",
	ImportNoteLevel:       engine.NoteWarning,
	Parent:                "account",
	Paths: engine.Paths{
		Create:   "/v1/accounts/{account}/external_accounts",
		Retrieve: "/v1/accounts/{account}/external_accounts/{id}",
		Update:   "/v1/accounts/{account}/external_accounts/{id}",
		Delete:   "/v1/accounts/{account}/external_accounts/{id}",
		List:     "/v1/accounts/{account}/external_accounts",
	},
	CreateWrap: &engine.CreateWrap{
		Key:     "external_account",
		Fixed:   wire.Object{{Name: "object", Value: wire.String("bank_account")}},
		Outside: []string{"default_for_currency", "metadata"},
	},
	Delete:          engine.Delete,
	KeepWhen:        &engine.FieldIs{Field: "default_for_currency", Value: wire.Bool(true)},
	GoneOnForbidden: "account_invalid",
	Sensitive:       []string{"account_number"},
	ServerDefaulted: []string{"default_for_currency"},
}

// NewResource returns the resource constructor the provider registers.
func NewResource() (func() resource.Resource, error) {
	return engine.NewResource(Declaration, account_external_account.Table)
}

// NewDataSource returns the data source constructor. It needs the `account` and the `id` of the bank account. Stripe documents no bank account field as unique, so there is no other lookup.
func NewDataSource() (func() datasource.DataSource, error) {
	return engine.NewDataSource(Declaration, account_external_account.Table)
}
