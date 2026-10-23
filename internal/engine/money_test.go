package engine

import (
	"context"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-framework/types/basetypes"
	"github.com/hashicorp/terraform-plugin-go/tftypes"

	"github.com/computomatic/terraform-provider-stripe/internal/fields"
	"github.com/computomatic/terraform-provider-stripe/internal/stripeapi"
	"github.com/computomatic/terraform-provider-stripe/internal/wire"
)

// moneyClient answers GET /v1/balance with balance, or err, and passes every other request to
// next. It counts the balance reads.
type moneyClient struct {
	mu      sync.Mutex
	balance wire.Object
	err     error
	reads   int
	next    Client
}

func (c *moneyClient) Do(ctx context.Context, req stripeapi.Request) (*stripeapi.Response, error) {
	if req.Method == "GET" && req.Path == "/v1/balance" {
		c.mu.Lock()
		defer c.mu.Unlock()
		c.reads++
		if c.err != nil {
			return nil, c.err
		}
		return &stripeapi.Response{Status: 200, Body: c.balance}, nil
	}
	return c.next.Do(ctx, req)
}

func (c *moneyClient) balanceReads() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.reads
}

func balanceIn(live bool) wire.Object {
	return wire.Object{{Name: "object", Value: wire.String("balance")}, {Name: "livemode", Value: wire.Bool(live)}}
}

func moneyDeclaration() Declaration {
	d := fixtureDeclaration()
	d.MovesMoney = true
	return d
}

// moneyResource is a resource configured with a live-mode check over client, as the provider
// configures one.
func moneyResource(t *testing.T, decl Declaration, table []fields.Field, client *moneyClient, allow bool) *Resource {
	t.Helper()
	ctor, err := NewResource(decl, table)
	if err != nil {
		t.Fatal(err)
	}
	r := ctor().(*Resource)
	var resp resource.ConfigureResponse
	r.Configure(context.Background(), resource.ConfigureRequest{ProviderData: &ProviderData{Client: client, Live: NewLiveCheck(client, allow, "")}}, &resp)
	noErrors(t, resp.Diagnostics)
	return r
}

func hasSummary(diags diag.Diagnostics, summary string) bool {
	for _, d := range diags.Errors() {
		if d.Summary() == summary {
			return true
		}
	}
	return false
}

// Concurrent changes of one provider configuration ask Stripe once, and test mode lets them
// all through.
func TestLiveCheckReadsTheBalanceOnce(t *testing.T) {
	c := &moneyClient{balance: balanceIn(false)}
	check := NewLiveCheck(c, false, "")
	var wg sync.WaitGroup
	for range 8 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if d := check.refusal(context.Background(), "stripe_widget"); d != nil {
				t.Errorf("test mode refused: %s", d.Detail())
			}
		}()
	}
	wg.Wait()
	if got := c.balanceReads(); got != 1 {
		t.Errorf("balance reads: %d, want 1", got)
	}
}

func TestLiveCheckRefusesLiveMode(t *testing.T) {
	c := &moneyClient{balance: balanceIn(true)}
	d := NewLiveCheck(c, false, "acct_1").refusal(context.Background(), "stripe_payment_intent")
	if d == nil || d.Summary() != liveRefusedSummary {
		t.Fatalf("want the live-mode refusal, got %v", d)
	}
	for _, want := range []string{"the connected account `acct_1`", "Creating or changing a payment intent", "allow_live_money_movement = true"} {
		if !strings.Contains(d.Detail(), want) {
			t.Errorf("detail lacks %q: %s", want, d.Detail())
		}
	}
}

// Anything but a boolean `livemode` refuses the change, and the failure is the answer for the
// rest of the configuration's life.
func TestLiveCheckFailsClosed(t *testing.T) {
	for name, c := range map[string]*moneyClient{
		"stripe error": {err: &stripeapi.APIError{Status: 403, Type: "invalid_request_error", Message: "The provided key does not have the required permissions", RequestID: "req_1"}},
		"no livemode":  {balance: wire.Object{{Name: "object", Value: wire.String("balance")}}},
		"not a bool":   {balance: wire.Object{{Name: "livemode", Value: wire.String("false")}}},
	} {
		check := NewLiveCheck(c, false, "")
		for range 2 {
			d := check.refusal(context.Background(), "stripe_widget")
			if d == nil || d.Summary() != cannotTellSummary || !strings.Contains(d.Detail(), "Balance read permission") {
				t.Fatalf("%s: want the fail-closed refusal, got %v", name, d)
			}
		}
		if c.balanceReads() != 1 {
			t.Errorf("%s: balance reads %d, want 1", name, c.balanceReads())
		}
		if name == "stripe error" && !strings.Contains(check.refusal(context.Background(), "stripe_widget").Detail(), "Request-Id: req_1") {
			t.Errorf("the detail names no request ID")
		}
	}
}

func TestLiveCheckAllowedAsksNothing(t *testing.T) {
	c := &moneyClient{balance: balanceIn(true)}
	if d := NewLiveCheck(c, true, "").refusal(context.Background(), "stripe_widget"); d != nil || c.balanceReads() != 0 {
		t.Errorf("allowed: refusal %v, balance reads %d", d, c.balanceReads())
	}
}

// blockingBalance answers GET /v1/balance in test mode once release is closed, or fails with
// the caller's context error when that ends first. It counts the reads.
type blockingBalance struct {
	release chan struct{}
	mu      sync.Mutex
	reads   int
}

func (b *blockingBalance) Do(ctx context.Context, _ stripeapi.Request) (*stripeapi.Response, error) {
	b.mu.Lock()
	b.reads++
	b.mu.Unlock()
	select {
	case <-b.release:
		return &stripeapi.Response{Status: 200, Body: balanceIn(false)}, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

func (b *blockingBalance) readCount() int {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.reads
}

// A read cut short by its caller's context refuses that change but is not the answer: the next
// change reads the balance again.
func TestLiveCheckDoesNotKeepAContextError(t *testing.T) {
	b := &blockingBalance{release: make(chan struct{})}
	check := NewLiveCheck(b, false, "")
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if d := check.refusal(ctx, "stripe_widget"); d == nil || d.Summary() != cannotTellSummary {
		t.Fatalf("a canceled read must refuse: %v", d)
	}
	close(b.release)
	if d := check.refusal(context.Background(), "stripe_widget"); d != nil {
		t.Fatalf("after a canceled read: %s", d.Detail())
	}
	if got := b.readCount(); got != 2 {
		t.Errorf("balance reads: %d, want 2", got)
	}
}

// A change that waits for another's read gives up when its own context ends, and the read it
// waited for still answers the rest.
func TestLiveCheckWaiterHonoursItsOwnContext(t *testing.T) {
	b := &blockingBalance{release: make(chan struct{})}
	check := NewLiveCheck(b, false, "")
	first := make(chan diag.Diagnostic, 1)
	go func() { first <- check.refusal(context.Background(), "stripe_widget") }()
	for deadline := time.Now().Add(5 * time.Second); b.readCount() == 0; time.Sleep(time.Millisecond) {
		if time.Now().After(deadline) {
			t.Fatal("the first read never started")
		}
	}

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	waited := make(chan diag.Diagnostic, 1)
	go func() { waited <- check.refusal(ctx, "stripe_widget") }()
	select {
	case d := <-waited:
		if d == nil || d.Summary() != cannotTellSummary {
			t.Errorf("the waiter's own deadline must refuse its change: %v", d)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("the waiter outlived its own context")
	}

	close(b.release)
	if d := <-first; d != nil {
		t.Errorf("the first read: %s", d.Detail())
	}
	if d := check.refusal(context.Background(), "stripe_widget"); d != nil || b.readCount() != 1 {
		t.Errorf("the first read is the answer: %v, balance reads %d", d, b.readCount())
	}
}

// ModifyPlan checks a create and a plan that changes the object, last, and nothing else: a
// plan that changes nothing, a destroy, a plan another check refused and a resource that moves
// no money ask Stripe nothing.
func TestModifyPlanChecksOnlyChanges(t *testing.T) {
	ctx := context.Background()
	stub := newStub()
	r := moneyResource(t, moneyDeclaration(), fixtureTable(), &moneyClient{balance: balanceIn(false), next: stub}, false)
	state, _ := r.create(t, r.fill(map[string]attr.Value{"name": types.StringValue("w"), "quantity": types.Int64Value(1)}))

	live := func(decl Declaration) (*Resource, *moneyClient) {
		c := &moneyClient{balance: balanceIn(true), next: stub}
		return moneyResource(t, decl, fixtureTable(), c, false), c
	}
	null := tftypes.NewValue(r.schema.Type().TerraformType(ctx), nil)

	lr, c := live(moneyDeclaration())
	if resp := lr.modifyPlan(t, state, state); resp.Diagnostics.HasError() || c.balanceReads() != 0 {
		t.Errorf("no change: %v, balance reads %d", resp.Diagnostics, c.balanceReads())
	}
	var destroy resource.ModifyPlanResponse
	lr.ModifyPlan(ctx, resource.ModifyPlanRequest{
		Config: tfsdk.Config{Schema: lr.schema, Raw: null}, Plan: tfsdk.Plan{Schema: lr.schema, Raw: null},
		State: tfsdk.State{Schema: lr.schema, Raw: raw(t, state)},
	}, &destroy)
	if destroy.Diagnostics.HasError() || c.balanceReads() != 0 {
		t.Errorf("destroy: %v, balance reads %d", destroy.Diagnostics, c.balanceReads())
	}
	changed := lr.fill(with(state, map[string]attr.Value{"name": types.StringValue("other")}))
	if resp := lr.modifyPlan(t, changed, state); !hasSummary(resp.Diagnostics, liveRefusedSummary) || c.balanceReads() != 1 {
		t.Errorf("a change: %v, balance reads %d", resp.Diagnostics, c.balanceReads())
	}
	plan := lr.fill(map[string]attr.Value{"name": types.StringValue("w")})
	create := resource.ModifyPlanResponse{Plan: tfsdk.Plan{Schema: lr.schema, Raw: raw(t, plan)}}
	lr.ModifyPlan(ctx, resource.ModifyPlanRequest{
		Config: tfsdk.Config{Schema: lr.schema, Raw: raw(t, plan)}, Plan: tfsdk.Plan{Schema: lr.schema, Raw: raw(t, plan)},
		State: tfsdk.State{Schema: lr.schema, Raw: null},
	}, &create)
	if !hasSummary(create.Diagnostics, liveRefusedSummary) {
		t.Errorf("a create: %v", create.Diagnostics)
	}

	// Leaving a final value is refused before the mode is asked.
	final := chainDeclaration()
	final.MovesMoney = true
	for i := range final.Transitions {
		final.Transitions[i].Final = []string{"stuck"}
	}
	stage, log := "stuck", []string{}
	fc := &moneyClient{balance: balanceIn(true), next: stageClient(&stage, &log)}
	fr := moneyResource(t, final, chainTable(), fc, true)
	stuck, _ := fr.create(t, fr.fill(map[string]attr.Value{"name": types.StringValue("w")}))
	fr.live = NewLiveCheck(fc, false, "")
	if resp := fr.modifyPlan(t, fr.fill(with(stuck, map[string]attr.Value{"stage": types.StringValue("done")})), stuck); hasSummary(resp.Diagnostics, liveRefusedSummary) || !resp.Diagnostics.HasError() || fc.balanceReads() != 0 {
		t.Errorf("a plan another check refuses: %v, balance reads %d", resp.Diagnostics, fc.balanceReads())
	}

	nr, nc := live(fixtureDeclaration())
	if resp := nr.modifyPlan(t, changed, state); resp.Diagnostics.HasError() || nc.balanceReads() != 0 {
		t.Errorf("no money: %v, balance reads %d", resp.Diagnostics, nc.balanceReads())
	}

	lr.live = nil
	if resp := lr.modifyPlan(t, changed, state); !hasSummary(resp.Diagnostics, cannotTellSummary) {
		t.Errorf("an unconfigured provider must refuse: %v", resp.Diagnostics)
	}
}

// The apply checks again before its first request, for a replacement or a saved plan that
// plan time did not see, and sends nothing in live mode.
func TestApplyRefusesLiveMoneyBeforeAnyRequest(t *testing.T) {
	ctx := context.Background()
	stub := newStub()
	r := moneyResource(t, moneyDeclaration(), fixtureTable(), &moneyClient{balance: balanceIn(false), next: stub}, false)
	state, _ := r.create(t, r.fill(map[string]attr.Value{"name": types.StringValue("w")}))
	stub.reset()

	lr := moneyResource(t, moneyDeclaration(), fixtureTable(), &moneyClient{balance: balanceIn(true), next: stub}, false)
	plan := lr.fill(map[string]attr.Value{"name": types.StringValue("w")})
	created := resource.CreateResponse{State: tfsdk.State{Schema: lr.schema, Raw: tftypes.NewValue(lr.schema.Type().TerraformType(ctx), nil)}}
	lr.Create(ctx, resource.CreateRequest{Plan: tfsdk.Plan{Schema: lr.schema, Raw: raw(t, plan)}}, &created)
	if !hasSummary(created.Diagnostics, liveRefusedSummary) {
		t.Errorf("create: %v", created.Diagnostics)
	}
	changed := lr.fill(with(state, map[string]attr.Value{"name": types.StringValue("other")}))
	updated := resource.UpdateResponse{State: tfsdk.State{Schema: lr.schema, Raw: raw(t, state)}}
	lr.Update(ctx, resource.UpdateRequest{Plan: tfsdk.Plan{Schema: lr.schema, Raw: raw(t, changed)}, State: tfsdk.State{Schema: lr.schema, Raw: raw(t, state)}}, &updated)
	if !hasSummary(updated.Diagnostics, liveRefusedSummary) {
		t.Errorf("update: %v", updated.Diagnostics)
	}
	if got := stub.requests(); len(got) != 0 {
		t.Errorf("requests reached Stripe: %v", got)
	}
}

// Reading a live object asks nothing: refresh and data sources never check the mode.
func TestReadsNeverAskForTheMode(t *testing.T) {
	stub := newStub()
	r := moneyResource(t, moneyDeclaration(), fixtureTable(), &moneyClient{balance: balanceIn(false), next: stub}, false)
	state, _ := r.create(t, r.fill(map[string]attr.Value{"name": types.StringValue("w"), "kind": types.StringValue("a")}))

	c := &moneyClient{balance: balanceIn(true), next: stub}
	if _, ok := moneyResource(t, moneyDeclaration(), fixtureTable(), c, false).read(t, state); !ok {
		t.Fatal("the object is gone")
	}
	dataSource(t, moneyDeclaration(), c).read(t, dataSource(t, moneyDeclaration(), c).config(map[string]string{"id": str(t, state, "id")}))
	if c.balanceReads() != 0 {
		t.Errorf("balance reads: %d", c.balanceReads())
	}
}

func chainTable() []fields.Field {
	return append(fixtureTable(), fields.Field{Name: "stage", Shape: fields.String, Response: true, Enum: []string{"ready", "held", "done", "stuck"}})
}

// chainDeclaration reaches `done` through two actions, each from its own stage, as a payment
// intent's `succeeded` confirms and then captures.
func chainDeclaration() Declaration {
	d := fixtureDeclaration()
	d.Transitions = []Transition{
		{Field: "stage", Value: "done", Path: "/v1/widgets/{id}/confirm", From: []string{"ready"}},
		{Field: "stage", Value: "done", Path: "/v1/widgets/{id}/capture", From: []string{"held"}},
	}
	return d
}

// stageClient serves one widget whose stage starts at *stage: confirm moves `ready` to `held`
// and capture `held` to `done`.
func stageClient(stage *string, log *[]string) Client {
	return clientFunc(func(_ context.Context, req stripeapi.Request) (*stripeapi.Response, error) {
		*log = append(*log, req.Method+" "+req.Path)
		switch {
		case req.Path == "/v1/widgets/wid_1/confirm" && *stage == "ready":
			*stage = "held"
		case req.Path == "/v1/widgets/wid_1/capture" && *stage == "held":
			*stage = "done"
		}
		return &stripeapi.Response{Body: wire.Object{
			{Name: "id", Value: wire.String("wid_1")},
			{Name: "name", Value: wire.String("w")},
			{Name: "stage", Value: wire.String(*stage)},
		}}, nil
	})
}

func (r *Resource) tryCreate(t *testing.T, plan basetypes.ObjectValue) resource.CreateResponse {
	t.Helper()
	ctx := context.Background()
	resp := resource.CreateResponse{State: tfsdk.State{Schema: r.schema, Raw: tftypes.NewValue(r.schema.Type().TerraformType(ctx), nil)}}
	r.Create(ctx, resource.CreateRequest{Plan: tfsdk.Plan{Schema: r.schema, Raw: raw(t, plan)}}, &resp)
	return resp
}

// Transitions that share a value run in order, each only from its own starting value, so one
// apply confirms and captures, and an object already past the first step only captures.
func TestTransitionsChainFromTheirStartingValues(t *testing.T) {
	for start, want := range map[string]string{
		"ready": "POST /v1/widgets;POST /v1/widgets/wid_1/confirm;POST /v1/widgets/wid_1/capture",
		"held":  "POST /v1/widgets;POST /v1/widgets/wid_1/capture",
	} {
		stage, log := start, []string{}
		r := configured(t, chainDeclaration(), chainTable(), stageClient(&stage, &log))
		state, _ := r.create(t, r.fill(map[string]attr.Value{"name": types.StringValue("w"), "stage": types.StringValue("done")}))
		if got := strings.Join(log, ";"); got != want || str(t, state, "stage") != "done" {
			t.Errorf("from %s: requests %s, stage %s", start, got, str(t, state, "stage"))
		}
	}
}

// An apply that leaves a transition field short of the planned value fails on the attribute
// and records what Stripe left. On a resource whose destroy is refused, a create that fails so
// says how to clear the taint.
func TestApplyFailsWhenTheTransitionDoesNotArrive(t *testing.T) {
	ctx := context.Background()
	for _, refuse := range []bool{false, true} {
		decl := chainDeclaration()
		if refuse {
			decl.Delete, decl.Paths.Delete, decl.RefuseReason = Refuse, "", "Stripe cannot delete a widget."
		}
		stage, log := "stuck", []string{}
		r := configured(t, decl, chainTable(), stageClient(&stage, &log))
		plan := r.fill(map[string]attr.Value{"name": types.StringValue("w"), "stage": types.StringValue("done")})
		resp := r.tryCreate(t, plan)
		if !hasSummary(resp.Diagnostics, "Stripe left the widget at `stuck`") {
			t.Fatalf("refuse %v: %v", refuse, resp.Diagnostics)
		}
		if got := strings.Join(log, ";"); got != "POST /v1/widgets" {
			t.Errorf("no action starts from `stuck`: %s", got)
		}
		var state basetypes.ObjectValue
		noErrors(t, resp.State.Get(ctx, &state))
		if str(t, state, "stage") != "stuck" {
			t.Errorf("the state must keep what Stripe left: %s", str(t, state, "stage"))
		}
		if tainted := hasSummary(resp.Diagnostics, "The widget exists and is tainted"); tainted != refuse {
			t.Errorf("refuse %v: taint guidance %v", refuse, tainted)
		}

		updated := resource.UpdateResponse{State: tfsdk.State{Schema: r.schema, Raw: raw(t, state)}}
		changed := r.fill(with(state, map[string]attr.Value{"stage": types.StringValue("done")}))
		r.Update(ctx, resource.UpdateRequest{Plan: tfsdk.Plan{Schema: r.schema, Raw: raw(t, changed)}, State: tfsdk.State{Schema: r.schema, Raw: raw(t, state)}}, &updated)
		if !hasSummary(updated.Diagnostics, "Stripe left the widget at `stuck`") || hasSummary(updated.Diagnostics, "The widget exists and is tainted") {
			t.Errorf("update: %v", updated.Diagnostics)
		}
	}
}

// A create whose response cannot be read leaves the object in Stripe, tainted: on a resource
// whose destroy is refused the error says how to clear the taint.
func TestCreateWithAnUnreadableResponseSaysToUntaint(t *testing.T) {
	decl := fixtureDeclaration()
	decl.Delete, decl.Paths.Delete, decl.RefuseReason = Refuse, "", "Stripe cannot delete a widget."
	client := clientFunc(func(_ context.Context, _ stripeapi.Request) (*stripeapi.Response, error) {
		return &stripeapi.Response{Body: wire.Object{
			{Name: "id", Value: wire.String("wid_1")},
			{Name: "name", Value: wire.String("w")},
			{Name: "address", Value: wire.String("not an object")},
		}}, nil
	})
	r := configured(t, decl, fixtureTable(), client)
	resp := r.tryCreate(t, r.fill(map[string]attr.Value{"name": types.StringValue("w")}))
	if !hasSummary(resp.Diagnostics, "Unexpected Stripe response") || !hasSummary(resp.Diagnostics, "The widget exists and is tainted") {
		t.Errorf("want the decode error and the taint guidance: %v", resp.Diagnostics)
	}
}

func TestMoneyAndChainDeclarationsMustBeConsistent(t *testing.T) {
	disagree := chainDeclaration()
	disagree.Transitions[0].Final = []string{"stuck"}
	fromItself := chainDeclaration()
	fromItself.Transitions[1].From = []string{"done"}
	noResource := moneyDeclaration()
	noResource.Delete = NoDelete
	for name, decl := range map[string]Declaration{"final disagrees": disagree, "from its own value": fromItself, "money without a resource": noResource} {
		if _, err := compile(decl, chainTable()); err == nil {
			t.Errorf("%s: want an error", name)
		}
	}
	agree := chainDeclaration()
	for i := range agree.Transitions {
		agree.Transitions[i].Final = []string{"stuck"}
	}
	if _, err := compile(agree, chainTable()); err != nil {
		t.Errorf("agreeing transitions: %v", err)
	}
}

func TestMoneyNote(t *testing.T) {
	plain, err := compile(moneyDeclaration(), chainTable())
	if err != nil {
		t.Fatal(err)
	}
	staged := chainDeclaration()
	staged.MovesMoney = true
	withStatus, err := compile(staged, chainTable())
	if err != nil {
		t.Fatal(err)
	}
	none, err := compile(fixtureDeclaration(), chainTable())
	if err != nil {
		t.Fatal(err)
	}
	if got, want := plain.moneyNote(), "A widget can move real money. In a live-mode Stripe account, creating or changing one fails at plan time unless the provider sets `allow_live_money_movement = true`. Reading or importing one never does. The provider asks Stripe for the account's mode. See the Managing Test Data guide."; got != want {
		t.Errorf("note:\n%s\nwant:\n%s", got, want)
	}
	if !strings.Contains(withStatus.moneyNote(), "creating or changing one, including setting its `stage`, fails") {
		t.Errorf("note with a transition: %s", withStatus.moneyNote())
	}
	if none.moneyNote() != "" {
		t.Errorf("a resource that moves no money has no note")
	}
}
