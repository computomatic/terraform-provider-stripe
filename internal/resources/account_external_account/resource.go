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
// Create sends the bank account as the hash `bank_account[...]`, next to
// `default_for_currency` and `metadata`, or a token in `external_account` on its own beside
// those two: Stripe refuses any other parameter beside a token. A token's bank account has a
// country, currency, routing number and holder of its own (`btok_us_verified` is `Jenny Rosen`'s,
// on the sandbox and the fake), so those are server-filled, and the holder changes in place
// after the create, on a token's bank account as on any other. The record proves that update
// only on a bank account from the hash, whose every_field create hosts the update probes. Stripe never returns the account
// number or the token, so both stay in state from the configuration, and the number is sensitive.
// Routing number, country, currency and account number are create-only (the update form has no
// such parameters). The suite proves
// updating the holder name and type, the metadata and the default for the currency. Stripe
// refuses to delete the default bank account of a currency (400 `invalid_request_error`), so
// destroy and replacement of the default are refused. `default_for_currency` is
// server-filled, because the first bank account of a currency becomes the default.
//
// A deleted bank account is a 404 on retrieve, and one under a deleted account is the 403
// `account_invalid`, which also means gone.
var Declaration = engine.Declaration{
	TypeName:              "stripe_account_external_account",
	IDPrefix:              "ba_",
	Description:           "A bank account that a connected account is paid out to. Set `external_account` to a token such as `btok_us_verified`, or set `account_number`, `country`, `currency` and `routing_number`, with `account_holder_name` and `account_holder_type` if you want them. A bank account from a token takes those from the token, so set none of them beside it when you create it: the bank details never, and `account_holder_name` or `account_holder_type` only once the bank account exists, when they change in place. `terraform destroy` deletes it. The first bank account added in a currency is its default: bank accounts created in one apply are added in no fixed order, so order them with `depends_on` or set `default_for_currency`. Stripe refuses `default_for_currency = false` on an account's first bank account in a currency. The bank account is also served as `bank_accounts` in Stripe's API: it is the same object. Stripe never returns the `account_number`, so state keeps the value you configured and treats it as sensitive. Stripe takes `account_type` only in some countries, and refuses it on a US account. Only `account_holder_name`, `account_holder_type`, `account_type`, `default_for_currency` and `metadata` change in place; changing anything else creates a new bank account. Removing `account_holder_name` or `account_holder_type` from the configuration leaves the value on the bank account.",
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
		Key:      "bank_account",
		Fixed:    wire.Object{{Name: "object", Value: wire.String("bank_account")}},
		Outside:  []string{"default_for_currency", "metadata"},
		Token:    "external_account",
		Requires: []string{"account_number", "country", "currency", "routing_number"},
	},
	Delete: engine.Delete,
	RefuseWhen: &engine.RefuseWhen{
		Field:  "default_for_currency",
		Value:  wire.Bool(true),
		Reason: "Stripe refuses to delete the default bank account of a currency; make another bank account the default first, or remove this one from the state before destroying the account, which deletes its bank accounts.",
	},
	GoneOnForbidden: "account_invalid",
	Sensitive:       []string{"account_number"},
	ServerDefaulted: []string{"account_holder_name", "account_holder_type", "country", "currency", "default_for_currency", "routing_number"},
}

// NewResource returns the resource constructor the provider registers.
func NewResource() (func() resource.Resource, error) {
	return engine.NewResource(Declaration, account_external_account.Table)
}

// NewDataSource returns the data source constructor. It needs the `account` and the `id` of the bank account. Stripe documents no bank account field as unique, so there is no other lookup.
func NewDataSource() (func() datasource.DataSource, error) {
	return engine.NewDataSource(Declaration, account_external_account.Table)
}
