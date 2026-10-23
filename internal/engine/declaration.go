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

// NoteLevel is how a documentation note is flagged on a page.
type NoteLevel int

const (
	// NoteInfo is a `->` callout: something to know.
	NoteInfo NoteLevel = iota
	// NoteWarning is a `~>` callout: a consequence that can surprise.
	NoteWarning
)

// Sigil is the Markdown that opens a callout paragraph of the level.
func (l NoteLevel) Sigil() string {
	if l == NoteWarning {
		return "~>"
	}
	return "->"
}

// DeleteKind selects what `terraform destroy` does (ADR D2, "Delete versus archive", as
// amended by adrs/1-pending/2026-10-22-destroy-semantics). Destroy deletes or archives, and
// is refused otherwise: it never creates an object and never sends a lifecycle action.
type DeleteKind int

const (
	// Delete calls DELETE. A 404 is success.
	Delete DeleteKind = iota
	// Archive retrieves the object, sends the declared reversible deactivation and removes it
	// from state.
	Archive
	// DeleteOrArchive calls DELETE and, when Stripe refuses in the way the declaration's
	// ArchiveOn names, archives the object instead and warns.
	DeleteOrArchive
	// Refuse means Stripe can neither delete nor archive the object. Destroy, and a change
	// that would replace the object, fail at plan time with RefuseReason and how to remove
	// the object from state. Destroy fails again at apply, for replacements plan time cannot
	// see, and sends nothing.
	Refuse
	// NoDelete means the object cannot be created here. There is no resource, only a data
	// source.
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
	// Value is the archived value.
	Value wire.Value
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

// RefuseWhen names a state in which Stripe refuses to delete or archive an object of a
// resource that otherwise deletes or archives, such as the account's default portal
// configuration. An object in that state is refused as under the Refuse strategy.
type RefuseWhen struct {
	// Field is a top-level Bool or String attribute that the response fills, such as
	// `is_default`.
	Field string
	// Value is the refused value, a wire.Bool or a wire.String.
	Value wire.Value
	// Reason is the sentence that says why, such as "Stripe refuses to deactivate the
	// account's default portal configuration."
	Reason string
}

// Transition is an action endpoint that sets a status-like field Stripe never takes on create
// or update, such as an alert's `status`, which `/activate` and `/deactivate` change.
type Transition struct {
	// Field is the top-level string or bool field the action sets.
	Field string
	// Value is what the field holds afterwards: a string field's value, or `true` or `false`
	// for a bool field.
	Value string
	// Path is the action endpoint, such as `/v1/billing/alerts/{id}/activate`. It is sent as
	// a bare POST, unless Send names fields. A `/v2` action is sent no `include`, which no
	// `/v2` action takes, and the object keeps the included fields its response leaves null.
	Path string
	// Send lists top-level fields whose values the action is sent, as the object holds them
	// when it is called, such as the `applied_configurations` a v2 account's close must name
	// exactly.
	Send []string
	// Final lists the values of Field, other than Value, that the transition cannot leave,
	// such as an alert's `archived`. A plan that moves the field from one of them to Value fails, because Stripe
	// refuses the action. A bool field has none.
	Final []string
}

// AdvanceAction moves a required time field forward in place through an action endpoint, as a
// test clock's `frozen_time` is advanced. Stripe takes the field on create only, so without
// the action a change would replace the object. Going backwards is refused at plan time,
// because Stripe refuses it.
type AdvanceAction struct {
	// Field is the top-level Int field, which the table must mark required on create.
	Field string
	// Path is the action endpoint, such as `/v1/test_helpers/test_clocks/{id}/advance`. It is
	// sent the new value of Field and answers at once, before the object has moved.
	Path string
	// StatusField and ReadyValue name the status that says the object has moved. Update waits
	// for it, within the update timeout.
	StatusField string
	ReadyValue  string
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

// Carry names a field of a list element that an update sends back from the state.
type Carry struct {
	// List is the top-level list, such as `phases`, and Field the element's field, such as
	// `start_date`.
	List, Field string
	// WhenPast carries the value only when it is a Unix time that has passed, so that only the
	// element that has begun keeps it and later ones are derived by Stripe.
	WhenPast bool
}

// CreateWrap says Stripe's create takes the object's fields as a hash under one key, such as
// `external_account[routing_number]`, while the object's own fields are flat.
type CreateWrap struct {
	// Key is the request parameter that holds the hash.
	Key string
	// Fixed are the hash's fields that no attribute sets, such as `object`.
	Fixed wire.Object
	// Outside lists the create fields that stay beside the hash, such as `metadata`. Every
	// other create field goes into it.
	Outside []string
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
	// DataSourceDescription is the data source's documentation lead. A data source destroys,
	// replaces and imports nothing, so it states what the object is and leaves out the
	// lifecycle sentences of Description. It defaults to Description.
	DataSourceDescription string
	// ImportNote is a Markdown paragraph for the resource page's Import section: what an
	// imported object lacks or does, when that differs from a configured one.
	ImportNote string
	// ImportNoteLevel is how strongly the page flags ImportNote. It defaults to NoteInfo.
	ImportNoteLevel NoteLevel
	// Paths say which namespace the object lives in: `/v2/` paths send JSON bodies and read
	// `/v2` errors and lists, any other path is `/v1`. All of a declaration's paths share one
	// namespace.
	Paths Paths
	// Singleton marks an object with no ID that Stripe serves from one path, such as the
	// balance. It has a data source only: no `id` attribute, no lookups.
	Singleton bool
	// CreateWrap nests the create fields under one request parameter. The update request is flat.
	CreateWrap *CreateWrap
	// GoneOnForbidden is the error code of a 403 that Stripe answers for an object, or the
	// parent it lives under, that no longer exists, such as `account_invalid` for a connected
	// account. Read and destroy treat that answer as a 404, and a data source lookup by ID as
	// not found, each with a warning, because the code does not tell a deleted object from one
	// the key may no longer reach.
	GoneOnForbidden string
	// Parent names the path parameter, and the required attribute of that name, for an
	// object addressed under a parent. Its import ID is `<parent_id>/<child_id>`.
	Parent string
	// Attach makes the resource the attachment of an existing object to its Parent, such as a
	// payment method (`payment_method`) to a customer. It names the required attribute that
	// holds the existing object's ID. The create path names it in braces
	// (`/v1/payment_methods/{payment_method}/attach`) and the request body carries the parent's
	// ID under the Parent's name. The resource's `id` is the ID Stripe returns, which can differ
	// from the attribute (attaching a test payment method ID such as `pm_card_visa` returns a new
	// payment method), so the attribute keeps the configured value. Changing it replaces the
	// resource. Import sets it to the imported ID.
	Attach string

	Delete  DeleteKind
	Archive *ArchiveAction
	// ArchiveOn is the refusal of DELETE after which DeleteOrArchive archives. Any other
	// error fails the destroy.
	ArchiveOn *Refusal
	// RefuseReason is, for the Refuse strategy, the sentence that says why Stripe cannot
	// remove the object and, where an attribute ends it, which one. It opens every refusal
	// and the page's callout.
	RefuseReason string
	// RefuseWhen refuses destroy and replacement of an object in the named state, for the
	// Delete, Archive and DeleteOrArchive strategies. When Stripe refuses the delete or
	// archive (400 `invalid_request_error`) and a retrieve shows the object in that state,
	// destroy fails with the same guidance.
	RefuseWhen *RefuseWhen
	// Advance makes a change to a create-only time field an update that moves the object
	// forward, and a change backwards a plan error.
	Advance *AdvanceAction
	// Transitions make a response-only top-level string or bool field, such as `status`, Optional and
	// Computed: a configured value that differs from the object's calls the action endpoint
	// that sets it, after create and on update. A value no transition sets is refused at
	// validation. The field is never sent in a request body.
	Transitions []Transition

	// OnceReturned lists outputs Stripe returns only at creation. They are Computed and
	// Sensitive, and read and update responses that omit them keep the prior value.
	OnceReturned []string
	// Expand lists top-level response fields Stripe returns only when asked for with
	// `expand[]`, such as a price's `tiers`. The engine asks for them on every request that
	// returns the object, so they are read back and drift in them is detected.
	Expand []string
	// Include lists the values of the `include` parameter that `/v2` takes in place of
	// `expand`, such as `webhook_endpoint.url`, which Stripe redacts unless asked. The engine
	// sends them on every request that returns the object, so the value is read back and
	// drift in it is detected. `include` is never an attribute.
	Include []string
	// ClearsWithNull lists top-level fields of a `/v2` object that Stripe is known to unset
	// when sent `null`. Removing any other field from the configuration fails the update,
	// because `/v2` has proven null only for a `metadata` key. A field joins the list when the
	// conformance suite proves it.
	ClearsWithNull []string
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
	// FilledFrom maps a field, by dotted path, to the server-defaulted fields Stripe fills in
	// according to its value, such as a dispute's `evidence.reason` to the evidence hash of each
	// reason. When the planned value differs from the state's, each of those fields that the
	// configuration leaves unset is unknown in the plan instead of kept from the state, because
	// Stripe answers with the fields of the new value. No path may cross a list.
	FilledFrom map[string][]string
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
	// MatchBy maps the name of a top-level list of objects to the string field that identifies
	// its elements, such as `items` to `price` for a subscription. Elements are matched by
	// that key, never by position: a reorder is not a change, an element whose key is in both
	// the state and the plan keeps its `id` and is changed in place, one whose key is only in
	// the plan is added, and one whose key is only in the state is sent as `{id, deleted:
	// true}`. A plan that lists a key twice is refused. An element with no key of its own (it
	// sets `price_data`) cannot be changed on update. The list needs an updatable `id` field.
	// The state holds only the elements the resource manages, so an element added from outside
	// is neither shown nor deleted (see managedKeys).
	MatchBy map[string]string
	// FixedKeys lists MatchBy lists whose elements Stripe cannot add, drop or reprice on
	// update, such as a payment link's `line_items`. A plan whose keys differ from the state's,
	// or that changes a create-only field of an element, replaces the resource. An update that
	// changes an element sends the whole list, because Stripe takes the update as a statement
	// of the list: every element goes with its `id` and the fields that change in place. Every
	// element Stripe returns is managed, so one added from outside shows as a change.
	FixedKeys []string
	// MatchData maps a MatchBy list to the object field an element sets in place of its key,
	// such as `items` to `price_data`, which creates the price the key names. An element with
	// no key of its own pairs with a state element that has equal data, and keeps its item when
	// only other fields change; it is matched to the response element whose key object (the
	// price) the data describes. Changing the data replaces the element. A plan that carries
	// the same data twice is refused.
	MatchData map[string]string
	// Carry lists fields of the elements of a top-level list that an update must send back to
	// name what it keeps. When the plan leaves one unknown, the update sends the state's value
	// for the matching element: the one with the same key in a MatchBy list, the one at the
	// same position in any other. The values go into the request only, never into the plan or
	// the state. A carried field is a string.
	Carry []Carry
	// SendWithUpdate lists top-level fields that say how Stripe applies a change, such as a
	// subscription's `proration_behavior`. Stripe never returns them. An update sends one when
	// it is set, whether or not its value changed, but only along with a change to one of the
	// fields in SendWithUpdateOn, because those are the changes the suite proves them with.
	// A field removed from the configuration is never sent as null.
	SendWithUpdate   []string
	SendWithUpdateOn []string
	// ExactlyOneOf lists groups of top-level writable fields of which a configuration sets
	// exactly one, for the alternatives Stripe's create takes, such as a climate order's
	// `amount` or `metric_tons`. A plan that sets none or several is refused before Stripe is
	// called. A value unknown at plan time is left for apply.
	ExactlyOneOf [][]string
	// RefuseChange lists top-level fields, optional or required, that Stripe's update takes but
	// that nothing proves it applies as asked, such as an account's `capabilities`. A plan that
	// changes one on an existing object fails before Stripe is called, and the error says to
	// revert, to ignore the change or to taint the object, or for a resource that refuses
	// destroy to remove it from state. The field is sent on create only. A plan that is a replace is not
	// refused when a top-level create-only or replace-on field, the parent, the attached object,
	// a currency options change or a fixed keys change forces it. compile refuses a declaration
	// with a replace source below the top level, which is not checked, unless it refuses
	// destroy, which fails every replacement first. Nor is a value not fully known at plan time. `-replace` cannot be told from an update at plan time.
	RefuseChange []string
	// Sets lists scalar arrays that are sets of values.
	Sets []string
	// Rename maps a Stripe field path to the attribute name used instead, for names
	// Terraform reserves.
	Rename map[string]string
	// FindInList says Stripe has no retrieve for the object, only a list under its Parent, such
	// as an invoice's lines. A data source finds it by `id` among the parent's list, following
	// pages. There is no resource and no retrieve path.
	FindInList bool
	// Lookups are the data source lookup keys besides `id`.
	Lookups []Lookup
	// FindBy says Stripe reads the object by required parameters in place of an ID, such as a
	// secret by its `name` and `scope`. A data source with FindBy has no `id` lookup: each
	// parameter is a Required attribute, sent with the configured value on a GET of the
	// retrieve path, which answers the object, or of the list path when there is no retrieve
	// path, which must list exactly one. There is no resource.
	FindBy []FindParam
}

// FindParam is a parameter a data source reads its object by (FindBy).
type FindParam struct {
	// Name is the request parameter and the attribute that holds it. A top-level field of the
	// table keeps its type, which is a string, an integer or an object of those, and the
	// configured value stays in state. A name the table lacks is a string the response does
	// not carry, such as a meter event summary's `customer`.
	Name string
	// Description documents a name the table lacks.
	Description string
}
