package main

import (
	"encoding/json"
	"fmt"
	"regexp"
	"slices"
	"sort"
	"strings"

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
	// ListPath is the `GET` path of an object that has neither a create nor a retrieve
	// operation, only a list under its parent, such as an invoice's lines. The object schema
	// is the element of the list's `data` array. Used only when CreatePath and ReadPath are
	// empty.
	ListPath string
	// Omit lists dotted field paths the table leaves out: fields Stripe returns only when
	// expanded, which the provider never requests, in shapes the engine does not model.
	Omit []string
	// Keep, when set, lists the dotted field paths the table holds: a field on, below or
	// above a kept path stays and every other is left out, as Omit leaves one out. It suits
	// an object of which the conformance suite proves a few fields among many, such as a v2
	// account's one requested capability among dozens.
	Keep []string
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
	// Response names the component schema that is the object's response, when the operation's
	// 200 is a union of kinds and the resource is one of them (an external account is a bank
	// account or a card).
	Response string
	// CreateHash names a create request property that holds the object as a hash, such as
	// `bank_account`. The table takes the hash's fields as create fields in place of the
	// property, and the engine's CreateWrap sends them back under it. The request's other
	// properties stay top-level fields. An `external_account` string beside it is an
	// alternative form of the same property, so list it in Omit.
	CreateHash string
	// NoResponse lists dotted field paths whose response has another shape than the request's,
	// such as an account's `capabilities` (a hash of `requested` flags on the way in, of status
	// strings on the way out). The table counts them as request-only: the state keeps what the
	// configuration says.
	NoResponse []string
	// DocsFrom maps a dotted field path to the `POST` path of another operation whose request
	// documents the same object, such as an account's `individual` and a person's create. A
	// field below the path that the spec leaves undescribed here takes the description of the
	// field at the same place there.
	DocsFrom map[string]string
	// Exclude is the exclusion register (ADR 2026-10-29-full-attribute-coverage, D1): a dotted
	// path, whose leaves on either side and in any union branch are left out, to the reason,
	// which names its evidence (a suite test, a Stripe doc URL or a framework limit). The
	// product owner approves each new entry in review.
	Exclude map[string]string
	// StatusConfirms says that the declaration's `status` transition confirms the object, so
	// the request's `confirm` is left out by rule X5.
	StatusConfirms bool
	// PagedLists maps the dotted path of each list envelope the object requires, which Stripe
	// returns a page at a time on every read (an invoice's `lines`), to the resource or data
	// source type that reads every element (`stripe_invoice_line`) (D3).
	PagedLists map[string]string
}

// view is a schema classified into one of the engine's shapes.
type view struct {
	shape    fields.Shape
	elem     fields.Shape
	obj      *Schema
	nullable bool
	enum     []string
	desc     string
	// refs are the component schemas followed to reach the view, which the walk below it
	// carries on its stack.
	refs []string
	// empty marks a hash with no members.
	empty bool
}

func (v *view) scalar() bool {
	switch v.shape {
	case fields.String, fields.Bool, fields.Int, fields.Float:
		return true
	}
	return false
}

// Build derives the field table of one resource from the spec, as the declaration trims it.
func Build(spec *Spec, res Resource) ([]fields.Field, error) {
	return buildGated(spec, res, nil)
}

// buildGated derives the field table of one resource from the spec, holding every field the
// gate names on its side in spite of Keep, Omit, ReadOnly and NoResponse (ADR
// 2026-10-29-full-attribute-coverage, "The evidence gate").
func buildGated(spec *Spec, res Resource, g *gate) ([]fields.Field, error) {
	b := builder{spec: spec, gate: g, omit: map[string]bool{}, keep: map[string]bool{}, createOnly: map[string]bool{}, readOnly: map[string]bool{}, require: map[string]bool{}, noResponse: map[string]bool{}}
	for _, p := range res.Keep {
		b.keep[p] = false
	}
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
	for _, p := range res.NoResponse {
		b.noResponse[p] = false
	}
	method, path := "POST", res.CreatePath
	if path == "" {
		method, path = "GET", res.ReadPath
	}
	if path == "" {
		method, path = "GET", res.ListPath
	}
	createOp, err := spec.operation(method, path)
	if err != nil {
		return nil, err
	}
	create, err := spec.requestSchema(createOp, path)
	if err != nil {
		return nil, err
	}
	if res.CreateHash != "" {
		if create, err = spec.flattenHash(create, res.CreateHash); err != nil {
			return nil, fmt.Errorf("%s: %w", res.Package, err)
		}
	}
	resp, err := spec.responseSchema(createOp)
	if err != nil {
		return nil, err
	}
	if res.CreatePath == "" && res.ReadPath == "" {
		if resp, err = spec.listElement(resp); err != nil {
			return nil, fmt.Errorf("%s: %w", res.Package, err)
		}
	}
	if res.Response != "" {
		if resp, err = spec.resolve(&Schema{Ref: refPrefix + res.Response}); err != nil {
			return nil, fmt.Errorf("%s: %w", res.Package, err)
		}
	} else if resp != nil && len(resp.Properties) == 0 && len(resp.AnyOf)+len(resp.OneOf) > 0 {
		return nil, fmt.Errorf("%s: the response is a union of kinds; name the one the resource is", res.Package)
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
	b.docsFrom = map[string]*Schema{}
	for at, path := range res.DocsFrom {
		op, err := spec.operation("POST", path)
		if err != nil {
			return nil, err
		}
		if b.docsFrom[at], err = spec.requestSchema(op, path); err != nil {
			return nil, err
		}
	}
	var root []string
	if r, err := responseRoot(spec, createOp, res); err == nil {
		root = []string{strings.TrimPrefix(r.Ref, refPrefix)}
	}
	table, err := b.object(level{create: create, update: update, resp: resp, raw: resp, stacks: stacks{resp: root}}, "", 0)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", res.Package, err)
	}
	for _, p := range res.Omit {
		if !b.omit[p] {
			return nil, fmt.Errorf("%s: omit entry %q names no field", res.Package, p)
		}
	}
	for _, p := range res.Keep {
		if !b.keep[p] {
			return nil, fmt.Errorf("%s: keep entry %q names no field", res.Package, p)
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
	for _, p := range res.NoResponse {
		if !b.noResponse[p] {
			return nil, fmt.Errorf("%s: no-response entry %q names no field", res.Package, p)
		}
	}
	for p := range res.DocsFrom {
		if b.docsFrom[p] != nil {
			return nil, fmt.Errorf("%s: docs-from entry %q names no object", res.Package, p)
		}
	}
	return table, nil
}

type builder struct {
	spec *Spec
	// gate holds the fields the pinned evidence verifies, or nil.
	gate *gate
	// omit maps each omitted path to whether the build met it.
	omit map[string]bool
	// keep maps each path of Resource.Keep to whether the build met it.
	keep map[string]bool
	// createOnly maps each path of Resource.CreateOnly to whether the build met it.
	createOnly map[string]bool
	// readOnly maps each path of Resource.ReadOnly to whether the build met it.
	readOnly map[string]bool
	// require maps each path of Resource.Require to whether the build met it.
	require map[string]bool
	// noResponse maps each path of Resource.NoResponse to whether the build met it.
	noResponse map[string]bool
	// docsFrom maps each path of Resource.DocsFrom to its request schema until the build
	// meets it.
	docsFrom map[string]*Schema
}

// gate is the table paths that the pinned evidence verifies, per side, and what it shows about
// them. A create-only hash (Resource.CreateHash) is already spread into the top level of its
// paths.
type gate struct {
	create, update, response []string
	// clears holds the request fields that Stripe leaves null on a minimal create and that an
	// update unsets as the engine sends the unset (E2).
	clears map[string]bool
	// stable holds the response fields that no update the suite made changed as a side effect,
	// nor fills from another field (E5).
	stable map[string]bool
}

// near reports whether a table path is on, above or below a path of one side of the gate: on
// or above it, the field holds the verified leaf or a hash on its way; below it, the field is
// inside a verified embedded copy or list.
func near(paths []string, at string) bool {
	for _, p := range paths {
		if p == at || strings.HasPrefix(p, at+".") || strings.HasPrefix(at, p+".") {
			return true
		}
	}
	return false
}

// onOrUnder reports whether a table path is one of paths or below one.
func onOrUnder(paths map[string]bool, at string) bool {
	for p := at; p != ""; p = parentPath(p) {
		if _, ok := paths[p]; ok {
			return true
		}
	}
	return false
}

// kept reports whether a field path is on, below or above a path of Resource.Keep, and marks
// a kept path met. Every path is kept when Keep is empty.
func (b *builder) kept(at string) bool {
	if len(b.keep) == 0 {
		return true
	}
	if _, ok := b.keep[at]; ok {
		b.keep[at] = true
		return true
	}
	for k := range b.keep {
		if strings.HasPrefix(at, k+".") || strings.HasPrefix(k, at+".") {
			return true
		}
	}
	return false
}

func prop(s *Schema, name string) *Schema {
	if s == nil {
		return nil
	}
	return s.Properties[name]
}

// level is one object level of the table: the create, update and response schemas as the
// trims leave them, the response's schema whatever the trims (which says whether Stripe
// reports a value only through a `<name>_provided` flag), the docs schemas, and per side the
// component schemas on the path, at which recursion stops.
type level struct {
	create, update, resp, raw *Schema
	// docs are other schemas of the same object level, in order: the response of a
	// NoResponse object, and the request of a DocsFrom operation.
	docs   []*Schema
	stacks stacks
	// restricted holds only the fields the gate verifies, as a trimmed subtree does: the
	// hash form of a token-or-hash parameter, which no table held before the gate.
	restricted bool
}

// stacks holds, per side, the component schemas on the path from the root.
type stacks struct{ create, update, resp []string }

// push returns the stack with the component schemas a view was reached through.
func push(stack []string, v *view) []string {
	if v == nil || len(v.refs) == 0 {
		return stack
	}
	return append(slices.Clone(stack), v.refs...)
}

// object merges the create, update and response schemas of one object level into fields.
// Any of the three may be nil. A field without a description of its own takes the first
// description of a field at the same place in the docs schemas that has the same shape.
func (b *builder) object(l level, where string, depth int) ([]fields.Field, error) {
	if depth > maxDepth {
		return nil, fmt.Errorf("%s: nesting deeper than %d levels", where, maxDepth)
	}
	names := map[string]bool{}
	for _, s := range []*Schema{l.create, l.update, l.resp} {
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

	var g gate
	if b.gate != nil {
		g = *b.gate
	}
	var out []fields.Field
	for _, name := range sorted {
		if where == "" && (name == "expand" || name == "object") {
			continue
		}
		at := where + name
		if _, omitted := b.omit[at]; omitted {
			b.omit[at] = true
		}
		// A trimmed field stays only on a side the gate verifies at, above or below it.
		trimmed := l.restricted || onOrUnder(b.omit, at) || !b.kept(at)
		// The hash form of a token-or-hash parameter, `<name>_data`, keeps the field when the
		// gate verifies a leaf of it, even where the token is trimmed.
		if trimmed && !near(g.create, at) && !near(g.update, at) && !near(g.response, at) && !near(g.create, at+"_data") && !near(g.update, at+"_data") {
			continue
		}
		// A hash with no keys in the response only says its payment method type is in use,
		// which `type` already does (rule X2). A request hash with none selects an option.
		if prop(l.create, name) == nil && prop(l.update, name) == nil && b.emptyObject(prop(l.resp, name)) {
			continue
		}
		cv, err := b.classify(prop(l.create, name), at, depth, l.stacks.create)
		if err != nil {
			return nil, err
		}
		uv, err := b.classify(prop(l.update, name), at, depth, l.stacks.update)
		if err != nil {
			return nil, err
		}
		_, createOnly := b.createOnly[at]
		if createOnly {
			b.createOnly[at] = true
			uv = nil
		}
		if _, only := b.readOnly[at]; only {
			b.readOnly[at] = true
		}
		readOnly := trimmed || onOrUnder(b.readOnly, at)
		if readOnly && !near(g.create, at) {
			cv = nil
		}
		if readOnly && !near(g.update, at) {
			uv = nil
		}
		rv, err := b.classify(prop(l.resp, name), at, depth, l.stacks.resp)
		if err != nil {
			return nil, err
		}
		var dvs []*view
		for _, d := range l.docs {
			dv, err := b.classify(prop(d, name), at, depth, nil)
			if err != nil {
				return nil, err
			}
			if dv != nil {
				dvs = append(dvs, dv)
			}
		}
		if _, none := b.noResponse[at]; none {
			b.noResponse[at] = true
		}
		if trimmed && !near(g.response, at) {
			rv = nil
		}
		if onOrUnder(b.noResponse, at) && !near(g.response, at) {
			if rv != nil {
				dvs = append([]*view{rv}, dvs...)
			}
			rv = nil
		}
		if other := b.docsFrom[at]; other != nil {
			delete(b.docsFrom, at)
			dvs = append(dvs, &view{shape: fields.Object, obj: other})
		}
		if rv, err = b.unwrapList(rv, cv, uv, at, depth, l.stacks.resp); err != nil {
			return nil, err
		}
		var base *view
		for _, v := range []*view{cv, uv} {
			if v == nil {
				continue
			}
			if base == nil {
				base = v
			} else if !reconcile(base, v) {
				return nil, fmt.Errorf("%s: the create and update requests disagree on the shape (%d/%d against %d/%d)", at, base.shape, base.elem, v.shape, v.elem)
			}
		}
		// A field the response gives another shape than the request (D3, "Same name,
		// different shape") takes the request's in the resource, which keeps the configured
		// value, and the response's in the data source.
		var split *view
		switch {
		case rv == nil:
		case base == nil:
			base = rv
		case !reconcile(base, rv):
			split, rv = rv, nil
		}
		required := l.create != nil && slices.Contains(l.create.Required, name)
		if _, req := b.require[at]; req {
			required = true
		}
		data, err := b.hashForm(l, name, at, createOnly, required, depth)
		if err != nil {
			return nil, err
		}
		if base == nil {
			if data != nil {
				out = append(out, *data)
			}
			continue
		}

		views := []*view{cv, uv, rv}
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
				f.Enum = slices.Clone(v.enum)
			} else if v == uv {
				// An update can take values a create does not, such as a card's `canceled`.
				for _, e := range v.enum {
					if !slices.Contains(f.Enum, e) {
						f.Enum = append(f.Enum, e)
					}
				}
			}
		}
		var kidDocs []*Schema
		for _, dv := range dvs {
			if dv.shape != base.shape {
				continue
			}
			if f.Description == "" {
				f.Description = cleanDescription(dv.desc)
			}
			if dv.obj != nil {
				kidDocs = append(kidDocs, dv.obj)
			}
		}
		if cv != nil && l.create != nil {
			f.Required = slices.Contains(l.create.Required, name)
		}
		if _, req := b.require[at]; req && cv != nil {
			b.require[at] = true
			f.Required = true
		}
		if cv != nil && !f.Required && rv != nil && !rv.nullable && l.resp != nil && slices.Contains(l.resp.Required, name) {
			f.ServerDefaulted = true
		}
		// A request field the gate exposes that the response returns, in its shape or another,
		// is Optional and Computed, unless the evidence shows Stripe leaves it null and unsets it
		// (E2). Today's fields keep today's rule until Step 6 of the ADR changes them.
		if readOnly && (cv != nil || uv != nil) && (rv != nil || split != nil) && !f.Required && !g.clears[at] {
			f.ServerDefaulted = true
		}
		// Only an optional argument an update takes can be left unset across an update; a
		// create-only one keeps its value already.
		f.Stable = uv != nil && rv != nil && !f.Required && g.stable[at]
		// A value Stripe never returns and reports only through a `<name>_provided` flag is
		// Sensitive (E3). Today's such arguments are Sensitive by their declarations, so the
		// table marks only those the gate exposes.
		f.Sensitive = (cv != nil || uv != nil) && prop(l.raw, name) == nil && b.providedFlag(l.raw, name) &&
			(near(g.create, at) || near(g.update, at))
		if nestedShape(base.shape) {
			kid := level{create: objOf(cv), update: objOf(uv), resp: objOf(rv), raw: b.objectSchema(prop(l.raw, name)), docs: kidDocs,
				stacks: stacks{push(l.stacks.create, cv), push(l.stacks.update, uv), push(l.stacks.resp, rv)}, restricted: l.restricted}
			if f.Fields, err = b.object(kid, at+".", depth+1); err != nil {
				return nil, err
			}
			if len(f.Fields) == 0 && !(cv != nil && cv.empty || uv != nil && uv.empty) {
				return nil, fmt.Errorf("%s: object has no fields", at)
			}
		}
		if split != nil {
			ds, err := b.dataSourceField(name, split, l, at, depth)
			if err != nil {
				return nil, err
			}
			f.DataSource = &ds
		}
		out = append(out, f)
		if data != nil {
			out = append(out, *data)
		}
	}
	return out, nil
}

// reconcile merges a view of another side into base, by the rules that let one attribute hold
// both, and reports false when none applies.
func reconcile(base, v *view) bool {
	switch {
	case v.shape == base.shape && v.elem == base.elem:
	// A union of an integer and a string (`up_to` is an integer or `inf`) is a string: the
	// response's integer decodes into one.
	case isIntOrString(base, v):
		base.shape = fields.String
	// A reference is sent as an ID and returned as the whole object (a product feature's
	// `entitlement_feature`): the ID is the value, and the decoder takes it from the object.
	case isStringOrObject(base, v):
		base.shape, base.obj = fields.String, nil
	// The same for a list: IDs are sent and the objects returned, as for a subscription's
	// `tax_rates`. The decoder takes each element's `id`.
	case isIDListOrObjects(base, v):
	default:
		return false
	}
	return true
}

func nestedShape(s fields.Shape) bool {
	return s == fields.Object || s == fields.ObjectList || s == fields.ObjectMap
}

// dataSourceField builds the response's form of a field whose request takes another shape.
func (b *builder) dataSourceField(name string, rv *view, l level, at string, depth int) (fields.Field, error) {
	f := fields.Field{Name: name, Shape: rv.shape, Elem: rv.elem, Response: true, Description: cleanDescription(rv.desc), Enum: slices.Clone(rv.enum)}
	if nestedShape(rv.shape) {
		kid := level{resp: rv.obj, raw: rv.obj, stacks: stacks{resp: push(l.stacks.resp, rv)}, restricted: l.restricted}
		var err error
		if f.Fields, err = b.object(kid, at+".", depth+1); err != nil {
			return f, err
		}
		if len(f.Fields) == 0 {
			return f, fmt.Errorf("%s: object has no fields", at)
		}
	}
	return f, nil
}

// hashForm builds `<name>_data`, the hash form of a parameter that takes a token or a hash
// (D3), sent on the wire under `<name>`, Required where the create request requires the
// parameter. The response's value at `<name>` is read into
// `<name>` only, so the hash form keeps the configured value. It is built only where the gate
// verifies a leaf of it, and it fails when `<name>_data` is already a field at that level.
func (b *builder) hashForm(l level, name, at string, createOnly, required bool, depth int) (*fields.Field, error) {
	var g gate
	if b.gate != nil {
		g = *b.gate
	}
	dataAt := at + "_data"
	ch, uh := tokenHash(b.spec, prop(l.create, name)), tokenHash(b.spec, prop(l.update, name))
	if !near(g.create, dataAt) {
		ch = nil
	}
	if createOnly || !near(g.update, dataAt) {
		uh = nil
	}
	if ch == nil && uh == nil {
		return nil, nil
	}
	for _, s := range []*Schema{l.create, l.update, l.resp} {
		if prop(s, name+"_data") != nil {
			return nil, fmt.Errorf("%s: `%s_data`, the hash form of `%s`, is already a field", dataAt, name, name)
		}
	}
	cv, err := b.classify(ch, dataAt, depth, l.stacks.create)
	if err != nil {
		return nil, err
	}
	uv, err := b.classify(uh, dataAt, depth, l.stacks.update)
	if err != nil {
		return nil, err
	}
	// Where the create request requires the parameter, the hash form is marked Required as
	// the token is; the engine reads the pair as one of the two required.
	f := fields.Field{Name: name, Attr: name + "_data", Shape: fields.Object, Required: required && cv != nil, Create: cv != nil, Update: uv != nil}
	for _, v := range []*view{cv, uv} {
		if v != nil && f.Description == "" {
			f.Description = cleanDescription(v.desc)
		}
	}
	kid := level{create: objOf(cv), update: objOf(uv), stacks: stacks{create: push(l.stacks.create, cv), update: push(l.stacks.update, uv)}, restricted: true}
	if f.Fields, err = b.object(kid, dataAt+".", depth+1); err != nil {
		return nil, err
	}
	if len(f.Fields) == 0 {
		return nil, fmt.Errorf("%s: object has no fields", dataAt)
	}
	return &f, nil
}

// tokenHash returns the hash variant of a request parameter that takes a token (or ID) or a
// hash, such as an account's `bank_account`, or nil for any other schema. The leaf walk names
// the same parameters.
func tokenHash(spec *Spec, sc *Schema) *Schema {
	sc, err := spec.resolve(sc)
	if err != nil || sc == nil || len(sc.ExpansionResources) > 0 {
		return nil
	}
	var str, hash int
	var out *Schema
	for _, v := range slices.Concat(sc.AnyOf, sc.OneOf) {
		r, err := spec.resolve(v)
		if err != nil {
			return nil
		}
		switch {
		case r.Type == "string" && onlyEmpty(r):
		case len(v.ExpansionResources) > 0:
			return nil
		case r.Type == "string":
			str++
		case len(r.Properties) > 0 && len(r.AnyOf)+len(r.OneOf) == 0:
			hash++
			out = v
		default:
			return nil
		}
	}
	if str == 1 && hash == 1 {
		return out
	}
	return nil
}

// providedFlag reports whether a response level has `<name>_provided`, a boolean.
func (b *builder) providedFlag(resp *Schema, name string) bool {
	sc, err := b.spec.resolve(prop(resp, name+"_provided"))
	return err == nil && sc != nil && sc.Type == "boolean"
}

// objectSchema returns the hash a schema holds, through references, a union with one hash,
// an array, a map or a list envelope, or nil. It reads the response's schema where the trims
// leave nothing to classify.
func (b *builder) objectSchema(sc *Schema) *Schema {
	for hops := 0; sc != nil && hops < 16; hops++ {
		r, err := b.spec.resolve(sc)
		if err != nil || r == nil {
			return nil
		}
		switch {
		case len(r.AnyOf)+len(r.OneOf) > 0:
			var hashes []*Schema
			for _, v := range slices.Concat(r.AnyOf, r.OneOf) {
				if h, err := b.spec.resolve(v); err == nil && h != nil && h.Type != "string" {
					hashes = append(hashes, v)
				}
			}
			if len(hashes) != 1 {
				return nil
			}
			sc = hashes[0]
		case r.Type == "array":
			sc = r.Items
		case len(r.Properties) > 0:
			if data := prop(r, "data"); data != nil && slices.Contains(enumStrings(b.deref(prop(r, "object"))), "list") {
				sc = data
				continue
			}
			return r
		case r.AdditionalProperties != nil && r.AdditionalProperties.Schema != nil:
			sc = r.AdditionalProperties.Schema
		default:
			return nil
		}
	}
	return nil
}

// deref resolves a schema, or returns an empty one.
func (b *builder) deref(sc *Schema) *Schema {
	r, err := b.spec.resolve(sc)
	if err != nil || r == nil {
		return &Schema{}
	}
	return r
}

// emptyObject reports a schema of an object that names no keys and no value schema.
func (b *builder) emptyObject(sc *Schema) bool {
	if sc == nil {
		return false
	}
	sc, err := b.spec.resolve(sc)
	if err != nil || len(sc.AnyOf)+len(sc.OneOf)+len(sc.AllOf)+len(sc.mergeOf) > 0 {
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
// value. stack holds the component schemas on the path: a schema met again on it is the
// recursion cut (D3), held as the repeat's `id` where it has one and left out otherwise.
func (b *builder) classify(sc *Schema, where string, depth int, stack []string) (*view, error) {
	if sc == nil {
		return nil, nil
	}
	if depth > maxDepth {
		return nil, fmt.Errorf("%s: nesting deeper than %d levels", where, maxDepth)
	}
	if len(sc.mergeOf) > 0 {
		return b.classifyMerged(sc, where, depth, stack)
	}
	var refs []string
	desc := sc.Description
	for sc.Ref != "" {
		name, ok := strings.CutPrefix(sc.Ref, refPrefix)
		target := b.spec.Components.Schemas[name]
		if !ok || target == nil || len(refs) > 16 {
			return nil, fmt.Errorf("%s: unsupported reference %q", where, sc.Ref)
		}
		if slices.Contains(stack, name) || slices.Contains(refs, name) {
			return recursionCut(target, desc), nil
		}
		refs = append(refs, name)
		sc = target
		if desc == "" {
			desc = sc.Description
		}
	}
	stack = append(slices.Clone(stack), refs...)

	variants := slices.Concat(sc.AnyOf, sc.OneOf, sc.AllOf)
	if len(variants) > 0 {
		v, err := b.classifyVariants(variants, where, depth, stack)
		if err != nil || v == nil {
			return nil, err
		}
		v.nullable = v.nullable || sc.Nullable
		if desc != "" {
			v.desc = desc
		}
		v.refs = append(refs, v.refs...)
		return v, nil
	}

	v := &view{nullable: sc.Nullable, desc: desc, refs: refs}
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
		item, err := b.classify(sc.Items, where+"[]", depth, stack)
		if err != nil {
			return nil, err
		}
		switch {
		case item == nil:
			return nil, fmt.Errorf("%s: array without a usable item schema", where)
		case item.scalar():
			v.shape, v.elem = fields.ScalarList, item.shape
		case item.shape == fields.Object:
			v.shape, v.obj, v.empty = fields.ObjectList, item.obj, item.empty
			v.refs = append(v.refs, item.refs...)
		default:
			return nil, fmt.Errorf("%s: arrays of this item shape are not supported", where)
		}
	case "object", "":
		switch {
		case len(sc.Properties) > 0:
			v.shape, v.obj = fields.Object, sc
		case sc.AdditionalProperties != nil && sc.AdditionalProperties.Schema != nil:
			value, err := b.classify(sc.AdditionalProperties.Schema, where+"{}", depth, stack)
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
				v.refs = append(v.refs, value.refs...)
			default:
				return nil, fmt.Errorf("%s: maps of this value shape are not supported", where)
			}
		default:
			// A hash with no members selects an option with Stripe's defaults (D3).
			v.shape, v.obj, v.empty = fields.Object, sc, true
		}
	default:
		return nil, fmt.Errorf("%s: unsupported schema type %q", where, sc.Type)
	}
	return v, nil
}

// recursionCut is the view of a schema met again on its own path: the repeat's ID, or nothing
// where the schema has no `id`.
func recursionCut(target *Schema, desc string) *view {
	if prop(target, "id") == nil {
		return nil
	}
	if desc == "" {
		desc = target.Description
	}
	return &view{shape: fields.String, desc: desc}
}

// classifyVariants picks the representation of an anyOf or oneOf. A real string variant
// wins, which covers expandable references (the ID is the value, ADR D2) and fields that
// take either a number or a word. A single remaining variant is used as it is, and several
// become one object holding every variant's members, merged by name (D3).
func (b *builder) classifyVariants(variants []*Schema, where string, depth int, stack []string) (*view, error) {
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
		return b.classify(str, where, depth, stack)
	case str != nil:
		return &view{shape: fields.String, nullable: str.Nullable, desc: str.Description}, nil
	case len(rest) == 0:
		return nil, nil
	case len(rest) == 1:
		return b.classify(rest[0], where, depth, stack)
	}
	return b.classify(&Schema{mergeOf: rest}, where, depth, stack)
}

// classifyMerged classifies a union of several schemas as one: each must have the same shape,
// and the members of hashes are merged by name, so that a name two variants give different
// shapes fails, naming it. A hash with no members adds nothing; the merged hash requires a
// member only where every variant does, and a merged string takes every variant's values.
func (b *builder) classifyMerged(sc *Schema, where string, depth int, stack []string) (*view, error) {
	var views []*view
	for _, m := range sc.mergeOf {
		v, err := b.classify(m, where, depth, stack)
		if err != nil {
			return nil, err
		}
		if v != nil && !(v.empty && v.shape == fields.Object && len(sc.mergeOf) > 1) {
			views = append(views, v)
		}
	}
	if len(views) == 0 {
		return nil, nil
	}
	out := *views[0]
	out.enum = slices.Clone(out.enum)
	for _, v := range views[1:] {
		switch {
		case v.shape == out.shape && v.elem == out.elem:
		// The integer-or-string rule stays: a member one variant gives as an integer and
		// another as a word is a string.
		case isIntOrString(&out, v):
			out.shape = fields.String
		default:
			return nil, fmt.Errorf("%s: the variants of a union give it different shapes (%d/%d against %d/%d); add a declaration override or a register entry", where, out.shape, out.elem, v.shape, v.elem)
		}
		out.nullable = out.nullable || v.nullable
		out.refs = append(out.refs, v.refs...)
		if out.desc == "" {
			out.desc = v.desc
		}
		for _, e := range v.enum {
			if !slices.Contains(out.enum, e) {
				out.enum = append(out.enum, e)
			}
		}
	}
	if !nestedShape(out.shape) {
		return &out, nil
	}
	merged := &Schema{Type: "object", Properties: map[string]*Schema{}}
	required := map[string]int{}
	for _, v := range views {
		for _, r := range v.obj.Required {
			required[r]++
		}
		for name, p := range v.obj.Properties {
			switch prev := merged.Properties[name]; {
			case prev == nil:
				merged.Properties[name] = p
			case prev == p:
			case len(prev.mergeOf) > 0:
				prev.mergeOf = append(prev.mergeOf, p)
			default:
				merged.Properties[name] = &Schema{mergeOf: []*Schema{prev, p}}
			}
		}
	}
	for _, name := range sortedNames(merged.Properties) {
		if required[name] == len(views) {
			merged.Required = append(merged.Required, name)
		}
	}
	out.obj = merged
	out.empty = len(merged.Properties) == 0
	return &out, nil
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
func (b *builder) unwrapList(rv, cv, uv *view, where string, depth int, stack []string) (*view, error) {
	isList := func(v *view) bool { return v != nil && v.shape == fields.ObjectList }
	if rv == nil || rv.shape != fields.Object || rv.obj == nil || !(isList(cv) || isList(uv)) {
		return rv, nil
	}
	data := rv.obj.Properties["data"]
	kind := rv.obj.Properties["object"]
	if data == nil || kind == nil || !slices.Contains(enumStrings(kind), "list") {
		return rv, nil
	}
	inner, err := b.classify(data, where+".data", depth+1, push(stack, rv))
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

// isStringOrObject reports whether the request's view is a plain string and the response's an
// object: an ID sent and the object returned. A hash sent and a string returned is no such
// reference; it splits between the resource and the data source.
func isStringOrObject(request, response *view) bool {
	return request.shape == fields.String && request.elem == 0 && response.shape == fields.Object
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
