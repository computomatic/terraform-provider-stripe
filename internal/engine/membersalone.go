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

// refuseMembersTogether fails an update plan that would send what no suite probe proves of a
// MembersAlone hash, and reports whether it did. The probes prove Stripe's answer to an update of
// each member alone, and of the sets MembersTogether names, so a plan that changes more than one
// member of the hash in another combination is refused: applying them one at a time sends what
// the probes sent. A plan that adds the hash with no member set is refused too, because the
// update would send nothing of it while the plan holds it.
func (r *Resource) refuseMembersTogether(ctx context.Context, req resource.ModifyPlanRequest, resp *resource.ModifyPlanResponse) bool {
	hashes := topMembersAlone(r.m.nodes)
	if len(hashes) == 0 {
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
	for _, n := range hashes {
		_, at, _ := r.m.at(n.Path)
		var names []string
		where := path.Empty()
		for _, step := range at.Steps() {
			names = append(names, string(step.(tftypes.AttributeName)))
			where = where.AtName(names[len(names)-1])
		}
		held, planned := attrAt(ctx, state, names), attrAt(ctx, plan, names)
		if planned == nil || planned.IsNull() || planned.IsUnknown() {
			continue
		}
		var changed []string
		set := false
		for _, member := range updateMembers(n, n.Path+".") {
			full := append(slices.Clone(names), member.names...)
			configured := attrAt(ctx, config, full)
			set = set || configured != nil && !configured.IsNull()
			if memberChanged(attrAt(ctx, plan, full), attrAt(ctx, state, full), configured) {
				changed = append(changed, member.path)
			}
		}
		attrName := strings.Join(names, ".")
		switch {
		case !set && isNull(held) && !isNull(attrAt(ctx, config, names)):
			resp.Diagnostics.AddAttributeError(where, "Cannot add `"+attrName+"` with no member set",
				"An update sends only the members of `"+attrName+"` that change, and this configuration adds it with none set, so Stripe would not hold it. Set one of its members, or remove it from the configuration.")
		case len(changed) > 1 && !slices.ContainsFunc(n.MembersTogether, func(set []string) bool { return slices.Equal(set, changed) }):
			resp.Diagnostics.AddAttributeError(where, "Cannot change these members of `"+attrName+"` in one apply",
				"This update changes `"+strings.Join(changed, "`, `")+"` of `"+attrName+"`. The conformance suite proves Stripe's answer to an update of each alone, not of them together, so apply them one at a time: change one, apply, then change the next.")
		}
	}
	return resp.Diagnostics.HasError()
}

// topMembersAlone returns the MembersAlone hashes no other one holds.
func topMembersAlone(nodes []*node) []*node {
	var out []*node
	for _, n := range nodes {
		if n.MembersAlone {
			out = append(out, n)
			continue
		}
		out = append(out, topMembersAlone(n.Kids)...)
	}
	return out
}

// member is a member an update takes, below a MembersAlone hash, that is not itself a hash: its
// table path below the hash, the form MembersTogether holds, and the attribute names that reach
// it from the hash.
type member struct {
	path  string
	names []string
}

// updateMembers returns the members an update takes below n, whose path is hash, sorted by path.
func updateMembers(n *node, hash string) []member {
	var out []member
	var walk func(n *node, names []string)
	walk = func(n *node, names []string) {
		for _, k := range n.Kids {
			if !k.Update {
				continue
			}
			at := append(slices.Clone(names), k.AttrName())
			if k.Shape == fields.Object {
				walk(k, at)
				continue
			}
			out = append(out, member{path: strings.TrimPrefix(k.Path, hash), names: at})
		}
	}
	walk(n, nil)
	slices.SortFunc(out, func(a, b member) int { return strings.Compare(a.path, b.path) })
	return out
}

// memberChanged reports whether a plan sends a member: a known value other than the state's, null
// where the state holds one, or a value not known at plan that the configuration sets, which the
// apply then sends. One not known that the configuration leaves unset is Stripe's to fill in.
func memberChanged(planned, held, configured attr.Value) bool {
	if planned != nil && planned.IsUnknown() {
		return configured != nil && !configured.IsNull()
	}
	if isNull(planned) || isNull(held) {
		return isNull(planned) != isNull(held)
	}
	return !planned.Equal(held)
}

// isNull reports a value that holds nothing: absent, null or not known.
func isNull(v attr.Value) bool {
	return v == nil || v.IsNull() || v.IsUnknown()
}
