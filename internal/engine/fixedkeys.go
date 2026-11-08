package engine

import (
	"context"
	"maps"
	"slices"

	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/types/basetypes"
	"github.com/hashicorp/terraform-plugin-go/tftypes"

	"github.com/computomatic/terraform-provider-stripe/internal/fields"
	"github.com/computomatic/terraform-provider-stripe/internal/wire"
)

// requiresReplaceForFixedKeys flags a plan that changes what Stripe cannot change in a
// FixedKeys list, and reports whether it did.
func (r *Resource) requiresReplaceForFixedKeys(ctx context.Context, req resource.ModifyPlanRequest, resp *resource.ModifyPlanResponse) bool {
	if len(r.m.decl.FixedKeys) == 0 {
		return false
	}
	var plan, state basetypes.ObjectValue
	diags := req.Plan.Get(ctx, &plan)
	diags.Append(req.State.Get(ctx, &state)...)
	if diags.HasError() {
		resp.Diagnostics.Append(diags...)
		return false
	}
	replace := false
	for _, list := range r.m.decl.FixedKeys {
		n, ok := r.m.top(list)
		if ok && r.m.fixedKeysChanged(n, plan.Attributes()[n.AttrName()], state.Attributes()[n.AttrName()], req.Config.Raw) {
			resp.RequiresReplace = append(resp.RequiresReplace, path.Root(n.AttrName()))
			replace = true
		}
	}
	return replace
}

// requiresReplaceForKeptInKeyed flags a plan that sets or changes, in an element of a MatchBy
// list, a create-only member the response never returns, such as a subscription item's
// `discounts`, and reports whether it did: an update cannot send it and a read cannot show it,
// so only a new object takes it. Elements pair by key, as the update pairs them, and a new
// element that sets one replaces too. A member the configuration leaves unset changes nothing,
// and the list's MatchData member, which pairs the elements, is the update's own to send.
//
// Where the response lists such a member in its own form (listedKept), a read leaves the state
// empty for an element Stripe lists none for, so that setting it replaces, and null for one
// Stripe lists some for, whose forms the provider cannot read, as after an import: there the
// configured value is adopted, with no replacement and nothing sent, and the state then holds it.
// A configured value not known at plan time still replaces, as any value that forces replacement
// does, and a null state of any other kept member, which a read cannot tell apart, replaces too.
func (r *Resource) requiresReplaceForKeptInKeyed(ctx context.Context, req resource.ModifyPlanRequest, resp *resource.ModifyPlanResponse) bool {
	if len(r.m.decl.MatchBy) == 0 {
		return false
	}
	var plan, state basetypes.ObjectValue
	diags := req.Plan.Get(ctx, &plan)
	diags.Append(req.State.Get(ctx, &state)...)
	if diags.HasError() {
		resp.Diagnostics.Append(diags...)
		return false
	}
	listed := r.m.listedKept()
	replace := false
	for _, list := range slices.Sorted(maps.Keys(r.m.decl.MatchBy)) {
		n, ok := r.m.top(list)
		if !ok {
			continue
		}
		kept := keptInElements(n, r.m.decl.MatchData[list])
		if len(kept) == 0 {
			continue
		}
		paired, _ := r.m.matchKeyed(n, listElements(plan.Attributes()[n.AttrName()]), listElements(state.Attributes()[n.AttrName()]), req.Config.Raw)
		for i, e := range paired {
			for _, k := range kept {
				if !configSets(req.Config.Raw, n.AttrName(), i, k.AttrName()) {
					continue
				}
				pv := e.plan.Attributes()[k.AttrName()]
				if e.state != nil && pv != nil {
					sv := e.state.Attributes()[k.AttrName()]
					if pv.Equal(sv) || slices.Contains(listed, k) && (sv == nil || sv.IsNull()) && fullyKnown(pv) {
						continue
					}
				}
				resp.RequiresReplace = append(resp.RequiresReplace, path.Root(n.AttrName()).AtListIndex(i).AtName(k.AttrName()))
				replace = true
			}
		}
	}
	return replace
}

// keptInKeyed reports whether a node is a member of a keyed list's elements whose change
// replaces the object (keptInElements).
func (m *model) keptInKeyed(n *node) bool {
	for list := range m.decl.MatchBy {
		if l, ok := m.top(list); ok && slices.Contains(keptInElements(l, m.decl.MatchData[list]), n) {
			return true
		}
	}
	return false
}

// listedKept returns the kept members of keyed lists' elements that the response lists in its
// own form, as a subscription item's `discounts`, which the record proves Stripe returns as
// discount IDs (the data source's form): a read tells an element Stripe lists none for.
func (m *model) listedKept() []*node {
	var out []*node
	for list := range m.decl.MatchBy {
		l, ok := m.top(list)
		if !ok {
			continue
		}
		for _, k := range keptInElements(l, m.decl.MatchData[list]) {
			if k.Shape == fields.ObjectList && k.DataSource != nil && k.DataSource.Response && k.DataSource.Shape == fields.ScalarList {
				out = append(out, k)
			}
		}
	}
	return out
}

// withKeptFromState returns the plan with each listed kept member (listedKept) that the plan
// leaves unknown, on an element the state holds, set to the state's known value, the element
// paired by key. The framework plans such an unset Computed member unknown on any in-place
// update of the list, and a read of the response there would turn the state's `[]` into null
// where Stripe gained a discount outside Terraform, which a later configured value would then be
// adopted over with nothing sent. Only a read with no value in state reads what Stripe lists.
func (m *model) withKeptFromState(plan, state basetypes.ObjectValue, config tftypes.Value) basetypes.ObjectValue {
	listed := m.listedKept()
	if len(listed) == 0 {
		return plan
	}
	attrs := maps.Clone(plan.Attributes())
	for list := range m.decl.MatchBy {
		n, ok := m.top(list)
		pl, isList := attrs[n.AttrName()].(basetypes.ListValue)
		if !ok || !isList || !known(pl) {
			continue
		}
		paired, _ := m.matchKeyed(n, pl.Elements(), listElements(state.Attributes()[n.AttrName()]), config)
		elems := make([]attr.Value, len(paired))
		for i, e := range paired {
			elems[i] = e.plan
			if e.state == nil {
				continue
			}
			ea := maps.Clone(e.plan.Attributes())
			for _, k := range listed {
				if v, held := ea[k.AttrName()]; held && v.IsUnknown() && known(e.state.Attributes()[k.AttrName()]) {
					ea[k.AttrName()] = e.state.Attributes()[k.AttrName()]
				}
			}
			if o, diags := basetypes.NewObjectValue(e.plan.AttributeTypes(context.Background()), ea); !diags.HasError() {
				elems[i] = o
			}
		}
		if l, diags := basetypes.NewListValue(pl.ElementType(context.Background()), elems); !diags.HasError() {
			attrs[n.AttrName()] = l
		}
	}
	out, diags := basetypes.NewObjectValue(plan.AttributeTypes(context.Background()), attrs)
	if diags.HasError() {
		return plan
	}
	return out
}

// keptInElements returns the members of a list's elements that only a create takes and the
// response never returns, but for the list's MatchData member.
func keptInElements(n *node, data string) []*node {
	var out []*node
	for _, k := range n.Kids {
		if k.CreateOnly() && !k.Response && k.Name != data {
			out = append(out, k)
		}
	}
	return out
}

// requiresReplaceOnRemoval flags a plan that removes a ReplaceOnRemoval field the state holds. A
// field below one the plan removes is not walked: its parent's own entry decides. An empty
// collection holds nothing: a read keeps `[]` where the configuration set it or Stripe emptied a
// known list, so its removal removes nothing, and a plan of `[]` over a list that holds values
// asks for the unset Stripe cannot do.
func (r *Resource) requiresReplaceOnRemoval(req resource.ModifyPlanRequest, resp *resource.ModifyPlanResponse) {
	holds := func(raw tftypes.Value, at *tftypes.AttributePath) bool {
		found, _, err := tftypes.WalkAttributePath(raw, at)
		v, ok := found.(tftypes.Value)
		return err == nil && ok && !v.IsNull() && !emptyRaw(v)
	}
	for _, name := range r.m.decl.ReplaceOnRemoval {
		_, at, ok := r.m.at(name)
		if !ok || !holds(req.State.Raw, at) || holds(req.Plan.Raw, at) || !holds(req.Plan.Raw, at.WithoutLastStep()) {
			continue
		}
		attr := path.Empty()
		for _, step := range at.Steps() {
			if name, isName := step.(tftypes.AttributeName); isName {
				attr = attr.AtName(string(name))
			}
		}
		resp.RequiresReplace = append(resp.RequiresReplace, attr)
	}
}

// emptyRaw reports a known list, set or map with no elements.
func emptyRaw(v tftypes.Value) bool {
	if !v.IsKnown() {
		return false
	}
	var list []tftypes.Value
	var byName map[string]tftypes.Value
	return v.As(&list) == nil && len(list) == 0 || v.Type().Is(tftypes.Map{}) && v.As(&byName) == nil && len(byName) == 0
}

// fixedKeysChanged reports whether the plan of a FixedKeys list adds, drops or reorders into
// a different set of keys, or changes a field of an element that Stripe cannot change in
// place. An element with no key of its own (it sets `price_data`) pairs with a state element
// only when the two are equal, so any change to it counts.
func (m *model) fixedKeysChanged(n *node, plan, state attr.Value, config tftypes.Value) bool {
	if plan == nil || !known(plan) || state == nil || !known(state) {
		return false
	}
	paired, removed := m.matchKeyed(n, listElements(plan), listElements(state), config)
	if len(removed) > 0 {
		return true
	}
	key := m.decl.MatchBy[n.Name]
	for _, e := range paired {
		if e.state == nil {
			return true
		}
		for _, k := range n.Kids {
			if !k.Fixed || k.Name == key {
				continue
			}
			pv, sv := e.plan.Attributes()[k.AttrName()], e.state.Attributes()[k.AttrName()]
			if pv != nil && !pv.IsUnknown() && !pv.Equal(sv) {
				return true
			}
		}
	}
	return false
}

// encodeFixedList builds the update of a FixedKeys list: nothing when no element changed,
// else every element restated in full, with its `id`, the fields Stripe changes in place and
// the state's value of each field the plan check guards (`adjustable_quantity`) that Stripe
// takes on update, so a list Stripe reads as a statement does not reset it. Fields only a
// create takes are not sent.
func (m *model) encodeFixedList(n *node, pv, sv attr.Value, config tftypes.Value) (wire.Value, bool, error) {
	paired, _ := m.matchKeyed(n, listElements(pv), listElements(sv), config)
	changed := false
	for i, e := range paired {
		if e.state == nil || !sameBesidesComputed(tftypes.NewAttributePath().WithAttributeName(n.AttrName()).WithElementKeyInt(i), e.plan, *e.state, config) {
			changed = true
		}
	}
	if !changed {
		return nil, false, nil
	}
	out := make(wire.Array, 0, len(paired))
	for _, e := range paired {
		attrs := e.plan.Attributes()
		if e.state != nil {
			attrs = maps.Clone(attrs)
			for _, k := range n.Kids {
				if sv, ok := e.state.Attributes()[k.AttrName()]; ok && k.Fixed && k.Update {
					attrs[k.AttrName()] = sv
				}
			}
		}
		obj, err := encodeObject(n.Kids, attrs, func(k *node) bool { return k.Update })
		if err != nil {
			return nil, false, err
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
	return out, true, nil
}
