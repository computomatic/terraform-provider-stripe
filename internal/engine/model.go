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
	"github.com/hashicorp/terraform-plugin-go/tftypes"

	"github.com/computomatic/terraform-provider-stripe/internal/fields"
	"github.com/computomatic/terraform-provider-stripe/internal/stripeapi"
	"github.com/computomatic/terraform-provider-stripe/internal/wire"
)

// node is a table field with every decision of the D2 rules resolved.
type node struct {
	fields.Field
	// Path is the dotted Stripe field path, which declaration entries name. The hash form of a
	// token-or-hash parameter, which shares the token's Stripe name, is named by its attribute
	// name, `<name>_data`, and so is everything below it, so that an entry names one form.
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
	// KeepOmitted keeps the prior value when the response leaves the field out or null.
	KeepOmitted bool
	// FixedOnceSet is the one value Stripe lets the field change from. A configured change
	// from any other value replaces the resource. Empty means none.
	FixedOnceSet string
	// ReplaceIfSet replaces the resource when the configuration sets a value that differs
	// from the state's, and never for an unknown one: a create-only value Stripe fills in for
	// a list element.
	ReplaceIfSet bool
	// IsHashForm marks the hash form of a token-or-hash parameter (`<name>_data`).
	IsHashForm bool
	// TokenForm is, on the hash form, the attribute name of its token form, and DataForm, on the
	// token form, the attribute name of its hash form: a configuration sets at most one of the
	// two, and exactly one where OneRequired is set, because the create request requires the
	// parameter.
	TokenForm, DataForm string
	OneRequired         bool
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
		if decl.Paths.Retrieve != "" || decl.Paths.List == "" || decl.Parent == "" || decl.Delete != NoDelete || len(decl.Lookups) > 0 || len(decl.FindBy) > 0 {
			return nil, fmt.Errorf("%s: an object found in a list has a parent, a list path, no retrieve path, no lookups, no find parameters and no delete strategy", decl.TypeName)
		}
	} else if len(decl.FindBy) > 0 {
		if (decl.Paths.Retrieve == "") == (decl.Paths.List == "") || decl.Singleton || decl.Delete != NoDelete || len(decl.Lookups) > 0 {
			return nil, fmt.Errorf("%s: an object found by parameters has a retrieve path or a list path, not both, no lookups and no delete strategy", decl.TypeName)
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
	case Refuse:
		if decl.RefuseReason == "" || decl.Archive != nil || decl.ArchiveOn != nil || decl.Paths.Delete != "" || decl.RefuseWhen != nil {
			return nil, fmt.Errorf("%s: the refuse strategy needs a reason and has no archive action, refusal, delete path or refuse-when state", decl.TypeName)
		}
	}
	if decl.MovesMoney && decl.Delete == NoDelete {
		return nil, fmt.Errorf("%s: only a resource can move money; a declaration without a delete strategy has none", decl.TypeName)
	}
	if decl.Delete == Refuse || decl.RefuseWhen != nil {
		// Those two force a replacement in ModifyPlan, which the refusal does not cover; no
		// refusing resource has either.
		if decl.CurrencyOptions != "" || len(decl.FixedKeys) > 0 {
			return nil, fmt.Errorf("%s: a resource that refuses destroy cannot have currency options or fixed keys", decl.TypeName)
		}
	}

	c := compiler{
		decl:         decl,
		once:         newPathSet(decl.OnceReturned),
		defaulted:    newPathSet(decl.ServerDefaulted),
		unDefaulted:  newPathSet(decl.NotServerDefaulted),
		sensitive:    newPathSet(decl.Sensitive),
		preserve:     newPathSet(decl.PreserveConfig),
		keepOmitted:  newPathSet(decl.KeepOmitted),
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
	if decl.Upload != nil && (decl.Upload.Part == "" || decl.Paths.Create == "" || v2) {
		return nil, fmt.Errorf("%s: an upload needs a part name and a /v1 create path", decl.TypeName)
	}
	if _, ok := m.top("id"); !ok && !decl.Singleton {
		return nil, fmt.Errorf("%s: the table has no `id` field", decl.TypeName)
	}
	for _, n := range nodes {
		if reservedNames[n.AttrName()] {
			return nil, fmt.Errorf("%s: field `%s` is a name Terraform reserves; add a rename to the declaration", decl.TypeName, n.AttrName())
		}
		if n.AttrName() == "timeouts" || n.AttrName() == decl.Parent || decl.Attach != "" && n.AttrName() == decl.Attach ||
			decl.Upload != nil && (n.AttrName() == sourceAttr || n.AttrName() == sourceHashAttr) {
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
	if w := decl.RefuseWhen; w != nil {
		if decl.Delete != Delete && decl.Delete != Archive && decl.Delete != DeleteOrArchive || w.Reason == "" {
			return nil, fmt.Errorf("%s: refuse-when needs the delete, archive or delete-or-archive strategy and a reason", decl.TypeName)
		}
		n, ok := m.top(w.Field)
		_, isBool := w.Value.(wire.Bool)
		_, isString := w.Value.(wire.String)
		if !ok || !n.Response || !(n.Shape == fields.Bool && isBool || n.Shape == fields.String && isString) {
			return nil, fmt.Errorf("%s: refuse-when field `%s` is not a top-level bool or string field the response returns, with a value of its type", decl.TypeName, w.Field)
		}
	}
	if f := decl.KeepWhenClosed; f != "" {
		if n, ok := m.top(f); !ok || !n.Response || n.Shape != fields.Bool {
			return nil, fmt.Errorf("%s: keep-when-closed field `%s` is not a top-level bool field the response returns", decl.TypeName, f)
		}
	}
	for _, e := range decl.Expand {
		if n, ok := m.top(e); !ok || !n.Response {
			return nil, fmt.Errorf("%s: expand entry `%s` is not a top-level field the response returns", decl.TypeName, e)
		}
	}
	for _, e := range decl.ExpandOnRetrieve {
		if n, ok := m.top(e); !ok || !n.Response || slices.Contains(decl.Expand, e) {
			return nil, fmt.Errorf("%s: expand-on-retrieve entry `%s` is not a top-level field the response returns, or is also in expand", decl.TypeName, e)
		}
	}
	if len(decl.ExpandOnRetrieve) > 0 && (len(decl.Lookups) > 0 || decl.FindInList || len(decl.FindBy) > 0 || decl.Singleton) {
		return nil, fmt.Errorf("%s: expand-on-retrieve needs an object read by its ID alone, with no lookups", decl.TypeName)
	}
	if v2 && len(decl.Expand)+len(decl.ExpandOnRetrieve) > 0 {
		return nil, fmt.Errorf("%s: /v2 does not support expand; declare include values instead", decl.TypeName)
	}
	for _, name := range decl.LowerCase {
		if n, ok := m.top(name); !ok || n.Shape != fields.String || !n.Optional && !n.Required {
			return nil, fmt.Errorf("%s: lower-case entry `%s` is not a configurable top-level string field", decl.TypeName, name)
		}
	}
	for name, from := range decl.FixedOnceSet {
		n, ok := m.top(name)
		if !ok || n.Shape != fields.String || !n.Update || n.Replace || from == "" {
			return nil, fmt.Errorf("%s: fixed-once-set entry `%s` is not a top-level string field Stripe updates, with a value it changes from", decl.TypeName, name)
		}
		n.FixedOnceSet = from
	}
	if decl.MetadataKeysStay {
		if n, ok := m.top("metadata"); !ok || n.Shape != fields.StringMap || !n.Update {
			return nil, fmt.Errorf("%s: metadata-keys-stay needs an updatable top-level `metadata` map", decl.TypeName)
		}
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
	for source, filled := range decl.FilledFrom {
		if n, _, ok := m.at(source); !ok || !n.Optional && !n.Required {
			return nil, fmt.Errorf("%s: filled-from source `%s` is not a configurable field outside a list", decl.TypeName, source)
		}
		for _, f := range filled {
			if n, _, ok := m.at(f); !ok || !n.Optional || !n.Computed {
				return nil, fmt.Errorf("%s: filled-from entry `%s` is not a server-defaulted field outside a list", decl.TypeName, f)
			}
		}
	}
	for _, name := range decl.RefuseChange {
		if n, _, ok := m.at(name); !ok || !n.Optional && !n.Required {
			return nil, fmt.Errorf("%s: refuse-change entry `%s` is not a configurable field outside a list", decl.TypeName, name)
		}
	}
	// refuseChanges tells a replace only by its top-level sources. A resource that refuses
	// destroy fails every real replacement in the attribute plan modifier, before refuseChanges
	// runs, so a source below the top level needs no telling there.
	if len(decl.RefuseChange) > 0 && decl.Delete != Refuse {
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
	for _, p := range decl.FindBy {
		if err := m.checkFindParam(p); err != nil {
			return nil, fmt.Errorf("%s: %w", decl.TypeName, err)
		}
	}
	for _, p := range decl.ListParams {
		if err := m.checkListParam(p); err != nil {
			return nil, fmt.Errorf("%s: %w", decl.TypeName, err)
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

// checkFindParam refuses a find parameter the engine cannot send: a field that is not a
// string, an integer or an object of those, one the response does not return, and a name
// that collides with the parent or has no description.
func (m *model) checkFindParam(p FindParam) error {
	n, ok := m.top(p.Name)
	if !ok {
		if p.Description == "" || p.Name == m.decl.Parent || p.Name == "id" {
			return fmt.Errorf("find parameter `%s` names no field, so it needs a description and a name of its own", p.Name)
		}
		return nil
	}
	scalar := func(s fields.Shape) bool { return s == fields.String || s == fields.Int }
	fits := n.Response && n.Name != "id" && scalar(n.Shape)
	if n.Response && n.Shape == fields.Object {
		fits = true
		for _, k := range n.Kids {
			fits = fits && scalar(k.Shape)
		}
	}
	if !fits {
		return fmt.Errorf("find parameter `%s` is not a top-level string, integer or object of those that the response returns", p.Name)
	}
	return nil
}

// checkListParam refuses a list parameter the engine cannot send or default: one of an object
// not found in a list, a field that is not a top-level string the response returns, the `id`,
// and one with no parent path under the parent, description or unset error.
func (m *model) checkListParam(p ListParam) error {
	if !m.decl.FindInList {
		return fmt.Errorf("list parameter `%s` needs an object found in a list", p.Name)
	}
	n, ok := m.top(p.Name)
	if !ok || n.Shape != fields.String || !n.Response || n.Name == "id" {
		return fmt.Errorf("list parameter `%s` is not a top-level string field the response returns", p.Name)
	}
	if !strings.Contains(p.ParentPath, "{"+m.decl.Parent+"}") || p.Description == "" || p.Unset == "" {
		return fmt.Errorf("list parameter `%s` needs a parent path under `{%s}`, a description and an unset error", p.Name, m.decl.Parent)
	}
	return nil
}

// listParam returns the list parameter the Stripe field is, if it is one.
func (m *model) listParam(name string) (ListParam, bool) {
	i := slices.IndexFunc(m.decl.ListParams, func(p ListParam) bool { return p.Name == name })
	if i < 0 {
		return ListParam{}, false
	}
	return m.decl.ListParams[i], true
}

// findParam reports whether the Stripe field is a find parameter.
func (m *model) findParam(name string) bool {
	return slices.ContainsFunc(m.decl.FindBy, func(p FindParam) bool { return p.Name == name })
}

// openTransitions makes each transition's field configurable. The field stays out of every
// request body, because it is not in a request schema.
func (m *model) openTransitions() error {
	for _, t := range m.decl.Transitions {
		n, ok := m.top(t.Field)
		if !ok || n.Shape != fields.String && n.Shape != fields.Bool || !n.Response || n.Writable() {
			return fmt.Errorf("%s: transition field `%s` is not a response-only top-level string or bool", m.decl.TypeName, t.Field)
		}
		if t.Value == "" || t.Path == "" {
			return fmt.Errorf("%s: a transition of `%s` needs a value and an action path", m.decl.TypeName, t.Field)
		}
		if n.Shape == fields.Bool && (t.Value != "true" && t.Value != "false" || len(t.Final) > 0 || len(t.Reached) > 0) {
			return fmt.Errorf("%s: a transition of the bool `%s` sets `true` or `false` and has no final or reached value", m.decl.TypeName, t.Field)
		}
		if slices.Contains(t.Reached, t.Value) || slices.ContainsFunc(t.Reached, func(v string) bool { return slices.Contains(t.Final, v) }) {
			return fmt.Errorf("%s: a transition of `%s` to `%s` cannot reach that value or a final one", m.decl.TypeName, t.Field, t.Value)
		}
		for _, name := range t.Send {
			if _, ok := m.top(name); !ok {
				return fmt.Errorf("%s: a transition of `%s` sends `%s`, which is no top-level field", m.decl.TypeName, t.Field, name)
			}
		}
		for _, other := range m.decl.Transitions {
			if other.Field == t.Field && other.Value == t.Value && (!slices.Equal(other.Final, t.Final) || !slices.Equal(other.Reached, t.Reached)) {
				return fmt.Errorf("%s: the transitions of `%s` to `%s` must declare the same final and reached values", m.decl.TypeName, t.Field, t.Value)
			}
		}
		if slices.Contains(t.From, t.Value) {
			return fmt.Errorf("%s: a transition of `%s` to `%s` cannot start from that value", m.decl.TypeName, t.Field, t.Value)
		}
		n.Optional = true
	}
	return nil
}

// checkListEntries rejects a MatchBy, MatchOnRead, Carry, SendWithUpdate or SendWithUpdateOn
// entry that names no field of the kind it needs.
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
	for name, key := range m.decl.MatchOnRead {
		n, ok := m.top(name)
		if !ok || n.Shape != fields.ObjectList {
			return fmt.Errorf("match_on_read entry `%s` is not a top-level list of objects", name)
		}
		if _, keyed := m.decl.MatchBy[name]; keyed {
			return fmt.Errorf("match_on_read entry `%s` is a match_by list too", name)
		}
		if k := kid(n, key); k == nil || k.Shape != fields.String || !k.Response {
			return fmt.Errorf("match_on_read entry `%s` names `%s`, which is not a string field Stripe returns in its elements", name, key)
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

// at returns the node at a dotted Stripe field path and its attribute path, through nested
// objects only.
func (m *model) at(dotted string) (*node, *tftypes.AttributePath, bool) {
	kids, p := m.nodes, tftypes.NewAttributePath()
	var n *node
	for i, name := range strings.Split(dotted, ".") {
		if i > 0 && n.Shape != fields.Object {
			return nil, nil, false
		}
		n = nil
		for _, k := range kids {
			if k.Name == name {
				n = k
			}
		}
		if n == nil {
			return nil, nil, false
		}
		p = p.WithAttributeName(n.AttrName())
		kids = n.Kids
	}
	return n, p, true
}

type compiler struct {
	decl         Declaration
	once         pathSet
	defaulted    pathSet
	unDefaulted  pathSet
	sensitive    pathSet
	preserve     pathSet
	keepOmitted  pathSet
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
		{"keep_omitted", c.keepOmitted},
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
		hashForm := f.HashForm()
		path := prefix + f.Name
		if hashForm {
			path = prefix + f.AttrName()
		}
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
		n := &node{Field: f, Path: path, IsHashForm: hashForm}
		if v, ok := c.decl.NullAs[path]; ok {
			if f.Shape != fields.String {
				return nil, fmt.Errorf("null_as entry `%s` is not a string field", path)
			}
			n.NullAs = v
			c.used["null_as/"+path] = true
		}

		once := c.mark("once_returned", c.once, path)
		n.Sensitive = c.mark("sensitive", c.sensitive, path) || once || f.Sensitive
		n.Preserve = c.mark("preserve_config", c.preserve, path)
		n.KeepOmitted = c.mark("keep_omitted", c.keepOmitted, path)
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
		if n.KeepOmitted && (!f.Response || inList) {
			return nil, fmt.Errorf("keep_omitted entry `%s` is not a field outside a list that the response returns", path)
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
		// when the configuration sets a value that differs from the state's. Below a list that
		// forces replacement the element is the state's own until the list changes, which
		// replaces the object, so the value keeps its prior one as any child of such a field does.
		serverFilledInList := inList && n.Computed && f.CreateOnly() && !replaceOn && !parentReplace
		n.ReplaceIfSet = serverFilledInList && !nestedShape(f.Shape)
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
	for _, hash := range out {
		if !hash.IsHashForm {
			continue
		}
		for _, token := range out {
			if token.IsHashForm || token.Name != hash.Name {
				continue
			}
			hash.TokenForm, token.DataForm = token.AttrName(), hash.AttrName()
			// A required parameter takes either form, so neither attribute is Required.
			if token.Required || hash.Required {
				token.Required, hash.Required = false, false
				token.Optional, hash.Optional = true, true
				token.OneRequired, hash.OneRequired = true, true
			}
			// A token the response returns is filled in when the hash form creates it, and
			// keeps its state value while neither form changes.
			if token.Response && !token.computedOnly() {
				token.Computed, token.UseState = true, true
			}
		}
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
