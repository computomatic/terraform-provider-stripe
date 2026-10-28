package engine

import (
	"context"
	"strings"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/provider"
	"github.com/hashicorp/terraform-plugin-framework/providerserver"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-framework/types/basetypes"
	"github.com/hashicorp/terraform-plugin-go/tfprotov6"
	"github.com/hashicorp/terraform-plugin-go/tftypes"

	"github.com/computomatic/terraform-provider-stripe/internal/fields"
	"github.com/computomatic/terraform-provider-stripe/internal/stripeapi"
	"github.com/computomatic/terraform-provider-stripe/internal/wire"
)

// numberedTable is a person whose ID numbers Stripe lists by type, never with their value.
func numberedTable() []fields.Field {
	return []fields.Field{
		{Name: "id", Shape: fields.String, Response: true},
		{Name: "name", Shape: fields.String, Required: true, Create: true, Update: true, Response: true},
		{Name: "numbers", Shape: fields.ObjectList, Create: true, Update: true, Response: true, Fields: []fields.Field{
			{Name: "type", Shape: fields.String, Required: true, Create: true, Update: true, Response: true},
			{Name: "value", Shape: fields.String, Required: true, Create: true, Update: true},
		}},
	}
}

func numberedDeclaration() Declaration {
	return Declaration{
		TypeName:     "stripe_numbered",
		IDPrefix:     "num_",
		Paths:        Paths{Create: "/v1/numbered", Retrieve: "/v1/numbered/{id}", Update: "/v1/numbered/{id}", Delete: "/v1/numbered/{id}"},
		Delete:       Delete,
		MatchOnRead:  map[string]string{"numbers": "type"},
		RefuseChange: []string{"numbers"},
	}
}

// answering is a client that answers every request with the body body points at.
func answering(body *wire.Object) Client {
	return clientFunc(func(context.Context, stripeapi.Request) (*stripeapi.Response, error) {
		return &stripeapi.Response{Body: *body}, nil
	})
}

// numberedBody is the object Stripe returns with numbers of the given types.
func numberedBody(types ...string) wire.Object {
	numbers := wire.Array{}
	for _, typ := range types {
		numbers = append(numbers, wire.Object{{Name: "type", Value: wire.String(typ)}})
	}
	return wire.Object{{Name: "id", Value: wire.String("num_1")}, {Name: "name", Value: wire.String("n")}, {Name: "numbers", Value: numbers}}
}

// numbers is the list value of the numbers given as type and value pairs; an empty value is
// null.
func numbers(r *Resource, pairs ...string) attr.Value {
	elemType := r.objType.AttrTypes["numbers"].(basetypes.ListType).ElemType
	elems := []attr.Value{}
	for i := 0; i < len(pairs); i += 2 {
		value := types.StringNull()
		if pairs[i+1] != "" {
			value = types.StringValue(pairs[i+1])
		}
		elems = append(elems, object(elemType, map[string]attr.Value{"type": sv(pairs[i]), "value": value}))
	}
	return types.ListValueMust(elemType, elems)
}

func numbered(r *Resource, list attr.Value) basetypes.ObjectValue {
	return r.fill(map[string]attr.Value{"id": sv("num_1"), "name": sv("n"), "numbers": list})
}

// Stripe lists a `us_ssn` before the configured `us_ssn_last_4` once it verifies a person.
// The refresh pairs by type, so the configured number keeps its value, the added one stays out
// of the state, and the unchanged configuration plans no change and no refusal. The control, a
// list paired by position, gives the value to the added number and is refused.
func TestMatchOnReadKeepsTheValueWhenStripeAddsANumber(t *testing.T) {
	body := numberedBody("us_ssn", "us_ssn_last_4")
	r := configured(t, numberedDeclaration(), numberedTable(), answering(&body))
	cfg := numbered(r, numbers(r, "us_ssn_last_4", "0000"))

	refreshed, _ := r.read(t, cfg)
	if got := refreshed.Attributes()["numbers"]; !got.Equal(numbers(r, "us_ssn_last_4", "0000")) {
		t.Errorf("the refresh must hold the configured number with its value: %v", got)
	}
	if resp := r.modifyPlan(t, cfg, refreshed); resp.Diagnostics.HasError() {
		t.Errorf("an unchanged configuration must plan: %v", resp.Diagnostics)
	}

	decl := numberedDeclaration()
	decl.MatchOnRead = nil
	positional := configured(t, decl, numberedTable(), answering(&body))
	refreshed, _ = positional.read(t, cfg)
	if got := refreshed.Attributes()["numbers"]; !got.Equal(numbers(positional, "us_ssn", "0000", "us_ssn_last_4", "")) {
		t.Errorf("the control: a positional list moves the value: %v", got)
	}
	if resp := positional.modifyPlan(t, cfg, refreshed); !resp.Diagnostics.HasError() {
		t.Error("the control: the positional refresh must be refused")
	}
}

// A configured number Stripe no longer lists leaves the state, so the plan shows the change
// and refuses it. A value the configuration changes is refused too.
func TestMatchOnReadShowsADroppedNumber(t *testing.T) {
	body := numberedBody("us_ssn")
	r := configured(t, numberedDeclaration(), numberedTable(), answering(&body))
	cfg := numbered(r, numbers(r, "us_ssn_last_4", "0000"))

	refreshed, _ := r.read(t, cfg)
	if got := refreshed.Attributes()["numbers"]; !got.Equal(numbers(r)) {
		t.Errorf("a dropped number must leave the state: %v", got)
	}
	if resp := r.modifyPlan(t, cfg, refreshed); !resp.Diagnostics.HasError() {
		t.Error("the dropped number must be refused at plan time")
	}
	if resp := r.modifyPlan(t, numbered(r, numbers(r, "us_ssn_last_4", "1111")), cfg); !resp.Diagnostics.HasError() {
		t.Error("a changed value must be refused at plan time")
	}
}

// clientProvider is planProvider with a client, so that its resource can call Stripe.
type clientProvider struct {
	planProvider
	client Client
}

func (p clientProvider) Configure(_ context.Context, _ provider.ConfigureRequest, resp *provider.ConfigureResponse) {
	resp.ResourceData = &ProviderData{Client: p.client}
}

// numberTypes reads the types of a protocol state's numbers, or nil for a null list.
func numberTypes(t *testing.T, typ tftypes.Type, state *tfprotov6.DynamicValue) []string {
	t.Helper()
	v, err := state.Unmarshal(typ)
	if err != nil {
		t.Fatal(err)
	}
	var attrs map[string]tftypes.Value
	var list []tftypes.Value
	if err := v.As(&attrs); err != nil || attrs["numbers"].As(&list) != nil {
		t.Fatalf("reading %v: %v", v, err)
	}
	var out []string
	for _, e := range list {
		var number map[string]tftypes.Value
		var typ string
		if err := e.As(&number); err != nil || number["type"].As(&typ) != nil {
			t.Fatalf("reading %v: %v", e, err)
		}
		out = append(out, typ)
	}
	return out
}

// An import has no numbers yet, and the read that follows it, which the import marks in the
// private state, adopts every number Stripe lists, without values. It clears the mark, so a
// later read of a state with no numbers, like one of an object created without them, adopts
// none.
func TestMatchOnReadImportAdoptsEveryNumber(t *testing.T) {
	ctx := context.Background()
	body := numberedBody("us_ssn", "us_ssn_last_4")
	ctor, err := NewResource(numberedDeclaration(), numberedTable())
	if err != nil {
		t.Fatal(err)
	}
	server := providerserver.NewProtocol6(clientProvider{planProvider{ctor: ctor}, answering(&body)})()
	schemas, err := server.GetProviderSchema(ctx, &tfprotov6.GetProviderSchemaRequest{})
	if err != nil {
		t.Fatal(err)
	}
	typ := schemas.ResourceSchemas["stripe_numbered"].ValueType()
	config, err := tfprotov6.NewDynamicValue(tftypes.Object{AttributeTypes: map[string]tftypes.Type{}}, tftypes.NewValue(tftypes.Object{AttributeTypes: map[string]tftypes.Type{}}, map[string]tftypes.Value{}))
	if err != nil {
		t.Fatal(err)
	}
	if resp, err := server.ConfigureProvider(ctx, &tfprotov6.ConfigureProviderRequest{Config: &config}); err != nil || len(resp.Diagnostics) > 0 {
		t.Fatalf("configure: %v %v", err, resp)
	}
	imported, err := server.ImportResourceState(ctx, &tfprotov6.ImportResourceStateRequest{TypeName: "stripe_numbered", ID: "num_1"})
	if err != nil || len(imported.Diagnostics) > 0 || len(imported.ImportedResources) != 1 {
		t.Fatalf("import: %v %+v", err, imported)
	}
	read := func(state *tfprotov6.DynamicValue, private []byte) *tfprotov6.ReadResourceResponse {
		t.Helper()
		resp, err := server.ReadResource(ctx, &tfprotov6.ReadResourceRequest{TypeName: "stripe_numbered", CurrentState: state, Private: private})
		if err != nil || len(resp.Diagnostics) > 0 {
			t.Fatalf("read: %v %+v", err, resp.Diagnostics)
		}
		return resp
	}
	first := read(imported.ImportedResources[0].State, imported.ImportedResources[0].Private)
	if got := numberTypes(t, typ, first.NewState); strings.Join(got, ",") != "us_ssn,us_ssn_last_4" {
		t.Errorf("an import adopts every number: %v", got)
	}
	if got := numberTypes(t, typ, read(imported.ImportedResources[0].State, first.Private).NewState); got != nil {
		t.Errorf("a read after the import's adopts no number into a state without numbers: %v", got)
	}
}

// A state without numbers, of an object created without them, never takes a number Stripe
// lists later, on a create, a read or an update, so the unset configuration plans no change.
func TestMatchOnReadNullListHoldsNoNumber(t *testing.T) {
	body := numberedBody("us_ssn")
	r := configured(t, numberedDeclaration(), numberedTable(), answering(&body))
	none := numbered(r, types.ListNull(r.objType.AttrTypes["numbers"].(basetypes.ListType).ElemType))
	created, _ := r.create(t, r.fill(map[string]attr.Value{"name": sv("n")}))
	refreshed, _ := r.read(t, none)
	updated := r.update(t, none, none)
	for name, got := range map[string]basetypes.ObjectValue{"create": created, "read": refreshed, "update": updated} {
		if !got.Attributes()["numbers"].IsNull() {
			t.Errorf("%s must hold no number: %v", name, got.Attributes()["numbers"])
		}
	}
	if resp := r.modifyPlan(t, none, refreshed); resp.Diagnostics.HasError() {
		t.Errorf("the unset configuration must plan: %v", resp.Diagnostics)
	}
}

// A list Stripe fills in, which the plan has unknown, keeps every element Stripe returns.
func TestMatchOnReadUnknownListKeepsEveryNumber(t *testing.T) {
	body := numberedBody("us_ssn")
	decl := numberedDeclaration()
	decl.RefuseChange = nil
	decl.ServerDefaulted = []string{"numbers"}
	r := configured(t, decl, numberedTable(), answering(&body))
	created, _ := r.create(t, r.fill(map[string]attr.Value{"name": sv("n")}))
	if got := created.Attributes()["numbers"]; !got.Equal(numbers(r, "us_ssn", "")) {
		t.Errorf("an unknown list keeps what Stripe returns: %v", got)
	}
}

// An update whose configuration adds a number and drops another pairs each number Stripe lists
// with the planned one of its type, so every value stays with its own number, the dropped
// number leaves the state and a number Stripe adds stays out.
func TestMatchOnReadUpdateKeepsValuesWithTheirNumbers(t *testing.T) {
	body := numberedBody("us_ssn", "us_itin", "us_ssn_last_4")
	r := configured(t, numberedDeclaration(), numberedTable(), answering(&body))
	state := numbered(r, numbers(r, "us_ein", "111111111", "us_ssn_last_4", "0000"))
	plan := numbered(r, numbers(r, "us_ssn_last_4", "0000", "us_itin", "900000000"))
	got := r.update(t, plan, state).Attributes()["numbers"]
	if !got.Equal(numbers(r, "us_ssn_last_4", "0000", "us_itin", "900000000")) {
		t.Errorf("the values must stay with their numbers: %v", got)
	}
}

// A prior element whose key is not known holds no key to keep, so every element Stripe lists
// stays, and the element with no key takes the one nothing else claimed.
func TestMatchOnReadKeepsEveryNumberAfterAKeylessOne(t *testing.T) {
	body := numberedBody("us_ssn_last_4", "us_ssn")
	r := configured(t, numberedDeclaration(), numberedTable(), answering(&body))
	elemType := r.objType.AttrTypes["numbers"].(basetypes.ListType).ElemType
	keyless := types.ListValueMust(elemType, []attr.Value{
		object(elemType, map[string]attr.Value{"type": sv("us_ssn"), "value": sv("000000000")}),
		object(elemType, map[string]attr.Value{"type": types.StringNull(), "value": sv("0000")}),
	})
	refreshed, _ := r.read(t, numbered(r, keyless))
	if got := refreshed.Attributes()["numbers"]; !got.Equal(numbers(r, "us_ssn", "000000000", "us_ssn_last_4", "0000")) {
		t.Errorf("every number must stay: %v", got)
	}
}

// A create that Stripe answers with a number besides the configured ones holds the configured
// ones only, with their values, so the apply is consistent with its plan.
func TestMatchOnReadCreateHoldsThePlannedNumbers(t *testing.T) {
	body := numberedBody("us_ssn", "us_ssn_last_4")
	r := configured(t, numberedDeclaration(), numberedTable(), answering(&body))
	plan := r.fill(map[string]attr.Value{"name": sv("n"), "numbers": numbers(r, "us_ssn_last_4", "0000")})
	state, _ := r.create(t, plan)
	if got := state.Attributes()["numbers"]; !got.Equal(numbers(r, "us_ssn_last_4", "0000")) {
		t.Errorf("create must hold the planned number with its value: %v", got)
	}
}

// A plan that lists a type twice is refused: the numbers could not be told apart.
func TestMatchOnReadRefusesADuplicateKey(t *testing.T) {
	body := numberedBody()
	r := configured(t, numberedDeclaration(), numberedTable(), answering(&body))
	plan := r.fill(map[string]attr.Value{"name": sv("n"), "numbers": numbers(r, "us_ssn_last_4", "0000", "us_ssn_last_4", "1111")})
	none := types.ObjectNull(r.objType.AttrTypes)
	resp := r.modifyPlan(t, plan, none)
	if !resp.Diagnostics.HasError() || !strings.Contains(resp.Diagnostics.Errors()[0].Detail(), "type `us_ssn_last_4` more than once") {
		t.Errorf("a duplicate type must be refused: %v", resp.Diagnostics)
	}
	plan = r.fill(map[string]attr.Value{"name": sv("n"), "numbers": numbers(r, "us_ssn_last_4", "0000", "us_ssn", "000000000")})
	if resp := r.modifyPlan(t, plan, none); resp.Diagnostics.HasError() {
		t.Errorf("the control: distinct types plan: %v", resp.Diagnostics)
	}
}

// A MatchOnRead entry must name a top-level list of objects that is not a MatchBy list, and a
// string field of its elements that Stripe returns.
func TestMatchOnReadMustNameAReturnedKey(t *testing.T) {
	for _, tc := range []struct {
		name  string
		entry map[string]string
		by    map[string]string
		want  string
	}{
		{"not a list", map[string]string{"name": "type"}, nil, "not a top-level list of objects"},
		{"key never returned", map[string]string{"numbers": "value"}, nil, "not a string field Stripe returns"},
		{"no such key", map[string]string{"numbers": "kind"}, nil, "not a string field Stripe returns"},
		{"match_by too", map[string]string{"numbers": "type"}, map[string]string{"numbers": "type"}, "is a match_by list too"},
	} {
		decl := numberedDeclaration()
		decl.MatchOnRead, decl.MatchBy = tc.entry, tc.by
		table := numberedTable()
		// A MatchBy list needs an updatable `id` in its elements.
		table[2].Fields = append(table[2].Fields, fields.Field{Name: "id", Shape: fields.String, Update: true, Response: true})
		if _, err := NewResource(decl, table); err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Errorf("%s: %v, want %q", tc.name, err, tc.want)
		}
	}
	if _, err := NewResource(numberedDeclaration(), numberedTable()); err != nil {
		t.Errorf("the control: %v", err)
	}
}
