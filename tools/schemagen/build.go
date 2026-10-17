package main

import (
	"encoding/json"
	"fmt"
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
	// UpdatePath is the `POST` path that updates the object, or empty when there is none.
	UpdatePath string
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
	b := builder{spec: spec}
	createOp, err := spec.operation("POST", res.CreatePath)
	if err != nil {
		return nil, err
	}
	create, err := spec.requestSchema(createOp)
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
		if update, err = spec.requestSchema(updateOp); err != nil {
			return nil, err
		}
	}
	table, err := b.object(create, update, resp, "", 0)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", res.Package, err)
	}
	return table, nil
}

type builder struct {
	spec *Spec
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
		cv, err := b.classify(prop(create, name), at, depth)
		if err != nil {
			return nil, err
		}
		uv, err := b.classify(prop(update, name), at, depth)
		if err != nil {
			return nil, err
		}
		rv, err := b.classify(prop(resp, name), at, depth)
		if err != nil {
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
				f.Description = v.desc
			}
			if f.Enum == nil && len(v.enum) > 0 {
				f.Enum = v.enum
			}
		}
		if cv != nil && create != nil {
			f.Required = slices.Contains(create.Required, name)
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
