package engine

import (
	"context"
	"fmt"
	"slices"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-framework/types/basetypes"
	"github.com/hashicorp/terraform-plugin-go/tftypes"

	"github.com/computomatic/terraform-provider-stripe/internal/fields"
	"github.com/computomatic/terraform-provider-stripe/internal/wire"
)

// encodeCreate builds the create request body from the plan: every configured field
// Stripe takes on create. Unknown and null values are left out.
//
// A `/v2` body leaves out an empty list, set, map or object unless Stripe requires the
// field: JSON has no unset marker the conformance suite has proven, and an Optional
// collection configured empty means the same as one left out.
func (m *model) encodeCreate(plan basetypes.ObjectValue) (wire.Object, error) {
	if err := m.checkForms(m.nodes, plan.Attributes(), "", false); err != nil {
		return nil, err
	}
	body, err := encodeObject(m.nodes, plan.Attributes(), func(n *node) bool { return n.Create })
	if err != nil {
		return nil, err
	}
	if err := m.checkTokenForm(body); err != nil {
		return nil, err
	}
	if err := m.refuseEmptyObjects(m.nodes, body, opCreate); err != nil {
		return nil, err
	}
	if m.v2 {
		body = pruneEmpty(m.nodes, body)
	}
	return m.wrapCreate(body), nil
}

// checkTokenForm refuses a create body that holds a CreateWrap's token beside a field of the
// hash, or neither the token nor every field the hash needs, as the plan does for what it knows
// (planCreateWrap): a token configured from another resource is unknown there. It names
// attributes and never their values, which include the sensitive account number.
func (m *model) checkTokenForm(body wire.Object) error {
	w := m.decl.CreateWrap
	if w == nil || w.Token == "" {
		return nil
	}
	attrName := func(name string) string {
		if n, ok := m.top(name); ok {
			return n.AttrName()
		}
		return name
	}
	var names []string
	if _, token := body.Get(w.Token); token {
		for _, f := range body {
			if f.Name != w.Token && !slices.Contains(w.Outside, f.Name) {
				names = append(names, "`"+attrName(f.Name)+"`")
			}
		}
		if len(names) > 0 {
			return fmt.Errorf("%s cannot be sent beside `%s` on a create: Stripe takes the token or the hash, not both", strings.Join(names, ", "), attrName(w.Token))
		}
		return nil
	}
	for _, name := range w.Requires {
		if _, set := body.Get(name); !set {
			names = append(names, "`"+attrName(name)+"`")
		}
	}
	if len(names) > 0 {
		return fmt.Errorf("%s required unless `%s` is set", strings.Join(names, ", "), attrName(w.Token))
	}
	return nil
}

// wrapCreate moves the create fields that belong in the declaration's hash under its key.
func (m *model) wrapCreate(body wire.Object) wire.Object {
	wrap := m.decl.CreateWrap
	if wrap == nil {
		return body
	}
	if _, token := body.Get(wrap.Token); token {
		return body
	}
	var outer wire.Object
	inner := append(wire.Object(nil), wrap.Fixed...)
	for _, f := range body {
		if slices.Contains(wrap.Outside, f.Name) {
			outer = append(outer, f)
		} else {
			inner = append(inner, f)
		}
	}
	return append(outer, wire.Field{Name: wrap.Key, Value: inner})
}

// pruneEmpty drops, at every depth, the empty arrays and objects of fields that are not
// required.
func pruneEmpty(kids []*node, obj wire.Object) wire.Object {
	var out wire.Object
	for _, f := range obj {
		k := findNodeFor(kids, f)
		if k == nil {
			out = append(out, f)
			continue
		}
		v := f.Value
		switch t := v.(type) {
		case wire.Object:
			if k.Shape == fields.ObjectMap {
				pruned := make(wire.Object, len(t))
				for i, entry := range t {
					inner, _ := entry.Value.(wire.Object)
					pruned[i] = wire.Field{Name: entry.Name, Value: pruneEmpty(k.Kids, inner)}
				}
				v = pruned
			} else if k.Shape == fields.Object {
				v = pruneEmpty(k.Kids, t)
			}
		case wire.Array:
			if k.Shape == fields.ObjectList {
				pruned := make(wire.Array, len(t))
				for i, item := range t {
					inner, _ := item.(wire.Object)
					pruned[i] = pruneEmpty(k.Kids, inner)
				}
				v = pruned
			}
		}
		// A hash with no members is sent only as configured, `{}`, which selects its option.
		if emptyWire(v) && !k.Required && !(k.Shape == fields.Object && len(k.Kids) == 0) {
			continue
		}
		out = append(out, wire.Field{Name: f.Name, Value: v})
	}
	return out
}

// findNodeFor returns the node a wire field was encoded from: by name, and for the two forms
// of a token-or-hash parameter, which share it, by whether the value is a hash.
func findNodeFor(kids []*node, f wire.Field) *node {
	_, hash := f.Value.(wire.Object)
	var found *node
	for _, k := range kids {
		if k.Name == f.Name {
			if found == nil || hash == (k.Shape == fields.Object) {
				found = k
			}
		}
	}
	return found
}

// checkForms refuses, at any depth, both forms of a token-or-hash parameter set, which
// plan-time validation cannot catch while the token is unknown. Read from a configuration
// (configured), it also refuses neither form of one the create requires, set: a token
// configured unknown at plan can be null at apply. A plan cannot show that, because an unset
// token, which is Computed, is unknown in it, never null.
func (m *model) checkForms(kids []*node, attrs map[string]attr.Value, prefix string, configured bool) error {
	set := func(v attr.Value) bool { return v != nil && !v.IsNull() && !v.IsUnknown() }
	null := func(v attr.Value) bool { return v == nil || v.IsNull() }
	for _, n := range kids {
		v := attrs[n.AttrName()]
		at := prefix + n.AttrName()
		if n.IsHashForm && n.TokenForm != "" {
			token := attrs[n.TokenForm]
			switch {
			case set(v) && set(token):
				return fmt.Errorf("both `%s%s` and `%s` are set: Stripe takes the token or the hash under one name, `%s`", prefix, n.TokenForm, at, n.Name)
			case configured && n.OneRequired && null(v) && null(token):
				return fmt.Errorf("neither `%s%s` nor `%s` is set: Stripe requires one", prefix, n.TokenForm, at)
			}
		}
		if !set(v) || len(n.Kids) == 0 {
			continue
		}
		var err error
		switch c := v.(type) {
		case basetypes.ObjectValue:
			err = m.checkForms(n.Kids, c.Attributes(), at+".", configured)
		case basetypes.ListValue:
			for i, e := range c.Elements() {
				if o, ok := e.(basetypes.ObjectValue); ok && err == nil {
					err = m.checkForms(n.Kids, o.Attributes(), fmt.Sprintf("%s[%d].", at, i), configured)
				}
			}
		case basetypes.MapValue:
			for _, k := range sortedKeys(c.Elements()) {
				if o, ok := c.Elements()[k].(basetypes.ObjectValue); ok && err == nil {
					err = m.checkForms(n.Kids, o.Attributes(), fmt.Sprintf("%s[%q].", at, k), configured)
				}
			}
		}
		if err != nil {
			return err
		}
	}
	return nil
}

// checkUpdateForms runs checkForms on an update's configuration, which alone says which form of
// a token-or-hash parameter is set: the plan carries the token Stripe returned for a hash. An
// update with no configuration, which only the engine's own follow-ups build, is checked on
// its plan.
func (m *model) checkUpdateForms(plan basetypes.ObjectValue, config tftypes.Value) error {
	if config.Type() == nil {
		return m.checkForms(m.nodes, plan.Attributes(), "", false)
	}
	v, err := plan.Type(context.Background()).ValueFromTerraform(context.Background(), config)
	if err != nil {
		return err
	}
	o, ok := v.(basetypes.ObjectValue)
	if !ok {
		return fmt.Errorf("the configuration is %T, not an object", v)
	}
	return m.checkForms(m.nodes, o.Attributes(), "", true)
}

// hashSent reports whether the hash form named data, among kids, is set and sent.
func hashSent(kids []*node, data string, attrs map[string]attr.Value, allow func(*node) bool) bool {
	for _, k := range kids {
		if k.IsHashForm && k.AttrName() == data {
			v := attrs[data]
			return allow(k) && v != nil && !v.IsNull() && !v.IsUnknown()
		}
	}
	return false
}

// otherFormSet reports whether the plan sets the other form of a token-or-hash parameter.
func (m *model) otherFormSet(n *node, plan map[string]attr.Value) bool {
	for _, other := range m.nodes {
		if other != n && other.Name == n.Name {
			if v := plan[other.AttrName()]; v != nil && !v.IsNull() {
				return true
			}
		}
	}
	return false
}

func findNode(kids []*node, name string) *node {
	for _, k := range kids {
		if k.Name == name {
			return k
		}
	}
	return nil
}

func emptyWire(v wire.Value) bool {
	switch t := v.(type) {
	case wire.Array:
		return len(t) == 0
	case wire.Object:
		return len(t) == 0
	}
	return false
}

// encodeFollowUp builds the update that sets fields Stripe takes only on update and never
// on create, such as a product's `default_price`.
func (m *model) encodeFollowUp(plan basetypes.ObjectValue) (wire.Object, error) {
	return encodeObject(m.nodes, plan.Attributes(), func(n *node) bool { return n.Update && !n.Create })
}

// encodeUpdate builds the update request body: the updatable fields whose planned value
// differs from state. A removed field is sent as null, which both codecs render as
// Stripe's way of unsetting it, except a SendWithUpdate field, which is never sent as null.
// Create-only fields are never sent, even unchanged, nor is a RefuseChange field, top-level or
// nested, whatever else in its object changes.
// Metadata sends only the keys that changed (ADR D2). A field whose plan differs from the
// state only in computed values the configuration leaves unset is unchanged: Terraform marks
// those unknown on every update, and sending the field again would, for a list of
// subscription items, ask Stripe to change them. config is the configuration; the zero
// value turns that check off.
func (m *model) encodeUpdate(plan, state basetypes.ObjectValue, config tftypes.Value) (wire.Object, error) {
	out, err := m.encodeChanges(plan, state, config)
	if err != nil {
		return nil, err
	}
	if err := m.refuseEmptyObjects(m.nodes, out, opUpdate); err != nil {
		return nil, err
	}
	return out, nil
}

// encodeChanges builds encodeUpdate's body.
func (m *model) encodeChanges(plan, state basetypes.ObjectValue, config tftypes.Value) (wire.Object, error) {
	var out wire.Object
	planAttrs, stateAttrs := plan.Attributes(), state.Attributes()
	if err := m.checkUpdateForms(plan, config); err != nil {
		return nil, err
	}
	updatable := func(k *node) bool { return k.Update && !slices.Contains(m.decl.RefuseChange, k.Path) }
	for _, n := range m.nodes {
		if !updatable(n) || slices.Contains(m.decl.SendWithUpdate, n.Name) {
			continue
		}
		pv, sv := planAttrs[n.AttrName()], stateAttrs[n.AttrName()]
		if pv == nil || pv.IsUnknown() || pv.Equal(sv) {
			continue
		}
		if _, keyed := m.decl.MatchBy[n.Name]; keyed && !pv.IsNull() {
			encode := m.encodeKeyedList
			if slices.Contains(m.decl.FixedKeys, n.Name) {
				encode = m.encodeFixedList
			}
			v, ok, err := encode(n, pv, sv, config)
			if err != nil {
				return nil, err
			}
			if ok {
				out = append(out, wire.Field{Name: n.Name, Value: v})
			}
			continue
		}
		if unchangedBesidesComputed(n, pv, sv, config) {
			continue
		}
		// On /v2 an empty collection and no value mean the same, and neither is proven as
		// something to send, so a change between the two sends nothing.
		if m.v2 && n.Name != "metadata" && (emptyCollection(pv) && (sv == nil || sv.IsNull()) || pv.IsNull() && sv != nil && emptyCollection(sv)) {
			continue
		}
		switch {
		case n.Name == "metadata" && n.Shape == fields.StringMap:
			diff, err := metadataDiff(pv, sv)
			if err != nil {
				return nil, err
			}
			if len(diff) > 0 {
				out = append(out, wire.Field{Name: n.Name, Value: diff})
			}
		case pv.IsNull() && m.v2 && !slices.Contains(m.decl.ClearsWithNull, n.Name):
			return nil, fmt.Errorf("clearing `%s` is not supported on this resource: Stripe's /v2 behaviour for null is not yet verified for it", n.Path)
		case pv.IsNull() && m.otherFormSet(n, planAttrs):
			// The other form of a token-or-hash parameter takes its place under the same name.
		case pv.IsNull():
			out = append(out, wire.Field{Name: n.Name, Value: wire.Null{}})
		case slices.Contains(m.decl.SendChangedMembers, n.Name):
			v, err := m.changedMembers(n, pv, sv, updatable)
			if err != nil {
				return nil, err
			}
			if len(v) > 0 {
				out = append(out, wire.Field{Name: n.Name, Value: v})
			}
		case n.Name == m.decl.CurrencyOptions:
			v, ok, err := encodeValue(n, addedKeys(pv, sv), updatable)
			if err != nil {
				return nil, err
			}
			if ok {
				out = append(out, wire.Field{Name: n.Name, Value: v})
			}
		default:
			v, ok, err := encodeValue(n, withoutUnchangedDefaults(n, pv, sv), updatable)
			if err != nil {
				return nil, err
			}
			if ok {
				v = m.carryOnUpdate(n, sv, v)
				out = append(out, wire.Field{Name: n.Name, Value: v})
			}
		}
	}
	if !slices.ContainsFunc(m.decl.SendWithUpdateOn, func(name string) bool { _, sent := out.Get(name); return sent }) {
		return out, nil
	}
	for _, n := range m.nodes {
		if !updatable(n) || !slices.Contains(m.decl.SendWithUpdate, n.Name) {
			continue
		}
		v, ok, err := encodeValue(n, planAttrs[n.AttrName()], updatable)
		if err != nil {
			return nil, err
		}
		if ok {
			out = append(out, wire.Field{Name: n.Name, Value: v})
		}
	}
	return out, nil
}

// unchangedBesidesComputed reports whether pv equals sv once the unknown values that the
// configuration leaves unset take their state values.
func unchangedBesidesComputed(n *node, pv, sv attr.Value, config tftypes.Value) bool {
	return sameBesidesComputed(tftypes.NewAttributePath().WithAttributeName(n.AttrName()), pv, sv, config)
}

// sameBesidesComputed is unchangedBesidesComputed for the value at root in the configuration.
func sameBesidesComputed(root *tftypes.AttributePath, pv, sv attr.Value, config tftypes.Value) bool {
	if sv == nil || sv.IsNull() || sv.IsUnknown() || config.Type() == nil {
		return false
	}
	ctx := context.Background()
	planRaw, err := pv.ToTerraformValue(ctx)
	if err != nil {
		return false
	}
	stateRaw, err := sv.ToTerraformValue(ctx)
	if err != nil {
		return false
	}
	filled, err := tftypes.Transform(planRaw, func(p *tftypes.AttributePath, v tftypes.Value) (tftypes.Value, error) {
		if v.IsKnown() {
			return v, nil
		}
		full := tftypes.NewAttributePathWithSteps(append(slices.Clone(root.Steps()), p.Steps()...))
		if set, _, err := tftypes.WalkAttributePath(config, full); err == nil {
			if cv, ok := set.(tftypes.Value); !ok || !cv.IsNull() {
				return v, nil
			}
		}
		if prior, _, err := tftypes.WalkAttributePath(stateRaw, p); err == nil {
			if pr, ok := prior.(tftypes.Value); ok {
				return pr, nil
			}
		}
		return v, nil
	})
	return err == nil && filled.Equal(stateRaw)
}

// listElements returns the elements of a list value, or none.
func listElements(v attr.Value) []attr.Value {
	if l, ok := v.(basetypes.ListValue); ok && known(l) {
		return l.Elements()
	}
	return nil
}

// keyedElement is an element of a keyed list in the plan, with the state element it
// continues.
type keyedElement struct {
	plan  basetypes.ObjectValue
	state *basetypes.ObjectValue
	// own is true when the element has a key of its own to match by.
	own bool
}

// matchKeyed pairs the plan's elements with the state's by key, in the plan's order, and
// returns the state elements nothing claimed. An element whose key is not yet known and that
// the configuration leaves unset (it sets `price_data`) pairs with a state element only when
// the two are equal; if its data changed, Stripe cannot change the item in place, so the old
// one is removed and the new one added. config is the configuration of the whole object.
func (m *model) matchKeyed(n *node, plan, state []attr.Value, config tftypes.Value) (paired []keyedElement, removed []basetypes.ObjectValue) {
	key := m.decl.MatchBy[n.Name]
	used := make([]bool, len(state))
	stateObj := func(i int) *basetypes.ObjectValue {
		o, ok := state[i].(basetypes.ObjectValue)
		if !ok || o.IsNull() || o.IsUnknown() {
			return nil
		}
		return &o
	}
	paired = make([]keyedElement, len(plan))
	for i, pe := range plan {
		po, _ := pe.(basetypes.ObjectValue)
		paired[i].plan = po
		pk, ok := attrKey(po, key)
		if !ok {
			continue
		}
		paired[i].own = true
		for j := range state {
			if sk, has := attrKey(state[j], key); has && sk == pk && !used[j] {
				used[j] = true
				paired[i].state = stateObj(j)
				break
			}
		}
	}
	data := m.decl.MatchData[n.Name]
	for i := range plan {
		if paired[i].own || configSets(config, n.AttrName(), i, key) {
			continue
		}
		root := tftypes.NewAttributePath().WithAttributeName(n.AttrName()).WithElementKeyInt(i)
		want := paired[i].plan.Attributes()[data]
		for j := range state {
			o := stateObj(j)
			if o == nil || used[j] {
				continue
			}
			// Elements built from the same data are the same item, whatever else changed.
			same := sameBesidesComputed(root, paired[i].plan, *o, config)
			if data != "" {
				same = want != nil && !want.IsNull() && !want.IsUnknown() && want.Equal(o.Attributes()[data])
			}
			if same {
				used[j] = true
				paired[i].state = o
				break
			}
		}
	}
	for j := range state {
		if o := stateObj(j); !used[j] && o != nil {
			removed = append(removed, *o)
		}
	}
	return paired, removed
}

// keyedHints says, for each MatchBy list, which state element each plan element continues
// (its ID, or "" for a new one), and whether the update adds an item with no key of its own,
// whose ID the update then has to learn by reading the list before and after.
func (m *model) keyedHints(plan, state basetypes.ObjectValue, config tftypes.Value) (hints map[string][]string, addsKeyless bool) {
	hints = map[string][]string{}
	for list, key := range m.decl.MatchBy {
		n, ok := m.top(list)
		if !ok {
			continue
		}
		paired, _ := m.matchKeyed(n, listElements(plan.Attributes()[n.AttrName()]), listElements(state.Attributes()[n.AttrName()]), config)
		ids := make([]string, len(paired))
		for i, e := range paired {
			if e.state != nil {
				ids[i], _ = attrKey(*e.state, "id")
			}
			if e.state == nil && !e.own && !configSets(config, n.AttrName(), i, key) {
				addsKeyless = true
			}
		}
		hints[list] = ids
	}
	return hints, addsKeyless
}

// configSets reports whether the configuration sets list[i].field.
func configSets(config tftypes.Value, list string, i int, field string) bool {
	if config.Type() == nil {
		return false
	}
	p := tftypes.NewAttributePath().WithAttributeName(list).WithElementKeyInt(i).WithAttributeName(field)
	set, _, err := tftypes.WalkAttributePath(config, p)
	if err != nil {
		return false
	}
	cv, ok := set.(tftypes.Value)
	return ok && !cv.IsNull()
}

// encodeKeyedList builds the update of a MatchBy list: the elements that changed, with the
// `id` of the state element each continues, the elements the plan adds, and `{id, deleted:
// true}` for the state elements the plan dropped. A list that only reordered sends nothing.
func (m *model) encodeKeyedList(n *node, pv, sv attr.Value, config tftypes.Value) (wire.Value, bool, error) {
	paired, removed := m.matchKeyed(n, listElements(pv), listElements(sv), config)
	var out wire.Array
	for i, e := range paired {
		if e.state != nil && sameBesidesComputed(tftypes.NewAttributePath().WithAttributeName(n.AttrName()).WithElementKeyInt(i), e.plan, *e.state, config) {
			continue
		}
		obj, err := encodeObject(n.Kids, e.plan.Attributes(), func(k *node) bool { return k.Update })
		if err != nil {
			return nil, false, err
		}
		if data := m.decl.MatchData[n.Name]; data != "" && e.state != nil && !e.own {
			// The data is unchanged: sending it again would make Stripe price the item anew.
			obj = slices.DeleteFunc(obj, func(f wire.Field) bool { return f.Name == data })
		}
		if e.state != nil {
			if _, sent := obj.Get("id"); !sent {
				if id, ok := attrKey(*e.state, "id"); ok {
					obj = append(obj, wire.Field{Name: "id", Value: wire.String(id)})
				}
			}
		}
		out = append(out, obj)
	}
	for _, r := range removed {
		if id, ok := attrKey(r, "id"); ok {
			out = append(out, wire.Object{{Name: "id", Value: wire.String(id)}, {Name: "deleted", Value: wire.Bool(true)}})
		}
	}
	return out, len(out) > 0, nil
}

// carryOnUpdate sends the state's value of each Carry field of an updated list whose plan
// left it unknown, for the element at the same position. v is the encoded list.
func (m *model) carryOnUpdate(n *node, state attr.Value, v wire.Value) wire.Value {
	arr, isArr := v.(wire.Array)
	old := listElements(state)
	if n.Shape != fields.ObjectList || !isArr || len(old) == 0 {
		return v
	}
	// A phase that began within a minute counts as current, so a clock a little behind
	// Stripe's does not lose its start.
	now := time.Now().Unix() + 60
	out := make(wire.Array, len(arr))
	for i, item := range arr {
		obj, _ := item.(wire.Object)
		out[i] = obj
		if i >= len(old) {
			continue
		}
		before, isObj := old[i].(basetypes.ObjectValue)
		if !isObj || before.IsNull() {
			continue
		}
		for _, c := range m.decl.Carry {
			if c.List != n.Name {
				continue
			}
			if _, sent := obj.Get(c.Field); sent {
				continue
			}
			s, isStr := before.Attributes()[c.Field].(basetypes.StringValue)
			if !isStr || !known(s) {
				continue
			}
			if c.WhenPast {
				at, err := strconv.ParseInt(s.ValueString(), 10, 64)
				if err != nil || at > now {
					continue
				}
			}
			obj = append(obj, wire.Field{Name: c.Field, Value: wire.String(s.ValueString())})
		}
		out[i] = obj
	}
	return out
}

// changedMembers is what an update sends of an object in SendChangedMembers: the members
// whose planned value differs from the state's, those of a nested object in SendChangedMembers
// likewise, and the whole of any other that has one. A member the plan leaves unknown is Stripe's
// to fill in, so it is not sent.
func (m *model) changedMembers(n *node, plan, state attr.Value, allow func(*node) bool) (wire.Object, error) {
	planned, held := objectAttrs(plan), objectAttrs(state)
	var out wire.Object
	for _, k := range n.Kids {
		pv, sv := planned[k.AttrName()], held[k.AttrName()]
		if !allow(k) || pv == nil || pv.IsUnknown() || pv.Equal(sv) {
			continue
		}
		var v wire.Value
		switch {
		case pv.IsNull():
			if sv == nil || emptyCollection(sv) {
				continue
			}
			v = wire.Null{}
		case slices.Contains(m.decl.SendWholeMembers, k.Path):
			members, err := wholeMembers(k, pv, sv)
			if err != nil {
				return nil, err
			}
			v = members
		case k.Shape == fields.Object && slices.Contains(m.decl.SendChangedMembers, k.Path):
			members, err := m.changedMembers(k, pv, sv, allow)
			if err != nil {
				return nil, err
			}
			if len(members) == 0 {
				continue
			}
			v = members
		case k.Shape == fields.StringMap:
			diff, err := metadataDiff(pv, sv)
			if err != nil {
				return nil, err
			}
			if len(diff) == 0 {
				continue
			}
			v = diff
		default:
			var ok bool
			var err error
			if v, ok, err = encodeValue(k, pv, allow); err != nil {
				return nil, err
			} else if !ok {
				continue
			}
		}
		out = append(out, wire.Field{Name: k.Name, Value: v})
	}
	return out, nil
}

// wholeMembers is what an update sends of an object in SendWholeMembers: every member an update
// takes, and null for one removed from the configuration, which an update of a hash whose update
// nothing proves must name. Stripe may clear what the request leaves out, so a member the plan
// leaves unknown goes with the state's value, and one RefuseChange refuses, which the plan cannot
// change, goes with the value it holds.
func wholeMembers(n *node, plan, state attr.Value) (wire.Object, error) {
	planned, held := objectAttrs(plan), objectAttrs(state)
	var out wire.Object
	for _, k := range n.Kids {
		if !k.Update {
			continue
		}
		pv, sv := planned[k.AttrName()], held[k.AttrName()]
		if pv == nil || pv.IsUnknown() || k.Shape != fields.Object && !fullyKnown(pv) {
			pv = sv
		}
		var v wire.Value
		switch {
		case pv == nil:
			continue
		case pv.IsNull():
			if sv == nil || sv.IsNull() {
				continue
			}
			v = wire.Null{}
		case k.Shape == fields.Object:
			members, err := wholeMembers(k, pv, sv)
			if err != nil {
				return nil, err
			}
			if len(members) == 0 {
				continue
			}
			v = members
		default:
			var ok bool
			var err error
			if v, ok, err = encodeValue(k, pv, func(m *node) bool { return m.Update }); err != nil {
				return nil, err
			} else if !ok {
				continue
			}
		}
		out = append(out, wire.Field{Name: k.Name, Value: v})
	}
	return out, nil
}

func hasRequiredMember(n *node) bool {
	return slices.ContainsFunc(n.Kids, func(k *node) bool { return k.Required })
}

// withoutUnchangedDefaults drops, from an object that changed, the nested objects Stripe
// filled in and the update leaves as they are, so the request does not send Stripe its own
// values back.
func withoutUnchangedDefaults(n *node, plan, state attr.Value) attr.Value {
	p, isObj := plan.(basetypes.ObjectValue)
	s, hasState := state.(basetypes.ObjectValue)
	if n.Shape != fields.Object || !isObj || !hasState || p.IsNull() || s.IsNull() {
		return plan
	}
	attrs := make(map[string]attr.Value, len(p.Attributes()))
	for _, k := range n.Kids {
		name := k.AttrName()
		pv, sv := p.Attributes()[name], s.Attributes()[name]
		switch {
		case pv == nil:
		case k.Computed && k.Shape == fields.Object && pv.Equal(sv):
			attrs[name] = nullOf(attrType(k))
		default:
			attrs[name] = withoutUnchangedDefaults(k, pv, sv)
		}
	}
	out, diags := types.ObjectValue(p.AttributeTypes(context.Background()), attrs)
	if diags.HasError() {
		return plan
	}
	return out
}

func metadataDiff(plan, state attr.Value) (wire.Object, error) {
	want, err := stringMap(plan)
	if err != nil {
		return nil, err
	}
	have, err := stringMap(state)
	if err != nil {
		return nil, err
	}
	keys := make([]string, 0, len(want)+len(have))
	for k := range want {
		keys = append(keys, k)
	}
	for k := range have {
		if _, kept := want[k]; !kept {
			keys = append(keys, k)
		}
	}
	sort.Strings(keys)
	var diff wire.Object
	for _, k := range keys {
		w, wanted := want[k]
		h, had := have[k]
		switch {
		case !wanted:
			diff = append(diff, wire.Field{Name: k, Value: wire.Null{}})
		case !had || w != h:
			diff = append(diff, wire.Field{Name: k, Value: wire.String(w)})
		}
	}
	return diff, nil
}

func stringMap(v attr.Value) (map[string]string, error) {
	out := map[string]string{}
	if v == nil || v.IsNull() || v.IsUnknown() {
		return out, nil
	}
	mv, ok := v.(basetypes.MapValue)
	if !ok {
		return nil, fmt.Errorf("metadata is a %T, not a map", v)
	}
	for k, e := range mv.Elements() {
		s, ok := e.(basetypes.StringValue)
		if !ok {
			return nil, fmt.Errorf("metadata value for %q is a %T, not a string", k, e)
		}
		out[k] = s.ValueString()
	}
	return out, nil
}

func encodeObject(kids []*node, attrs map[string]attr.Value, allow func(*node) bool) (wire.Object, error) {
	var out wire.Object
	for _, k := range kids {
		if !allow(k) {
			continue
		}
		// A token is known beside its set hash form only because the plan kept it from the
		// state (checkForms refuses a configuration that sets both), so only the hash, which the
		// configuration sets, is sent.
		if k.DataForm != "" && hashSent(kids, k.DataForm, attrs, allow) {
			continue
		}
		v, ok, err := encodeValue(k, attrs[k.AttrName()], allow)
		if err != nil {
			return nil, err
		}
		if ok {
			out = append(out, wire.Field{Name: k.Name, Value: v})
		}
	}
	return out, nil
}

// encodeValue renders one attribute value. ok is false for a null or unknown value.
func encodeValue(n *node, v attr.Value, allow func(*node) bool) (value wire.Value, ok bool, err error) {
	if v == nil || v.IsNull() || v.IsUnknown() {
		return nil, false, nil
	}
	mismatch := func() (wire.Value, bool, error) {
		return nil, false, fmt.Errorf("%s: unexpected value type %T", n.Path, v)
	}
	switch n.Shape {
	case fields.Object:
		o, isObj := v.(basetypes.ObjectValue)
		if !isObj {
			return mismatch()
		}
		obj, err := encodeObject(n.Kids, o.Attributes(), allow)
		return obj, err == nil, err
	case fields.ObjectList:
		l, isList := v.(basetypes.ListValue)
		if !isList {
			return mismatch()
		}
		arr := wire.Array{}
		for _, e := range l.Elements() {
			o, isObj := e.(basetypes.ObjectValue)
			if !isObj {
				return mismatch()
			}
			obj, err := encodeObject(n.Kids, o.Attributes(), allow)
			if err != nil {
				return nil, false, err
			}
			arr = append(arr, obj)
		}
		return arr, true, nil
	case fields.ObjectMap:
		mv, isMap := v.(basetypes.MapValue)
		if !isMap {
			return mismatch()
		}
		obj := wire.Object{}
		for _, key := range sortedKeys(mv.Elements()) {
			o, isObj := mv.Elements()[key].(basetypes.ObjectValue)
			if !isObj {
				return mismatch()
			}
			inner, err := encodeObject(n.Kids, o.Attributes(), allow)
			if err != nil {
				return nil, false, err
			}
			obj = append(obj, wire.Field{Name: key, Value: inner})
		}
		return obj, true, nil
	case fields.StringMap:
		mv, isMap := v.(basetypes.MapValue)
		if !isMap {
			return mismatch()
		}
		obj := wire.Object{}
		for _, key := range sortedKeys(mv.Elements()) {
			s, err := encodeScalar(fields.String, mv.Elements()[key])
			if err != nil {
				return nil, false, fmt.Errorf("%s: %w", n.Path, err)
			}
			obj = append(obj, wire.Field{Name: key, Value: s})
		}
		return obj, true, nil
	case fields.IntMap:
		mv, isMap := v.(basetypes.MapValue)
		if !isMap {
			return mismatch()
		}
		obj := wire.Object{}
		for _, key := range sortedKeys(mv.Elements()) {
			i, err := encodeScalar(fields.Int, mv.Elements()[key])
			if err != nil {
				return nil, false, fmt.Errorf("%s: %w", n.Path, err)
			}
			obj = append(obj, wire.Field{Name: key, Value: i})
		}
		return obj, true, nil
	case fields.ScalarList, fields.ScalarSet:
		var elems []attr.Value
		switch c := v.(type) {
		case basetypes.ListValue:
			elems = c.Elements()
		case basetypes.SetValue:
			elems = c.Elements()
		default:
			return mismatch()
		}
		arr := wire.Array{}
		for _, e := range elems {
			s, err := encodeScalar(n.Elem, e)
			if err != nil {
				return nil, false, fmt.Errorf("%s: %w", n.Path, err)
			}
			arr = append(arr, s)
		}
		return arr, true, nil
	default:
		s, err := encodeScalar(n.Shape, v)
		if err != nil {
			return nil, false, fmt.Errorf("%s: %w", n.Path, err)
		}
		return s, true, nil
	}
}

func sortedKeys(m map[string]attr.Value) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

func encodeScalar(shape fields.Shape, v attr.Value) (wire.Value, error) {
	switch shape {
	case fields.Bool:
		if b, ok := v.(basetypes.BoolValue); ok {
			return wire.Bool(b.ValueBool()), nil
		}
	case fields.Int:
		if i, ok := v.(basetypes.Int64Value); ok {
			return wire.Int(i.ValueInt64()), nil
		}
	case fields.Float:
		if f, ok := v.(basetypes.Float64Value); ok {
			return wire.Decimal(strconv.FormatFloat(f.ValueFloat64(), 'f', -1, 64)), nil
		}
	default:
		if s, ok := v.(basetypes.StringValue); ok {
			return wire.String(s.ValueString()), nil
		}
	}
	return nil, fmt.Errorf("unexpected value type %T", v)
}
