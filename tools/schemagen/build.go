package main

import (
	"encoding/json"
	"fmt"
	"regexp"
	"slices"
	"sort"

	"github.com/computomatic/terraform-provider-stripe/internal/fields"
)

const maxDepth = 12

// Resource names the Stripe operations a field table is built from.
type Resource struct {
	// Package is the Go package under internal/generated and the table's identity.
	Package string
	// CreatePath is the `POST` path that creates the object. Its 200 response is the object
	// schema.
	CreatePath string
	// ReadPath is the `GET` path of an object that has no create operation, such as a
	// singleton. Its 200 response is the object schema. Used only when CreatePath is empty.
	ReadPath string
	// Omit lists dotted field paths the table leaves out: fields Stripe returns only when
	// expanded, which the provider never requests, in shapes the engine does not model.
	Omit []string
	// UpdatePath is the `POST` path that updates the object, or empty when there is none.
	UpdatePath string
	// CreateOnly lists dotted field paths whose update request takes another shape than the
	// create request, such as a subscription's `billing_cycle_anchor` (a timestamp on create,
	// a reset action on update). The table counts them as create-only: the update form is an
	// action, not a value.
	CreateOnly []string
	// ReadOnly lists dotted field paths that a request takes but the conformance suite has
	// not proven, on an object where a replacement is no remedy (a detached payment method
	// cannot be attached again). The table counts them as response-only.
	ReadOnly []string
	// Require lists dotted field paths of the create request that the resource requires
	// although the spec leaves them optional, because the spec's alternative to the field is
	// left out (a payment link line item takes `price` or `price_data`, and the suite proves
	// only `price`).
	Require []string
}

// view is a schema classified into one of the engine's shapes.
type view struct {
	shape    fields.Shape
	elem     fields.Shape
	obj      *Schema
	nullable bool
	enum     []string
	desc     string
}

func (v *view) scalar() bool {
	switch v.shape {
	case fields.String, fields.Bool, fields.Int, fields.Float:
		return true
	}
	return false
}

// Build derives the field table of one resource from the spec.
func Build(spec *Spec, res Resource) ([]fields.Field, error) {
	b := builder{spec: spec, omit: map[string]bool{}, createOnly: map[string]bool{}, readOnly: map[string]bool{}, require: map[string]bool{}}
	for _, p := range res.Omit {
		b.omit[p] = false
	}
	for _, p := range res.CreateOnly {
		b.createOnly[p] = false
	}
	for _, p := range res.ReadOnly {
		b.readOnly[p] = false
	}
	for _, p := range res.Require {
		b.require[p] = false
	}
	method, path := "POST", res.CreatePath
	if path == "" {
		method, path = "GET", res.ReadPath
	}
	createOp, err := spec.operation(method, path)
	if err != nil {
		return nil, err
	}
	create, err := spec.requestSchema(createOp, path)
	if err != nil {
		return nil, err
	}
	resp, err := spec.responseSchema(createOp)
	if err != nil {
		return nil, err
	}
	var update *Schema
	if res.UpdatePath != "" {
		updateOp, err := spec.operation("POST", res.UpdatePath)
		if err != nil {
			return nil, err
		}
		if update, err = spec.requestSchema(updateOp, res.UpdatePath); err != nil {
			return nil, err
		}
	}
	table, err := b.object(create, update, resp, "", 0)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", res.Package, err)
	}
	for _, p := range res.Omit {
		if !b.omit[p] {
			return nil, fmt.Errorf("%s: omit entry %q names no field", res.Package, p)
		}
	}
	for _, p := range res.CreateOnly {
		if !b.createOnly[p] {
			return nil, fmt.Errorf("%s: create-only entry %q names no field", res.Package, p)
		}
	}
	for _, p := range res.Require {
		if !b.require[p] {
			return nil, fmt.Errorf("%s: require entry %q names no create field", res.Package, p)
		}
	}
	for _, p := range res.ReadOnly {
		if !b.readOnly[p] {
			return nil, fmt.Errorf("%s: read-only entry %q names no field", res.Package, p)
		}
	}
	return table, nil
}

type builder struct {
	spec *Spec
	// omit maps each omitted path to whether the build met it.
	omit map[string]bool
	// createOnly maps each path of Resource.CreateOnly to whether the build met it.
	createOnly map[string]bool
	// readOnly maps each path of Resource.ReadOnly to whether the build met it.
	readOnly map[string]bool
	// require maps each path of Resource.Require to whether the build met it.
	require map[string]bool
}

func prop(s *Schema, name string) *Schema {
	if s == nil {
		return nil
	}
	return s.Properties[name]
}

// object merges the create, update and response schemas of one object level into fields.
// Any of the three may be nil.
func (b *builder) object(create, update, resp *Schema, where string, depth int) ([]fields.Field, error) {
	if depth > maxDepth {
		return nil, fmt.Errorf("%s: nesting deeper than %d levels", where, maxDepth)
	}
	names := map[string]bool{}
	for _, s := range []*Schema{create, update, resp} {
		if s != nil {
			for name := range s.Properties {
				names[name] = true
			}
		}
	}
	sorted := make([]string, 0, len(names))
	for name := range names {
		sorted = append(sorted, name)
	}
	sort.Strings(sorted)

	var out []fields.Field
	for _, name := range sorted {
		if where == "" && (name == "expand" || name == "object") {
			continue
		}
		at := where + name
		if _, omitted := b.omit[at]; omitted {
			b.omit[at] = true
			continue
		}
		// A hash with no keys in the response only says its payment method type is in use,
		// which `type` already does. A request hash with none is still an error: it might
		// be a field the provider needs to send.
		if prop(create, name) == nil && prop(update, name) == nil && b.emptyObject(prop(resp, name)) {
			continue
		}
		cv, err := b.classify(prop(create, name), at, depth)
		if err != nil {
			return nil, err
		}
		uv, err := b.classify(prop(update, name), at, depth)
		if err != nil {
			return nil, err
		}
		if _, only := b.createOnly[at]; only {
			b.createOnly[at] = true
			uv = nil
		}
		if _, only := b.readOnly[at]; only {
			b.readOnly[at] = true
			cv, uv = nil, nil
		}
		rv, err := b.classify(prop(resp, name), at, depth)
		if err != nil {
			return nil, err
		}
		if rv, err = b.unwrapList(rv, cv, uv, at, depth); err != nil {
			return nil, err
		}
		views := []*view{cv, uv, rv}
		var base *view
		for _, v := range views {
			if v == nil {
				continue
			}
			if base == nil {
				base = v
			} else if v.shape != base.shape || v.elem != base.elem {
				// A union of an integer and a string (`up_to` is an integer or `inf`)
				// is a string: the response's integer decodes into one.
				if isIntOrString(base, v) {
					base.shape = fields.String
					continue
				}
				// A reference is sent as an ID and returned as the whole object (a product
				// feature's `entitlement_feature`): the ID is the value, and the decoder
				// takes it from the object.
				if isStringOrObject(base, v) {
					base.shape, base.obj = fields.String, nil
					continue
				}
				// The same for a list: IDs are sent and the objects returned, as for a
				// subscription's `tax_rates`. The decoder takes each element's `id`.
				if isIDListOrObjects(base, v) {
					continue
				}
				return nil, fmt.Errorf("%s: request and response disagree on the shape (%d/%d against %d/%d)", at, base.shape, base.elem, v.shape, v.elem)
			}
		}
		if base == nil {
			continue
		}

		f := fields.Field{
			Name:     name,
			Shape:    base.shape,
			Elem:     base.elem,
			Create:   cv != nil,
			Update:   uv != nil,
			Response: rv != nil,
		}
		for _, v := range views {
			if v == nil {
				continue
			}
			if f.Description == "" {
				f.Description = cleanDescription(v.desc)
			}
			if f.Enum == nil && len(v.enum) > 0 {
				f.Enum = v.enum
			}
		}
		if cv != nil && create != nil {
			f.Required = slices.Contains(create.Required, name)
		}
		if _, req := b.require[at]; req && cv != nil {
			b.require[at] = true
			f.Required = true
		}
		if cv != nil && !f.Required && rv != nil && !rv.nullable && resp != nil && slices.Contains(resp.Required, name) {
			f.ServerDefaulted = true
		}
		switch base.shape {
		case fields.Object, fields.ObjectList, fields.ObjectMap:
			f.Fields, err = b.object(objOf(cv), objOf(uv), objOf(rv), at+".", depth+1)
			if err != nil {
				return nil, err
			}
			if len(f.Fields) == 0 {
				return nil, fmt.Errorf("%s: object has no fields", at)
			}
		}
		out = append(out, f)
	}
	return out, nil
}

// emptyObject reports a schema of an object that names no keys and no value schema.
func (b *builder) emptyObject(sc *Schema) bool {
	if sc == nil {
		return false
	}
	sc, err := b.spec.resolve(sc)
	if err != nil || len(sc.AnyOf)+len(sc.OneOf)+len(sc.AllOf) > 0 {
		return false
	}
	return (sc.Type == "object" || sc.Type == "") && len(sc.Properties) == 0 && (sc.AdditionalProperties == nil || sc.AdditionalProperties.Schema == nil)
}

func objOf(v *view) *Schema {
	if v == nil {
		return nil
	}
	return v.obj
}

// classify maps a schema to a shape. It returns nil for a nil schema and for a schema that
// only admits the empty string (Stripe's way of unsetting a field), which is not a field
// value.
func (b *builder) classify(sc *Schema, where string, depth int) (*view, error) {
	if sc == nil {
		return nil, nil
	}
	if depth > maxDepth {
		return nil, fmt.Errorf("%s: nesting deeper than %d levels", where, maxDepth)
	}
	sc, err := b.spec.resolve(sc)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", where, err)
	}

	variants := slices.Concat(sc.AnyOf, sc.OneOf, sc.AllOf)
	if len(variants) > 0 {
		v, err := b.classifyVariants(variants, where, depth)
		if err != nil || v == nil {
			return nil, err
		}
		v.nullable = v.nullable || sc.Nullable
		if sc.Description != "" {
			v.desc = sc.Description
		}
		return v, nil
	}

	v := &view{nullable: sc.Nullable, desc: sc.Description}
	switch sc.Type {
	case "string":
		v.shape = fields.String
		v.enum = enumStrings(sc)
		if onlyEmpty(sc) {
			return nil, nil
		}
	case "integer":
		v.shape = fields.Int
	case "number":
		v.shape = fields.Float
	case "boolean":
		v.shape = fields.Bool
	case "array":
		item, err := b.classify(sc.Items, where+"[]", depth)
		if err != nil {
			return nil, err
		}
		switch {
		case item == nil:
			return nil, fmt.Errorf("%s: array without a usable item schema", where)
		case item.scalar():
			v.shape, v.elem = fields.ScalarList, item.shape
		case item.shape == fields.Object:
			v.shape, v.obj = fields.ObjectList, item.obj
		default:
			return nil, fmt.Errorf("%s: arrays of this item shape are not supported", where)
		}
	case "object", "":
		switch {
		case len(sc.Properties) > 0:
			v.shape, v.obj = fields.Object, sc
		case sc.AdditionalProperties != nil && sc.AdditionalProperties.Schema != nil:
			value, err := b.classify(sc.AdditionalProperties.Schema, where+"{}", depth)
			if err != nil {
				return nil, err
			}
			switch {
			case value == nil:
				return nil, fmt.Errorf("%s: map without a usable value schema", where)
			case value.shape == fields.String:
				v.shape = fields.StringMap
			case value.shape == fields.Int:
				v.shape = fields.IntMap
			case value.shape == fields.Object:
				v.shape, v.obj = fields.ObjectMap, value.obj
			default:
				return nil, fmt.Errorf("%s: maps of this value shape are not supported", where)
			}
		default:
			return nil, fmt.Errorf("%s: object with neither properties nor a value schema", where)
		}
	default:
		return nil, fmt.Errorf("%s: unsupported schema type %q", where, sc.Type)
	}
	return v, nil
}

// classifyVariants picks the representation of an anyOf or oneOf. A real string variant
// wins, which covers expandable references (the ID is the value, ADR D2) and fields that
// take either a number or a word. Otherwise the single remaining variant is used.
func (b *builder) classifyVariants(variants []*Schema, where string, depth int) (*view, error) {
	var rest []*Schema
	var str *Schema
	strings := 0
	for _, raw := range variants {
		sc, err := b.spec.resolve(raw)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", where, err)
		}
		if sc.Type == "string" && !onlyEmpty(sc) {
			str = sc
			strings++
			continue
		}
		if sc.Type == "string" {
			continue
		}
		rest = append(rest, raw)
	}
	switch {
	case str != nil && len(rest) == 0 && strings == 1:
		return b.classify(str, where, depth)
	case str != nil:
		return &view{shape: fields.String, nullable: str.Nullable, desc: str.Description}, nil
	case len(rest) == 0:
		return nil, nil
	case len(rest) == 1:
		return b.classify(rest[0], where, depth)
	}
	return nil, fmt.Errorf("%s: %d alternative schemas without a string form are not supported", where, len(rest))
}

func enumStrings(sc *Schema) []string {
	var out []string
	for _, raw := range sc.Enum {
		var s string
		if json.Unmarshal(raw, &s) == nil && s != "" {
			out = append(out, s)
		}
	}
	return out
}

// onlyEmpty reports a string schema whose one allowed value is the empty string.
func onlyEmpty(sc *Schema) bool {
	if len(sc.Enum) == 0 {
		return false
	}
	return len(enumStrings(sc)) == 0
}

// unwrapList turns the response's list envelope, an object whose `object` is `list` and whose
// `data` is an array, into the
// list inside it when the request sends a list of objects, as a subscription does for its
// `items`. The fields of an element are then merged with the request's.
func (b *builder) unwrapList(rv, cv, uv *view, where string, depth int) (*view, error) {
	isList := func(v *view) bool { return v != nil && v.shape == fields.ObjectList }
	if rv == nil || rv.shape != fields.Object || rv.obj == nil || !(isList(cv) || isList(uv)) {
		return rv, nil
	}
	data := rv.obj.Properties["data"]
	kind := rv.obj.Properties["object"]
	if data == nil || kind == nil || !slices.Contains(enumStrings(kind), "list") {
		return rv, nil
	}
	inner, err := b.classify(data, where+".data", depth+1)
	if err != nil {
		return nil, err
	}
	if inner == nil || inner.shape != fields.ObjectList {
		return rv, nil
	}
	inner.nullable = rv.nullable
	return inner, nil
}

// isIDListOrObjects reports whether a is a list of strings and b a list of objects.
func isIDListOrObjects(a, b *view) bool {
	return a.shape == fields.ScalarList && a.elem == fields.String && b.shape == fields.ObjectList
}

// isIntOrString reports whether one view is a plain integer and the other a plain string.
func isIntOrString(a, b *view) bool {
	plain := func(v *view, shape fields.Shape) bool { return v.shape == shape && v.elem == 0 }
	return plain(a, fields.Int) && plain(b, fields.String) || plain(a, fields.String) && plain(b, fields.Int)
}

// isStringOrObject reports whether one view is a plain string and the other an object.
func isStringOrObject(a, b *view) bool {
	str := func(v *view) bool { return v.shape == fields.String && v.elem == 0 }
	obj := func(v *view) bool { return v.shape == fields.Object }
	return str(a) && obj(b) || obj(a) && str(b)
}

var (
	// relativeLink matches a Markdown link into Stripe's docs, such as `(/tax/tax-codes)`.
	relativeLink   = regexp.MustCompile(`\]\((/[^)]*)\)`)
	paragraphBreak = regexp.MustCompile(`\s*\n\s*\n\s*`)
)

// cleanDescription makes a Stripe description fit one Markdown list item on the registry:
// links into Stripe's docs become absolute, and paragraph breaks become a space so the text
// stays in its bullet.
func cleanDescription(d string) string {
	d = relativeLink.ReplaceAllString(d, "](https://docs.stripe.com$1)")
	return paragraphBreak.ReplaceAllString(d, " ")
}
