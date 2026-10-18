package engine

import (
	"context"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/provider"
	"github.com/hashicorp/terraform-plugin-framework/providerserver"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-framework/types/basetypes"
	"github.com/hashicorp/terraform-plugin-go/tfprotov6"
	"github.com/hashicorp/terraform-plugin-go/tftypes"

	"github.com/computomatic/terraform-provider-stripe/internal/fields"
)

// planProvider serves one resource so a test can send PlanResourceChange through the real
// framework, with the prior state, proposed state and configuration Terraform would send.
type planProvider struct{ ctor func() resource.Resource }

func (planProvider) Metadata(_ context.Context, _ provider.MetadataRequest, resp *provider.MetadataResponse) {
	resp.TypeName = "stripe"
}
func (planProvider) Schema(context.Context, provider.SchemaRequest, *provider.SchemaResponse) {}
func (planProvider) Configure(context.Context, provider.ConfigureRequest, *provider.ConfigureResponse) {
}
func (p planProvider) Resources(context.Context) []func() resource.Resource {
	return []func() resource.Resource{p.ctor}
}
func (planProvider) DataSources(context.Context) []func() datasource.DataSource { return nil }

// planner plans changes to a resource declared by decl and table.
type planner struct {
	t      *testing.T
	server tfprotov6.ProviderServer
	typ    tftypes.Type
	name   string
}

func newPlanner(t *testing.T, decl Declaration, table []fields.Field) *planner {
	t.Helper()
	ctor, err := NewResource(decl, table)
	if err != nil {
		t.Fatal(err)
	}
	server := providerserver.NewProtocol6(planProvider{ctor: ctor})()
	schemas, err := server.GetProviderSchema(context.Background(), &tfprotov6.GetProviderSchemaRequest{})
	if err != nil {
		t.Fatal(err)
	}
	for _, d := range schemas.Diagnostics {
		t.Fatalf("schema: %s %s", d.Summary, d.Detail)
	}
	schema := schemas.ResourceSchemas[decl.TypeName]
	if schema == nil {
		t.Fatalf("no schema for %s", decl.TypeName)
	}
	return &planner{t: t, server: server, typ: schema.ValueType(), name: decl.TypeName}
}

// value builds an object value of the resource's type from attributes; any attribute not
// given is null.
func (p *planner) value(set map[string]tftypes.Value) tftypes.Value {
	p.t.Helper()
	return fillObject(p.t, p.typ, set)
}

func fillObject(t *testing.T, typ tftypes.Type, set map[string]tftypes.Value) tftypes.Value {
	ot, ok := typ.(tftypes.Object)
	if !ok {
		panic(typ.String() + " is not an object type")
	}
	vals := make(map[string]tftypes.Value, len(ot.AttributeTypes))
	for name, at := range ot.AttributeTypes {
		if v, given := set[name]; given {
			vals[name] = v
		} else {
			vals[name] = tftypes.NewValue(at, nil)
		}
	}
	return tftypes.NewValue(typ, vals)
}

// plan sends one PlanResourceChange and returns the planned state and the paths that force
// replacement.
func (p *planner) plan(prior, proposed, config tftypes.Value) (tftypes.Value, []*tftypes.AttributePath) {
	p.t.Helper()
	dv := func(v tftypes.Value) *tfprotov6.DynamicValue {
		d, err := tfprotov6.NewDynamicValue(p.typ, v)
		if err != nil {
			p.t.Fatal(err)
		}
		return &d
	}
	resp, err := p.server.PlanResourceChange(context.Background(), &tfprotov6.PlanResourceChangeRequest{
		TypeName:         p.name,
		PriorState:       dv(prior),
		ProposedNewState: dv(proposed),
		Config:           dv(config),
	})
	if err != nil {
		p.t.Fatal(err)
	}
	for _, d := range resp.Diagnostics {
		p.t.Fatalf("plan: %s %s", d.Summary, d.Detail)
	}
	planned, err := resp.PlannedState.Unmarshal(p.typ)
	if err != nil {
		p.t.Fatal(err)
	}
	return planned, resp.RequiresReplace
}

func tfString(s string) tftypes.Value { return tftypes.NewValue(tftypes.String, s) }
func tfUnknown() tftypes.Value {
	return tftypes.NewValue(tftypes.String, tftypes.UnknownValue)
}

func obj(attrs map[string]tftypes.Type, vals map[string]tftypes.Value) tftypes.Value {
	return fillObject(nil, tftypes.Object{AttributeTypes: attrs}, vals)
}

func planTable() []fields.Field {
	return []fields.Field{
		{Name: "id", Shape: fields.String, Response: true},
		{Name: "name", Shape: fields.String, Required: true, Create: true, Update: true, Response: true},
		// Stripe changes it on its own, so it has no UseStateForUnknown.
		{Name: "updated", Shape: fields.String, Response: true},
		{Name: "label", Shape: fields.String, Create: true, Update: true, Response: true},
		// A nested object Stripe fills in when it is left out.
		{Name: "defaults", Shape: fields.Object, Create: true, Update: true, Response: true, ServerDefaulted: true, Fields: []fields.Field{
			{Name: "mode", Shape: fields.String, Create: true, Update: true, Response: true},
		}},
		// A create-only object with a computed child.
		{Name: "settings", Shape: fields.Object, Required: true, Create: true, Response: true, Fields: []fields.Field{
			{Name: "value", Shape: fields.String, Required: true, Create: true, Response: true},
			{Name: "kind", Shape: fields.String, Response: true},
		}},
		{Name: "products", Shape: fields.ObjectList, Create: true, Update: true, Response: true, Fields: []fields.Field{
			{Name: "sku", Shape: fields.String, Required: true, Create: true, Update: true, Response: true},
			{Name: "limits", Shape: fields.Object, Create: true, Update: true, Response: true, ServerDefaulted: true, Fields: []fields.Field{
				{Name: "max", Shape: fields.Int, Create: true, Update: true, Response: true},
			}},
		}},
	}
}

func planDecl() Declaration {
	d := fixtureDeclaration()
	d.OnceReturned, d.Sensitive, d.PreserveConfig, d.Sets, d.Rename, d.Lookups = nil, nil, nil, nil, nil, nil
	return d
}

// attrTypes returns the attribute types of the object type at name.
func (p *planner) attrTypes(name string) map[string]tftypes.Type {
	ot := p.typ.(tftypes.Object).AttributeTypes[name]
	if l, ok := ot.(tftypes.List); ok {
		ot = l.ElementType
	}
	return ot.(tftypes.Object).AttributeTypes
}

// base returns a state and configuration pair that agree, for the tests to change.
func (p *planner) base() (state map[string]tftypes.Value) {
	settings := obj(p.attrTypes("settings"), map[string]tftypes.Value{"value": tfString("v"), "kind": tfString("k")})
	defaults := obj(p.attrTypes("defaults"), map[string]tftypes.Value{"mode": tfString("auto")})
	return map[string]tftypes.Value{
		"id": tfString("id_1"), "updated": tfString("u"), "name": tfString("n"), "label": tfString("l"), "settings": settings, "defaults": defaults,
		"timeouts": tftypes.NewValue(p.typ.(tftypes.Object).AttributeTypes["timeouts"], nil),
	}
}

func clone(m map[string]tftypes.Value, with map[string]tftypes.Value) map[string]tftypes.Value {
	out := map[string]tftypes.Value{}
	for k, v := range m {
		out[k] = v
	}
	for k, v := range with {
		out[k] = v
	}
	return out
}

// Terraform proposes null for a nested attribute Stripe fills in and the configuration
// leaves out; with nothing else changed the plan must stay the state.
func TestPlanOfNothingStaysTheState(t *testing.T) {
	p := newPlanner(t, planDecl(), planTable())
	state := p.base()
	config := clone(state, map[string]tftypes.Value{"id": tftypes.NewValue(tftypes.String, nil), "updated": tftypes.NewValue(tftypes.String, nil), "defaults": tftypes.NewValue(p.typ.(tftypes.Object).AttributeTypes["defaults"], nil)})
	proposed := clone(state, map[string]tftypes.Value{"defaults": tftypes.NewValue(p.typ.(tftypes.Object).AttributeTypes["defaults"], nil)})
	planned, replace := p.plan(p.value(state), p.value(proposed), p.value(config))
	if !planned.Equal(p.value(state)) || len(replace) != 0 {
		t.Fatalf("a plan that changes nothing must equal the state\nplanned: %v\nstate: %v", planned, p.value(state))
	}
}

// An unknown the configuration sets, such as a reference to an object being replaced, is a
// pending change and must not collapse into the state.
func TestPlanKeepsAConfiguredUnknown(t *testing.T) {
	p := newPlanner(t, planDecl(), planTable())
	state := p.base()
	defaultsNull := tftypes.NewValue(p.typ.(tftypes.Object).AttributeTypes["defaults"], nil)
	config := clone(state, map[string]tftypes.Value{"id": tftypes.NewValue(tftypes.String, nil), "updated": tftypes.NewValue(tftypes.String, nil), "defaults": defaultsNull, "name": tfUnknown()})
	proposed := clone(state, map[string]tftypes.Value{"defaults": defaultsNull, "name": tfUnknown()})
	planned, _ := p.plan(p.value(state), p.value(proposed), p.value(config))
	name, err := planned.ApplyTerraform5AttributePathStep(tftypes.AttributeName("name"))
	if err != nil {
		t.Fatal(err)
	}
	if v, ok := name.(tftypes.Value); !ok || v.IsKnown() {
		t.Fatalf("name was configured as unknown and must stay unknown in the plan, got %v", name)
	}
}

// Changing an unrelated field next to a create-only object with a computed child is an
// update in place: the computed child keeps its value instead of reading as a change.
func TestPlanNextToACreateOnlyObjectIsNotAReplacement(t *testing.T) {
	p := newPlanner(t, planDecl(), planTable())
	state := p.base()
	settingsCfg := obj(p.attrTypes("settings"), map[string]tftypes.Value{"value": tfString("v")})
	config := clone(state, map[string]tftypes.Value{"id": tftypes.NewValue(tftypes.String, nil), "updated": tftypes.NewValue(tftypes.String, nil), "label": tfString("changed"), "settings": settingsCfg})
	proposed := clone(state, map[string]tftypes.Value{"label": tfString("changed")})
	planned, replace := p.plan(p.value(state), p.value(proposed), p.value(config))
	// Terraform drops a replacement path whose planned value equals the prior one, so the
	// object only replaces if the computed child reads as a change.
	for _, rp := range replace {
		at, _, err := tftypes.WalkAttributePath(planned, rp)
		if err != nil {
			t.Fatal(err)
		}
		before, _, err := tftypes.WalkAttributePath(p.value(state), rp)
		if err != nil {
			t.Fatal(err)
		}
		if !at.(tftypes.Value).Equal(before.(tftypes.Value)) {
			t.Fatalf("changing label would replace through %v: planned %v, state %v", rp, at, before)
		}
	}
	got, _, err := tftypes.WalkAttributePath(planned, tftypes.NewAttributePath().WithAttributeName("settings").WithAttributeName("kind"))
	if err != nil {
		t.Fatal(err)
	}
	if v := got.(tftypes.Value); !v.IsKnown() {
		t.Fatalf("settings.kind must keep its value below a create-only object, got unknown")
	}
}

// Inside a list, prior state is paired by index, so an element inserted at the front must
// not inherit the computed object of the element that used to be there.
func TestPlanDoesNotCopyComputedObjectsAcrossListElements(t *testing.T) {
	p := newPlanner(t, planDecl(), planTable())
	elemType := p.typ.(tftypes.Object).AttributeTypes["products"].(tftypes.List).ElementType
	elemAttrs := elemType.(tftypes.Object).AttributeTypes
	limitsType := elemAttrs["limits"]
	limits := obj(p.attrTypes("products")["limits"].(tftypes.Object).AttributeTypes, map[string]tftypes.Value{"max": tftypes.NewValue(tftypes.Number, 1)})
	elem := func(sku string, l tftypes.Value) tftypes.Value {
		return fillObject(t, elemType, map[string]tftypes.Value{"sku": tfString(sku), "limits": l})
	}
	list := func(elems ...tftypes.Value) tftypes.Value {
		return tftypes.NewValue(tftypes.List{ElementType: elemType}, elems)
	}
	state := clone(p.base(), map[string]tftypes.Value{"products": list(elem("a", limits))})
	nullLimits := tftypes.NewValue(limitsType, nil)
	inserted := list(elem("b", nullLimits), elem("a", nullLimits))
	config := clone(state, map[string]tftypes.Value{"id": tftypes.NewValue(tftypes.String, nil), "updated": tftypes.NewValue(tftypes.String, nil), "products": inserted})
	// Terraform proposes null for the unset nested attribute in every element.
	proposed := clone(state, map[string]tftypes.Value{"products": inserted})
	planned, _ := p.plan(p.value(state), p.value(proposed), p.value(config))
	got, _, err := tftypes.WalkAttributePath(planned, tftypes.NewAttributePath().WithAttributeName("products").WithElementKeyInt(0).WithAttributeName("limits"))
	if err != nil {
		t.Fatal(err)
	}
	if v := got.(tftypes.Value); v.IsKnown() {
		t.Fatalf("the inserted element's limits must be unknown, got %v", v)
	}
}

// An update does not send Stripe back the nested objects it filled in and the update leaves
// alone, but it does send one whose own child changed.
func TestUpdateSkipsUnchangedServerFilledObjects(t *testing.T) {
	flag := func(name string) fields.Field {
		return fields.Field{Name: name, Shape: fields.Object, Create: true, Update: true, Response: true, ServerDefaulted: true, Fields: []fields.Field{
			{Name: "enabled", Shape: fields.Bool, Required: true, Create: true, Update: true, Response: true},
		}}
	}
	table := []fields.Field{
		{Name: "id", Shape: fields.String, Response: true},
		{Name: "features", Shape: fields.Object, Required: true, Create: true, Update: true, Response: true, Fields: []fields.Field{flag("a"), flag("b")}},
	}
	r := configured(t, planDecl(), table, newStub())
	featuresType := r.objType.AttrTypes["features"]
	flagType := featuresType.(basetypes.ObjectType).AttrTypes["a"]
	flagOf := func(on bool) attr.Value {
		return object(flagType, map[string]attr.Value{"enabled": types.BoolValue(on)})
	}
	features := func(a, b bool) attr.Value {
		return object(featuresType, map[string]attr.Value{"a": flagOf(a), "b": flagOf(b)})
	}
	state := r.fill(map[string]attr.Value{"id": types.StringValue("x"), "features": features(true, false)})
	plan := r.fill(map[string]attr.Value{"id": types.StringValue("x"), "features": features(false, false)})

	params, err := r.m.encodeUpdate(plan, state, tftypes.Value{})
	if err != nil {
		t.Fatal(err)
	}
	if got, want := wireOf(t, params), `{"features":{"a":{"enabled":false}}}`; got != want {
		t.Fatalf("update body %s, want %s", got, want)
	}
}
