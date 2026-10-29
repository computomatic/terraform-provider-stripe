// Package fields defines the field tables the schema generator emits and the engine
// consumes. A table describes one resource: every request and response field with its
// Stripe name, shape, and where Stripe accepts it.
package fields

import "strings"

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
	// ServerDefaulted is true for an Optional request field that Stripe may fill in, so that
	// it is Computed too: one the response always carries (the response schema marks it
	// required and not nullable), or one the evidence gate exposes that the response returns,
	// unless the evidence shows Stripe leaves it null and unsets it (ADR
	// 2026-10-29-full-attribute-coverage, E2).
	ServerDefaulted bool
	// Stable is true for an optional field an update takes and the response returns that the
	// evidence shows no update changes unless it names the field (E5): left unset, it keeps its
	// state value.
	Stable bool
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
	// Default is the default of an optional argument: `not sent` for one only the update takes,
	// or else what the pinned evidence records, or nil where it records none (ADR
	// 2026-10-29-full-attribute-coverage, D4).
	Default *Default
	// Verified is true when the pinned fake release verifies a request leaf of the field, so
	// that its page must state its default.
	Verified bool
	// Copy is, on an embedded copy of another Stripe object, the data source type that reads
	// that object (D3).
	Copy string
	// PagedBy is, on a list Stripe returns a page at a time on every read, the type that reads
	// every element (D3).
	PagedBy string
}

// DefaultKind is where an optional argument's value comes from when the configuration leaves
// it unset (D4).
type DefaultKind int

const (
	// DefaultValue is a fixed value Stripe applies.
	DefaultValue DefaultKind = iota + 1
	// DefaultRule is a value Stripe derives from the account or another argument.
	DefaultRule
	// DefaultVaries is a value the variants of the suite's minimal create give differently,
	// which a declaration words as a rule. Until it does, there is no statement.
	DefaultVaries
	// DefaultNone is an argument Stripe leaves unset.
	DefaultNone
	// DefaultNotSent is an argument only the update takes.
	DefaultNotSent
	// DefaultProvider is a value, or a rule, the provider applies.
	DefaultProvider
)

// Default is an optional argument's default.
type Default struct {
	Kind DefaultKind
	// Value is the literal of a DefaultValue or DefaultProvider, as a configuration writes it.
	Value string
	// Rule words a DefaultRule, or a DefaultProvider that is no literal.
	Rule string
	// Observed is, on a DefaultRule, the value the suite observed on its own account and where,
	// such as "`usd` on a US account".
	Observed string
}

// Statement is the sentence that ends the argument's line on its page, or "" where there is
// none yet.
func (d Default) Statement() string {
	switch d.Kind {
	case DefaultValue:
		return "Default (Stripe): " + Code(d.Value) + "."
	case DefaultRule:
		s := "Default (Stripe): " + strings.TrimSuffix(d.Rule, ".")
		if d.Observed != "" {
			s += " (" + d.Observed + ")"
		}
		return s + "."
	case DefaultNone:
		return "Default: none."
	case DefaultNotSent:
		return "Default: not sent."
	case DefaultProvider:
		if d.Rule != "" {
			return "Default (provider): " + strings.TrimSuffix(d.Rule, ".") + "."
		}
		return "Default (provider): " + Code(d.Value) + "."
	}
	return ""
}

// Code is a value as a Markdown code span. The empty string shows as `""`, and a value that
// holds backticks takes a fence one longer than its longest run of them, with a space on each
// side, which Markdown strips, so that a backtick at either end stays in the value.
func Code(v string) string {
	if v == "" {
		v = `""`
	}
	longest, run := 0, 0
	for _, r := range v {
		if r != '`' {
			run = 0
			continue
		}
		run++
		longest = max(longest, run)
	}
	fence := strings.Repeat("`", longest+1)
	if longest > 0 {
		return fence + " " + v + " " + fence
	}
	return fence + v + fence
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
