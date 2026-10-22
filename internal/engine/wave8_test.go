package engine

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-framework/types/basetypes"

	"github.com/computomatic/terraform-provider-stripe/internal/fields"
	"github.com/computomatic/terraform-provider-stripe/internal/stripeapi"
	"github.com/computomatic/terraform-provider-stripe/internal/wire"
)

func transitionTable() []fields.Field {
	return append(fixtureTable(), fields.Field{Name: "phase", Shape: fields.String, Response: true, Enum: []string{"off", "on", "gone"}})
}

func transitionDeclaration() Declaration {
	decl := fixtureDeclaration()
	decl.Transitions = []Transition{
		{Field: "phase", Value: "on", Path: "/v1/widgets/{id}/start"},
		{Field: "phase", Value: "off", Path: "/v1/widgets/{id}/stop"},
	}
	return decl
}

// phaseClient serves one widget that starts `on` and moves with its action endpoints.
func phaseClient(phase *string, log *[]string) Client {
	return clientFunc(func(_ context.Context, req stripeapi.Request) (*stripeapi.Response, error) {
		*log = append(*log, req.Method+" "+req.Path)
		switch req.Path {
		case "/v1/widgets/wid_1/start":
			*phase = "on"
		case "/v1/widgets/wid_1/stop":
			*phase = "off"
		}
		return &stripeapi.Response{Body: wire.Object{
			{Name: "id", Value: wire.String("wid_1")},
			{Name: "name", Value: wire.String("w")},
			{Name: "phase", Value: wire.String(*phase)},
		}}, nil
	})
}

// A configured value of a transition field is reached by its action endpoint, after create
// and on update, only when the object does not hold it already, and never as a parameter.
func TestTransitionsCallTheActionEndpoint(t *testing.T) {
	phase, log := "on", []string{}
	r := configured(t, transitionDeclaration(), transitionTable(), phaseClient(&phase, &log))

	state, _ := r.create(t, r.fill(map[string]attr.Value{"name": types.StringValue("w"), "phase": types.StringValue("on")}))
	if got := strings.Join(log, ";"); got != "POST /v1/widgets" {
		t.Fatalf("a value the object holds sends no action: %s", got)
	}

	log = nil
	state = r.update(t, r.fill(map[string]attr.Value{"id": state.Attributes()["id"], "name": types.StringValue("w"), "phase": types.StringValue("off")}), state)
	if got := strings.Join(log, ";"); got != "GET /v1/widgets/wid_1;POST /v1/widgets/wid_1/stop" || str(t, state, "phase") != "off" {
		t.Fatalf("requests %s, phase %s", got, str(t, state, "phase"))
	}

	log = nil
	state = r.update(t, r.fill(map[string]attr.Value{"id": state.Attributes()["id"], "name": types.StringValue("w")}), state)
	if got := strings.Join(log, ";"); got != "GET /v1/widgets/wid_1" {
		t.Fatalf("an unset value sends no action: %s", got)
	}

	phase, log = "off", nil
	r.create(t, r.fill(map[string]attr.Value{"name": types.StringValue("w"), "phase": types.StringValue("on")}))
	if got := strings.Join(log, ";"); got != "POST /v1/widgets;POST /v1/widgets/wid_1/start" {
		t.Fatalf("create requests: %s", got)
	}
}

func TestTransitionsRefuseAValueNoActionSets(t *testing.T) {
	r := configured(t, transitionDeclaration(), transitionTable(), noCalls(t))
	for value, wantErr := range map[string]bool{"on": false, "off": false, "gone": true} {
		config := r.fill(map[string]attr.Value{"name": types.StringValue("w"), "phase": types.StringValue(value)})
		var resp resource.ValidateConfigResponse
		r.ValidateConfig(context.Background(), resource.ValidateConfigRequest{Config: tfsdk.Config{Schema: r.schema, Raw: raw(t, config)}}, &resp)
		if resp.Diagnostics.HasError() != wantErr {
			t.Errorf("%s: error = %v, want %v", value, resp.Diagnostics.HasError(), wantErr)
		}
	}
}

func TestTransitionsMustNameAResponseOnlyField(t *testing.T) {
	for name, transition := range map[string]Transition{
		"writable field": {Field: "name", Value: "on", Path: "/v1/widgets/{id}/start"},
		"no such field":  {Field: "nothing", Value: "on", Path: "/v1/widgets/{id}/start"},
		"no path":        {Field: "phase", Value: "on"},
	} {
		decl := fixtureDeclaration()
		decl.Transitions = []Transition{transition}
		if _, err := NewResource(decl, transitionTable()); err == nil {
			t.Errorf("%s: want an error", name)
		}
	}
}

// Retain sends nothing, warns with the declaration's sentence and leaves the state to
// Terraform.
func TestRetainWarnsAndSendsNothing(t *testing.T) {
	decl := fixtureDeclaration()
	decl.Delete = Retain
	decl.RetainWarning = "The widget stays on the shelf."
	r := configured(t, decl, fixtureTable(), noCalls(t))
	diags := r.destroy(t, r.fill(map[string]attr.Value{"id": types.StringValue("wid_1")}))
	if diags.HasError() || len(diags) != 1 || !strings.Contains(diags[0].Detail(), "The widget stays on the shelf.") || !strings.Contains(diags[0].Detail(), "wid_1") {
		t.Fatalf("diagnostics: %v", diags)
	}

	decl.RetainWarning = ""
	if _, err := NewResource(decl, fixtureTable()); err == nil {
		t.Error("a retain strategy with no warning must be refused")
	}
}

// Stripe answers a missing billing alert with a 404 that carries no error code. Read and
// destroy go by the status, so the object leaves the state and a destroy of it succeeds.
func TestNotFoundWithoutACodeIsGone(t *testing.T) {
	client := clientFunc(func(context.Context, stripeapi.Request) (*stripeapi.Response, error) {
		return nil, &stripeapi.APIError{Status: 404, Type: "invalid_request_error", Message: "The alert wid_1 was not found."}
	})
	decl := fixtureDeclaration()
	decl.Delete = Archive
	decl.Archive = &ArchiveAction{Field: "active", Value: wire.Bool(false)}
	r := configured(t, decl, fixtureTable(), client)
	state := r.fill(map[string]attr.Value{"id": types.StringValue("wid_1")})
	if _, found := r.read(t, state); found {
		t.Error("a 404 with no code must remove the object from state")
	}
	noErrors(t, r.destroy(t, state))
}

// actionStub serves widgets like stubStripe plus the start and stop action endpoints. An
// action's response omits the once-returned `secret`, and no response carries `options`
// unless the request asked to expand it, as Stripe does for an expandable field.
type actionStub struct {
	stub *stubStripe
	// failStop makes the stop action fail.
	failStop bool
	log      []stripeapi.Request
}

func (a *actionStub) Do(ctx context.Context, req stripeapi.Request) (*stripeapi.Response, error) {
	a.log = append(a.log, req)
	var action, id string
	for _, name := range []string{"start", "stop"} {
		if rest, ok := strings.CutSuffix(req.Path, "/"+name); ok {
			action, id = name, strings.TrimPrefix(rest, "/v1/widgets/")
		}
	}
	if action == "stop" && a.failStop {
		return nil, &stripeapi.APIError{Status: 400, Message: "cannot stop"}
	}
	var body wire.Object
	status := 200
	if action != "" {
		phase := map[string]string{"start": "on", "stop": "off"}[action]
		a.stub.objects[id] = put(a.stub.objects[id], "phase", wire.String(phase))
		body = drop(append(wire.Object(nil), a.stub.objects[id]...), "secret")
	} else {
		resp, err := a.stub.Do(ctx, req)
		if err != nil {
			return nil, err
		}
		body, status = append(wire.Object(nil), resp.Body.(wire.Object)...), resp.Status
	}
	if expand, _ := req.Params.Get("expand"); expand == nil || !strings.Contains(fmt.Sprint(expand), "options") {
		body = drop(body, "options")
	}
	return &stripeapi.Response{Status: status, Body: body}, nil
}

func (a *actionStub) paths() string {
	var out []string
	for _, req := range a.log {
		out = append(out, req.Method+" "+req.Path)
	}
	return strings.Join(out, ";")
}

func actionResource(t *testing.T, decl Declaration) (*Resource, *actionStub) {
	t.Helper()
	a := &actionStub{stub: newStub()}
	return configured(t, decl, transitionTable(), a), a
}

func startedPlan(r *Resource, extra map[string]attr.Value) attr.Value {
	set := map[string]attr.Value{"name": types.StringValue("w"), "phase": types.StringValue("on")}
	for k, v := range extra {
		set[k] = v
	}
	return r.fill(set)
}

// The action response omits a once-returned field, so create keeps the one the create
// response carried.
func TestTransitionOnCreateKeepsOnceReturnedFields(t *testing.T) {
	r, a := actionResource(t, transitionDeclaration())
	state, _ := r.create(t, startedPlan(r, nil).(basetypes.ObjectValue))
	if !strings.Contains(a.paths(), "POST /v1/widgets/wid_1/start") {
		t.Fatalf("no action ran: %s", a.paths())
	}
	if got := str(t, state, "secret"); got != "whsec_wid_1" {
		t.Errorf("secret after create with an action: %q", got)
	}
	if got := str(t, state, "phase"); got != "on" {
		t.Errorf("phase: %q", got)
	}
}

// The action asks for the declared expansion, so the response it returns can be decoded.
func TestTransitionActionExpandsTheDeclaredFields(t *testing.T) {
	decl := transitionDeclaration()
	decl.Expand = []string{"options"}
	r, a := actionResource(t, decl)
	r.create(t, startedPlan(r, nil).(basetypes.ObjectValue))
	for _, req := range a.log {
		if strings.HasSuffix(req.Path, "/start") && !hasExpand(t, req, `["options"]`) {
			t.Errorf("the action sent no expand: %+v", req.Params)
		}
	}
}

// A change to a normal field and to the transition field is one update, then the action;
// the state shows both.
func TestTransitionRunsAfterTheUpdate(t *testing.T) {
	r, a := actionResource(t, transitionDeclaration())
	state, _ := r.create(t, r.fill(map[string]attr.Value{"name": types.StringValue("w"), "phase": types.StringValue("off")}))
	a.log = nil
	plan := r.fill(map[string]attr.Value{"id": state.Attributes()["id"], "name": types.StringValue("renamed"), "phase": types.StringValue("on")})
	next := r.update(t, plan, state)
	if got := a.paths(); got != "POST /v1/widgets/wid_1;POST /v1/widgets/wid_1/start" {
		t.Errorf("requests: %s", got)
	}
	if str(t, next, "name") != "renamed" || str(t, next, "phase") != "on" {
		t.Errorf("state: name %q, phase %q", str(t, next, "name"), str(t, next, "phase"))
	}
}

// An action that fails after the update succeeded fails the update.
func TestTransitionFailureAfterTheUpdateFailsTheUpdate(t *testing.T) {
	r, a := actionResource(t, transitionDeclaration())
	state, _ := r.create(t, startedPlan(r, nil).(basetypes.ObjectValue))
	a.failStop = true
	a.log = nil
	plan := r.fill(map[string]attr.Value{"id": state.Attributes()["id"], "name": types.StringValue("renamed"), "phase": types.StringValue("off")})
	resp := resource.UpdateResponse{State: tfsdk.State{Schema: r.schema, Raw: raw(t, state)}}
	r.Update(context.Background(), resource.UpdateRequest{
		Plan:  tfsdk.Plan{Schema: r.schema, Raw: raw(t, plan)},
		State: tfsdk.State{Schema: r.schema, Raw: raw(t, state)},
	}, &resp)
	if !resp.Diagnostics.HasError() {
		t.Fatalf("the failed action must fail the update; requests: %s", a.paths())
	}
	if got := a.paths(); got != "POST /v1/widgets/wid_1;POST /v1/widgets/wid_1/stop" {
		t.Errorf("requests: %s", got)
	}
}

// The transition field is set by the action endpoint, never sent as a parameter.
func TestTransitionFieldIsNeverARequestParameter(t *testing.T) {
	r, a := actionResource(t, transitionDeclaration())
	state, _ := r.create(t, startedPlan(r, nil).(basetypes.ObjectValue))
	plan := r.fill(map[string]attr.Value{"id": state.Attributes()["id"], "name": types.StringValue("renamed"), "phase": types.StringValue("off")})
	r.update(t, plan, state)
	if len(a.log) < 3 {
		t.Fatalf("too few requests: %s", a.paths())
	}
	for _, req := range a.log {
		if _, sent := req.Params.Get("phase"); sent {
			t.Errorf("%s %s sent phase: %+v", req.Method, req.Path, req.Params)
		}
	}
}
