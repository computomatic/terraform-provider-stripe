package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"net/http"
	"slices"
	"strings"

	"github.com/computomatic/terraform-provider-stripe/internal/fields"
	"github.com/computomatic/terraform-provider-stripe/internal/wire"
)

// genKinds is the closed set of values the suite generates in a request it records (`$gen`),
// each of which stands for any value of its kind, but where a translation refuses it below.
var genKinds = []string{"account_currency", "email", "fresh", "now", "read", "ref", "shared", "unique", "url"}

// meaningfulGenKinds are the generated values that say more than "some value": the account's
// own currency, and a time relative to when the probe sent it (with its `offset`), such as a
// start a day after the create. A refused request holding one is not translated as any value.
var meaningfulGenKinds = []string{"account_currency", "now"}

// recordObject is a JSON object of the record decoded as wire values, which keep each number's
// text, so that a large integer or an exponent reaches a pattern as recorded.
type recordObject wire.Object

// UnmarshalJSON decodes a JSON object, or null as none.
func (o *recordObject) UnmarshalJSON(data []byte) error {
	obj, err := decodeObject(data)
	*o = recordObject(obj)
	return err
}

// decodeObject decodes a recorded JSON object as wire values, absent or null as none.
func decodeObject(data json.RawMessage) (wire.Object, error) {
	if absent(data) {
		return nil, nil
	}
	v, err := wire.DecodeJSON(data)
	if err != nil {
		return nil, err
	}
	obj, ok := v.(wire.Object)
	if !ok {
		return nil, errors.New("not a JSON object")
	}
	return obj, nil
}

// markPlanRefusals puts on each exposed request leaf the requests that the record's answer
// rules for it show Stripe refusing (PlanRefusal), so that the plan refuses them too. A refused
// request becomes a pattern of the whole request the probe sent and, on an update, the whole
// object it was created with, so that a refusal holds only where everything the probe sent or
// held matches. A rule the generator cannot translate fails generation unless
// Resource.LeftToStripe lists it: one whose refused request names another object in a state of
// its own (`$gen` `shared`, or a `ref` such as `coupon:expired`) or one that does not exist
// (`resource_missing`), or sends a value read from the object (`read`), on which Stripe's answer
// can turn, none of which the plan can know, or one that a request Stripe took also matches: one
// of the same rule, or the object's minimal create. A refused request the table cannot send, or
// to an object it cannot hold, is left out: no plan sends it. An entry that adds the probe to the
// rule's key leaves that refused request alone to Stripe, and the plan refuses the rule's others.
func markPlanRefusals(res Resource, object, create, update string, table []fields.Field, leaves []Leaf, records []Record, ev *Pinned) error {
	used := map[string]bool{}
	if ev != nil {
		for _, r := range ev.Evidence.Rules {
			if !r.answer() || r.Object != object || r.Operation == "" || r.Operation != create && r.Operation != update {
				continue
			}
			at := exposedRequestLeaf(leaves, records, object, r.Operation, r.Path)
			if at == "" {
				continue
			}
			key := r.Operation + " " + r.Path
			if _, left := res.LeftToStripe[key]; left {
				used[key] = true
				continue
			}
			leftProbes := map[string]bool{}
			for _, o := range r.Observations {
				probeKey := key + " " + o.Condition.Probe
				if _, left := res.LeftToStripe[probeKey]; left && o.Value.Status != http.StatusOK {
					leftProbes[o.Condition.Probe], used[probeKey] = true, true
				}
			}
			isUpdate := r.Operation == update
			var base wire.Object
			if !isUpdate {
				base = ev.baseCreate(object, create)
			}
			form := !strings.HasPrefix(res.CreatePath, "/v2/") && !strings.HasPrefix(res.UpdatePath, "/v2/")
			refusals, err := translateRule(r, isUpdate, base, table, leftProbes, form)
			if err != nil {
				return fmt.Errorf("%s: the record's rule on %s: %w; put it to the plan, or list it in LeftToStripe with the reason", res.Package, key, err)
			}
			f := fieldAt(table, at)
			f.PlanRefusals = append(f.PlanRefusals, refusals...)
		}
	}
	for _, key := range slices.Sorted(maps.Keys(res.LeftToStripe)) {
		if !used[key] {
			return fmt.Errorf("%s: LeftToStripe names %s, which is no rule of an exposed argument with a refused request, nor a refused request of one", res.Package, key)
		}
	}
	return nil
}

// exposedRequestLeaf is the table path of the exposed request leaf of an operation at a dotted
// path, in any union branch, or "".
func exposedRequestLeaf(leaves []Leaf, records []Record, object, operation, path string) string {
	for i, l := range leaves {
		if l.Object == object && l.Side == SideRequest && l.Operation == operation && l.Path == path && records[i].State == StateExposed {
			return l.At
		}
	}
	return ""
}

// baseCreate is the request of the object's minimal create with nothing else set, or nil.
func (p *Pinned) baseCreate(object, create string) wire.Object {
	for _, c := range p.creates[object+" "+create] {
		if c.Variant == "" && c.Status == http.StatusOK {
			return wire.Object(c.Params)
		}
	}
	return nil
}

// taken is a request Stripe took, as a pattern of what it sent and what its object held.
type taken struct {
	probe       string
	sent, holds wire.Object
}

// translateRule turns each refused request of an answer rule into a PlanRefusal, and fails on
// one it cannot translate or that a request Stripe took also matches: one of the rule's, or, on a
// create, the minimal create base, which Stripe took. On a form API it also fails on a refused
// request whose params or object hold an empty list, an empty hash or null: the suite's form
// encoder sends no pair for the first two and refuses the last, so such a member says the probe
// left the key out, which a pattern cannot state, while the engine compares a request as its form
// sends it, where a removal is the empty value.
func translateRule(r EvidenceRule, update bool, base wire.Object, table []fields.Field, left map[string]bool, form bool) ([]fields.PlanRefusal, error) {
	var accepted []taken
	if base != nil {
		sent, err := pattern(base, false)
		if err != nil {
			return nil, fmt.Errorf("the minimal create: %w", err)
		}
		accepted = append(accepted, taken{"the minimal create", sent, nil})
	}
	for _, o := range r.Observations {
		if o.Value.Status != http.StatusOK {
			continue
		}
		sent, err := patternOf(o.Condition.Params, false)
		if err != nil {
			return nil, fmt.Errorf("probe %s: %w", o.Condition.Probe, err)
		}
		holds, err := patternOf(o.Condition.CreatedWith, false)
		if err != nil {
			return nil, fmt.Errorf("probe %s: %w", o.Condition.Probe, err)
		}
		accepted = append(accepted, taken{"probe " + o.Condition.Probe, sent, holds})
	}
	var out []fields.PlanRefusal
	for _, o := range r.Observations {
		if o.Value.Status == http.StatusOK || left[o.Condition.Probe] {
			continue
		}
		c := o.Condition
		switch {
		case o.Value.Message == "":
			return nil, fmt.Errorf("probe %s: the refusal has no message", c.Probe)
		case o.Value.Code == "resource_missing":
			return nil, fmt.Errorf("probe %s: Stripe refused an object that does not exist, which the plan cannot know", c.Probe)
		}
		sends, err := patternOf(c.Params, true)
		if err != nil {
			return nil, fmt.Errorf("probe %s: %w", c.Probe, err)
		}
		var holds wire.Object
		if update {
			if holds, err = patternOf(c.CreatedWith, true); err != nil {
				return nil, fmt.Errorf("probe %s: %w", c.Probe, err)
			}
		}
		if len(sends) == 0 {
			return nil, fmt.Errorf("probe %s: the refused request sends nothing", c.Probe)
		}
		if !takes(table, sends, update) || !takes(table, holds, false) {
			continue
		}
		if form {
			for _, p := range []wire.Object{sends, holds} {
				if at := emptyMember(p, ""); at != "" {
					return nil, fmt.Errorf("probe %s: `%s` is empty or null, which the suite's form sent as no value at all, and a plan cannot tell from the removal it sends as the empty value", c.Probe, at)
				}
			}
		}
		for _, a := range accepted {
			if wire.SendsAll(sends, a.sent) && wire.HoldsAll(holds, a.holds) {
				return nil, fmt.Errorf("Stripe took the request of %s, which sends what probe %s sent, to an object that holds what it held, so the plan cannot tell them apart", a.probe, c.Probe)
			}
		}
		refusal := fields.PlanRefusal{Update: update, Probe: c.Probe, AccountCountry: c.AccountCountry, Message: o.Value.Message}
		if refusal.Sends, err = jsonOf(sends); err != nil {
			return nil, err
		}
		if len(holds) > 0 {
			if refusal.Holds, err = jsonOf(holds); err != nil {
				return nil, err
			}
		}
		out = append(out, refusal)
	}
	return out, nil
}

// emptyMember is the dotted path of the first member, at any depth, that is null, an empty
// list or an empty hash, or "".
func emptyMember(v wire.Value, at string) string {
	switch t := v.(type) {
	case wire.Null:
		return at
	case wire.Array:
		if len(t) == 0 && at != "" {
			return at
		}
		for _, e := range t {
			if p := emptyMember(e, at); p != "" {
				return p
			}
		}
	case wire.Object:
		if len(t) == 0 && at != "" {
			return at
		}
		for _, f := range t {
			name := f.Name
			if at != "" {
				name = at + "." + f.Name
			}
			if p := emptyMember(f.Value, name); p != "" {
				return p
			}
		}
	}
	return ""
}

// patternOf is a recorded request or object, whole, as a pattern (pattern).
func patternOf(raw json.RawMessage, strict bool) (wire.Object, error) {
	obj, err := decodeObject(raw)
	if err != nil {
		return nil, err
	}
	return pattern(obj, strict)
}

// pattern is a recorded object with each value the suite generated as wire.Any. Where strict, it
// refuses a generated value whose identity Stripe's answer can turn on: another object in a state
// of its own, or a value read from the object.
func pattern(obj wire.Object, strict bool) (wire.Object, error) {
	if obj == nil {
		return nil, nil
	}
	v, err := patternValue(obj, strict)
	if err != nil {
		return nil, err
	}
	return v.(wire.Object), nil
}

func patternValue(v wire.Value, strict bool) (wire.Value, error) {
	switch t := v.(type) {
	case wire.Object:
		if kind, ok := t.GetString("$gen"); ok {
			if !slices.Contains(genKinds, kind) {
				return nil, fmt.Errorf("the generated value %q is not one the generator knows (%s)", kind, strings.Join(genKinds, ", "))
			}
			arg, _ := t.GetString("arg")
			switch {
			case !strict:
			case kind == "shared" || kind == "ref" && strings.Contains(arg, ":"):
				return nil, fmt.Errorf("the refused request names %s `%s`, another object in a state of its own, which the plan cannot know", kind, arg)
			case kind == "read":
				return nil, fmt.Errorf("the refused request sends `%s` as read from the object, and Stripe's answer can turn on which value that is", arg)
			case slices.Contains(meaningfulGenKinds, kind):
				return nil, fmt.Errorf("the refused request holds `$gen` `%s`, a value that means something of its own (the account's currency, or a time relative to the request), which a pattern cannot state and any value would widen", kind)
			}
			return wire.Any, nil
		}
		obj := make(wire.Object, 0, len(t))
		for _, f := range t {
			inner, err := patternValue(f.Value, strict)
			if err != nil {
				return nil, fmt.Errorf("%s: %w", f.Name, err)
			}
			obj = append(obj, wire.Field{Name: f.Name, Value: inner})
		}
		return obj, nil
	case wire.Array:
		arr := make(wire.Array, 0, len(t))
		for _, e := range t {
			inner, err := patternValue(e, strict)
			if err != nil {
				return nil, err
			}
			arr = append(arr, inner)
		}
		return arr, nil
	}
	return v, nil
}

// takes reports whether the table's request of the operation (the update's where update, else
// the create's) takes every member of a pattern: a request the table cannot send, or an object
// it cannot hold, matches no plan.
func takes(table []fields.Field, pattern wire.Object, update bool) bool {
	for _, f := range pattern {
		if !takesMember(table, f, update) {
			return false
		}
	}
	return true
}

func takesMember(table []fields.Field, member wire.Field, update bool) bool {
	for _, field := range table {
		if field.Name != member.Name || update && !field.Update || !update && !field.Create {
			continue
		}
		switch v := member.Value.(type) {
		case wire.Object:
			if wire.IsAny(v) || field.Shape != fields.Object && field.Shape != fields.ObjectList {
				return true
			}
			if takes(field.Fields, v, update) {
				return true
			}
		case wire.Array:
			if field.Shape != fields.ObjectList {
				return true
			}
			all := true
			for _, e := range v {
				o, ok := e.(wire.Object)
				all = all && ok && takes(field.Fields, o, update)
			}
			if all {
				return true
			}
		default:
			return true
		}
	}
	return false
}

func jsonOf(o wire.Object) (string, error) {
	data, err := wire.EncodeJSON(o)
	return string(data), err
}
