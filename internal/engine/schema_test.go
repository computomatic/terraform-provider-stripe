package engine

import (
	"context"
	"strings"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/datasource"
	dschema "github.com/hashicorp/terraform-plugin-framework/datasource/schema"
	rschema "github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-framework/types/basetypes"
	"github.com/hashicorp/terraform-plugin-go/tftypes"

	"github.com/computomatic/terraform-provider-stripe/internal/fields"
)

// The engine builds a valid schema for a declaration whose table has every shape of the
// ADR D2 nested-object table, and applies the mapping rules to it.
func TestResourceSchemaFromFixture(t *testing.T) {
	ctx := context.Background()
	r := fixtureResource(t, newStub())
	if diags := r.schema.ValidateImplementation(ctx); diags.HasError() {
		t.Fatalf("invalid schema: %v", diags)
	}
	a := r.schema.Attributes

	if _, ok := a["address"].(rschema.SingleNestedAttribute); !ok {
		t.Errorf("hash with fixed keys: %T", a["address"])
	}
	if _, ok := a["items"].(rschema.ListNestedAttribute); !ok {
		t.Errorf("array of hashes: %T", a["items"])
	}
	if _, ok := a["events"].(rschema.SetAttribute); !ok {
		t.Errorf("array of scalars declared a set: %T", a["events"])
	}
	if _, ok := a["tags"].(rschema.ListAttribute); !ok {
		t.Errorf("array of scalars: %T", a["tags"])
	}
	if _, ok := a["options"].(rschema.MapNestedAttribute); !ok {
		t.Errorf("hash keyed by a variable: %T", a["options"])
	}
	meta, ok := a["metadata"].(rschema.MapAttribute)
	if !ok || meta.ElementType != types.StringType || !meta.Optional || meta.Computed {
		t.Errorf("metadata must be an Optional map of strings: %#v", a["metadata"])
	}

	name := a["name"].(rschema.StringAttribute)
	if !name.Required || len(name.PlanModifiers) != 0 || !strings.Contains(name.MarkdownDescription, "display name") {
		t.Errorf("name: %#v", name)
	}
	kind := a["kind"].(rschema.StringAttribute)
	if !kind.Optional || kind.Computed || len(kind.PlanModifiers) != 1 || !strings.Contains(kind.MarkdownDescription, "`a`, `b`") {
		t.Errorf("create-only kind: %#v", kind)
	}
	id := a["id"].(rschema.StringAttribute)
	if !id.Optional || !id.Computed || len(id.PlanModifiers) != 2 {
		t.Errorf("id is Optional+Computed, kept across updates and create-only: %#v", id)
	}
	if active := a["active"].(rschema.BoolAttribute); !active.Optional || !active.Computed || len(active.PlanModifiers) != 0 {
		t.Errorf("server-defaulted active: %#v", active)
	}
	if created := a["created"].(rschema.Int64Attribute); !created.Computed || created.Optional || len(created.PlanModifiers) != 1 {
		t.Errorf("created: %#v", created)
	}
	if secret := a["secret"].(rschema.StringAttribute); !secret.Computed || !secret.Sensitive || secret.Optional {
		t.Errorf("once-returned secret: %#v", secret)
	}
	if passcode := a["passcode"].(rschema.StringAttribute); !passcode.Sensitive || !passcode.Optional || passcode.Computed {
		t.Errorf("request-only sensitive passcode: %#v", passcode)
	}
	if ref := a["default_ref"].(rschema.StringAttribute); !ref.Optional || !ref.Computed {
		t.Errorf("update-only default_ref is settable and returned: %#v", ref)
	}
	if _, ok := a["provider_"]; !ok {
		t.Error("reserved name must be renamed")
	}
	if _, ok := a["provider"]; ok {
		t.Error("reserved name must not appear")
	}
	zip := a["address"].(rschema.SingleNestedAttribute).Attributes["zip"].(rschema.StringAttribute)
	if len(zip.PlanModifiers) != 1 {
		t.Errorf("create-only nested field must force replacement: %#v", zip)
	}
	if _, ok := a["timeouts"]; !ok {
		t.Error("no timeouts attribute")
	}
	if diags := r.m.identitySchema().ValidateImplementation(ctx); diags.HasError() {
		t.Errorf("identity schema: %v", diags)
	}
}

func TestDataSourceSchemaFromFixture(t *testing.T) {
	ctor, err := NewDataSource(fixtureDeclaration(), fixtureTable())
	if err != nil {
		t.Fatal(err)
	}
	ds := ctor().(*DataSource)
	if diags := ds.schema.ValidateImplementation(context.Background()); diags.HasError() {
		t.Fatalf("invalid schema: %v", diags)
	}
	a := ds.schema.Attributes
	for _, key := range []string{"id", "kind"} {
		if s := a[key].(dschema.StringAttribute); !s.Optional || !s.Computed {
			t.Errorf("lookup key %s: %#v", key, s)
		}
	}
	if s := a["name"].(dschema.StringAttribute); !s.Computed || s.Optional {
		t.Errorf("name: %#v", s)
	}
	if s := a["secret"].(dschema.StringAttribute); !s.Sensitive {
		t.Errorf("once-returned secrets stay sensitive")
	}
}

func TestDeclarationsAreCheckedAgainstTheTable(t *testing.T) {
	cases := map[string]func(*Declaration){
		"once":                   func(d *Declaration) { d.OnceReturned = []string{"nope"} },
		"pruned":                 func(d *Declaration) { d.NotServerDefaulted = []string{"nope"} },
		"sensitive":              func(d *Declaration) { d.Sensitive = []string{"nope"} },
		"preserve":               func(d *Declaration) { d.PreserveConfig = []string{"secret"} },
		"set":                    func(d *Declaration) { d.Sets = []string{"name"} },
		"rename":                 func(d *Declaration) { d.Rename = map[string]string{"nope": "x"} },
		"replace on":             func(d *Declaration) { d.ReplaceOn = []string{"nope"} },
		"replace on create-only": func(d *Declaration) { d.ReplaceOn = []string{"kind"} },
		"lookup":                 func(d *Declaration) { d.Lookups = []Lookup{{Field: "quantity"}} },
		"reserved":               func(d *Declaration) { d.Rename = nil },
		"collision":              func(d *Declaration) { d.Rename = map[string]string{"name": "timeouts"} },
		"archive":                func(d *Declaration) { d.Delete = Archive },
		"refuse":                 func(d *Declaration) { d.Delete = Refuse },
		"delete path":            func(d *Declaration) { d.Paths.Delete = "" },
		"type name":              func(d *Declaration) { d.TypeName = "" },
	}
	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			decl := fixtureDeclaration()
			mutate(&decl)
			if _, err := NewResource(decl, fixtureTable()); err == nil {
				t.Error("expected the declaration to be rejected")
			}
		})
	}

	noID := fixtureTable()[1:]
	if _, err := NewResource(fixtureDeclaration(), noID); err == nil {
		t.Error("a table without an id must be rejected")
	}
	decl := fixtureDeclaration()
	decl.Delete = NoDelete
	if _, err := NewResource(decl, fixtureTable()); err == nil {
		t.Error("a declaration with no delete strategy has no resource")
	}
	if _, err := NewDataSource(decl, fixtureTable()); err != nil {
		t.Errorf("it still has a data source: %v", err)
	}
}

func TestParentAttribute(t *testing.T) {
	decl := fixtureDeclaration()
	decl.Parent = "owner"
	r := configured(t, decl, fixtureTable(), newStub())
	owner, ok := r.schema.Attributes["owner"].(rschema.StringAttribute)
	if !ok || !owner.Required || len(owner.PlanModifiers) != 1 {
		t.Errorf("parent attribute: %#v", r.schema.Attributes["owner"])
	}
}

func dataSource(t *testing.T, decl Declaration, client Client) *DataSource {
	t.Helper()
	ctor, err := NewDataSource(decl, fixtureTable())
	if err != nil {
		t.Fatal(err)
	}
	ds := ctor().(*DataSource)
	var resp datasource.ConfigureResponse
	ds.Configure(context.Background(), datasource.ConfigureRequest{ProviderData: client}, &resp)
	noErrors(t, resp.Diagnostics)
	return ds
}

func (d *DataSource) config(set map[string]string) basetypes.ObjectValue {
	values := make(map[string]attr.Value, len(d.objType.AttrTypes))
	for name, typ := range d.objType.AttrTypes {
		if v, ok := set[name]; ok {
			values[name] = types.StringValue(v)
		} else {
			values[name] = nullOf(typ)
		}
	}
	return types.ObjectValueMust(d.objType.AttrTypes, values)
}

func (d *DataSource) read(t *testing.T, cfg basetypes.ObjectValue) (basetypes.ObjectValue, datasource.ReadResponse) {
	t.Helper()
	ctx := context.Background()
	resp := datasource.ReadResponse{State: tfsdk.State{Schema: d.schema, Raw: tftypes.NewValue(d.schema.Type().TerraformType(ctx), nil)}}
	d.Read(ctx, datasource.ReadRequest{Config: tfsdk.Config{Schema: d.schema, Raw: raw(t, cfg)}}, &resp)
	var state basetypes.ObjectValue
	if !resp.Diagnostics.HasError() {
		noErrors(t, resp.State.Get(ctx, &state))
	}
	return state, resp
}

func TestDataSourceLookups(t *testing.T) {
	stub := newStub()
	r := fixtureResource(t, stub)
	for _, kind := range []string{"a", "b", "b"} {
		plan := createPlan(r)
		attrs := plan.Attributes()
		attrs["kind"] = types.StringValue(kind)
		attrs["id"] = types.StringUnknown()
		r.create(t, types.ObjectValueMust(r.objType.AttrTypes, attrs))
	}
	ds := dataSource(t, fixtureDeclaration(), stub)

	byID, resp := ds.read(t, ds.config(map[string]string{"id": "wid_1"}))
	noErrors(t, resp.Diagnostics)
	if str(t, byID, "name") != "w" || str(t, byID, "secret") == "" {
		t.Errorf("by id: %v", byID)
	}

	byKey, resp := ds.read(t, ds.config(map[string]string{"kind": "a"}))
	noErrors(t, resp.Diagnostics)
	if str(t, byKey, "id") != "wid_1" {
		t.Errorf("by key: %v", byKey)
	}

	_, resp = ds.read(t, ds.config(map[string]string{"kind": "zzz"}))
	if !resp.Diagnostics.HasError() || !strings.Contains(resp.Diagnostics.Errors()[0].Detail(), "no stripe_widget found for kind=zzz") {
		t.Errorf("zero matches: %v", resp.Diagnostics)
	}
	_, resp = ds.read(t, ds.config(map[string]string{"kind": "b"}))
	if !resp.Diagnostics.HasError() || !strings.Contains(resp.Diagnostics.Errors()[0].Detail(), "matches 2 objects") {
		t.Errorf("many matches: %v", resp.Diagnostics)
	}
	_, resp = ds.read(t, ds.config(map[string]string{"id": "wid_404"}))
	if !resp.Diagnostics.HasError() {
		t.Error("an unknown id must be an error")
	}
}

func TestDataSourceRequiresExactlyOneLookup(t *testing.T) {
	ds := dataSource(t, fixtureDeclaration(), newStub())
	validate := func(set map[string]string) bool {
		ctx := context.Background()
		var resp datasource.ValidateConfigResponse
		ds.ValidateConfig(ctx, datasource.ValidateConfigRequest{Config: tfsdk.Config{Schema: ds.schema, Raw: raw(t, ds.config(set))}}, &resp)
		return !resp.Diagnostics.HasError()
	}
	if validate(nil) || validate(map[string]string{"id": "x", "kind": "a"}) {
		t.Error("none or several lookups must be rejected")
	}
	if !validate(map[string]string{"id": "x"}) || !validate(map[string]string{"kind": "a"}) {
		t.Error("one lookup must be accepted")
	}
}

// ReplaceOn makes a nested updatable field force replacement, as a create-only field does,
// and leaves its siblings updatable in place.
func TestReplaceOnForcesReplacementOfANestedField(t *testing.T) {
	decl := fixtureDeclaration()
	decl.ReplaceOn = []string{"address.line1", "quantity"}
	r := configured(t, decl, fixtureTable(), newStub())
	address := r.schema.Attributes["address"].(rschema.SingleNestedAttribute)
	if line1 := address.Attributes["line1"].(rschema.StringAttribute); len(line1.PlanModifiers) != 1 {
		t.Errorf("address.line1 must require replacement: %#v", line1)
	}
	if quantity := r.schema.Attributes["quantity"].(rschema.Int64Attribute); len(quantity.PlanModifiers) != 1 {
		t.Errorf("quantity must require replacement: %#v", quantity)
	}
	if name := r.schema.Attributes["name"].(rschema.StringAttribute); len(name.PlanModifiers) != 0 {
		t.Errorf("name stays updatable in place: %#v", name)
	}
}

// A ReplaceOn field Stripe fills in when it is left out is Optional+Computed. It keeps its
// state value when something else changes, or RequiresReplace would fire on an unrelated edit.
func TestReplaceOnOfAServerDefaultedFieldKeepsItsState(t *testing.T) {
	table := []fields.Field{
		{Name: "id", Shape: fields.String, Response: true},
		{Name: "name", Shape: fields.String, Required: true, Create: true, Update: true, Response: true},
		{Name: "region", Shape: fields.String, Create: true, Update: true, Response: true, ServerDefaulted: true},
	}
	decl := planDecl()
	decl.ReplaceOn = []string{"region"}

	r := configured(t, decl, table, newStub())
	region := r.schema.Attributes["region"].(rschema.StringAttribute)
	if !region.Optional || !region.Computed || len(region.PlanModifiers) != 2 {
		t.Fatalf("region must be Optional+Computed with UseStateForUnknown and RequiresReplace: %#v", region)
	}

	p := newPlanner(t, decl, table)
	state := map[string]tftypes.Value{"id": tfString("id_1"), "name": tfString("n"), "region": tfString("us")}
	config := map[string]tftypes.Value{"name": tfString("changed")}
	proposed := map[string]tftypes.Value{"id": tfString("id_1"), "name": tfString("changed"), "region": tfString("us")}
	planned, replace := p.plan(p.value(state), p.value(proposed), p.value(config))
	if len(replace) != 0 {
		t.Fatalf("changing name must not replace the resource: %v", replace)
	}
	got, err := planned.ApplyTerraform5AttributePathStep(tftypes.AttributeName("region"))
	if err != nil {
		t.Fatal(err)
	}
	if v := got.(tftypes.Value); !v.Equal(tfString("us")) {
		t.Fatalf("region must keep its state value, got %v", v)
	}
}
