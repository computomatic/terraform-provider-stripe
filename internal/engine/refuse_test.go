package engine

import (
	"context"
	"strings"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-go/tfprotov6"
	"github.com/hashicorp/terraform-plugin-go/tftypes"

	"github.com/computomatic/terraform-provider-stripe/internal/fields"
	"github.com/computomatic/terraform-provider-stripe/internal/stripeapi"
	"github.com/computomatic/terraform-provider-stripe/internal/wire"
)

// defaultTable is the fixture table with the response-only `is_default` RefuseWhen reads.
func defaultTable() []fields.Field {
	return append(fixtureTable(), fields.Field{Name: "is_default", Shape: fields.Bool, Response: true})
}

// refusedDestroy reports whether diags hold the engine's refusal of destroy.
func refusedDestroy(diags diag.Diagnostics) bool {
	for _, d := range diags.Errors() {
		if strings.HasPrefix(d.Summary(), "Cannot destroy ") && strings.Contains(d.Detail(), "removed {") {
			return true
		}
	}
	return false
}

// lifecycleTable has a create-only field of each kind a replacement can come from: top-level
// scalars, a field of a nested object and a field of a list element.
func lifecycleTable() []fields.Field {
	return []fields.Field{
		{Name: "id", Shape: fields.String, Response: true},
		{Name: "name", Shape: fields.String, Required: true, Create: true, Update: true, Response: true},
		{Name: "kind", Shape: fields.String, Create: true, Response: true},
		{Name: "quantity", Shape: fields.Int, Create: true, Response: true},
		{Name: "status", Shape: fields.String, Response: true},
		{Name: "address", Shape: fields.Object, Create: true, Update: true, Response: true, Fields: []fields.Field{
			{Name: "line1", Shape: fields.String, Create: true, Update: true, Response: true},
			{Name: "zip", Shape: fields.String, Create: true, Response: true},
		}},
		{Name: "phases", Shape: fields.ObjectList, Create: true, Update: true, Response: true, Fields: []fields.Field{
			{Name: "note", Shape: fields.String, Create: true, Update: true, Response: true},
			{Name: "currency", Shape: fields.String, Create: true, Response: true},
			// Server-filled and create-only in a list: it replaces only when set (ReplaceIfSet).
			{Name: "rate", Shape: fields.String, Create: true, Response: true, ServerDefaulted: true},
		}},
		{Name: "url", Shape: fields.String, Create: true, Update: true, Response: true},
		// A create-only object with a child Stripe fills in, and a nested object Stripe fills
		// in when the configuration leaves it out.
		{Name: "source", Shape: fields.Object, Create: true, Response: true, Fields: []fields.Field{
			{Name: "ref", Shape: fields.String, Required: true, Create: true, Response: true},
			{Name: "flag", Shape: fields.Bool, Create: true, Response: true, ServerDefaulted: true},
		}},
		{Name: "defaults", Shape: fields.Object, Create: true, Update: true, Response: true, ServerDefaulted: true, Fields: []fields.Field{
			{Name: "mode", Shape: fields.String, Create: true, Update: true, Response: true},
		}},
	}
}

const widgetReason = "Stripe cannot delete a widget."

func refusingDeclaration() Declaration {
	d := planDecl()
	d.Delete = Refuse
	d.Paths.Delete = ""
	d.RefuseReason = widgetReason
	return d
}

// endedDeclaration archives a widget, except one whose status is `ended`.
func endedDeclaration() Declaration {
	d := planDecl()
	d.Delete = Archive
	d.Paths.Delete = ""
	d.Archive = &ArchiveAction{Field: "status", Value: wire.String("inactive"), Path: "/v1/widgets/{id}/deactivate"}
	d.RefuseWhen = &RefuseWhen{Field: "status", Value: wire.String("ended"), Reason: "Stripe cannot deactivate an ended widget."}
	return d
}

// lifecycleState is a widget in state with the given status; with lists the changes a test
// makes to it.
func (p *planner) lifecycleState(status string, with map[string]tftypes.Value) map[string]tftypes.Value {
	attrs := p.typ.(tftypes.Object).AttributeTypes
	phase := p.attrTypes("phases")
	state := map[string]tftypes.Value{
		"id": tfString("wid_1"), "name": tfString("n"), "kind": tfString("a"),
		"quantity": tftypes.NewValue(tftypes.Number, 1), "status": tfString(status),
		"address": obj(p.attrTypes("address"), map[string]tftypes.Value{"line1": tfString("1 Main"), "zip": tfString("10001")}),
		"phases": tftypes.NewValue(attrs["phases"], []tftypes.Value{
			obj(phase, map[string]tftypes.Value{"note": tfString("first"), "currency": tfString("usd"), "rate": tfString("flat")}),
		}),
		"url":      tfString("https://example.com"),
		"timeouts": tftypes.NewValue(attrs["timeouts"], nil),
	}
	return clone(state, with)
}

// change plans the state with the changes applied, as Terraform sends a configuration that
// changes them.
func (p *planner) change(state, changes map[string]tftypes.Value) *tfprotov6.PlanResourceChangeResponse {
	p.t.Helper()
	proposed := clone(state, changes)
	config := clone(proposed, map[string]tftypes.Value{"id": tftypes.NewValue(tftypes.String, nil), "status": tftypes.NewValue(tftypes.String, nil)})
	return p.planResponse(p.value(state), p.value(proposed), p.value(config))
}

func (p *planner) destroyPlan(state map[string]tftypes.Value) *tfprotov6.PlanResourceChangeResponse {
	p.t.Helper()
	return p.planResponse(p.value(state), tftypes.NewValue(p.typ, nil), tftypes.NewValue(p.typ, nil))
}

func errorsOf(resp *tfprotov6.PlanResourceChangeResponse) []*tfprotov6.Diagnostic {
	var out []*tfprotov6.Diagnostic
	for _, d := range resp.Diagnostics {
		if d.Severity == tfprotov6.DiagnosticSeverityError {
			out = append(out, d)
		}
	}
	return out
}

// A destroy plan of a refusing resource fails with the reason, a removed block and
// `terraform state rm`, at plan time, before anything reaches Stripe.
func TestRefuseFailsTheDestroyPlan(t *testing.T) {
	p := newPlanner(t, refusingDeclaration(), lifecycleTable())
	errs := errorsOf(p.destroyPlan(p.lifecycleState("open", nil)))
	if len(errs) != 1 || errs[0].Summary != "Cannot destroy stripe_widget" {
		t.Fatalf("want the refusal: %+v", errs)
	}
	for _, want := range []string{widgetReason, "removed {\n  from = stripe_widget.<name>\n\n  lifecycle {\n    destroy = false\n  }\n}", "`terraform state rm stripe_widget.<name>`", "OpenTofu 1.10"} {
		if !strings.Contains(errs[0].Detail, want) {
			t.Errorf("detail lacks %q:\n%s", want, errs[0].Detail)
		}
	}
}

// A resource that deletes plans its destroy without a diagnostic.
func TestDeletePlansTheDestroy(t *testing.T) {
	p := newPlanner(t, planDecl(), lifecycleTable())
	if errs := errorsOf(p.destroyPlan(p.lifecycleState("open", nil))); len(errs) != 0 {
		t.Fatalf("diagnostics: %+v", errs)
	}
}

// Every change that would replace a refusing resource fails at plan time on the attribute
// that forces it, whatever its depth, and nothing is planned as a replacement. A change
// Stripe makes in place plans as an update.
func TestRefuseFailsAReplacement(t *testing.T) {
	p := newPlanner(t, refusingDeclaration(), lifecycleTable())
	state := p.lifecycleState("open", nil)
	attrs := p.typ.(tftypes.Object).AttributeTypes
	for name, tc := range map[string]struct {
		changes map[string]tftypes.Value
		path    string
	}{
		"top-level string": {map[string]tftypes.Value{"kind": tfString("b")}, "kind"},
		"top-level int":    {map[string]tftypes.Value{"quantity": tftypes.NewValue(tftypes.Number, 2)}, "quantity"},
		"nested field": {map[string]tftypes.Value{
			"address": obj(p.attrTypes("address"), map[string]tftypes.Value{"line1": tfString("1 Main"), "zip": tfString("94105")}),
		}, "address.zip"},
		"list element field": {map[string]tftypes.Value{
			"phases": tftypes.NewValue(attrs["phases"], []tftypes.Value{
				obj(p.attrTypes("phases"), map[string]tftypes.Value{"note": tfString("first"), "currency": tfString("eur"), "rate": tfString("flat")}),
			}),
		}, "phases[0].currency"},
		"server-filled list element field": {map[string]tftypes.Value{
			"phases": tftypes.NewValue(attrs["phases"], []tftypes.Value{
				obj(p.attrTypes("phases"), map[string]tftypes.Value{"note": tfString("first"), "currency": tfString("usd"), "rate": tfString("tiered")}),
			}),
		}, "phases[0].rate"},
	} {
		t.Run(name, func(t *testing.T) {
			resp := p.change(state, tc.changes)
			errs := errorsOf(resp)
			if len(errs) != 1 || errs[0].Summary != "Cannot replace stripe_widget" {
				t.Fatalf("want the refusal: %+v", resp.Diagnostics)
			}
			if !strings.Contains(errs[0].Detail, "Changing `"+tc.path+"`") || !strings.Contains(errs[0].Detail, widgetReason) || !strings.Contains(errs[0].Detail, "ignore_changes = ["+tc.path+"]") {
				t.Errorf("detail:\n%s", errs[0].Detail)
			}
			if len(resp.RequiresReplace) != 0 {
				t.Errorf("nothing may be planned as a replacement: %v", resp.RequiresReplace)
			}
		})
	}

	resp := p.change(state, map[string]tftypes.Value{"name": tfString("renamed")})
	if errs := errorsOf(resp); len(errs) != 0 || len(resp.RequiresReplace) != 0 {
		t.Fatalf("an update must plan: %+v %v", errs, resp.RequiresReplace)
	}
}

// Terraform proposes null for a nested object Stripe fills in, so the framework marks every
// computed value unknown, among them a child of a create-only object, before that child keeps
// its state. The object's replacement modifier sees an unknown child then; it is no change and
// must not be refused, and the plan stays the state. A configured change of the object in the
// same plan is refused.
func TestRefuseIgnoresAChildThatIsNotKnownYet(t *testing.T) {
	p := newPlanner(t, refusingDeclaration(), lifecycleTable())
	source := func(flag tftypes.Value) tftypes.Value {
		return obj(p.attrTypes("source"), map[string]tftypes.Value{"ref": tfString("src_1"), "flag": flag})
	}
	defaults := p.typ.(tftypes.Object).AttributeTypes["defaults"]
	state := p.lifecycleState("open", map[string]tftypes.Value{
		"source":   source(tftypes.NewValue(tftypes.Bool, true)),
		"defaults": obj(p.attrTypes("defaults"), map[string]tftypes.Value{"mode": tfString("auto")}),
	})
	proposed := clone(state, map[string]tftypes.Value{"defaults": tftypes.NewValue(defaults, nil)})
	config := clone(proposed, map[string]tftypes.Value{
		"id": tftypes.NewValue(tftypes.String, nil), "status": tftypes.NewValue(tftypes.String, nil),
		"source": source(tftypes.NewValue(tftypes.Bool, nil)),
	})
	resp := p.planResponse(p.value(state), p.value(proposed), p.value(config))
	if errs := errorsOf(resp); len(errs) != 0 {
		t.Fatalf("a plan of nothing must not be refused: %+v", errs)
	}
	planned, err := resp.PlannedState.Unmarshal(p.typ)
	if err != nil || !planned.Equal(p.value(state)) {
		t.Errorf("the plan must stay the state: %v %v", err, planned)
	}

	moved := obj(p.attrTypes("source"), map[string]tftypes.Value{"ref": tfString("src_2"), "flag": tftypes.NewValue(tftypes.Bool, true)})
	proposed["source"] = moved
	config["source"] = obj(p.attrTypes("source"), map[string]tftypes.Value{"ref": tfString("src_2"), "flag": tftypes.NewValue(tftypes.Bool, nil)})
	if errs := errorsOf(p.planResponse(p.value(state), p.value(proposed), p.value(config))); len(errs) != 1 || errs[0].Summary != "Cannot replace stripe_widget" {
		t.Fatalf("a configured change must be refused: %+v", errs)
	}
}

// A change of the parent would replace the object too, and is refused like a create-only
// field.
func TestRefuseFailsAParentChange(t *testing.T) {
	decl := refusingDeclaration()
	decl.Parent = "account"
	decl.Paths = Paths{Create: "/v1/accounts/{account}/widgets", Retrieve: "/v1/accounts/{account}/widgets/{id}", Update: "/v1/accounts/{account}/widgets/{id}"}
	p := newPlanner(t, decl, lifecycleTable())
	state := p.lifecycleState("open", map[string]tftypes.Value{"account": tfString("acct_1")})
	resp := p.change(state, map[string]tftypes.Value{"account": tfString("acct_2")})
	if errs := errorsOf(resp); len(errs) != 1 || errs[0].Summary != "Cannot replace stripe_widget" || len(resp.RequiresReplace) != 0 {
		t.Fatalf("want the refusal: %+v %v", resp.Diagnostics, resp.RequiresReplace)
	}
}

// RefuseWhen refuses destroy and replacement of an object in the named state only. Any other
// object plans its destroy and its replacement as the strategy does.
func TestRefuseWhenFollowsTheState(t *testing.T) {
	p := newPlanner(t, endedDeclaration(), lifecycleTable())
	live, ended := p.lifecycleState("active", nil), p.lifecycleState("ended", nil)
	change := map[string]tftypes.Value{"kind": tfString("b")}

	if errs := errorsOf(p.destroyPlan(live)); len(errs) != 0 {
		t.Errorf("a live widget plans its destroy: %+v", errs)
	}
	if errs := errorsOf(p.destroyPlan(ended)); len(errs) != 1 || !strings.Contains(errs[0].Detail, "Stripe cannot deactivate an ended widget.") {
		t.Errorf("an ended widget's destroy is refused: %+v", errs)
	}
	if resp := p.change(live, change); len(errorsOf(resp)) != 0 || len(resp.RequiresReplace) != 1 {
		t.Errorf("a live widget is replaced: %+v %v", resp.Diagnostics, resp.RequiresReplace)
	}
	if resp := p.change(ended, change); len(errorsOf(resp)) != 1 || len(resp.RequiresReplace) != 0 {
		t.Errorf("an ended widget's replacement is refused: %+v %v", resp.Diagnostics, resp.RequiresReplace)
	}
}

// draftOnlyDeclaration deletes a widget only while its status is `draft`, as Stripe deletes
// only a draft invoice.
func draftOnlyDeclaration() Declaration {
	d := planDecl()
	d.RefuseWhen = &RefuseWhen{Field: "status", Value: wire.String("draft"), Except: true, Reason: "Stripe deletes only a draft widget."}
	return d
}

// RefuseWhen with Except refuses destroy and replacement of an object in any state but the
// named one, at plan time from the state and at apply when Stripe refuses the delete of an
// object the state still shows in it.
func TestRefuseWhenExceptRefusesEveryOtherState(t *testing.T) {
	p := newPlanner(t, draftOnlyDeclaration(), lifecycleTable())
	draft, open := p.lifecycleState("draft", nil), p.lifecycleState("open", nil)
	change := map[string]tftypes.Value{"kind": tfString("b")}
	if errs := errorsOf(p.destroyPlan(draft)); len(errs) != 0 {
		t.Errorf("a draft plans its destroy: %+v", errs)
	}
	if errs := errorsOf(p.destroyPlan(open)); len(errs) != 1 || !strings.Contains(errs[0].Detail, "Stripe deletes only a draft widget.") {
		t.Errorf("an open widget's destroy is refused: %+v", errs)
	}
	if resp := p.change(draft, change); len(errorsOf(resp)) != 0 || len(resp.RequiresReplace) != 1 {
		t.Errorf("a draft is replaced: %+v %v", resp.Diagnostics, resp.RequiresReplace)
	}
	if resp := p.change(open, change); len(errorsOf(resp)) != 1 || len(resp.RequiresReplace) != 0 {
		t.Errorf("an open widget's replacement is refused: %+v %v", resp.Diagnostics, resp.RequiresReplace)
	}

	r := configured(t, draftOnlyDeclaration(), lifecycleTable(), clientFunc(func(_ context.Context, req stripeapi.Request) (*stripeapi.Response, error) {
		if req.Method == "DELETE" {
			return nil, &stripeapi.APIError{Status: 400, Type: "invalid_request_error", Message: "This invoice is finalized and cannot be deleted."}
		}
		return &stripeapi.Response{Status: 200, Body: wire.Object{{Name: "id", Value: wire.String("wid_1")}, {Name: "status", Value: wire.String("open")}}}, nil
	}))
	if diags := r.destroy(t, r.fill(map[string]attr.Value{"id": types.StringValue("wid_1"), "status": types.StringValue("draft")})); !refusedDestroy(diags) {
		t.Fatalf("a stale draft that Stripe has finalized: want the refusal: %v", diags)
	}
}

// A create-only list keeps the values Stripe fills in for its elements while another field
// changes, so the change is an update and not a replacement, which a refusing resource would
// fail.
func TestCreateOnlyListKeepsWhatStripeFillsIn(t *testing.T) {
	table := append(lifecycleTable(), fields.Field{Name: "lines", Shape: fields.ObjectList, Create: true, Response: true, Fields: []fields.Field{
		{Name: "kind", Shape: fields.String, Create: true, Response: true},
		{Name: "amount", Shape: fields.Int, Create: true, Response: true, ServerDefaulted: true},
	}})
	p := newPlanner(t, refusingDeclaration(), table)
	line := func(amount tftypes.Value) tftypes.Value {
		return tftypes.NewValue(p.typ.(tftypes.Object).AttributeTypes["lines"], []tftypes.Value{
			obj(p.attrTypes("lines"), map[string]tftypes.Value{"kind": tfString("credit"), "amount": amount}),
		})
	}
	state := p.lifecycleState("open", map[string]tftypes.Value{"lines": line(tftypes.NewValue(tftypes.Number, 500))})
	proposed := clone(state, map[string]tftypes.Value{"name": tfString("renamed")})
	config := clone(proposed, map[string]tftypes.Value{
		"id": tftypes.NewValue(tftypes.String, nil), "status": tftypes.NewValue(tftypes.String, nil),
		"lines": line(tftypes.NewValue(tftypes.Number, nil)),
	})
	// The list's own modifier runs before its elements' and names it for replacement, which
	// Terraform drops when the planned list equals the state's.
	planned, _ := p.plan(p.value(state), p.value(proposed), p.value(config))
	if got := valueAt(planned, tftypes.NewAttributePath().WithAttributeName("lines")); !got.Equal(line(tftypes.NewValue(tftypes.Number, 500))) {
		t.Errorf("planned lines %v, want the state's", got)
	}
}

// topLevelTable is lifecycleTable with its nested create-only fields updatable, as
// RefuseChange requires.
func topLevelTable() []fields.Field {
	table := lifecycleTable()
	for i := range table {
		for j := range table[i].Fields {
			table[i].Fields[j].Update = true
		}
	}
	return table
}

// A RefuseChange field of a refusing resource tells the user to remove the object from state,
// not to taint it, because a tainted object's destroy fails too.
func TestRefuseChangeOnARefusingResourceSaysToRemoveIt(t *testing.T) {
	decl := refusingDeclaration()
	decl.RefuseChange = []string{"url"}
	p := newPlanner(t, decl, topLevelTable())
	errs := errorsOf(p.change(p.lifecycleState("open", nil), map[string]tftypes.Value{"url": tfString("https://example.org")}))
	if len(errs) != 1 || !strings.Contains(errs[0].Detail, "removed block") || strings.Contains(errs[0].Detail, "taint") {
		t.Fatalf("want the removal advice: %+v", errs)
	}

	p = newPlanner(t, func() Declaration { d := planDecl(); d.RefuseChange = []string{"url"}; return d }(), topLevelTable())
	errs = errorsOf(p.change(p.lifecycleState("open", nil), map[string]tftypes.Value{"url": tfString("https://example.org")}))
	if len(errs) != 1 || !strings.Contains(errs[0].Detail, "terraform taint") {
		t.Fatalf("a deleting resource keeps the taint advice: %+v", errs)
	}
}

// The destroy of a refusing resource fails at apply too, for the replacements plan time
// cannot see (`-replace`, taint), and sends nothing to Stripe.
func TestRefuseFailsTheDestroyAtApply(t *testing.T) {
	r := configured(t, refusingDeclaration(), lifecycleTable(), noCalls(t))
	if diags := r.destroy(t, r.fill(map[string]attr.Value{"id": types.StringValue("wid_1")})); !refusedDestroy(diags) {
		t.Fatalf("want the refusal: %v", diags)
	}
}

// RefuseWhen refuses at apply from the state, sending nothing, and archives any other object.
func TestRefuseWhenFailsTheDestroyAtApply(t *testing.T) {
	r := configured(t, endedDeclaration(), lifecycleTable(), noCalls(t))
	state := r.fill(map[string]attr.Value{"id": types.StringValue("wid_1"), "status": types.StringValue("ended")})
	if diags := r.destroy(t, state); !refusedDestroy(diags) {
		t.Fatalf("want the refusal: %v", diags)
	}

	var sent []string
	r = configured(t, endedDeclaration(), lifecycleTable(), clientFunc(func(_ context.Context, req stripeapi.Request) (*stripeapi.Response, error) {
		sent = append(sent, req.Method+" "+req.Path)
		return &stripeapi.Response{Status: 200, Body: wire.Object{{Name: "id", Value: wire.String("wid_1")}, {Name: "status", Value: wire.String("active")}}}, nil
	}))
	state = r.fill(map[string]attr.Value{"id": types.StringValue("wid_1"), "status": types.StringValue("active")})
	noErrors(t, r.destroy(t, state))
	if strings.Join(sent, ",") != "GET /v1/widgets/wid_1,POST /v1/widgets/wid_1/deactivate" {
		t.Errorf("requests: %v", sent)
	}
}

// The page of a refusing resource carries the callout, and a create-only field's description
// says its change fails the plan.
func TestRefusingResourceDocuments(t *testing.T) {
	r := configured(t, refusingDeclaration(), lifecycleTable(), noCalls(t))
	note := r.DestroyNote()
	if !strings.HasPrefix(note, widgetReason+" `terraform destroy`, and a change that would replace the widget, fail at plan time.") || !strings.Contains(note, "(#removing-from-state)") {
		t.Errorf("note: %s", note)
	}
	if d := r.schema.Attributes["kind"].GetMarkdownDescription(); !strings.HasSuffix(d, "Changing it fails the plan, because this resource cannot be replaced.") {
		t.Errorf("kind: %s", d)
	}

	r = configured(t, endedDeclaration(), lifecycleTable(), noCalls(t))
	if note := r.DestroyNote(); !strings.Contains(note, "fail at plan time for such a widget.") {
		t.Errorf("refuse-when note: %s", note)
	}
	if d := r.schema.Attributes["kind"].GetMarkdownDescription(); !strings.HasSuffix(d, "Changing it replaces the resource.") {
		t.Errorf("a refuse-when resource replaces: %s", d)
	}

	if note := fixtureResource(t, noCalls(t)).DestroyNote(); note != "" {
		t.Errorf("a deleting resource has no note: %s", note)
	}
}

func TestRefuseDeclarationsMustBeComplete(t *testing.T) {
	for name, mutate := range map[string]func(d *Declaration){
		"refuse without a reason":   func(d *Declaration) { d.RefuseReason = "" },
		"refuse with a delete path": func(d *Declaration) { d.Paths.Delete = "/v1/widgets/{id}" },
		"refuse with an archive":    func(d *Declaration) { d.Archive = &ArchiveAction{Field: "status", Value: wire.String("x")} },
		"refuse with refuse-when": func(d *Declaration) {
			d.RefuseWhen = &RefuseWhen{Field: "status", Value: wire.String("x"), Reason: "r"}
		},
		"refuse with currency option": func(d *Declaration) { d.CurrencyOptions = "address" },
		"refuse-when without reason": func(d *Declaration) {
			d.Delete, d.Paths.Delete, d.RefuseReason = Delete, "/v1/widgets/{id}", ""
			d.RefuseWhen = &RefuseWhen{Field: "status", Value: wire.String("x")}
		},
		"refuse-when on an unknown field": func(d *Declaration) {
			d.Delete, d.Paths.Delete, d.RefuseReason = Delete, "/v1/widgets/{id}", ""
			d.RefuseWhen = &RefuseWhen{Field: "nope", Value: wire.String("x"), Reason: "r"}
		},
		"refuse-when of the wrong type": func(d *Declaration) {
			d.Delete, d.Paths.Delete, d.RefuseReason = Delete, "/v1/widgets/{id}", ""
			d.RefuseWhen = &RefuseWhen{Field: "status", Value: wire.Bool(true), Reason: "r"}
		},
	} {
		decl := refusingDeclaration()
		mutate(&decl)
		if _, err := NewResource(decl, lifecycleTable()); err == nil {
			t.Errorf("%s: must not compile", name)
		}
	}
	if _, err := NewResource(endedDeclaration(), lifecycleTable()); err != nil {
		t.Errorf("refuse-when on archive: %v", err)
	}
}
