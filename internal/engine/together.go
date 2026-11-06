package engine

import (
	"context"
	"fmt"
	"slices"
	"strings"

	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types/basetypes"

	"github.com/computomatic/terraform-provider-stripe/internal/fields"
)

// checkTogether resolves each set of a hash's members that Stripe takes only all at once (the
// table's Together) and words the line of each member. It refuses a set whose member is not an
// argument the hash holds through hashes alone, which a configuration could not set in one
// element, and a hash an update sends only the changed members of, which could send one member of
// a set without the others.
func checkTogether(decl Declaration, nodes []*node) error {
	for _, n := range nodes {
		for _, set := range n.Together {
			if n.Shape != fields.Object && n.Shape != fields.ObjectList {
				return fmt.Errorf("%s: `%s` holds a set Stripe takes only all at once, and is not a hash", decl.TypeName, n.Path)
			}
			if slices.Contains(decl.SendChangedMembers, n.Path) {
				return fmt.Errorf("%s: `%s` holds a set Stripe takes only all at once, which sending only its changed members could split: name it in SendWholeMembers", decl.TypeName, n.Path)
			}
			members := make([]*node, len(set.Members))
			for i, p := range set.Members {
				members[i] = memberAt(n.Kids, p)
				if members[i] == nil || !members[i].Writable() {
					return fmt.Errorf("%s: `%s.%s`, of a set Stripe takes only all at once, is not an argument the hash holds through hashes alone", decl.TypeName, n.Path, p)
				}
			}
			for i, member := range members {
				var others []string
				for j, p := range set.Members {
					if j != i {
						others = append(others, "`"+p+"`")
					}
				}
				member.TogetherDoc = strings.TrimSpace(fmt.Sprintf("Stripe takes it only together with %s, so a configuration sets all of them or none. %s", joinAnd(others), set.Refused))
			}
		}
		if err := checkTogether(decl, n.Kids); err != nil {
			return err
		}
	}
	return nil
}

// memberAt is the node at a dotted path of attribute names below kids, through hashes alone, or
// nil.
func memberAt(kids []*node, path string) *node {
	name, rest, nested := strings.Cut(path, ".")
	for _, k := range kids {
		if k.AttrName() != name {
			continue
		}
		if !nested {
			return k
		}
		if k.Shape != fields.Object {
			return nil
		}
		return memberAt(k.Kids, rest)
	}
	return nil
}

// joinAnd joins words as a list in prose: `a`, `a and b`, `a, b and c`.
func joinAnd(words []string) string {
	if len(words) < 2 {
		return strings.Join(words, "")
	}
	return strings.Join(words[:len(words)-1], ", ") + " and " + words[len(words)-1]
}

// togetherSets refuses, at plan, a hash whose configuration sets some members of a set Stripe
// takes only all at once and not the others. A member not known at plan counts as set, since
// the apply sends it, and a hash between the set's hash and a member that is not known leaves the
// set for the next plan.
type togetherSets struct{ sets []fields.Together }

var _ validator.Object = togetherSets{}

func (togetherSets) Description(context.Context) string {
	return "Refuses a configuration that sets some members of a set Stripe takes only all at once and not the others."
}

func (v togetherSets) MarkdownDescription(ctx context.Context) string { return v.Description(ctx) }

func (v togetherSets) ValidateObject(_ context.Context, req validator.ObjectRequest, resp *validator.ObjectResponse) {
	if req.ConfigValue.IsNull() || req.ConfigValue.IsUnknown() {
		return
	}
	for _, set := range v.sets {
		var given, unset []string
		known := true
		for _, p := range set.Members {
			switch memberIn(req.ConfigValue, p) {
			case memberUnknownAbove:
				known = false
			case memberNull:
				unset = append(unset, "`"+p+"`")
			default:
				given = append(given, "`"+p+"`")
			}
		}
		if known && len(given) > 0 && len(unset) > 0 {
			resp.Diagnostics.AddAttributeError(req.Path, "Incomplete set of arguments",
				fmt.Sprintf("`%s`: Stripe takes %s only all at once, and the configuration sets %s without %s. Set all of them, or none.", req.Path, joinAnd(quoted(set.Members)), joinAnd(given), joinAnd(unset)))
		}
	}
}

func quoted(paths []string) []string {
	out := make([]string, len(paths))
	for i, p := range paths {
		out[i] = "`" + p + "`"
	}
	return out
}

// memberState is what a configuration holds of a member of a set.
type memberState int

const (
	memberNull memberState = iota
	memberSet
	// memberUnknownAbove is a member below a hash the configuration does not know yet.
	memberUnknownAbove
)

// memberIn is what the configured object holds at a dotted path of attribute names through
// hashes.
func memberIn(obj basetypes.ObjectValue, path string) memberState {
	name, rest, nested := strings.Cut(path, ".")
	v, ok := obj.Attributes()[name]
	switch {
	case !ok || v.IsNull():
		return memberNull
	case !nested:
		return memberSet
	case v.IsUnknown():
		return memberUnknownAbove
	}
	inner, isObject := v.(basetypes.ObjectValue)
	if !isObject {
		return memberNull
	}
	return memberIn(inner, rest)
}

// togetherValidators is the refusal of an incomplete set for a hash that holds sets, or nil.
func togetherValidators(n *node) []validator.Object {
	if len(n.Together) == 0 {
		return nil
	}
	return []validator.Object{togetherSets{sets: n.Together}}
}
