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

// ArchiveAction is the update that archives an object.
type ArchiveAction struct {
	// Field is the Stripe field set on the update, such as `active`.
	Field string
	// Value is the archived value. Ignored when Now is set.
	Value wire.Value
	// Now sets the field to the current Unix time, which is how file links expire.
	Now bool
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
}

// Lookup is a data source lookup key: a field Stripe documents as unique per account and
// that the list endpoint filters by.
type Lookup struct {
	// Field is the Stripe field name.
	Field string
	// Param is the list endpoint's filter parameter. Empty means the same as Field.
	Param string
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
	// Parent names the path parameter, and the required attribute of that name, for an
	// object addressed under a parent. Its import ID is `<parent_id>/<child_id>`.
	Parent string

	Delete  DeleteKind
	Archive *ArchiveAction
	Expire  *ExpireAction

	// OnceReturned lists outputs Stripe returns only at creation. They are Computed and
	// Sensitive, and read and update responses that omit them keep the prior value.
	OnceReturned []string
	// NotServerDefaulted prunes fields the generator marked as server-defaulted.
	NotServerDefaulted []string
	// Sensitive marks attributes Sensitive and masks them in logs.
	Sensitive []string
	// PreserveConfig lists attributes whose response is a redacted or reformatted value.
	// The configured value stays in state.
	PreserveConfig []string
	// Sets lists scalar arrays that are sets of values.
	Sets []string
	// Rename maps a Stripe field path to the attribute name used instead, for names
	// Terraform reserves.
	Rename map[string]string
	// Lookups are the data source lookup keys besides `id`.
	Lookups []Lookup
}
