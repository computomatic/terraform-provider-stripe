package engine

import (
	"context"
	"strings"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-framework/types/basetypes"

	"github.com/computomatic/terraform-provider-stripe/internal/fields"
)

// secureTable has a hash whose three members Stripe takes only all at once, as a setup intent's
// `three_d_secure`, beside one it takes alone, and a nested hash of a set.
func secureTable() []fields.Field {
	return append(fixtureTable(), fields.Field{Name: "secure", Shape: fields.Object, Create: true, Update: true, Response: true,
		Together: []fields.Together{{Members: []string{"cryptogram", "plan.count", "version"}, Refused: "Sent together, Stripe refused them."}},
		Fields: []fields.Field{
			{Name: "cryptogram", Shape: fields.String, Create: true, Update: true},
			{Name: "version", Shape: fields.String, Create: true, Update: true},
			{Name: "indicator", Shape: fields.String, Create: true, Update: true},
			{Name: "plan", Shape: fields.Object, Create: true, Update: true, Fields: []fields.Field{
				{Name: "count", Shape: fields.Int, Create: true, Update: true},
			}},
		}})
}

// A configuration sets all the members of a set or none: one that sets some is refused, a member
// not known at plan counts as set, and a hash on the way to a member that is not known leaves the
// set for the next plan.
func TestTogetherRefusesAPartOfASet(t *testing.T) {
	r := configured(t, fixtureDeclaration(), secureTable(), noCalls(t))
	secureType := r.objType.AttrTypes["secure"].(basetypes.ObjectType)
	planType := secureType.AttrTypes["plan"].(basetypes.ObjectType)
	plan := func(count attr.Value) attr.Value {
		return types.ObjectValueMust(planType.AttrTypes, map[string]attr.Value{"count": count})
	}
	for name, tc := range map[string]struct {
		set  map[string]attr.Value
		fail bool
	}{
		"none":                       {map[string]attr.Value{"indicator": sv("05")}, false},
		"all":                        {map[string]attr.Value{"cryptogram": sv("c"), "version": sv("2.2.0"), "plan": plan(types.Int64Value(3))}, false},
		"all, one unknown":           {map[string]attr.Value{"cryptogram": types.StringUnknown(), "version": sv("2.2.0"), "plan": plan(types.Int64Value(3))}, false},
		"some":                       {map[string]attr.Value{"cryptogram": sv("c"), "version": sv("2.2.0")}, true},
		"some, the nested one unset": {map[string]attr.Value{"cryptogram": sv("c"), "version": sv("2.2.0"), "plan": plan(types.Int64Null())}, true},
		"one unknown alone":          {map[string]attr.Value{"version": types.StringUnknown()}, true},
		"a hash above unknown":       {map[string]attr.Value{"cryptogram": sv("c"), "plan": types.ObjectUnknown(planType.AttrTypes)}, false},
	} {
		v := togetherSets{sets: secureTable()[len(fixtureTable())].Together}
		resp := &validator.ObjectResponse{}
		v.ValidateObject(context.Background(), validator.ObjectRequest{Path: path.Root("secure"), ConfigValue: object(secureType, tc.set)}, resp)
		if resp.Diagnostics.HasError() != tc.fail {
			t.Errorf("%s: refused %t, want %t: %v", name, resp.Diagnostics.HasError(), tc.fail, resp.Diagnostics)
		}
	}
	if a, ok := r.schema.Attributes["secure"].(interface{ ObjectValidators() []validator.Object }); !ok || len(a.ObjectValidators()) == 0 {
		t.Error("the hash that holds a set has no validator")
	}
}

// Each member's line names the rest of its set, with Stripe's refusal of the set.
func TestTogetherDocsNameTheSet(t *testing.T) {
	r := configured(t, fixtureDeclaration(), secureTable(), noCalls(t))
	docs := r.AttributeDocs()
	if got, want := docs["secure.plan.count"].Together, "Stripe takes it only together with `cryptogram` and `version`, so a configuration sets all of them or none. Sent together, Stripe refused them."; got != want {
		t.Errorf("count's line:\n got %q\nwant %q", got, want)
	}
	if got := docs["secure.indicator"].Together; got != "" {
		t.Errorf("a member of no set says %q", got)
	}
}

// A set must name arguments of its hash reached through hashes, and the hash may not send only
// its changed members, which could split the set.
func TestTogetherIsChecked(t *testing.T) {
	for name, tc := range map[string]struct {
		members []string
		mutate  func(*Declaration)
		want    string
	}{
		"a missing member": {[]string{"cryptogram", "nothing"}, nil, "is not an argument"},
		"sent in part":     {[]string{"cryptogram", "version"}, func(d *Declaration) { d.SendChangedMembers = []string{"secure"} }, "name it in SendWholeMembers"},
	} {
		table := secureTable()
		table[len(table)-1].Together = []fields.Together{{Members: tc.members}}
		decl := fixtureDeclaration()
		if tc.mutate != nil {
			tc.mutate(&decl)
		}
		if _, err := NewResource(decl, table); err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Errorf("%s: %v, want %q", name, err, tc.want)
		}
	}
}

// An element of a MatchBy list that continues a state element sends the keys a merged map of it
// lost as the unset, since Stripe keeps the keys such an update leaves out; a replaced map goes
// as planned, and a new element has nothing to lose.
func TestKeyedElementsSendLostKeysOfMergedMaps(t *testing.T) {
	for name, tc := range map[string]struct {
		update *fields.MapUpdate
		want   string
	}{
		"merged":   {&fields.MapUpdate{}, `{"items":[{"price":"pa","tags":{"a":"1","b":null},"id":"si_A"},{"price":"pn","tags":{"a":"1"}}]}`},
		"replaced": {&fields.MapUpdate{Replaced: true}, `{"items":[{"price":"pa","tags":{"a":"1"},"id":"si_A"},{"price":"pn","tags":{"a":"1"}}]}`},
	} {
		table := listTable()
		for i := range table {
			if table[i].Name == "items" {
				table[i].Fields = append(table[i].Fields, fields.Field{Name: "tags", Shape: fields.StringMap, Create: true, Update: true, Response: true, MapUpdate: tc.update})
			}
		}
		r := configured(t, keyedDeclaration(), table, newStub())
		tags := func(kv ...string) attr.Value {
			m := map[string]attr.Value{}
			for i := 0; i < len(kv); i += 2 {
				m[kv[i]] = sv(kv[i+1])
			}
			return types.MapValueMust(types.StringType, m)
		}
		row := func(base map[string]attr.Value, t attr.Value) map[string]attr.Value {
			base["tags"] = t
			return base
		}
		state := r.fill(map[string]attr.Value{"name": sv("n"), "items": elements(r, row(stateRow("si_A", "pa"), tags("a", "1", "b", "2")))})
		plan := r.fill(map[string]attr.Value{"name": sv("n"), "items": elements(r, row(planRow("pa", 0), tags("a", "1")), row(planRow("pn", 0), tags("a", "1")))})
		config := r.fill(map[string]attr.Value{"name": sv("n"), "items": elements(r, row(cfgRow("pa", 0), tags("a", "1")), row(cfgRow("pn", 0), tags("a", "1")))})
		params, err := r.m.encodeUpdate(plan, state, raw(t, config))
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if got := wireOf(t, params); got != tc.want {
			t.Errorf("%s:\n got %s\nwant %s", name, got, tc.want)
		}
	}
}

// A merged map in a list element the engine does not pair with the state's, by MatchBy outside
// FixedKeys, is refused: an update could not tell which keys the element lost.
func TestMergedMapsInUnpairedElementsAreRefused(t *testing.T) {
	for name, tc := range map[string]struct {
		update *fields.MapUpdate
		mutate func(*Declaration)
		ok     bool
	}{
		"merged, matched by price": {&fields.MapUpdate{}, nil, true},
		"replaced, unmatched":      {&fields.MapUpdate{Replaced: true}, func(d *Declaration) { d.MatchBy, d.MatchData = nil, nil }, true},
		"merged, unmatched":        {&fields.MapUpdate{}, func(d *Declaration) { d.MatchBy, d.MatchData = nil, nil }, false},
		"merged, fixed keys":       {&fields.MapUpdate{}, func(d *Declaration) { d.FixedKeys = []string{"items"} }, false},
	} {
		table := listTable()
		for i := range table {
			if table[i].Name == "items" {
				table[i].Fields = append(table[i].Fields, fields.Field{Name: "tags", Shape: fields.StringMap, Create: true, Update: true, Response: true, MapUpdate: tc.update})
			}
		}
		decl := keyedDeclaration()
		if tc.mutate != nil {
			tc.mutate(&decl)
		}
		if _, err := NewResource(decl, table); (err == nil) != tc.ok {
			t.Errorf("%s: %v", name, err)
		}
	}
}
