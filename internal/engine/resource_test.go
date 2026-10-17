package engine

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-framework/types/basetypes"
	"github.com/hashicorp/terraform-plugin-go/tftypes"

	"github.com/computomatic/terraform-provider-stripe/internal/stripeapi"
	"github.com/computomatic/terraform-provider-stripe/internal/wire"
)

type clientFunc func(ctx context.Context, req stripeapi.Request) (*stripeapi.Response, error)

func (f clientFunc) Do(ctx context.Context, req stripeapi.Request) (*stripeapi.Response, error) {
	return f(ctx, req)
}

func configureResource(t *testing.T, r *Resource, client Client) {
	t.Helper()
	var resp resource.ConfigureResponse
	r.Configure(context.Background(), resource.ConfigureRequest{ProviderData: client}, &resp)
	noErrors(t, resp.Diagnostics)
}

func noErrors(t *testing.T, diags diag.Diagnostics) {
	t.Helper()
	if diags.HasError() {
		t.Fatalf("unexpected diagnostics: %v", diags)
	}
}

func raw(t *testing.T, v basetypes.ObjectValue) tftypes.Value {
	t.Helper()
	tv, err := v.ToTerraformValue(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	return tv
}

func (r *Resource) create(t *testing.T, plan basetypes.ObjectValue) (basetypes.ObjectValue, resource.CreateResponse) {
	t.Helper()
	ctx := context.Background()
	identity := r.m.identitySchema()
	resp := resource.CreateResponse{
		State:    tfsdk.State{Schema: r.schema, Raw: tftypes.NewValue(r.schema.Type().TerraformType(ctx), nil)},
		Identity: &tfsdk.ResourceIdentity{Schema: identity, Raw: tftypes.NewValue(identity.Type().TerraformType(ctx), nil)},
	}
	r.Create(ctx, resource.CreateRequest{Plan: tfsdk.Plan{Schema: r.schema, Raw: raw(t, plan)}}, &resp)
	noErrors(t, resp.Diagnostics)
	var state basetypes.ObjectValue
	noErrors(t, resp.State.Get(ctx, &state))
	return state, resp
}

func (r *Resource) read(t *testing.T, state basetypes.ObjectValue) (basetypes.ObjectValue, bool) {
	t.Helper()
	ctx := context.Background()
	resp := resource.ReadResponse{State: tfsdk.State{Schema: r.schema, Raw: raw(t, state)}}
	r.Read(ctx, resource.ReadRequest{State: tfsdk.State{Schema: r.schema, Raw: raw(t, state)}}, &resp)
	noErrors(t, resp.Diagnostics)
	if resp.State.Raw.IsNull() {
		return basetypes.ObjectValue{}, false
	}
	var next basetypes.ObjectValue
	noErrors(t, resp.State.Get(ctx, &next))
	return next, true
}

func (r *Resource) update(t *testing.T, plan, state basetypes.ObjectValue) basetypes.ObjectValue {
	t.Helper()
	ctx := context.Background()
	resp := resource.UpdateResponse{State: tfsdk.State{Schema: r.schema, Raw: raw(t, state)}}
	r.Update(ctx, resource.UpdateRequest{
		Plan:  tfsdk.Plan{Schema: r.schema, Raw: raw(t, plan)},
		State: tfsdk.State{Schema: r.schema, Raw: raw(t, state)},
	}, &resp)
	noErrors(t, resp.Diagnostics)
	var next basetypes.ObjectValue
	noErrors(t, resp.State.Get(ctx, &next))
	return next
}

func (r *Resource) destroy(t *testing.T, state basetypes.ObjectValue) diag.Diagnostics {
	t.Helper()
	var resp resource.DeleteResponse
	r.Delete(context.Background(), resource.DeleteRequest{State: tfsdk.State{Schema: r.schema, Raw: raw(t, state)}}, &resp)
	return resp.Diagnostics
}

func str(t *testing.T, obj basetypes.ObjectValue, name string) string {
	t.Helper()
	s, ok := obj.Attributes()[name].(basetypes.StringValue)
	if !ok {
		t.Fatalf("%s is %T", name, obj.Attributes()[name])
	}
	return s.ValueString()
}

func wireStrings(v []string) string { return strings.Join(v, ",") }

func wireOf(t *testing.T, o wire.Object) string {
	t.Helper()
	b, err := wire.EncodeJSON(o)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func createPlan(r *Resource) basetypes.ObjectValue {
	addrType := r.objType.AttrTypes["address"]
	return r.fill(map[string]attr.Value{
		"name":    types.StringValue("w"),
		"kind":    types.StringValue("a"),
		"address": object(addrType, map[string]attr.Value{"line1": types.StringValue("x"), "zip": types.StringValue("1")}),
		"events":  types.SetValueMust(types.StringType, strs("b", "a")),
		"metadata": stringMapValue(map[string]string{
			"a": "1",
		}),
		"url":         types.StringValue("https://configured.example"),
		"passcode":    types.StringValue("pw"),
		"default_ref": types.StringValue("ref_1"),
	})
}

func TestResourceLifecycle(t *testing.T) {
	stub := newStub()
	r := fixtureResource(t, stub)

	// Create sends what Stripe takes on create, then the update-only field in a follow-up.
	state, resp := r.create(t, createPlan(r))
	reqs := stub.requests()
	if len(reqs) != 2 || reqs[0].Method != "POST" || reqs[1].Method != "POST" {
		t.Fatalf("requests: %+v", reqs)
	}
	if got := wireOf(t, reqs[0].Params); got != `{"name":"w","kind":"a","address":{"line1":"x","zip":"1"},"events":["b","a"],"metadata":{"a":"1"},"url":"https://configured.example","passcode":"pw"}` {
		t.Errorf("create body: %s", got)
	}
	if got := wireOf(t, reqs[1].Params); got != `{"default_ref":"ref_1"}` {
		t.Errorf("follow-up body: %s", got)
	}
	if reqs[0].Path != "/v1/widgets" || reqs[1].Path != "/v1/widgets/wid_1" {
		t.Errorf("paths: %s, %s", reqs[0].Path, reqs[1].Path)
	}
	if wireStrings(reqs[0].Sensitive) != "passcode,secret" {
		t.Errorf("sensitive names: %v", reqs[0].Sensitive)
	}

	if str(t, state, "id") != "wid_1" || str(t, state, "secret") != "whsec_wid_1" || str(t, state, "default_ref") != "ref_1" {
		t.Errorf("state: %v", state)
	}
	if str(t, state, "url") != "https://configured.example" {
		t.Errorf("a redacted response must not replace the configured url: %s", str(t, state, "url"))
	}
	if str(t, state, "passcode") != "pw" {
		t.Errorf("request-only passcode lost")
	}
	if !state.Attributes()["tags"].IsNull() || !state.Attributes()["items"].IsNull() || !state.Attributes()["quantity"].IsNull() {
		t.Errorf("empty or absent optional fields must stay null: %v", state)
	}
	if !state.Attributes()["active"].Equal(types.BoolValue(true)) || !state.Attributes()["provider_"].IsNull() {
		t.Errorf("computed fields: active=%v provider_=%v", state.Attributes()["active"], state.Attributes()["provider_"])
	}
	if events := state.Attributes()["events"]; events.IsNull() || len(events.(basetypes.SetValue).Elements()) != 2 {
		t.Errorf("events: %v", events)
	}
	var id string
	if diags := resp.Identity.GetAttribute(context.Background(), path.Root("id"), &id); diags.HasError() || id != "wid_1" {
		t.Errorf("identity id = %q, %v", id, diags)
	}

	// Read maps the response; drift appears; a field Stripe stopped returning keeps its
	// once-returned value.
	stub.mu.Lock()
	obj := stub.objects["wid_1"]
	obj = put(obj, "name", wire.String("renamed outside"))
	stub.objects["wid_1"] = drop(obj, "secret")
	stub.mu.Unlock()
	drifted, found := r.read(t, state)
	if !found || str(t, drifted, "name") != "renamed outside" {
		t.Fatalf("drift not read: %v", drifted)
	}
	if str(t, drifted, "secret") != "whsec_wid_1" {
		t.Errorf("once-returned secret not carried forward")
	}

	// Update sends only changed updatable fields, metadata as a key diff, and never a
	// create-only field.
	stub.reset()
	planAttrs := map[string]attr.Value{}
	for k, v := range state.Attributes() {
		planAttrs[k] = v
	}
	planAttrs["name"] = types.StringValue("w2")
	planAttrs["metadata"] = stringMapValue(map[string]string{"b": "2"})
	planAttrs["active"] = types.BoolUnknown()
	planAttrs["default_ref"] = types.StringUnknown()
	planAttrs["provider_"] = types.StringUnknown()
	updated := r.update(t, types.ObjectValueMust(r.objType.AttrTypes, planAttrs), state)
	reqs = stub.requests()
	if len(reqs) != 1 || reqs[0].Method != "POST" || reqs[0].Path != "/v1/widgets/wid_1" {
		t.Fatalf("update requests: %+v", reqs)
	}
	if got := wireOf(t, reqs[0].Params); got != `{"name":"w2","metadata":{"a":null,"b":"2"}}` {
		t.Errorf("update body: %s", got)
	}
	if str(t, updated, "name") != "w2" || str(t, updated, "secret") != "whsec_wid_1" {
		t.Errorf("updated state: %v", updated)
	}

	// Removing an Optional field sends the unset value.
	planAttrs = map[string]attr.Value{}
	for k, v := range updated.Attributes() {
		planAttrs[k] = v
	}
	planAttrs["events"] = nullOf(r.objType.AttrTypes["events"])
	stub.reset()
	r.update(t, types.ObjectValueMust(r.objType.AttrTypes, planAttrs), updated)
	if got := wireOf(t, stub.requests()[0].Params); got != `{"events":null}` {
		t.Errorf("unset body: %s", got)
	}

	// Destroy deletes; a later read removes the resource from state.
	stub.reset()
	noErrors(t, r.destroy(t, updated))
	if reqs = stub.requests(); len(reqs) != 1 || reqs[0].Method != "DELETE" {
		t.Fatalf("delete requests: %+v", reqs)
	}
	if _, found := r.read(t, updated); found {
		t.Error("a 404 must remove the resource from state")
	}
	noErrors(t, r.destroy(t, updated))
}

func TestResourceImport(t *testing.T) {
	stub := newStub()
	r := fixtureResource(t, stub)
	state, _ := r.create(t, createPlan(r))

	ctx := context.Background()
	resp := resource.ImportStateResponse{State: tfsdk.State{Schema: r.schema, Raw: tftypes.NewValue(r.schema.Type().TerraformType(ctx), nil)}}
	r.ImportState(ctx, resource.ImportStateRequest{ID: "wid_1"}, &resp)
	noErrors(t, resp.Diagnostics)
	var imported basetypes.ObjectValue
	noErrors(t, resp.State.Get(ctx, &imported))
	if str(t, imported, "id") != "wid_1" || !imported.Attributes()["name"].IsNull() {
		t.Fatalf("imported: %v", imported)
	}
	read, found := r.read(t, imported)
	if !found || str(t, read, "name") != str(t, state, "name") {
		t.Fatalf("read after import: %v", read)
	}
	if !read.Attributes()["passcode"].IsNull() {
		t.Error("a field Stripe never returns cannot be imported")
	}

	resp = resource.ImportStateResponse{State: tfsdk.State{Schema: r.schema, Raw: tftypes.NewValue(r.schema.Type().TerraformType(ctx), nil)}}
	r.ImportState(ctx, resource.ImportStateRequest{ID: "other_1"}, &resp)
	if len(resp.Diagnostics.Warnings()) != 1 {
		t.Errorf("an ID without the prefix must warn: %v", resp.Diagnostics)
	}
}

func TestImportWithParent(t *testing.T) {
	decl := fixtureDeclaration()
	decl.Parent = "owner"
	decl.Paths = Paths{Create: "/v1/owners/{owner}/widgets", Retrieve: "/v1/owners/{owner}/widgets/{id}", Delete: "/v1/owners/{owner}/widgets/{id}", List: "/v1/owners/{owner}/widgets"}
	r := configured(t, decl, fixtureTable(), newStub())

	ctx := context.Background()
	empty := func() resource.ImportStateResponse {
		return resource.ImportStateResponse{State: tfsdk.State{Schema: r.schema, Raw: tftypes.NewValue(r.schema.Type().TerraformType(ctx), nil)}}
	}
	resp := empty()
	r.ImportState(ctx, resource.ImportStateRequest{ID: "own_1/wid_9"}, &resp)
	noErrors(t, resp.Diagnostics)
	var imported basetypes.ObjectValue
	noErrors(t, resp.State.Get(ctx, &imported))
	if str(t, imported, "owner") != "own_1" || str(t, imported, "id") != "wid_9" {
		t.Fatalf("imported: %v", imported)
	}
	if got := r.m.expand(decl.Paths.Retrieve, "wid_9", "own_1"); got != "/v1/owners/own_1/widgets/wid_9" {
		t.Errorf("expanded path: %s", got)
	}
	resp = empty()
	r.ImportState(ctx, resource.ImportStateRequest{ID: "wid_9"}, &resp)
	if !resp.Diagnostics.HasError() {
		t.Error("an import ID without a parent must be rejected")
	}
}

func TestArchiveStrategy(t *testing.T) {
	decl := fixtureDeclaration()
	decl.Delete = Archive
	decl.Archive = &ArchiveAction{Field: "active", Value: wire.Bool(false)}
	stub := newStub()
	r := configured(t, decl, fixtureTable(), stub)
	state, _ := r.create(t, createPlan(r))

	stub.reset()
	noErrors(t, r.destroy(t, state))
	reqs := stub.requests()
	if len(reqs) != 2 || reqs[0].Method != "GET" || reqs[1].Method != "POST" || wireOf(t, reqs[1].Params) != `{"active":false}` {
		t.Fatalf("archive requests: %+v", reqs)
	}

	stub.reset()
	noErrors(t, r.destroy(t, state))
	if reqs = stub.requests(); len(reqs) != 1 || reqs[0].Method != "GET" {
		t.Fatalf("an archived object must not be archived again: %+v", reqs)
	}

	// Read keeps an archived object in state and shows the change as drift on `active`.
	read, found := r.read(t, state)
	if !found || !read.Attributes()["active"].Equal(types.BoolValue(false)) {
		t.Fatalf("archived read: %v", read)
	}
}

func TestArchiveNow(t *testing.T) {
	now := time.Unix(1000, 0)
	a := &ArchiveAction{Field: "expires_at", Now: true}
	if archived(wire.Object{{Name: "expires_at", Value: wire.Int(2000)}}, a, now) {
		t.Error("a future expiry is not archived")
	}
	if !archived(wire.Object{{Name: "expires_at", Value: wire.Int(900)}}, a, now) {
		t.Error("a past expiry is archived")
	}
	if archived(wire.Object{{Name: "expires_at", Value: wire.Null{}}}, a, now) {
		t.Error("no expiry is not archived")
	}
}

func TestExpireStrategy(t *testing.T) {
	decl := fixtureDeclaration()
	decl.Delete = Expire
	decl.Expire = &ExpireAction{Field: "expires_at", Seconds: 5, StatusField: "status", StatusValue: "expired"}
	polls := 0
	var sent wire.Object
	client := clientFunc(func(_ context.Context, req stripeapi.Request) (*stripeapi.Response, error) {
		if req.Method == "POST" {
			sent = req.Params
			return &stripeapi.Response{Body: wire.Object{}}, nil
		}
		polls++
		status := "active"
		if polls >= 3 {
			status = "expired"
		}
		return &stripeapi.Response{Body: wire.Object{{Name: "status", Value: wire.String(status)}}}, nil
	})
	r := configured(t, decl, fixtureTable(), client)
	r.pollInterval = time.Millisecond

	state := r.fill(map[string]attr.Value{"id": types.StringValue("wid_1")})
	noErrors(t, r.destroy(t, state))
	at, ok := sent.Get("expires_at")
	if !ok || int64(at.(wire.Int)) < time.Now().Unix() {
		t.Errorf("expiry sent: %v", sent)
	}
	if polls < 3 {
		t.Errorf("did not wait for the status: %d reads", polls)
	}
}

func TestCreateFailureKeepsID(t *testing.T) {
	stub := newStub()
	failing := clientFunc(func(ctx context.Context, req stripeapi.Request) (*stripeapi.Response, error) {
		if req.Method == "POST" && len(req.Params) == 1 && req.Params[0].Name == "default_ref" {
			return nil, &stripeapi.APIError{Status: 400, Code: "parameter_invalid_empty", Param: "default_ref", Message: "bad"}
		}
		return stub.Do(ctx, req)
	})
	r := fixtureResource(t, failing)
	ctx := context.Background()
	resp := resource.CreateResponse{State: tfsdk.State{Schema: r.schema, Raw: tftypes.NewValue(r.schema.Type().TerraformType(ctx), nil)}}
	r.Create(ctx, resource.CreateRequest{Plan: tfsdk.Plan{Schema: r.schema, Raw: raw(t, createPlan(r))}}, &resp)
	if !resp.Diagnostics.HasError() {
		t.Fatal("expected the follow-up failure")
	}
	if resp.Diagnostics.Errors()[0].Summary() != "Stripe API error: parameter_invalid_empty" {
		t.Errorf("diagnostic: %v", resp.Diagnostics)
	}
	var id types.String
	resp.State.GetAttribute(ctx, path.Root("id"), &id)
	if id.ValueString() != "wid_1" {
		t.Errorf("the created object's ID must be in state so destroy can find it, got %q", id.ValueString())
	}
}

func deleteOrArchiveDeclaration() Declaration {
	decl := fixtureDeclaration()
	decl.Delete = DeleteOrArchive
	decl.Archive = &ArchiveAction{Field: "active", Value: wire.Bool(false)}
	decl.ArchiveOn = &Refusal{Status: 400, Type: "invalid_request_error"}
	return decl
}

// refusingDeletes answers DELETE with err and everything else from the stub.
func refusingDeletes(stub *stubStripe, err error) clientFunc {
	return func(ctx context.Context, req stripeapi.Request) (*stripeapi.Response, error) {
		if req.Method == "DELETE" {
			return nil, err
		}
		return stub.Do(ctx, req)
	}
}

func TestDeleteOrArchiveDeletesWhenStripeAllows(t *testing.T) {
	stub := newStub()
	r := configured(t, deleteOrArchiveDeclaration(), fixtureTable(), stub)
	state, _ := r.create(t, createPlan(r))
	stub.reset()
	diags := r.destroy(t, state)
	noErrors(t, diags)
	if len(diags.Warnings()) != 0 {
		t.Errorf("a plain delete must not warn: %v", diags)
	}
	if reqs := stub.requests(); len(reqs) != 1 || reqs[0].Method != "DELETE" {
		t.Fatalf("requests: %+v", reqs)
	}
}

func TestDeleteOrArchiveArchivesOnTheNamedRefusal(t *testing.T) {
	stub := newStub()
	refusal := &stripeapi.APIError{Status: 400, Type: "invalid_request_error", Message: "This product has prices."}
	r := configured(t, deleteOrArchiveDeclaration(), fixtureTable(), refusingDeletes(stub, refusal))
	state, _ := r.create(t, createPlan(r))
	stub.reset()
	diags := r.destroy(t, state)
	noErrors(t, diags)
	if len(diags.Warnings()) != 1 || diags.Warnings()[0].Summary() != "Archived instead of deleted" {
		t.Errorf("want one archive warning: %v", diags)
	}
	reqs := stub.requests()
	if len(reqs) != 2 || reqs[0].Method != "GET" || reqs[1].Method != "POST" || wireOf(t, reqs[1].Params) != `{"active":false}` {
		t.Fatalf("archive requests: %+v", reqs)
	}
}

func TestDeleteOrArchiveFailsOnOtherErrors(t *testing.T) {
	for name, apiErr := range map[string]*stripeapi.APIError{
		"another status":   {Status: 403, Type: "invalid_request_error", Message: "no"},
		"another type":     {Status: 400, Type: "api_error", Message: "no"},
		"a server failure": {Status: 500, Type: "api_error", Message: "no"},
	} {
		t.Run(name, func(t *testing.T) {
			stub := newStub()
			r := configured(t, deleteOrArchiveDeclaration(), fixtureTable(), refusingDeletes(stub, apiErr))
			state, _ := r.create(t, createPlan(r))
			stub.reset()
			if !r.destroy(t, state).HasError() {
				t.Error("the error must fail the destroy")
			}
			for _, req := range stub.requests() {
				if req.Method == "POST" {
					t.Errorf("nothing may be archived: %+v", req)
				}
			}
		})
	}
}

func TestDeleteOrArchiveTreatsMissingAsGone(t *testing.T) {
	stub := newStub()
	r := configured(t, deleteOrArchiveDeclaration(), fixtureTable(), refusingDeletes(stub, notFound()))
	state, _ := r.create(t, createPlan(r))
	noErrors(t, r.destroy(t, state))
}

func TestArchiveThroughAnActionEndpoint(t *testing.T) {
	decl := fixtureDeclaration()
	decl.Delete = Archive
	decl.Archive = &ArchiveAction{Field: "active", Value: wire.Bool(false), Path: "/v1/widgets/{id}/deactivate"}
	var calls []string
	client := clientFunc(func(_ context.Context, req stripeapi.Request) (*stripeapi.Response, error) {
		calls = append(calls, req.Method+" "+req.Path)
		return &stripeapi.Response{Body: wire.Object{{Name: "active", Value: wire.Bool(true)}}}, nil
	})
	r := configured(t, decl, fixtureTable(), client)
	state := r.fill(map[string]attr.Value{"id": types.StringValue("wid_1")})
	noErrors(t, r.destroy(t, state))
	if got := strings.Join(calls, ","); got != "GET /v1/widgets/wid_1,POST /v1/widgets/wid_1/deactivate" {
		t.Errorf("calls: %s", got)
	}
}

// Stripe sends once-returned fields on the create response only, so a follow-up update
// (the create plan sets an update-only field) must not replace them.
func TestCreateKeepsOnceReturnedFieldsAcrossFollowUp(t *testing.T) {
	stub := newStub()
	followUpOmitsSecret := clientFunc(func(ctx context.Context, req stripeapi.Request) (*stripeapi.Response, error) {
		resp, err := stub.Do(ctx, req)
		if err != nil || req.Method != "POST" || req.Path == "/v1/widgets" {
			return resp, err
		}
		body, _ := resp.Body.(wire.Object)
		return &stripeapi.Response{Status: resp.Status, Body: drop(append(wire.Object(nil), body...), "secret")}, nil
	})
	r := fixtureResource(t, followUpOmitsSecret)
	state, _ := r.create(t, createPlan(r))
	if got := str(t, state, "secret"); got != "whsec_wid_1" {
		t.Fatalf("secret after create with a follow-up update: %q", got)
	}
	var followUps int
	for _, req := range stub.requests() {
		if req.Method == "POST" && req.Path != "/v1/widgets" {
			followUps++
		}
	}
	if followUps != 1 {
		t.Fatalf("the plan sets default_ref, so create must send one follow-up update, got %d", followUps)
	}
}

func TestMergeBodies(t *testing.T) {
	got := mergeBodies(
		wire.Object{{Name: "a", Value: wire.Int(1)}, {Name: "b", Value: wire.Int(2)}},
		wire.Object{{Name: "b", Value: wire.Int(3)}, {Name: "c", Value: wire.Int(4)}},
	)
	if s := wireOf(t, got); s != `{"a":1,"b":3,"c":4}` {
		t.Errorf("merged: %s", s)
	}
}

func hasExpand(t *testing.T, req stripeapi.Request, want string) bool {
	t.Helper()
	v, ok := req.Params.Get("expand")
	if !ok {
		return false
	}
	b, err := wire.EncodeJSON(v)
	if err != nil {
		t.Fatal(err)
	}
	return string(b) == want
}

// A declared expand list rides on every request that returns the object.
func TestExpandIsSentOnEveryRequestThatReturnsTheObject(t *testing.T) {
	decl := fixtureDeclaration()
	decl.Expand = []string{"options"}
	stub := newStub()
	r := configured(t, decl, fixtureTable(), stub)
	state, _ := r.create(t, createPlan(r))
	r.read(t, state)
	plan := r.fill(map[string]attr.Value{})
	attrs := plan.Attributes()
	for k, v := range state.Attributes() {
		attrs[k] = v
	}
	attrs["name"] = types.StringValue("renamed")
	r.update(t, r.fill(attrs), state)
	// A plan with nothing to send reads instead; that read expands too.
	r.update(t, state, state)
	noErrors(t, r.destroy(t, state))

	for _, req := range stub.requests() {
		switch req.Method {
		case "DELETE":
			if _, sent := req.Params.Get("expand"); sent {
				t.Errorf("DELETE must not send expand: %+v", req)
			}
		default:
			if !hasExpand(t, req, `["options"]`) {
				t.Errorf("%s %s sent no expand: %+v", req.Method, req.Path, req.Params)
			}
		}
	}
}

func TestExpandMustNameAResponseField(t *testing.T) {
	for _, name := range []string{"nothing", "passcode"} {
		decl := fixtureDeclaration()
		decl.Expand = []string{name}
		if _, err := NewResource(decl, fixtureTable()); err == nil {
			t.Errorf("expand %q names no response field and must fail", name)
		}
	}
}

func TestNullAsDecodesNullToTheString(t *testing.T) {
	decl := fixtureDeclaration()
	decl.NullAs = map[string]string{"default_ref": "inf", "address.zip": "inf"}
	client := clientFunc(func(_ context.Context, req stripeapi.Request) (*stripeapi.Response, error) {
		return &stripeapi.Response{Body: wire.Object{
			{Name: "id", Value: wire.String("wid_1")},
			{Name: "default_ref", Value: wire.Null{}},
			{Name: "address", Value: wire.Object{{Name: "line1", Value: wire.String("x")}, {Name: "zip", Value: wire.Null{}}}},
		}}, nil
	})
	r := configured(t, decl, fixtureTable(), client)
	state := r.fill(map[string]attr.Value{"id": types.StringValue("wid_1")})
	read, found := r.read(t, state)
	if !found {
		t.Fatal("not found")
	}
	if got := str(t, read, "default_ref"); got != "inf" {
		t.Errorf("a null default_ref decodes to %q, want inf", got)
	}
	addr, _ := read.Attributes()["address"].(basetypes.ObjectValue)
	if got := str(t, addr, "zip"); got != "inf" {
		t.Errorf("a null address.zip decodes to %q, want inf", got)
	}

	decl.NullAs = map[string]string{"nothing": "inf"}
	if _, err := NewResource(decl, fixtureTable()); err == nil {
		t.Error("a null_as entry that names no field must fail")
	}
	decl.NullAs = map[string]string{"quantity": "inf"}
	if _, err := NewResource(decl, fixtureTable()); err == nil {
		t.Error("a null_as entry on a non-string field must fail")
	}
}
