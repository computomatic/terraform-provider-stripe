// Package fields defines the field tables the schema generator emits and the engine
// consumes. A table describes one resource: every request and response field with its
// Stripe name, shape, and where Stripe accepts it.
package fields

// Shape is how a Stripe value maps to a Terraform attribute (ADR D2, "Nested objects").
type Shape int

const (
	// String is a Stripe string, including enums and `*_decimal` values.
	String Shape = iota
	// Bool is a Stripe boolean.
	Bool
	// Int is a Stripe integer (amounts in minor units, Unix timestamps).
	Int
	// Float is a Stripe number that is not an integer.
	Float
	// Object is a hash with fixed keys: a single nested attribute.
	Object
	// ObjectList is an array of hashes: a list nested attribute.
	ObjectList
	// ScalarList is an array of scalars: a list attribute, or a set when the declaration
	// says so.
	ScalarList
	// ScalarSet is an array of scalars the docs call a set of values. The generator never
	// emits it; the engine applies it from a declaration override.
	ScalarSet
	// ObjectMap is a hash keyed by a variable (currency, country): a map nested attribute.
	ObjectMap
	// StringMap is a free-form map of strings, such as `metadata`.
	StringMap
	// IntMap is a hash of integers keyed by a variable, such as amounts by currency.
	IntMap
)

// Field is one entry of a resource's table. Nested fields hang off Fields.
type Field struct {
	// Name is the Stripe field name, used on the wire and, unless Attr is set, as the
	// attribute name.
	Name string
	// Attr overrides the attribute name. The generator sets it to `<name>_data` on the hash
	// form of a parameter that takes a token or a hash, which is sent under Name; the engine
	// applies declaration renames to it.
	Attr  string
	Shape Shape
	// Elem is the element shape of a ScalarList or ScalarSet.
	Elem Shape
	// Fields are the members of an Object, or of the elements of an ObjectList or
	// ObjectMap.
	Fields []Field
	// Required is true when Stripe requires the field on create. On both forms of a
	// token-or-hash parameter it means that Stripe requires one of the two.
	Required bool
	// Create is true when the field is in the create request schema.
	Create bool
	// Update is true when the field is in the update request schema. A field with Create
	// and not Update is create-only: Stripe rejects it on update, so changing it replaces
	// the resource.
	Update bool
	// Response is true when the field is in the object returned by Stripe.
	Response bool
	// ServerDefaulted is true for an Optional request field the response always carries
	// (the response schema marks it required and not nullable): Stripe may fill it in.
	ServerDefaulted bool
	// Sensitive is true for a value Stripe never returns and reports only as set, through a
	// `<name>_provided` flag beside it in the response.
	Sensitive bool
	// DataSource is the field as the data source reads it, where the response gives it another
	// shape than the request does (an account's `capabilities`: `requested` flags in,
	// statuses out). The resource keeps the request's shape and the configured value.
	DataSource *Field
	// Enum lists the values Stripe documents for a string field.
	Enum        []string
	Description string
}

// AttrName returns the Terraform attribute name.
func (f Field) AttrName() string {
	if f.Attr != "" {
		return f.Attr
	}
	return f.Name
}

// CreateOnly reports whether Stripe takes the field on create and rejects it on update.
func (f Field) CreateOnly() bool { return f.Create && !f.Update }

// Writable reports whether the field is in any request schema.
func (f Field) Writable() bool { return f.Create || f.Update }

// HashForm reports the hash form of a token-or-hash parameter, `<name>_data`.
func (f Field) HashForm() bool { return f.Attr != "" && f.Attr == f.Name+"_data" }

// ForDataSource returns the table as the data source reads it: each field with a DataSource
// form replaced by it, and without the hash forms of token-or-hash parameters, which only a
// request takes.
func ForDataSource(table []Field) []Field {
	out := make([]Field, 0, len(table))
	for _, f := range table {
		if f.HashForm() {
			continue
		}
		if f.DataSource != nil {
			f = *f.DataSource
		}
		f.Fields = ForDataSource(f.Fields)
		out = append(out, f)
	}
	return out
}

// Find returns the top-level field with the given Stripe name.
func Find(table []Field, name string) (Field, bool) {
	for _, f := range table {
		if f.Name == name {
			return f, true
		}
	}
	return Field{}, false
}
