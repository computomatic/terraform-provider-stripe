package engine

import (
	"context"
	"maps"

	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/types/basetypes"
	"github.com/hashicorp/terraform-plugin-go/tftypes"

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
