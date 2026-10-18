package engine

import (
	"context"
	"fmt"
	"slices"
	"sort"
	"strconv"

	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-framework/types/basetypes"

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
	body, err := encodeObject(m.nodes, plan.Attributes(), func(n *node) bool { return n.Create })
	if err != nil || !m.v2 {
		return body, err
	}
	return pruneEmpty(m.nodes, body), nil
}

// pruneEmpty drops, at every depth, the empty arrays and objects of fields that are not
// required.
func pruneEmpty(kids []*node, obj wire.Object) wire.Object {
	var out wire.Object
	for _, f := range obj {
		k := findNode(kids, f.Name)
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
		if emptyWire(v) && !k.Required {
			continue
		}
		out = append(out, wire.Field{Name: f.Name, Value: v})
	}
	return out
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
// Stripe's way of unsetting it. Create-only fields are never sent, even unchanged.
// Metadata sends only the keys that changed (ADR D2).
func (m *model) encodeUpdate(plan, state basetypes.ObjectValue) (wire.Object, error) {
	var out wire.Object
	planAttrs, stateAttrs := plan.Attributes(), state.Attributes()
	for _, n := range m.nodes {
		if !n.Update {
			continue
		}
		pv, sv := planAttrs[n.AttrName()], stateAttrs[n.AttrName()]
		if pv == nil || pv.IsUnknown() || pv.Equal(sv) {
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
		case pv.IsNull():
			out = append(out, wire.Field{Name: n.Name, Value: wire.Null{}})
		case n.Name == m.decl.CurrencyOptions:
			v, ok, err := encodeValue(n, addedKeys(pv, sv), func(k *node) bool { return k.Update })
			if err != nil {
				return nil, err
			}
			if ok {
				out = append(out, wire.Field{Name: n.Name, Value: v})
			}
		default:
			v, ok, err := encodeValue(n, withoutUnchangedDefaults(n, pv, sv), func(k *node) bool { return k.Update })
			if err != nil {
				return nil, err
			}
			if ok {
				out = append(out, wire.Field{Name: n.Name, Value: v})
			}
		}
	}
	return out, nil
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
