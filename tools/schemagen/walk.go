package main

import (
	"encoding/json"
	"fmt"
	"slices"
	"sort"
	"strconv"
	"strings"
)

// Side is the side of the wire a leaf is on.
type Side string

const (
	// SideRequest is a parameter of a create or update request.
	SideRequest Side = "request"
	// SideResponse is a field of the object.
	SideResponse Side = "response"
)

// Kind is what a leaf holds.
type Kind string

const (
	// KindScalar is a string, integer, number or boolean, or a list or map of them.
	KindScalar Kind = "scalar"
	// KindEmptyHash is a hash with no keys, such as a payment method's type marker.
	KindEmptyHash Kind = "empty_hash"
	// KindReference is an expandable field: an ID, or the object when expanded.
	KindReference Kind = "reference"
	// KindEmbedded is a copy of another object, named by Target.
	KindEmbedded Kind = "embedded"
	// KindPagedList is a list envelope (`object: list`) of the objects named by Target.
	KindPagedList Kind = "paged_list"
	// KindRecursion is where a schema meets itself on its own path; Target names it.
	KindRecursion Kind = "recursion"
)

// Key identifies a leaf as the conformance suite's evidence record and the fake's leaf index
// key it: the object, the side, the operation (request leaves only), a dotted path in which an
// array or a map of hashes adds no segment, and the union branch.
type Key struct {
	Object    string `json:"object"`
	Side      Side   `json:"side"`
	Operation string `json:"operation,omitempty"`
	Path      string `json:"path"`
	Branch    string `json:"branch,omitempty"`
}

// String is the key as the suite names a leaf test: `<side>/<operation or object>/<path>`,
// with `@<branch>` in a union.
func (k Key) String() string {
	owner := k.Operation
	if k.Side == SideResponse {
		owner = k.Object
	}
	s := string(k.Side) + "/" + owner + "/" + k.Path
	if k.Branch != "" {
		s += "@" + k.Branch
	}
	return s
}

// Leaf is one attribute of an object or one parameter of an operation.
type Leaf struct {
	Key
	Kind Kind
	// Type is a scalar's JSON type.
	Type string
	// Array and Map mark a leaf that is a list or a map of what Kind describes.
	Array, Map bool
	// Required is whether the hash holding the leaf requires it. For a list envelope it says
	// that Stripe returns the list on every read (rule X3).
	Required bool
	// Target names the copied object of an embedded copy, the element object of a paged
	// list, or the schema a recursion meets.
	Target string
	// BranchAt is the path of the union that names Branch.
	BranchAt string
	// At is where the table holds the leaf, and PointAt where it holds the union at
	// BranchAt: the path, with a create-only hash (Resource.CreateHash) spread into the top
	// level and the hash form of a token-or-hash parameter under `<name>_data` (D3).
	At, PointAt string
}

// Shape describes the leaf for the coverage report: its kind or JSON type, with `list of`
// or `map of` in front.
func (l Leaf) Shape() string {
	d := string(l.Kind)
	if l.Kind == KindScalar {
		d = l.Type
	}
	if l.Array {
		d = "list of " + d
	}
	if l.Map {
		d = "map of " + d
	}
	return d
}

// walkTable walks the operations of one table into its leaves, keyed as the suite keys them
// (suite ADR 2026-10-29-attribute-coverage, A1 and A2). The walk follows `$ref`, `anyOf`,
// `oneOf`, arrays and maps; a union gives each branch its own leaves; recursion stops where a
// schema meets itself on its own path; an expandable field is one reference leaf; and a list
// envelope is one leaf at its path. A nested API object is one embedded-copy leaf, unless the
// table's own request writes it as a hash, in which case it is walked into its own leaves
// (decided by the owner, 2026-10-29). The response's own `object` field is left out, as the
// provider's measurement leaves it out.
func walkTable(spec *Spec, res Resource) (object string, leaves []Leaf, err error) {
	method, path := "POST", res.CreatePath
	if path == "" {
		method, path = "GET", res.ReadPath
	}
	if path == "" {
		method, path = "GET", res.ListPath
	}
	first, err := spec.operation(method, path)
	if err != nil {
		return "", nil, err
	}
	root, err := responseRoot(spec, first, res)
	if err != nil {
		return "", nil, fmt.Errorf("%s: %w", res.Package, err)
	}
	name := strings.TrimPrefix(root.Ref, refPrefix)
	if object = objectName(name, spec.Components.Schemas[name]); object == "" {
		return "", nil, fmt.Errorf("%s: the response schema %s is not an API object", res.Package, name)
	}

	var request []Leaf
	hashes := map[string]bool{}
	for _, at := range []string{res.CreatePath, res.UpdatePath} {
		if at == "" {
			continue
		}
		op, err := spec.operation("POST", at)
		if err != nil {
			return "", nil, err
		}
		body, err := spec.requestSchema(op, at)
		if err != nil {
			return "", nil, err
		}
		if body == nil {
			continue
		}
		w := &walker{spec: spec, key: Key{Object: object, Side: SideRequest, Operation: op.OperationID}, out: map[Key]*Leaf{}, tokens: map[string]bool{}}
		if err := w.walk(body, "", union{}, carried{}); err != nil {
			return "", nil, fmt.Errorf("%s: %w", res.Package, err)
		}
		flatten := ""
		if at == res.CreatePath {
			flatten = res.CreateHash
		}
		for _, l := range w.leaves() {
			l.At, l.PointAt = tableAt(l.Path, w.tokens, flatten), tableAt(l.BranchAt, w.tokens, flatten)
			request = append(request, l)
			for p := parentPath(l.Path); p != ""; p = parentPath(p) {
				hashes[p] = true
			}
		}
	}

	w := &walker{spec: spec, key: Key{Object: object, Side: SideResponse}, out: map[Key]*Leaf{}, hashes: hashes}
	if err := w.walk(root, "", union{}, carried{}); err != nil {
		return "", nil, fmt.Errorf("%s: %w", res.Package, err)
	}
	for _, l := range w.leaves() {
		l.At, l.PointAt = l.Path, l.BranchAt
		request = append(request, l)
	}
	return object, request, nil
}

// tableAt is where the table holds a request path: with the create-only hash flatten spread
// into the top level, and each token-or-hash parameter above the path's last segment as its
// hash form, `<name>_data`. The hash flatten itself has no place in the table, "".
func tableAt(path string, tokens map[string]bool, flatten string) string {
	if path == "" || path == flatten {
		return ""
	}
	segs := strings.Split(path, ".")
	var out []string
	for i, seg := range segs {
		orig := strings.Join(segs[:i+1], ".")
		switch {
		case orig == flatten:
			out = nil
			continue
		case tokens[orig] && i < len(segs)-1:
			seg += "_data"
		}
		out = append(out, seg)
	}
	return strings.Join(out, ".")
}

// responseRoot returns the object schema of a table as a `$ref`, so that the walk knows its
// name: the named Response schema, a list response's element, or the operation's 200.
func responseRoot(spec *Spec, op *Operation, res Resource) (*Schema, error) {
	if res.Response != "" {
		return &Schema{Ref: refPrefix + res.Response}, nil
	}
	r := op.Responses["200"]
	if r == nil || r.Content["application/json"] == nil {
		return nil, fmt.Errorf("%s has no JSON 200 response", op.OperationID)
	}
	root := r.Content["application/json"].Schema
	if res.CreatePath == "" && res.ReadPath == "" {
		list, err := spec.resolve(root)
		if err != nil {
			return nil, err
		}
		data := prop(list, "data")
		if data == nil || data.Items == nil {
			return nil, fmt.Errorf("the response is not a list: it has no `data` array")
		}
		root = data.Items
	}
	if root == nil || root.Ref == "" {
		return nil, fmt.Errorf("%s: the response is not a named schema", op.OperationID)
	}
	return root, nil
}

// objectName returns the object a component schema is, or "" when it is not one: its
// `x-resourceId`, or for a `/v2` schema, which has none, its name when its `object` field is
// that name and nothing else. The suite and the fake name objects the same way.
func objectName(name string, sc *Schema) string {
	if sc == nil {
		return ""
	}
	if sc.ResourceID != "" {
		return sc.ResourceID
	}
	if !strings.HasPrefix(name, "v2.") {
		return ""
	}
	if o := sc.Properties["object"]; o != nil && len(o.Enum) == 1 && string(o.Enum[0]) == strconv.Quote(name) {
		return name
	}
	return ""
}

// carried is what a leaf inherits from the property that holds it.
type carried struct {
	array, mapped bool
	required      bool
}

type walker struct {
	spec *Spec
	key  Key
	// stack holds the component schemas on the current path, root first.
	stack []string
	out   map[Key]*Leaf
	// hashes holds every path the table's request writes as a hash, on a response walk.
	hashes map[string]bool
	// tokens holds every request parameter that takes a token or a hash.
	tokens map[string]bool
}

// union is the branch a leaf is in, and the path of the union that names it.
type union struct{ name, at string }

func (w *walker) leaf(l Leaf, path string, br union, c carried) error {
	l.Key = w.key
	l.Path, l.Branch, l.BranchAt = path, br.name, br.at
	l.Array, l.Map, l.Required = c.array, c.mapped, c.required
	if prev, ok := w.out[l.Key]; ok {
		if prev.Kind != l.Kind || prev.Type != l.Type || prev.Target != l.Target || prev.Array != l.Array || prev.Map != l.Map {
			return fmt.Errorf("%s has two shapes, %s and %s", l.Key, prev.Shape(), l.Shape())
		}
		prev.Required = prev.Required || l.Required
		return nil
	}
	w.out[l.Key] = &l
	return nil
}

func (w *walker) leaves() []Leaf {
	out := make([]Leaf, 0, len(w.out))
	for _, l := range w.out {
		out = append(out, *l)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Key.String() < out[j].Key.String() })
	return out
}

func (w *walker) walk(sc *Schema, path string, br union, c carried) error {
	if sc == nil {
		return fmt.Errorf("%s: no schema", path)
	}
	expandable := len(sc.ExpansionResources) > 0
	if sc.Ref != "" {
		name, ok := strings.CutPrefix(sc.Ref, refPrefix)
		target := w.spec.Components.Schemas[name]
		if !ok || target == nil {
			return fmt.Errorf("%s: unresolved %s", path, sc.Ref)
		}
		if slices.Contains(w.stack, name) {
			return w.leaf(Leaf{Kind: KindRecursion, Target: name}, path, br, c)
		}
		if object := objectName(name, target); object != "" && len(w.stack) > 0 && !w.hashes[path] {
			return w.leaf(Leaf{Kind: KindEmbedded, Target: object}, path, br, c)
		}
		w.stack = append(w.stack, name)
		defer func() { w.stack = w.stack[:len(w.stack)-1] }()
		return w.walk(target, path, br, c)
	}

	var variants []*Schema
	for _, v := range slices.Concat(sc.AnyOf, sc.OneOf) {
		if w.onlyEmptyString(v) {
			continue
		}
		expandable = expandable || len(v.ExpansionResources) > 0
		variants = append(variants, v)
	}
	if expandable && len(variants) > 1 {
		return w.leaf(Leaf{Kind: KindReference}, path, br, c)
	}
	switch {
	case len(variants) == 1:
		return w.walk(variants[0], path, br, c)
	case len(variants) > 1:
		names, err := w.branches(variants)
		if err != nil {
			return fmt.Errorf("%s: %w", path, err)
		}
		if w.key.Side == SideRequest && tokenHash(w.spec, sc) != nil {
			w.tokens[path] = true
		}
		for i, v := range variants {
			if err := w.walk(v, path, union{names[i], path}, c); err != nil {
				return err
			}
		}
		return nil
	}
	if len(sc.AllOf) > 0 {
		return fmt.Errorf("%s: allOf is not supported", path)
	}

	switch sc.Type {
	case "array":
		c.array = true
		return w.walk(sc.Items, path, br, c)
	case "object", "":
		if path != "" && w.key.Side == SideResponse && w.isListEnvelope(sc) {
			return w.leaf(Leaf{Kind: KindPagedList, Target: w.elementObject(sc)}, path, br, c)
		}
		if len(sc.Properties) > 0 {
			for _, name := range sortedNames(sc.Properties) {
				if path == "" && w.key.Side == SideResponse && name == "object" {
					continue
				}
				pc := carried{required: slices.Contains(sc.Required, name)}
				if err := w.walk(sc.Properties[name], join(path, name), br, pc); err != nil {
					return err
				}
			}
			return nil
		}
		if sc.AdditionalProperties != nil && sc.AdditionalProperties.Schema != nil {
			c.mapped = true
			return w.walk(sc.AdditionalProperties.Schema, path, br, c)
		}
		return w.leaf(Leaf{Kind: KindEmptyHash}, path, br, c)
	default:
		return w.leaf(Leaf{Kind: KindScalar, Type: sc.Type}, path, br, c)
	}
}

// onlyEmptyString reports a union member that is only the empty string, which a v1 request
// sends to unset a field: a way to send nothing, not a branch.
func (w *walker) onlyEmptyString(v *Schema) bool {
	r := w.deref(v)
	return r != nil && r.Type == "string" && len(r.Enum) > 0 && !slices.ContainsFunc(r.Enum, func(e json.RawMessage) bool { return string(e) != `""` })
}

// branches names a union's members as the suite does: on the request side by JSON type; on
// the response side by each member's single-valued `object` field when every member has one,
// else by the member's schema name, else by its JSON type. Request members of one JSON type
// share their branch, because a request cannot tell them apart, as the fake's index has it;
// response members must be named apart.
func (w *walker) branches(variants []*Schema) ([]string, error) {
	names := make([]string, len(variants))
	discriminated := w.key.Side == SideResponse
	for _, v := range variants {
		if _, ok := w.objectValue(v); !ok {
			discriminated = false
		}
	}
	for i, v := range variants {
		switch {
		case discriminated:
			names[i], _ = w.objectValue(v)
		case w.key.Side == SideResponse && v.Ref != "":
			names[i] = strings.TrimPrefix(v.Ref, refPrefix)
		default:
			names[i] = w.jsonType(v)
		}
	}
	for i := range names {
		if w.key.Side == SideResponse && slices.Contains(names[:i], names[i]) {
			return nil, fmt.Errorf("union has two members named %s", names[i])
		}
	}
	return names, nil
}

// objectValue returns the single value of a member's `object` field.
func (w *walker) objectValue(v *Schema) (string, bool) {
	o := w.deref(prop(w.deref(v), "object"))
	if o == nil || len(o.Enum) != 1 {
		return "", false
	}
	var s string
	if json.Unmarshal(o.Enum[0], &s) != nil {
		return "", false
	}
	return s, true
}

func (w *walker) jsonType(v *Schema) string {
	if t := w.deref(v).Type; t != "" {
		return t
	}
	return "object"
}

// isListEnvelope reports a v1 list: a hash whose `object` is `list` and whose `data` is an
// array.
func (w *walker) isListEnvelope(sc *Schema) bool {
	value, ok := w.objectValue(sc)
	data := w.deref(prop(sc, "data"))
	return ok && value == "list" && data != nil && data.Type == "array"
}

// elementObject names the object a list holds, or "" when its elements are not one object.
func (w *walker) elementObject(sc *Schema) string {
	items := w.deref(prop(sc, "data")).Items
	if items == nil {
		return ""
	}
	var objects []string
	for _, v := range append([]*Schema{items}, slices.Concat(w.deref(items).AnyOf, w.deref(items).OneOf)...) {
		if name, ok := strings.CutPrefix(v.Ref, refPrefix); ok {
			if o := objectName(name, w.spec.Components.Schemas[name]); o != "" && !slices.Contains(objects, o) {
				objects = append(objects, o)
			}
		}
	}
	if len(objects) == 1 {
		return objects[0]
	}
	return ""
}

// deref follows a chain of `$ref` to the schema it names, or nil.
func (w *walker) deref(sc *Schema) *Schema {
	for hops := 0; sc != nil && sc.Ref != "" && hops <= 16; hops++ {
		sc = w.spec.Components.Schemas[strings.TrimPrefix(sc.Ref, refPrefix)]
	}
	return sc
}

func sortedNames(m map[string]*Schema) []string {
	names := make([]string, 0, len(m))
	for n := range m {
		names = append(names, n)
	}
	sort.Strings(names)
	return names
}

func join(a, b string) string {
	if a == "" {
		return b
	}
	return a + "." + b
}

// parentPath returns the path of the hash holding a dotted path, "" at the top level.
func parentPath(p string) string {
	if i := strings.LastIndex(p, "."); i >= 0 {
		return p[:i]
	}
	return ""
}
