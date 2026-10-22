package engine

import (
	"context"
	"strings"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/datasource"
	dschema "github.com/hashicorp/terraform-plugin-framework/datasource/schema"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-framework/types/basetypes"

	"github.com/computomatic/terraform-provider-stripe/internal/fields"
	"github.com/computomatic/terraform-provider-stripe/internal/stripeapi"
	"github.com/computomatic/terraform-provider-stripe/internal/wire"
)

func vaultTable() []fields.Field {
	return []fields.Field{
		{Name: "id", Shape: fields.String, Response: true},
		{Name: "name", Shape: fields.String, Response: true},
		{Name: "payload", Shape: fields.String, Response: true},
		{Name: "scope", Shape: fields.Object, Response: true, Fields: []fields.Field{
			{Name: "type", Shape: fields.String, Response: true},
			{Name: "user", Shape: fields.String, Response: true},
		}},
		{Name: "window", Shape: fields.Int, Response: true},
		{Name: "tags", Shape: fields.ScalarList, Elem: fields.String, Response: true},
	}
}

func vaultDeclaration() Declaration {
	return Declaration{
		TypeName: "stripe_vault_secret",
		Paths:    Paths{Retrieve: "/v1/vault/find"},
		Delete:   NoDelete,
		Expand:   []string{"payload"},
		FindBy:   []FindParam{{Name: "name"}, {Name: "scope"}},
	}
}

func findDataSource(t *testing.T, decl Declaration, table []fields.Field, client Client) *DataSource {
	t.Helper()
	ctor, err := NewDataSource(decl, table)
	if err != nil {
		t.Fatal(err)
	}
	ds := ctor().(*DataSource)
	var resp datasource.ConfigureResponse
	ds.Configure(context.Background(), datasource.ConfigureRequest{ProviderData: client}, &resp)
	noErrors(t, resp.Diagnostics)
	return ds
}

// configOf builds a configuration from the values given, the rest null.
func (d *DataSource) configOf(set map[string]attr.Value) basetypes.ObjectValue {
	values := make(map[string]attr.Value, len(d.objType.AttrTypes))
	for name, typ := range d.objType.AttrTypes {
		if v, ok := set[name]; ok {
			values[name] = v
		} else {
			values[name] = nullOf(typ)
		}
	}
	return types.ObjectValueMust(d.objType.AttrTypes, values)
}

// A secret is read by its name and scope: both are Required, `id` is only an output and no
// lookup is asked for, the request sends the configured values with the expansion, and the
// state keeps them.
func TestFindByReadsTheObjectFromTheRetrievePath(t *testing.T) {
	var got stripeapi.Request
	ds := findDataSource(t, vaultDeclaration(), vaultTable(), clientFunc(func(_ context.Context, req stripeapi.Request) (*stripeapi.Response, error) {
		got = req
		name, _ := req.Params.Get("name")
		if name != wire.String("api") {
			return nil, &stripeapi.APIError{Status: 404, Type: "invalid_request_error", Code: "resource_missing"}
		}
		return &stripeapi.Response{Body: wire.Object{
			{Name: "id", Value: wire.String("sec_1")},
			{Name: "name", Value: wire.String("api")},
			{Name: "payload", Value: wire.String("hunter2")},
			{Name: "scope", Value: wire.Object{{Name: "type", Value: wire.String("account")}}},
		}}, nil
	}))

	for name, want := range map[string]bool{"name": true, "scope": true} {
		a := ds.schema.Attributes[name]
		if a.IsRequired() != want {
			t.Errorf("%s: required %v", name, a.IsRequired())
		}
	}
	if id := ds.schema.Attributes["id"]; id.IsOptional() || !id.IsComputed() {
		t.Errorf("id must be an output only: %#v", id)
	}
	scope := ds.schema.Attributes["scope"].(dschema.SingleNestedAttribute)
	if user := scope.Attributes["user"]; !user.IsOptional() || user.IsComputed() {
		t.Errorf("a field of an object parameter is Optional: %#v", user)
	}

	scopeType := map[string]attr.Type{"type": types.StringType, "user": types.StringType}
	account := types.ObjectValueMust(scopeType, map[string]attr.Value{"type": types.StringValue("account"), "user": types.StringNull()})
	config := ds.configOf(map[string]attr.Value{"name": types.StringValue("api"), "scope": account})
	var vr datasource.ValidateConfigResponse
	ds.ValidateConfig(context.Background(), datasource.ValidateConfigRequest{Config: tfsdk.Config{Schema: ds.schema, Raw: raw(t, config)}}, &vr)
	noErrors(t, vr.Diagnostics)
	state, resp := ds.read(t, config)
	noErrors(t, resp.Diagnostics)
	if got.Method != "GET" || got.Path != "/v1/vault/find" {
		t.Errorf("request: %s %s", got.Method, got.Path)
	}
	want := wire.Object{
		{Name: "name", Value: wire.String("api")},
		{Name: "scope", Value: wire.Object{{Name: "type", Value: wire.String("account")}}},
		{Name: "expand", Value: wire.Array{wire.String("payload")}},
	}
	if wireOf(t, got.Params) != wireOf(t, want) {
		t.Errorf("params: %v", got.Params)
	}
	if str(t, state, "id") != "sec_1" || str(t, state, "payload") != "hunter2" || !state.Attributes()["scope"].Equal(account) {
		t.Errorf("state: %v", state)
	}

	_, resp = ds.read(t, ds.configOf(map[string]attr.Value{"name": types.StringValue("gone"), "scope": account}))
	if !resp.Diagnostics.HasError() || !strings.Contains(resp.Diagnostics.Errors()[0].Detail(), "no stripe_vault_secret found for name=gone, scope.type=account") {
		t.Errorf("want the not found error: %v", resp.Diagnostics)
	}
}

func summaryDeclaration() Declaration {
	return Declaration{
		TypeName: "stripe_gauge_summary",
		Parent:   "gauge",
		Paths:    Paths{List: "/v1/gauges/{gauge}/summaries"},
		Delete:   NoDelete,
		FindBy:   []FindParam{{Name: "customer", Description: "The customer."}, {Name: "window"}},
	}
}

// A summary is listed under its gauge by a customer the object does not carry and a window
// it does. The list must hold exactly one.
func TestFindByReadsTheOneObjectTheListHolds(t *testing.T) {
	var got stripeapi.Request
	count := 1
	ds := findDataSource(t, summaryDeclaration(), vaultTable(), clientFunc(func(_ context.Context, req stripeapi.Request) (*stripeapi.Response, error) {
		got = req
		var data wire.Array
		for range count {
			data = append(data, wire.Object{{Name: "id", Value: wire.String("sum_1")}, {Name: "window", Value: wire.Int(99)}})
		}
		return &stripeapi.Response{Body: wire.Object{{Name: "data", Value: data}, {Name: "has_more", Value: wire.Bool(false)}}}, nil
	}))
	customer, ok := ds.schema.Attributes["customer"].(dschema.StringAttribute)
	if !ok || !customer.Required || customer.MarkdownDescription != "The customer." {
		t.Errorf("customer: %#v", ds.schema.Attributes["customer"])
	}
	if w, ok := ds.schema.Attributes["window"].(dschema.Int64Attribute); !ok || !w.Required {
		t.Errorf("window: %#v", ds.schema.Attributes["window"])
	}

	config := ds.configOf(map[string]attr.Value{"gauge": types.StringValue("g_1"), "customer": types.StringValue("cus_1"), "window": types.Int64Value(60)})
	state, resp := ds.read(t, config)
	noErrors(t, resp.Diagnostics)
	want := wire.Object{
		{Name: "customer", Value: wire.String("cus_1")},
		{Name: "window", Value: wire.Int(60)},
		{Name: "limit", Value: wire.Int(listLimit)},
	}
	if got.Path != "/v1/gauges/g_1/summaries" || wireOf(t, got.Params) != wireOf(t, want) {
		t.Errorf("request: %s %v", got.Path, got.Params)
	}
	if str(t, state, "id") != "sum_1" || str(t, state, "customer") != "cus_1" || str(t, state, "gauge") != "g_1" {
		t.Errorf("state: %v", state)
	}
	if w := state.Attributes()["window"]; !w.Equal(types.Int64Value(60)) {
		t.Errorf("the window keeps the configured value, got %v", w)
	}

	for n, want := range map[int]string{0: "no stripe_gauge_summary found for customer=cus_1, window=60", 2: "stripe_gauge_summary matches 2 objects for customer=cus_1, window=60"} {
		count = n
		_, resp = ds.read(t, config)
		if !resp.Diagnostics.HasError() || resp.Diagnostics.Errors()[0].Detail() != want {
			t.Errorf("%d listed: want %q, got %v", n, want, resp.Diagnostics)
		}
	}
}

// A find declaration reads by parameters only, and each parameter must be one the engine can
// send.
func TestFindByDeclarationRules(t *testing.T) {
	for name, edit := range map[string]func(*Declaration){
		"both paths": func(d *Declaration) { d.Paths.List = "/v1/vault" },
		"no path":    func(d *Declaration) { d.Paths.Retrieve = "" },
		"a lookup": func(d *Declaration) {
			d.Lookups = []Lookup{{Field: "name"}}
			d.Paths.List = "/v1/vault"
			d.Paths.Retrieve = ""
		},
		"a delete strategy": func(d *Declaration) { d.Delete = Delete },
		"found in a list": func(d *Declaration) {
			d.FindInList = true
			d.Parent = "owner"
			d.Paths = Paths{List: "/v1/o/{owner}/v"}
		},
		"a list parameter": func(d *Declaration) { d.FindBy = append(d.FindBy, FindParam{Name: "tags"}) },
		"the id":           func(d *Declaration) { d.FindBy = append(d.FindBy, FindParam{Name: "id"}) },
		"no description":   func(d *Declaration) { d.FindBy = append(d.FindBy, FindParam{Name: "customer"}) },
		"the parent's name": func(d *Declaration) {
			d.Parent = "owner"
			d.FindBy = append(d.FindBy, FindParam{Name: "owner", Description: "x"})
		},
		"a nested object field": func(d *Declaration) { d.FindBy = []FindParam{{Name: "scope.type"}} },
	} {
		decl := vaultDeclaration()
		edit(&decl)
		if _, err := NewDataSource(decl, vaultTable()); err == nil {
			t.Errorf("%s: want an error", name)
		}
	}
	if _, err := NewDataSource(summaryDeclaration(), vaultTable()); err != nil {
		t.Errorf("a list with a parent and a parameter the table lacks: %v", err)
	}
}
