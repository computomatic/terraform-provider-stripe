package engine

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/hashicorp/terraform-plugin-framework/attr"
	rschema "github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-framework/types/basetypes"

	"github.com/computomatic/terraform-provider-stripe/internal/fields"
	"github.com/computomatic/terraform-provider-stripe/internal/stripeapi"
	"github.com/computomatic/terraform-provider-stripe/internal/wire"
)

func clockTable() []fields.Field {
	return []fields.Field{
		{Name: "id", Shape: fields.String, Response: true},
		{Name: "name", Shape: fields.String, Create: true, Response: true},
		{Name: "frozen_time", Shape: fields.Int, Required: true, Create: true, Response: true},
		{Name: "status", Shape: fields.String, Response: true},
	}
}

func clockDeclaration() Declaration {
	return Declaration{
		TypeName:    "stripe_clock",
		IDPrefix:    "clock_",
		Description: "A clock.",
		Paths: Paths{
			Create:   "/v1/clocks",
			Retrieve: "/v1/clocks/{id}",
			Delete:   "/v1/clocks/{id}",
		},
		Delete:  Delete,
		Advance: &AdvanceAction{Field: "frozen_time", Path: "/v1/clocks/{id}/advance", StatusField: "status", ReadyValue: "ready"},
	}
}

// clockStub serves one clock. An advance answers at once with the clock still moving, and
// the clock is ready again after `busy` more reads.
type clockStub struct {
	frozen int64
	status string
	busy   int
	target int64
	log    []string
	params []wire.Object
}

func (c *clockStub) Do(_ context.Context, req stripeapi.Request) (*stripeapi.Response, error) {
	c.log = append(c.log, req.Method+" "+req.Path)
	c.params = append(c.params, req.Params)
	switch {
	case req.Method == "POST" && strings.HasSuffix(req.Path, "/advance"):
		v, _ := req.Params.Get("frozen_time")
		c.target, c.status, c.busy = int64(v.(wire.Int)), "advancing", 2
	case req.Method == "GET" && c.status == "advancing":
		if c.busy--; c.busy <= 0 {
			c.frozen, c.status = c.target, "ready"
		}
	}
	return &stripeapi.Response{Body: wire.Object{
		{Name: "id", Value: wire.String("clock_1")},
		{Name: "name", Value: wire.String("c")},
		{Name: "frozen_time", Value: wire.Int(c.frozen)},
		{Name: "status", Value: wire.String(c.status)},
	}}, nil
}

func clockPlan(r *Resource, state basetypes.ObjectValue, name string, frozen int64) basetypes.ObjectValue {
	return r.fill(map[string]attr.Value{"id": state.Attributes()["id"], "name": types.StringValue(name), "frozen_time": types.Int64Value(frozen)})
}

// A larger value of the advance field is an update: the action endpoint is sent the value, and
// the update waits until the status is ready, so the state holds the moved value.
func TestAdvanceMovesTheFieldInPlaceAndWaitsForReady(t *testing.T) {
	c := &clockStub{frozen: 1000, status: "ready"}
	r := configured(t, clockDeclaration(), clockTable(), c)
	r.pollInterval = time.Millisecond
	state, _ := r.create(t, r.fill(map[string]attr.Value{"name": types.StringValue("c"), "frozen_time": types.Int64Value(1000)}))

	c.log, c.params = nil, nil
	next := r.update(t, clockPlan(r, state, "c", 5000), state)
	if got := strings.Join(c.log, ";"); !strings.HasPrefix(got, "GET /v1/clocks/clock_1;POST /v1/clocks/clock_1/advance;GET /v1/clocks/clock_1;GET /v1/clocks/clock_1;") {
		t.Fatalf("requests: %s", got)
	}
	if v, _ := c.params[1].Get("frozen_time"); v != wire.Int(5000) || len(c.params[1]) != 1 {
		t.Errorf("the advance sends the new value alone: %+v", c.params[1])
	}
	if got := next.Attributes()["frozen_time"]; !got.Equal(types.Int64Value(5000)) {
		t.Errorf("frozen_time: %v", got)
	}
	if str(t, next, "status") != "ready" {
		t.Errorf("status: %s", str(t, next, "status"))
	}

	c.log = nil
	r.update(t, clockPlan(r, next, "c", 5000), next)
	if got := strings.Join(c.log, ";"); got != "GET /v1/clocks/clock_1" {
		t.Errorf("an unchanged value sends no advance: %s", got)
	}
}

// The wait is bounded by the update timeout, whatever the status.
func TestAdvanceWaitEndsWithTheContext(t *testing.T) {
	c := &clockStub{frozen: 1000, status: "ready"}
	r := configured(t, clockDeclaration(), clockTable(), clientFunc(func(ctx context.Context, req stripeapi.Request) (*stripeapi.Response, error) {
		resp, err := c.Do(ctx, req)
		c.busy = 100
		return resp, err
	}))
	r.pollInterval = time.Millisecond
	state, _ := r.create(t, r.fill(map[string]attr.Value{"name": types.StringValue("c"), "frozen_time": types.Int64Value(1000)}))
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	err := r.advance(ctx, clockPlan(r, state, "c", 5000), state, "clock_1", "")
	if err == nil || !strings.Contains(err.Error(), "waiting for clock_1") {
		t.Fatalf("want the wait to end with an error, got %v", err)
	}
}

// A plan that moves the advance field back is refused before Stripe is asked, unless the plan
// replaces the object, whose create starts at the new value. A value not known yet is left
// for apply.
func TestAdvanceRefusesGoingBackwardsAtPlanTime(t *testing.T) {
	r := configured(t, clockDeclaration(), clockTable(), noCalls(t))
	state := r.fill(map[string]attr.Value{"id": types.StringValue("clock_1"), "name": types.StringValue("c"), "frozen_time": types.Int64Value(5000), "status": types.StringValue("ready")})

	if resp := r.modifyPlan(t, clockPlan(r, state, "c", 4000), state); !resp.Diagnostics.HasError() || !strings.Contains(resp.Diagnostics.Errors()[0].Detail(), "5000") {
		t.Errorf("going back must be refused with the current time: %v", resp.Diagnostics)
	}
	for name, plan := range map[string]basetypes.ObjectValue{
		"forward":  clockPlan(r, state, "c", 6000),
		"same":     clockPlan(r, state, "c", 5000),
		"replaced": clockPlan(r, state, "other", 4000),
		"unknown":  r.fill(map[string]attr.Value{"id": state.Attributes()["id"], "name": types.StringValue("c"), "frozen_time": types.Int64Unknown()}),
	} {
		if resp := r.modifyPlan(t, plan, state); resp.Diagnostics.HasError() {
			t.Errorf("%s: %v", name, resp.Diagnostics)
		}
	}
}

func TestAdvanceFieldDoesNotForceReplacement(t *testing.T) {
	r := configured(t, clockDeclaration(), clockTable(), noCalls(t))
	if n, _ := r.m.top("frozen_time"); n.Replace {
		t.Error("the advance field must change in place")
	}
	if n, _ := r.m.top("name"); !n.Replace {
		t.Error("the control: another create-only field still replaces")
	}
}

func TestAdvanceMustNameARequiredIntegerAndAReadyStatus(t *testing.T) {
	for name, edit := range map[string]func(*AdvanceAction){
		"no such field":  func(a *AdvanceAction) { a.Field = "nothing" },
		"not an integer": func(a *AdvanceAction) { a.Field = "name" },
		"no path":        func(a *AdvanceAction) { a.Path = "" },
		"no ready":       func(a *AdvanceAction) { a.ReadyValue = "" },
		"no such status": func(a *AdvanceAction) { a.StatusField = "nothing" },
	} {
		decl := clockDeclaration()
		a := *decl.Advance
		edit(&a)
		decl.Advance = &a
		if _, err := NewResource(decl, clockTable()); err == nil {
			t.Errorf("%s: want an error", name)
		}
	}
}

func lineDeclaration() Declaration {
	return Declaration{
		TypeName:   "stripe_widget_line",
		IDPrefix:   "li_",
		Parent:     "widget",
		FindInList: true,
		Paths:      Paths{List: "/v1/widgets/{widget}/lines"},
		Delete:     NoDelete,
		Rename:     map[string]string{"provider": "provider_"},
	}
}

// listStub serves a parent's lines in pages of two.
func listStub(ids []string, log *[]string) clientFunc {
	return func(_ context.Context, req stripeapi.Request) (*stripeapi.Response, error) {
		after, _ := req.Params.Get("starting_after")
		*log = append(*log, req.Path)
		start := 0
		if a, ok := after.(wire.String); ok {
			for i, id := range ids {
				if id == string(a) {
					start = i + 1
				}
			}
		}
		end := min(start+2, len(ids))
		var data wire.Array
		for _, id := range ids[start:end] {
			data = append(data, wire.Object{{Name: "id", Value: wire.String(id)}, {Name: "name", Value: wire.String("line " + id)}})
		}
		return &stripeapi.Response{Body: wire.Object{{Name: "data", Value: data}, {Name: "has_more", Value: wire.Bool(end < len(ids))}}}, nil
	}
}

// A line has no retrieve, so the data source reads its parent's list page by page and picks
// the element with the ID.
func TestFindInListReadsPagesUntilTheIDIsFound(t *testing.T) {
	var log []string
	ds := dataSource(t, lineDeclaration(), listStub([]string{"li_1", "li_2", "li_3", "li_4", "li_5"}, &log))
	state, resp := ds.read(t, ds.config(map[string]string{"widget": "w_1", "id": "li_5"}))
	noErrors(t, resp.Diagnostics)
	if str(t, state, "name") != "line li_5" || str(t, state, "widget") != "w_1" {
		t.Errorf("state: %v", state)
	}
	if len(log) != 3 {
		t.Errorf("pages read: %v", log)
	}

	log = nil
	_, resp = ds.read(t, ds.config(map[string]string{"widget": "w_1", "id": "li_9"}))
	if !resp.Diagnostics.HasError() || !strings.Contains(resp.Diagnostics.Errors()[0].Detail(), "no stripe_widget_line found for id=li_9") {
		t.Errorf("want the not found error: %v", resp.Diagnostics)
	}
}

// A list that answers its first page again for every cursor must end the search.
func TestFindInListStopsWhenThePageDoesNotMove(t *testing.T) {
	calls := 0
	ds := dataSource(t, lineDeclaration(), clientFunc(func(context.Context, stripeapi.Request) (*stripeapi.Response, error) {
		calls++
		data := wire.Array{wire.Object{{Name: "id", Value: wire.String("li_1")}}}
		return &stripeapi.Response{Body: wire.Object{{Name: "data", Value: data}, {Name: "has_more", Value: wire.Bool(true)}}}, nil
	}))
	_, resp := ds.read(t, ds.config(map[string]string{"widget": "w_1", "id": "li_9"}))
	if !resp.Diagnostics.HasError() || calls > 3 {
		t.Errorf("calls %d, diagnostics %v", calls, resp.Diagnostics)
	}
}

func TestFindInListDeclarationRules(t *testing.T) {
	for name, edit := range map[string]func(*Declaration){
		"a retrieve path": func(d *Declaration) { d.Paths.Retrieve = "/v1/widgets/{widget}/lines/{id}" },
		"no list path":    func(d *Declaration) { d.Paths.List = "" },
		"no parent":       func(d *Declaration) { d.Parent = "" },
		"a lookup":        func(d *Declaration) { d.Lookups = []Lookup{{Field: "name"}} },
	} {
		decl := lineDeclaration()
		edit(&decl)
		if _, err := NewDataSource(decl, fixtureTable()); err == nil {
			t.Errorf("%s: want an error", name)
		}
	}
}

// The clock has an update timeout, because its advance waits within it.
func TestAdvanceExposesTheUpdateTimeout(t *testing.T) {
	r := configured(t, clockDeclaration(), clockTable(), noCalls(t))
	if _, has := r.schema.Attributes["timeouts"].(rschema.SingleNestedAttribute).Attributes["update"]; !has {
		t.Error("an advance waits within the update timeout, so the schema must offer it")
	}
}

// scriptedClock answers each request from a list of steps and records the paths.
type scriptedClock struct {
	reads []wire.Object
	log   []string
}

func (c *scriptedClock) Do(_ context.Context, req stripeapi.Request) (*stripeapi.Response, error) {
	c.log = append(c.log, req.Method+" "+req.Path[len("/v1/clocks/"):])
	if req.Method == "POST" {
		return &stripeapi.Response{Body: wire.Object{}}, nil
	}
	body := c.reads[0]
	if len(c.reads) > 1 {
		c.reads = c.reads[1:]
	}
	return &stripeapi.Response{Body: body}, nil
}

func reading(frozen int64, status string) wire.Object {
	return wire.Object{{Name: "frozen_time", Value: wire.Int(frozen)}, {Name: "status", Value: wire.String(status)}}
}

func advanceWith(t *testing.T, c *scriptedClock, target int64) error {
	t.Helper()
	r := configured(t, clockDeclaration(), clockTable(), c)
	r.pollInterval = time.Millisecond
	state := r.fill(map[string]attr.Value{"id": types.StringValue("clock_1"), "name": types.StringValue("c"), "frozen_time": types.Int64Value(1000)})
	return r.advance(context.Background(), clockPlan(r, state, "c", target), state, "clock_1", "")
}

// A repeat after a timed-out wait first waits for the advance still running.
func TestAdvanceWaitsForARunningAdvanceBeforeSending(t *testing.T) {
	c := &scriptedClock{reads: []wire.Object{reading(1000, "advancing"), reading(1000, "advancing"), reading(1000, "ready"), reading(5000, "ready")}}
	if err := advanceWith(t, c, 5000); err != nil {
		t.Fatal(err)
	}
	if got := strings.Join(c.log, ";"); got != "GET clock_1;GET clock_1;GET clock_1;POST clock_1/advance;GET clock_1" {
		t.Errorf("requests: %s", got)
	}
}

// A clock already at or past the target needs no request.
func TestAdvanceSkipsAClockAlreadyAtTheTarget(t *testing.T) {
	for _, at := range []int64{5000, 6000} {
		c := &scriptedClock{reads: []wire.Object{reading(at, "ready")}}
		if err := advanceWith(t, c, 5000); err != nil {
			t.Fatal(err)
		}
		if got := strings.Join(c.log, ";"); got != "GET clock_1" {
			t.Errorf("at %d: requests: %s", at, got)
		}
	}
}

// A ready answer with the old time, just after the request, does not end the wait.
func TestAdvanceWaitsUntilTheTargetIsReached(t *testing.T) {
	c := &scriptedClock{reads: []wire.Object{reading(1000, "ready"), reading(1000, "ready"), reading(1000, "ready"), reading(5000, "ready")}}
	if err := advanceWith(t, c, 5000); err != nil {
		t.Fatal(err)
	}
	if got := strings.Join(c.log, ";"); got != "GET clock_1;POST clock_1/advance;GET clock_1;GET clock_1;GET clock_1" {
		t.Errorf("requests: %s", got)
	}
}

// A list that cycles over more than one page, or lists ids already seen, ends the search.
func TestFindInListStopsOnACycleOfPages(t *testing.T) {
	calls := 0
	pages := [][]string{{"li_1", "li_2"}, {"li_3", "li_4"}}
	ds := dataSource(t, lineDeclaration(), clientFunc(func(context.Context, stripeapi.Request) (*stripeapi.Response, error) {
		page := pages[calls%2]
		calls++
		var data wire.Array
		for _, id := range page {
			data = append(data, wire.Object{{Name: "id", Value: wire.String(id)}})
		}
		return &stripeapi.Response{Body: wire.Object{{Name: "data", Value: data}, {Name: "has_more", Value: wire.Bool(true)}}}, nil
	}))
	_, resp := ds.read(t, ds.config(map[string]string{"widget": "w_1", "id": "li_9"}))
	if !resp.Diagnostics.HasError() || calls != 3 {
		t.Errorf("calls %d, diagnostics %v", calls, resp.Diagnostics)
	}
}

// Fresh ids on every page cannot loop for ever either.
func TestFindInListStopsAtThePageCap(t *testing.T) {
	calls := 0
	ds := dataSource(t, lineDeclaration(), clientFunc(func(context.Context, stripeapi.Request) (*stripeapi.Response, error) {
		calls++
		data := wire.Array{wire.Object{{Name: "id", Value: wire.String("li_" + strings.Repeat("x", calls))}}}
		return &stripeapi.Response{Body: wire.Object{{Name: "data", Value: data}, {Name: "has_more", Value: wire.Bool(true)}}}, nil
	}))
	_, resp := ds.read(t, ds.config(map[string]string{"widget": "w_1", "id": "li_9"}))
	if !resp.Diagnostics.HasError() || calls != maxFindPages {
		t.Errorf("calls %d, diagnostics %v", calls, resp.Diagnostics)
	}
}
