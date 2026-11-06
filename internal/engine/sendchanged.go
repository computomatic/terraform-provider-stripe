package engine

import (
	"context"
	"slices"
	"strings"

	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/types/basetypes"
	"github.com/hashicorp/terraform-plugin-go/tftypes"

	"github.com/computomatic/terraform-provider-stripe/internal/fields"
)

// refuseAddingNothing fails an update plan that adds a SendChangedMembers hash, which the state
// holds none of, with no member an update takes set in the configuration, and reports whether it
// did: the update sends only the members that change, so it would send nothing of the hash while
// the plan holds it, and every later plan would add it again, as a v2 account's
// `configuration.recipient = {}` would. Only the innermost such hash is named.
func (r *Resource) refuseAddingNothing(ctx context.Context, req resource.ModifyPlanRequest, resp *resource.ModifyPlanResponse) bool {
	if len(r.m.decl.SendChangedMembers) == 0 {
		return false
	}
	var plan, state, config basetypes.ObjectValue
	diags := req.Plan.Get(ctx, &plan)
	diags.Append(req.State.Get(ctx, &state)...)
	diags.Append(req.Config.Get(ctx, &config)...)
	if resp.Diagnostics.Append(diags...); diags.HasError() {
		return true
	}
	if r.replaces(plan, state, config) {
		return false
	}
	// Each added hash with nothing set qualifies, and the innermost one is named: one that adds
	// `configuration = { recipient = {} }` sets `recipient` in `configuration`, but nothing in
	// `recipient`, whether or not the state holds `configuration`.
	type added struct {
		hash  string
		names []string
		where path.Path
	}
	var empty []added
	for _, hash := range slices.Sorted(slices.Values(r.m.decl.SendChangedMembers)) {
		n, at, ok := r.m.at(hash)
		if !ok {
			continue
		}
		var names []string
		where := path.Empty()
		for _, step := range at.Steps() {
			names = append(names, string(step.(tftypes.AttributeName)))
			where = where.AtName(names[len(names)-1])
		}
		if !isNull(attrAt(ctx, state, names)) || isNull(attrAt(ctx, config, names)) || configuresMember(ctx, n, config, names) {
			continue
		}
		empty = append(empty, added{hash, names, where})
	}
	refused := false
	for _, a := range empty {
		if slices.ContainsFunc(empty, func(b added) bool { return strings.HasPrefix(b.hash, a.hash+".") }) {
			continue
		}
		refused = true
		attrName := strings.Join(a.names, ".")
		resp.Diagnostics.AddAttributeError(a.where, "Cannot add `"+attrName+"` with no member set",
			"An update sends only the members of `"+attrName+"` that change, and this configuration adds it with none set, so Stripe would not hold it. Set one of its members, or remove it from the configuration.")
	}
	return refused
}

// configuresMember reports whether the configuration sets a member an update takes below n, which
// names reach, through hashes; a member not known at plan counts as set, since the apply sends it.
// An empty map Stripe merges key by key does not, because an update sends its changed keys and it
// has none.
func configuresMember(ctx context.Context, n *node, config basetypes.ObjectValue, names []string) bool {
	return slices.ContainsFunc(n.Kids, func(k *node) bool {
		if !k.Update {
			return false
		}
		at := append(slices.Clone(names), k.AttrName())
		if k.Shape == fields.Object {
			return configuresMember(ctx, k, config, at)
		}
		configured := attrAt(ctx, config, at)
		if m, ok := configured.(basetypes.MapValue); ok && k.Shape == fields.StringMap && keyByKey(k, true) && known(m) && len(m.Elements()) == 0 {
			return false
		}
		return configured != nil && !configured.IsNull()
	})
}

// isNull reports a value that holds nothing: absent, null or not known.
func isNull(v attr.Value) bool {
	return v == nil || v.IsNull() || v.IsUnknown()
}
