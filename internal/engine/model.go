package engine

import (
	"errors"
	"fmt"
	"net/http"
	"slices"
	"sort"
	"strings"

	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/computomatic/terraform-provider-stripe/internal/fields"
	"github.com/computomatic/terraform-provider-stripe/internal/stripeapi"
)

// node is a table field with every decision of the D2 rules resolved.
type node struct {
	fields.Field
	// Path is the dotted Stripe field path.
	Path string
	Kids []*node

	Required bool
	Optional bool
	Computed bool
	// Sensitive hides the value in plans and masks it in logs.
	Sensitive bool
	// Replace attaches RequiresReplace: Stripe rejects the field on update.
	Replace bool
	// UseState attaches UseStateForUnknown: Stripe never changes the value by itself.
	UseState bool
	// Once is a once-returned output: a response that omits it keeps the prior value.
	Once bool
	// NullAs is the string a null response value stands for. Empty means none.
	NullAs string
	// Preserve keeps the configured value when the response redacts or reformats it.
	Preserve bool
	// ReplaceIfSet replaces the resource when the configuration sets a value that differs
	// from the state's, and never for an unknown one: a create-only value Stripe fills in for
	// a list element.
	ReplaceIfSet bool
	// Fixed is a field of an element of a FixedKeys list that Stripe cannot change in place.
	// The plan check replaces the resource when it changes, element by element, so a
	// reorder does not.
	Fixed bool
}

// computedOnly reports an output the user cannot set.
func (n *node) computedOnly() bool { return n.Computed && !n.Optional && !n.Required }

// model is a declaration compiled against its field table. It is read-only once built and
// shared by the resource and the data source.
type model struct {
	decl  Declaration
	nodes []*node
	// v2 is true when the declaration's paths are in the `/v2` namespace.
	v2 bool
	// sensitiveNames are the field names masked in request logs.
	sensitiveNames []string
}

// reservedNames are the root attribute names Terraform does not allow.
var reservedNames = map[string]bool{
	"count": true, "depends_on": true, "for_each": true, "lifecycle": true, "provider": true, "providers": true,
}

type pathSet map[string]bool

func newPathSet(paths []string) pathSet {
	s := pathSet{}
	for _, p := range paths {
		s[p] = true
	}
	return s
}

// compile resolves the declaration against the table and rejects any declaration entry
// that names no field, so a stale declaration fails when the schema is built.
func compile(decl Declaration, table []fields.Field) (*model, error) {
	if decl.TypeName == "" {
		return nil, fmt.Errorf("declaration has no type name")
	}
	if decl.FindInList {
		if decl.Paths.Retrieve != "" || decl.Paths.List == "" || decl.Parent == "" || decl.Delete != NoDelete || len(decl.Lookups) > 0 {
			return nil, fmt.Errorf("%s: an object found in a list has a parent, a list path, no retrieve path, no lookups and no delete strategy", decl.TypeName)
		}
	} else if decl.Paths.Retrieve == "" {
		return nil, fmt.Errorf("%s: declaration has no retrieve path", decl.TypeName)
	}
	v2, err := namespaceOf(decl)
	if err != nil {
		return nil, err
	}
	if decl.Singleton && (decl.Delete != NoDelete || len(decl.Lookups) > 0) {
		return nil, fmt.Errorf("%s: a singleton has no delete strategy and no lookups", decl.TypeName)
	}
	if decl.Delete != NoDelete && decl.Paths.Create == "" {
		return nil, fmt.Errorf("%s: declaration has no create path", decl.TypeName)
	}
	switch decl.Delete {
	case Delete:
		if decl.Paths.Delete == "" {
			return nil, fmt.Errorf("%s: the delete strategy needs a delete path", decl.TypeName)
		}
	case DeleteOrArchive:
		if decl.Paths.Delete == "" || decl.ArchiveOn == nil || !decl.archivable() {
			return nil, fmt.Errorf("%s: the delete-or-archive strategy needs a delete path, a refusal, an archive action and an update path or action endpoint", decl.TypeName)
		}
	case Archive:
		if !decl.archivable() {
			return nil, fmt.Errorf("%s: the archive strategy needs an archive action and an update path or action endpoint", decl.TypeName)
		}
	case Retain:
		if decl.RetainWarning == "" {
			return nil, fmt.Errorf("%s: the retain strategy needs a warning that says what stays behind", decl.TypeName)
		}
	case Expire:
		if decl.Expire == nil || decl.Paths.Update == "" {
			return nil, fmt.Errorf("%s: the expire strategy needs an expire action and an update path", decl.TypeName)
		}
	}

	c := compiler{
		decl:         decl,
		once:         newPathSet(decl.OnceReturned),
		defaulted:    newPathSet(decl.ServerDefaulted),
		unDefaulted:  newPathSet(decl.NotServerDefaulted),
		sensitive:    newPathSet(decl.Sensitive),
		preserve:     newPathSet(decl.PreserveConfig),
		sets:         newPathSet(decl.Sets),
		replaceOn:    newPathSet(decl.ReplaceOn),
		used:         pathSet{},
		sensitiveSet: pathSet{},
	}
	nodes, err := c.build(table, "", true, false, false, false, false)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", decl.TypeName, err)
	}
	if err := c.unused(); err != nil {
		return nil, fmt.Errorf("%s: %w", decl.TypeName, err)
	}

	m := &model{decl: decl, nodes: nodes, v2: v2}
	if decl.Attach != "" && (decl.Parent == "" || !strings.Contains(decl.Paths.Create, "{"+decl.Attach+"}")) {
		return nil, fmt.Errorf("%s: an attachment needs a parent and a create path that names `{%s}`", decl.TypeName, decl.Attach)
	}
	if _, ok := m.top("id"); !ok && !decl.Singleton {
		return nil, fmt.Errorf("%s: the table has no `id` field", decl.TypeName)
	}
	for _, n := range nodes {
		if reservedNames[n.AttrName()] {
			return nil, fmt.Errorf("%s: field `%s` is a name Terraform reserves; add a rename to the declaration", decl.TypeName, n.AttrName())
		}
		if n.AttrName() == "timeouts" || n.AttrName() == decl.Parent || decl.Attach != "" && n.AttrName() == decl.Attach {
			return nil, fmt.Errorf("%s: field `%s` collides with a provider-added attribute; rename it", decl.TypeName, n.AttrName())
		}
	}
	if a := decl.Advance; a != nil {
		n, ok := m.top(a.Field)
		if !ok || n.Shape != fields.Int || !n.Required || a.Path == "" || a.StatusField == "" || a.ReadyValue == "" || decl.Delete == NoDelete {
			return nil, fmt.Errorf("%s: the advance action needs a required top-level integer field `%s` on a resource, an action path and a ready status", decl.TypeName, a.Field)
		}
		if st, ok := m.top(a.StatusField); !ok || st.Shape != fields.String || !st.Response {
			return nil, fmt.Errorf("%s: advance status `%s` is not a top-level string field the response returns", decl.TypeName, a.StatusField)
		}
	}
	if err := m.openTransitions(); err != nil {
		return nil, err
	}
	for _, e := range decl.Expand {
		if n, ok := m.top(e); !ok || !n.Response {
			return nil, fmt.Errorf("%s: expand entry `%s` is not a top-level field the response returns", decl.TypeName, e)
		}
	}
	if v2 && len(decl.Expand) > 0 {
		return nil, fmt.Errorf("%s: /v2 does not support expand; declare include values instead", decl.TypeName)
	}
	if !v2 && (len(decl.Include) > 0 || len(decl.ClearsWithNull) > 0) {
		return nil, fmt.Errorf("%s: include and clears-with-null belong to /v2 declarations", decl.TypeName)
	}
	for _, name := range decl.ClearsWithNull {
		if n, ok := m.top(name); !ok || !n.Update || n.Required {
			return nil, fmt.Errorf("%s: clears-with-null entry `%s` is not an optional top-level field Stripe takes on update", decl.TypeName, name)
		}
	}
	if decl.CurrencyOptions != "" {
		n, ok := m.top(decl.CurrencyOptions)
		if !ok || n.Shape != fields.ObjectMap || !n.Response {
			return nil, fmt.Errorf("%s: currency options entry `%s` is not a top-level map the response returns", decl.TypeName, decl.CurrencyOptions)
		}
		if c, ok := m.top("currency"); !ok || c.Shape != fields.String || !c.Response {
			return nil, fmt.Errorf("%s: currency options need a top-level string field `currency` the response returns", decl.TypeName)
		}
	}
	if w := decl.CreateWrap; w != nil {
		if w.Key == "" {
			return nil, fmt.Errorf("%s: create wrap has no key", decl.TypeName)
		}
		for _, name := range w.Outside {
			if n, ok := m.top(name); !ok || !n.Create {
				return nil, fmt.Errorf("%s: create wrap outside entry `%s` is not a top-level create field", decl.TypeName, name)
			}
		}
	}
	for _, name := range decl.RefuseChange {
		if n, ok := m.top(name); !ok || !n.Optional {
			return nil, fmt.Errorf("%s: refuse-change entry `%s` is not an optional top-level field", decl.TypeName, name)
		}
	}
	if len(decl.RefuseChange) > 0 {
		for _, n := range m.nodes {
			if below := nestedReplaceSource(n.Kids); below != "" {
				return nil, fmt.Errorf("%s: field `%s.%s` forces replacement, and RefuseChange supports only top-level replace sources", decl.TypeName, n.Name, below)
			}
		}
	}
	for _, group := range decl.ExactlyOneOf {
		if len(group) < 2 {
			return nil, fmt.Errorf("%s: an exactly-one-of group needs at least two fields", decl.TypeName)
		}
		for _, name := range group {
			if n, ok := m.top(name); !ok || !n.Optional {
				return nil, fmt.Errorf("%s: exactly-one-of entry `%s` is not an optional top-level field", decl.TypeName, name)
			}
		}
	}
	if err := m.checkListEntries(); err != nil {
		return nil, fmt.Errorf("%s: %w", decl.TypeName, err)
	}
	for _, l := range decl.Lookups {
		n, ok := m.top(l.Field)
		if !ok || n.Shape != fields.String || !n.Response {
			return nil, fmt.Errorf("%s: lookup key `%s` is not a top-level string field the response returns", decl.TypeName, l.Field)
		}
		if decl.Paths.List == "" {
			return nil, fmt.Errorf("%s: lookup keys need a list path", decl.TypeName)
		}
	}

	names := map[string]bool{}
	for p := range c.sensitiveSet {
		names[p[strings.LastIndex(p, ".")+1:]] = true
	}
	for name := range names {
		m.sensitiveNames = append(m.sensitiveNames, name)
	}
	sort.Strings(m.sensitiveNames)
	return m, nil
}

// openTransitions makes each transition's field configurable. The field stays out of every
// request body, because it is not in a request schema.
func (m *model) openTransitions() error {
	for _, t := range m.decl.Transitions {
		n, ok := m.top(t.Field)
		if !ok || n.Shape != fields.String || !n.Response || n.Writable() {
			return fmt.Errorf("%s: transition field `%s` is not a response-only top-level string", m.decl.TypeName, t.Field)
		}
		if t.Value == "" || t.Path == "" {
			return fmt.Errorf("%s: a transition of `%s` needs a value and an action path", m.decl.TypeName, t.Field)
		}
		n.Optional = true
	}
	return nil
}

// checkListEntries rejects a MatchBy, Carry, SendWithUpdate or SendWithUpdateOn entry that
// names no field of the kind it needs.
func kidNamed(n *node, name string) *node {
	for _, k := range n.Kids {
		if k.Name == name {
			return k
		}
	}
	return nil
}

func (m *model) checkListEntries() error {
	list := func(entry, name string) (*node, error) {
		n, ok := m.top(name)
		if !ok || n.Shape != fields.ObjectList || !n.Update {
			return nil, fmt.Errorf("%s entry `%s` is not an updatable top-level list of objects", entry, name)
		}
		return n, nil
	}
	kid := func(n *node, name string) *node {
		for _, k := range n.Kids {
			if k.Name == name {
				return k
			}
		}
		return nil
	}
	for name, key := range m.decl.MatchBy {
		n, err := list("match_by", name)
		if err != nil {
			return err
		}
		if k := kid(n, key); k == nil || k.Shape != fields.String {
			return fmt.Errorf("match_by entry `%s` names `%s`, which is not a string field of its elements", name, key)
		}
		if id := kid(n, "id"); id == nil || id.Shape != fields.String || !id.Update {
			return fmt.Errorf("match_by entry `%s` needs an updatable string `id` field in its elements", name)
		}
	}
	for _, name := range m.decl.FixedKeys {
		if _, keyed := m.decl.MatchBy[name]; !keyed {
			return fmt.Errorf("fixed_keys entry `%s` is not a match_by list", name)
		}
	}
	for name, field := range m.decl.MatchData {
		n, err := list("match_data", name)
		if err != nil {
			return err
		}
		if _, keyed := m.decl.MatchBy[name]; !keyed {
			return fmt.Errorf("match_data entry `%s` is not a match_by list", name)
		}
		if k := kidNamed(n, field); k == nil || k.Shape != fields.Object || !k.Update {
			return fmt.Errorf("match_data entry `%s` names `%s`, which is not an updatable object field of its elements", name, field)
		}
	}
	for _, c := range m.decl.Carry {
		n, err := list("carry", c.List)
		if err != nil {
			return err
		}
		if k := kid(n, c.Field); k == nil || k.Shape != fields.String {
			return fmt.Errorf("carry entry `%s.%s` is not a string field of the list's elements; only strings can be carried", c.List, c.Field)
		}
	}
	if (len(m.decl.SendWithUpdate) > 0) != (len(m.decl.SendWithUpdateOn) > 0) {
		return fmt.Errorf("send_with_update and send_with_update_on go together")
	}
	for kind, names := range map[string][]string{"send_with_update": m.decl.SendWithUpdate, "send_with_update_on": m.decl.SendWithUpdateOn} {
		for _, name := range names {
			if n, ok := m.top(name); !ok || !n.Update {
				return fmt.Errorf("%s entry `%s` is not an updatable top-level field", kind, name)
			}
		}
	}
	return nil
}

// namespaceOf reports whether the declaration is `/v2`, and fails when its paths mix
// namespaces or name neither.
func namespaceOf(decl Declaration) (bool, error) {
	paths := []string{decl.Paths.Create, decl.Paths.Retrieve, decl.Paths.Update, decl.Paths.Delete, decl.Paths.List}
	if decl.Archive != nil {
		paths = append(paths, decl.Archive.Path)
	}
	for _, t := range decl.Transitions {
		paths = append(paths, t.Path)
	}
	v1, v2 := 0, 0
	for _, p := range paths {
		switch {
		case p == "":
		case stripeapi.IsV2Path(p):
			v2++
		case strings.HasPrefix(p, "/v1/"):
			v1++
		default:
			return false, fmt.Errorf("%s: path %q is in neither /v1 nor /v2", decl.TypeName, p)
		}
	}
	if v1 > 0 && v2 > 0 {
		return false, fmt.Errorf("%s: paths mix /v1 and /v2", decl.TypeName)
	}
	return v2 > 0, nil
}

// archivable reports whether the declaration says how to archive: an archive action with
// an update to send or an action endpoint to call.
func (d Declaration) archivable() bool {
	return d.Archive != nil && (d.Archive.Path != "" || d.Paths.Update != "")
}

// gone reports that err says the object does not exist: a 404, or the 403 the declaration
// names. The 403 is also what Stripe answers when the key can no longer reach the object, so
// it adds a warning to diags that says so.
func (m *model) gone(err error, diags *diag.Diagnostics) bool {
	if stripeapi.IsNotFound(err) {
		return true
	}
	var apiErr *stripeapi.APIError
	code := m.decl.GoneOnForbidden
	if code == "" || !errors.As(err, &apiErr) || apiErr.Status != http.StatusForbidden || apiErr.Code != code {
		return false
	}
	diags.AddWarning("Treated as gone: Stripe answered 403 "+code,
		fmt.Sprintf("Stripe answered 403 `%s` for a %s. The account is either deleted or no longer reachable with this key (for example because access was revoked). Terraform treats it as gone.", code, m.decl.TypeName))
	return true
}

// nestedReplaceSource returns the dotted path, below its parent, of the first node in kids
// that forces replacement, or "".
func nestedReplaceSource(kids []*node) string {
	for _, k := range kids {
		if k.Replace || k.ReplaceIfSet {
			return k.Name
		}
		if below := nestedReplaceSource(k.Kids); below != "" {
			return k.Name + "." + below
		}
	}
	return ""
}

// top returns the top-level node with the given Stripe field name.
func (m *model) top(name string) (*node, bool) {
	for _, n := range m.nodes {
		if n.Name == name {
			return n, true
		}
	}
	return nil, false
}

type compiler struct {
	decl         Declaration
	once         pathSet
	defaulted    pathSet
	unDefaulted  pathSet
	sensitive    pathSet
	preserve     pathSet
	sets         pathSet
	replaceOn    pathSet
	used         pathSet
	sensitiveSet pathSet
}

func (c *compiler) mark(kind string, set pathSet, path string) bool {
	if set[path] {
		c.used[kind+"/"+path] = true
		return true
	}
	return false
}

// unused reports a declaration entry that matched no field.
func (c *compiler) unused() error {
	for _, e := range []struct {
		kind string
		set  pathSet
	}{
		{"once_returned", c.once},
		{"server_defaulted", c.defaulted},
		{"not_server_defaulted", c.unDefaulted},
		{"sensitive", c.sensitive},
		{"preserve_config", c.preserve},
		{"sets", c.sets},
		{"replace_on", c.replaceOn},
	} {
		for p := range e.set {
			if !c.used[e.kind+"/"+p] {
				return fmt.Errorf("%s entry `%s` names no field", e.kind, p)
			}
		}
	}
	for p := range c.decl.NullAs {
		if !c.used["null_as/"+p] {
			return fmt.Errorf("null_as entry `%s` names no field", p)
		}
	}
	for p := range c.decl.Rename {
		if !c.used["rename/"+p] {
			return fmt.Errorf("rename entry `%s` names no field", p)
		}
	}
	return nil
}

// build resolves one level of the table. parentOutput is true when every field below is a
// Computed output; parentReplace when an enclosing field already forces replacement.
func (c *compiler) build(table []fields.Field, prefix string, top, parentOutput, parentReplace, inList, inFixed bool) ([]*node, error) {
	var out []*node
	for _, f := range table {
		path := prefix + f.Name
		if attrName, ok := c.decl.Rename[path]; ok {
			f.Attr = attrName
			c.used["rename/"+path] = true
		}
		if c.mark("sets", c.sets, path) {
			if f.Shape != fields.ScalarList {
				return nil, fmt.Errorf("sets entry `%s` is not a scalar array", path)
			}
			f.Shape = fields.ScalarSet
		}
		n := &node{Field: f, Path: path}
		if v, ok := c.decl.NullAs[path]; ok {
			if f.Shape != fields.String {
				return nil, fmt.Errorf("null_as entry `%s` is not a string field", path)
			}
			n.NullAs = v
			c.used["null_as/"+path] = true
		}

		once := c.mark("once_returned", c.once, path)
		n.Sensitive = c.mark("sensitive", c.sensitive, path) || once
		n.Preserve = c.mark("preserve_config", c.preserve, path)
		notDefaulted := c.mark("not_server_defaulted", c.unDefaulted, path)
		if c.mark("server_defaulted", c.defaulted, path) {
			f.ServerDefaulted = true
		}
		if n.Sensitive {
			c.sensitiveSet[path] = true
		}
		if n.Preserve && !f.Writable() {
			return nil, fmt.Errorf("preserve_config entry `%s` is not a writable field", path)
		}

		switch {
		case once:
			n.Once, n.Computed = true, true
		case !f.Writable() || parentOutput:
			n.Computed = true
		default:
			n.Required = f.Required && f.Create
			n.Optional = !n.Required
			n.Computed = !n.Required &&
				((f.ServerDefaulted && !notDefaulted) || (f.Response && !f.Create))
			if top && f.Name == "metadata" && f.Shape == fields.StringMap {
				n.Computed = false
			}
		}
		replaceOn := c.mark("replace_on", c.replaceOn, path)
		if replaceOn && !f.Update {
			return nil, fmt.Errorf("replace_on entry `%s` is not an updatable field; a create-only field already forces replacement", path)
		}
		// A create-only value Stripe fills in for an element of a list (a schedule phase's
		// `currency`) cannot be told from a new element's: the element has no prior value to
		// keep, and an unknown one would force replacement. It is computed, and replaces only
		// when the configuration sets a value that differs from the state's.
		serverFilledInList := inList && n.Computed && f.CreateOnly() && !replaceOn
		n.ReplaceIfSet = serverFilledInList && !parentReplace && !nestedShape(f.Shape)
		n.Replace = (f.CreateOnly() || replaceOn) && !parentReplace && !n.computedOnly() && !serverFilledInList && !(top && c.decl.Advance != nil && f.Name == c.decl.Advance.Field)
		// Below a field that forces replacement nothing changes in place, so a computed
		// value there stays as it is: an unknown one would read as a change of the parent.
		// Terraform proposes null for an unset nested attribute even when it is Computed
		// (it keeps the prior value of a scalar), which would read as a change on every plan,
		// so a nested attribute Stripe fills in keeps its prior value too. Inside a list element
		// that would copy another element's value after an insert or reorder, so it does not apply.
		if inFixed {
			n.Fixed = (f.CreateOnly() || replaceOn) && !n.computedOnly()
			n.Replace, n.ReplaceIfSet = false, false
		}
		n.UseState = n.Computed && !n.Required && !serverFilledInList &&
			((top && (f.Name == "id" || f.Name == "created" || f.Name == "livemode")) || f.CreateOnly() || replaceOn || n.Once || parentReplace ||
				n.Optional && !inList && nestedShape(f.Shape))

		switch f.Shape {
		case fields.Object, fields.ObjectList, fields.ObjectMap:
			kids, err := c.build(f.Fields, path+".", false, n.computedOnly(), parentReplace || n.Replace, inList || f.Shape == fields.ObjectList, inFixed || top && slices.Contains(c.decl.FixedKeys, f.Name))
			if err != nil {
				return nil, err
			}
			n.Kids = kids
		}
		out = append(out, n)
	}
	return out, nil
}

// nestedShape reports a shape that becomes a nested attribute.
func nestedShape(s fields.Shape) bool {
	return s == fields.Object || s == fields.ObjectList || s == fields.ObjectMap
}

// attrType returns the framework type of the attribute built from n.
func attrType(n *node) attr.Type {
	switch n.Shape {
	case fields.Bool:
		return types.BoolType
	case fields.Int:
		return types.Int64Type
	case fields.Float:
		return types.Float64Type
	case fields.Object:
		return objectType(n.Kids)
	case fields.ObjectList:
		return types.ListType{ElemType: objectType(n.Kids)}
	case fields.ObjectMap:
		return types.MapType{ElemType: objectType(n.Kids)}
	case fields.StringMap:
		return types.MapType{ElemType: types.StringType}
	case fields.IntMap:
		return types.MapType{ElemType: types.Int64Type}
	case fields.ScalarList:
		return types.ListType{ElemType: scalarType(n.Elem)}
	case fields.ScalarSet:
		return types.SetType{ElemType: scalarType(n.Elem)}
	default:
		return types.StringType
	}
}

func scalarType(s fields.Shape) attr.Type {
	switch s {
	case fields.Bool:
		return types.BoolType
	case fields.Int:
		return types.Int64Type
	case fields.Float:
		return types.Float64Type
	default:
		return types.StringType
	}
}

func objectType(kids []*node) types.ObjectType {
	attrs := make(map[string]attr.Type, len(kids))
	for _, k := range kids {
		attrs[k.AttrName()] = attrType(k)
	}
	return types.ObjectType{AttrTypes: attrs}
}
