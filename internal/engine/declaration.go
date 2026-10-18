// Package engine builds Terraform resources and data sources from a hand-written
// Declaration and a generated field table (ADR D1, D2). It holds every rule of the
// mapping once; a resource package states only what the spec cannot.
package engine

import (
	"context"

	"github.com/computomatic/terraform-provider-stripe/internal/stripeapi"
	"github.com/computomatic/terraform-provider-stripe/internal/wire"
)

// Client is the part of the Stripe client the engine uses.
type Client interface {
	Do(ctx context.Context, req stripeapi.Request) (*stripeapi.Response, error)
}

// DeleteKind selects what `terraform destroy` does (ADR D2, "Delete versus archive").
type DeleteKind int

const (
	// Delete calls DELETE. A 404 is success.
	Delete DeleteKind = iota
	// Archive retrieves the object, sends the declared update and removes it from state.
	Archive
	// DeleteOrArchive calls DELETE and, when Stripe refuses in the way the declaration's
	// ArchiveOn names, archives the object instead and warns.
	DeleteOrArchive
	// Expire sends a time a few seconds ahead and waits for the declared status.
	Expire
	// NoDelete means Stripe offers no way to remove the object. There is no resource, only
	// a data source.
	NoDelete
)

// Paths are the path templates of an object. `{id}` stands for the object's ID and
// `{<parent>}` for the parent's ID when the declaration names a Parent.
type Paths struct {
	Create   string
	Retrieve string
	// Update may be empty for an object Stripe never updates.
	Update string
	// Delete is used by the Delete strategy.
	Delete string
	// List serves data source lookups by key.
	List string
}

// ArchiveAction is the request that archives an object.
type ArchiveAction struct {
	// Field is the Stripe field that says the object is archived, such as `active`. It is
	// the field the update sets, unless Path names an action endpoint.
	Field string
	// Value is the archived value. Ignored when Now is set.
	Value wire.Value
	// Now sends the literal `now`, which is how file links expire. A Unix time taken here can
	// already be in the past by the time Stripe reads it, and Stripe refuses a past time.
	Now bool
	// Path is an action endpoint, such as `/v1/billing/meters/{id}/deactivate`, that
	// archives the object by a bare POST. Empty means an update of Field.
	Path string
}

// Refusal names a Stripe error response by its status and type. Stripe answers the
// refusals the engine acts on without an error code, so the code is not part of it.
type Refusal struct {
	Status int
	// Type is the error `type`, such as `invalid_request_error`.
	Type string
}

// FieldIs says that a response field has a value, such as `is_default` being true.
type FieldIs struct {
	Field string
	Value wire.Value
}

// ExpireAction ends an object at a time a few seconds ahead.
type ExpireAction struct {
	// Field is the time field, such as `expires_at`.
	Field string
	// Seconds is how far ahead of now the time is set.
	Seconds int64
	// StatusField and StatusValue name the status that says the object has ended.
	StatusField string
	StatusValue string
	// NotStarted and Start name the status of an object that has not started yet, such as
	// `scheduled`, and the update that starts it. Such an object cannot end before it
	// starts, so destroy sends Start before the end time. Empty NotStarted means every object
	// can be ended as it is.
	NotStarted string
	Start      wire.Object
}

// Lookup is a data source lookup key: a field Stripe documents as unique per account and
// that the list endpoint filters by.
type Lookup struct {
	// Field is the Stripe field name.
	Field string
	// Param is the list endpoint's filter parameter. Empty means the same as Field.
	Param string
	// Array sends the value as a one-element array, for a filter such as `lookup_keys`.
	Array bool
}

// Declaration is what the spec cannot say about a resource. It holds no CRUD logic.
// Paths in the lists are dotted Stripe field names, such as `tax_details.tax_code`.
type Declaration struct {
	// TypeName is the Terraform type name, such as `stripe_product`.
	TypeName string
	// IDPrefix is the prefix of Stripe IDs for the object, such as `prod_`. An import ID
	// without it draws a warning.
	IDPrefix string
	// Description is the resource's documentation lead.
	Description string
	// V2 sends JSON bodies instead of forms.
	V2    bool
	Paths Paths
	// Singleton marks an object with no ID that Stripe serves from one path, such as the
	// balance. It has a data source only: no `id` attribute, no lookups.
	Singleton bool
	// Parent names the path parameter, and the required attribute of that name, for an
	// object addressed under a parent. Its import ID is `<parent_id>/<child_id>`.
	Parent string

	Delete  DeleteKind
	Archive *ArchiveAction
	// ArchiveOn is the refusal of DELETE after which DeleteOrArchive archives. Any other
	// error fails the destroy.
	ArchiveOn *Refusal
	// KeepWhen names the object Stripe protects from the Archive strategy, such as the
	// account's default portal configuration. When the archive update fails and a retrieve
	// afterwards shows the object so, destroy warns, leaves the object as it is and removes
	// it from state. Any other failure, and a failed retrieve, fails the destroy.
	KeepWhen *FieldIs
	Expire   *ExpireAction

	// OnceReturned lists outputs Stripe returns only at creation. They are Computed and
	// Sensitive, and read and update responses that omit them keep the prior value.
	OnceReturned []string
	// Expand lists top-level response fields Stripe returns only when asked for with
	// `expand[]`, such as a price's `tiers`. The engine asks for them on every request that
	// returns the object, so they are read back and drift in them is detected.
	Expand []string
	// CurrencyOptions names a top-level map of per-currency options, such as a price's
	// `currency_options`, on an object with a top-level `currency`. Stripe always returns the
	// object's own currency in that map and refuses a request that names it, so the engine
	// leaves that key out of state and rejects it in the configuration. Stripe also refuses to
	// change an existing currency's option, but accepts a new currency, so changing or
	// removing a key replaces the object and adding one is an update that sends only the new
	// keys.
	CurrencyOptions string
	// NullAs maps a field path to the string Stripe's null stands for, such as `inf` for
	// the unbounded last tier's `up_to`. A null or missing value decodes to the string.
	NullAs map[string]string
	// ServerDefaulted lists optional fields Stripe fills in from other fields although the
	// response schema is nullable, such as `unit_amount_decimal` from `unit_amount`. They are
	// Optional and Computed.
	ServerDefaulted []string
	// NotServerDefaulted prunes fields the generator marked as server-defaulted.
	NotServerDefaulted []string
	// Sensitive marks attributes Sensitive and masks them in logs.
	Sensitive []string
	// PreserveConfig lists attributes whose response is a redacted or reformatted value.
	// The configured value stays in state.
	PreserveConfig []string
	// ReplaceOn lists updatable fields, by dotted path, that Stripe nevertheless refuses to
	// change on update, such as a terminal location's `address.country`. A change forces
	// replacement, as for a create-only field.
	ReplaceOn []string
	// Sets lists scalar arrays that are sets of values.
	Sets []string
	// Rename maps a Stripe field path to the attribute name used instead, for names
	// Terraform reserves.
	Rename map[string]string
	// Lookups are the data source lookup keys besides `id`.
	Lookups []Lookup
}
