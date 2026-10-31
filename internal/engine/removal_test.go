package engine

import (
	"context"
	"strings"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-framework/types/basetypes"
	"github.com/hashicorp/terraform-plugin-go/tftypes"

	"github.com/computomatic/terraform-provider-stripe/internal/fields"
	"github.com/computomatic/terraform-provider-stripe/internal/wire"
)

func removalTable() []fields.Field {
	member := func(name string, shape fields.Shape) fields.Field {
		return fields.Field{Name: name, Shape: shape, Create: true, Update: true, Response: true}
	}
	terms := member("terms", fields.Object)
	terms.Fields = []fields.Field{member("note", fields.String), member("group", fields.String)}
	return append(fixtureTable(), terms)
}

// Removing a field Stripe cannot unset replaces the object, and any other change stays an update,
// as does the removal of a member that has no entry. A member's entry says nothing when its parent
// goes, whose own entry decides.
func TestReplaceOnRemoval(t *testing.T) {
	decl := fixtureDeclaration()
	decl.ReplaceOnRemoval = []string{"quantity", "terms", "terms.note"}
	r := configured(t, decl, removalTable(), newStub())
	termsType := r.objType.AttrTypes["terms"].(basetypes.ObjectType)
	terms := func(note, group attr.Value) attr.Value {
		return object(termsType, map[string]attr.Value{"note": note, "group": group})
	}
	a, g := types.StringValue("a"), types.StringValue("g")
	state := r.fill(map[string]attr.Value{"id": types.StringValue("wid_1"), "name": types.StringValue("w"), "quantity": types.Int64Value(1), "terms": terms(a, g)})
	replaced := func(changes map[string]attr.Value) []string {
		plan := r.fill(with(state, changes))
		resp := &resource.ModifyPlanResponse{Plan: tfsdk.Plan{Schema: r.schema, Raw: raw(t, plan)}}
		r.ModifyPlan(context.Background(), resource.ModifyPlanRequest{
			Config: tfsdk.Config{Schema: r.schema, Raw: raw(t, plan)},
			Plan:   tfsdk.Plan{Schema: r.schema, Raw: raw(t, plan)},
			State:  tfsdk.State{Schema: r.schema, Raw: raw(t, state)},
		}, resp)
		noErrors(t, resp.Diagnostics)
		var paths []string
		for _, p := range resp.RequiresReplace {
			paths = append(paths, p.String())
		}
		return paths
	}
	for _, tc := range []struct {
		name    string
		changes map[string]attr.Value
		want    string
	}{
		{"a scalar", map[string]attr.Value{"quantity": types.Int64Null()}, "quantity"},
		{"a hash", map[string]attr.Value{"terms": nullOf(termsType)}, "terms"},
		{"a member", map[string]attr.Value{"terms": terms(types.StringNull(), g)}, "terms.note"},
		{"a member with no entry", map[string]attr.Value{"terms": terms(a, types.StringNull())}, ""},
		{"a change", map[string]attr.Value{"quantity": types.Int64Value(2), "terms": terms(types.StringValue("b"), g)}, ""},
	} {
		if got := strings.Join(replaced(tc.changes), ","); got != tc.want {
			t.Errorf("%s: replacement on %q, want %q", tc.name, got, tc.want)
		}
	}
}

// An entry is an optional field an update takes, not computed, on an object a replacement can make
// again.
func TestReplaceOnRemovalIsChecked(t *testing.T) {
	for name, tc := range map[string]struct {
		entry  string
		delete DeleteKind
		want   bool
	}{
		"optional":      {entry: "quantity", delete: Delete},
		"a hash member": {entry: "terms.note", delete: Delete},
		"required":      {entry: "name", delete: Delete, want: true},
		"computed":      {entry: "active", delete: Delete, want: true},
		"create-only":   {entry: "kind", delete: Delete, want: true},
		"no such path":  {entry: "gone", delete: Delete, want: true},
		"refused":       {entry: "quantity", delete: Refuse, want: true},
	} {
		decl := fixtureDeclaration()
		decl.ReplaceOnRemoval, decl.Delete = []string{tc.entry}, tc.delete
		if tc.delete == Refuse {
			decl.Paths.Delete, decl.RefuseReason = "", "Stripe cannot."
		}
		_, err := compile(decl, removalTable())
		if tc.want != (err != nil && strings.Contains(err.Error(), "replace-on-removal entry")) {
			t.Errorf("%s: %v", name, err)
		}
	}
}

// A declaration words a rule, a default the variants give differently or a none, which the
// generator checks against the suite's probes, but no field without a statement, and no literal.
func TestDefaultsWordWhatTheRecordLeavesOpen(t *testing.T) {
	none, varies := &fields.Default{Kind: fields.DefaultNone}, &fields.Default{Kind: fields.DefaultVaries}
	table := func(top, member *fields.Default) []fields.Field {
		out := removalTable()
		for i := range out {
			switch out[i].Name {
			case "quantity":
				out[i].Default = top
			case "terms":
				out[i].Fields[0].Default = member
			}
		}
		return out
	}
	for name, tc := range map[string]struct {
		entry       string
		top, member *fields.Default
		fails       bool
	}{
		"a default that varies":  {entry: "quantity", top: varies},
		"a member's none":        {entry: "terms.note", member: none},
		"a top-level none":       {entry: "quantity", top: none},
		"a literal":              {entry: "quantity", top: &fields.Default{Kind: fields.DefaultValue, Value: "1"}, fails: true},
		"no statement to refine": {entry: "terms.note", fails: true},
	} {
		decl := fixtureDeclaration()
		decl.Defaults = map[string]string{tc.entry: "a rule"}
		_, err := compile(decl, table(tc.top, tc.member))
		if tc.fails != (err != nil && strings.Contains(err.Error(), "defaults entry")) {
			t.Errorf("%s: %v", name, err)
		}
	}
}

// An empty collection holds nothing: its removal is no removal and sends no unset, and `[]` over a
// list that holds values is one.
func TestReplaceOnRemovalOfACollection(t *testing.T) {
	decl := fixtureDeclaration()
	decl.ReplaceOnRemoval = []string{"tags"}
	r := configured(t, decl, removalTable(), newStub())
	list := func(values ...string) attr.Value {
		elems := make([]attr.Value, len(values))
		for i, v := range values {
			elems[i] = types.StringValue(v)
		}
		return types.ListValueMust(types.StringType, elems)
	}
	null := types.ListNull(types.StringType)
	for _, tc := range []struct {
		name        string
		state, plan attr.Value
		replaces    bool
		sends       bool
	}{
		{name: "values removed", state: list("a"), plan: null, replaces: true},
		{name: "values emptied", state: list("a"), plan: list(), replaces: true},
		{name: "empty removed", state: list(), plan: null},
		{name: "a change", state: list("a"), plan: list("b"), sends: true},
	} {
		state := r.fill(map[string]attr.Value{"id": types.StringValue("wid_1"), "name": types.StringValue("w"), "tags": tc.state})
		plan := r.fill(with(state, map[string]attr.Value{"tags": tc.plan}))
		resp := &resource.ModifyPlanResponse{Plan: tfsdk.Plan{Schema: r.schema, Raw: raw(t, plan)}}
		r.ModifyPlan(context.Background(), resource.ModifyPlanRequest{
			Config: tfsdk.Config{Schema: r.schema, Raw: raw(t, plan)},
			Plan:   tfsdk.Plan{Schema: r.schema, Raw: raw(t, plan)},
			State:  tfsdk.State{Schema: r.schema, Raw: raw(t, state)},
		}, resp)
		noErrors(t, resp.Diagnostics)
		if got := len(resp.RequiresReplace) > 0; got != tc.replaces {
			t.Errorf("%s: replaces %v, want %v", tc.name, got, tc.replaces)
		}
		if tc.replaces {
			continue
		}
		params, err := r.m.encodeUpdate(plan, state, tftypes.Value{})
		if err != nil {
			t.Fatal(err)
		}
		if _, sent := params.Get("tags"); sent != tc.sends {
			t.Errorf("%s: update body %s", tc.name, wireOf(t, params))
		}
	}
}

// A read keeps an empty list the plan set, and one Stripe empties under a known value, which is
// why a removal has to expect it in the state.
func TestReadKeepsAnEmptyListItKnows(t *testing.T) {
	r := configured(t, fixtureDeclaration(), removalTable(), newStub())
	empty := wire.Array{}
	for name, prior := range map[string]attr.Value{
		"configured empty": types.ListValueMust(types.StringType, nil),
		"held a value":     types.ListValueMust(types.StringType, []attr.Value{types.StringValue("a")}),
	} {
		state := r.fill(map[string]attr.Value{"id": types.StringValue("wid_1"), "name": types.StringValue("w"), "tags": prior})
		got, diags := r.m.decodeState(wire.Object{{Name: "id", Value: wire.String("wid_1")}, {Name: "name", Value: wire.String("w")}, {Name: "tags", Value: empty}}, state, r.objType, false, nil)
		noErrors(t, diags)
		if tags := got.Attributes()["tags"]; tags.IsNull() || !emptyCollection(tags) {
			t.Errorf("%s: read %v, want an empty list", name, tags)
		}
	}
}

// A MatchData element sets its key or its data, but one not known at plan may yet: it waits for the
// plan Terraform makes at apply, when the same configuration, known, decides.
func TestKeyAndDataWaitForValuesKnownAtApply(t *testing.T) {
	r := configured(t, keyedDeclaration(), listTable(), newStub())
	itemType := r.objType.AttrTypes["items"].(interface{ ElementType() attr.Type }).ElementType().(basetypes.ObjectType)
	dataType := itemType.AttrTypes["price_data"].(basetypes.ObjectType)
	null, unknown := types.StringNull(), types.StringUnknown()
	price := types.StringValue("price_1")
	data := object(dataType, map[string]attr.Value{"product": types.StringValue("prod_1")})
	nullData, unknownData := nullOf(dataType), types.ObjectUnknown(dataType.AttrTypes)
	item := func(key, built attr.Value) attr.Value {
		return object(itemType, map[string]attr.Value{"price": key, "price_data": built, "quantity": types.Int64Value(1)})
	}
	for _, tc := range []struct {
		name    string
		element attr.Value
		refused bool
	}{
		{name: "a key", element: item(price, nullData)},
		{name: "data", element: item(null, data)},
		{name: "both", element: item(price, data), refused: true},
		{name: "neither", element: item(null, nullData), refused: true},
		{name: "an unknown key beside data", element: item(unknown, data)},
		{name: "an unknown key alone", element: item(unknown, nullData)},
		{name: "unknown data alone", element: item(null, unknownData)},
		{name: "unknown data beside a key", element: item(price, unknownData)},
		{name: "both unknown", element: item(unknown, unknownData)},
		{name: "an unknown element", element: types.ObjectUnknown(itemType.AttrTypes)},
	} {
		config := r.fill(map[string]attr.Value{"name": types.StringValue("w"), "items": types.ListValueMust(itemType, []attr.Value{tc.element})})
		resp := &resource.ModifyPlanResponse{Plan: tfsdk.Plan{Schema: r.schema, Raw: raw(t, config)}}
		r.ModifyPlan(context.Background(), resource.ModifyPlanRequest{
			Config: tfsdk.Config{Schema: r.schema, Raw: raw(t, config)},
			Plan:   tfsdk.Plan{Schema: r.schema, Raw: raw(t, config)},
			State:  tfsdk.State{Schema: r.schema, Raw: tftypes.NewValue(r.objType.TerraformType(context.Background()), nil)},
		}, resp)
		if got := resp.Diagnostics.HasError(); got != tc.refused {
			t.Errorf("%s: refused %v, want %v: %v", tc.name, got, tc.refused, resp.Diagnostics)
		}
	}
}
