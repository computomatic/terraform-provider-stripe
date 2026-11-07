package engine

import (
	"context"
	"errors"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-framework/types/basetypes"

	"github.com/computomatic/terraform-provider-stripe/internal/fields"
	"github.com/computomatic/terraform-provider-stripe/internal/stripeapi"
	"github.com/computomatic/terraform-provider-stripe/internal/wire"
)

// refusedTable is the fixture with two recorded refusals: a create that sends the items `x` and
// `y` twice, and an update that sends `address.line1` `Blocked` to a widget of kind `a` that
// holds an item.
func refusedTable() []fields.Field {
	table := fixtureTable()
	for i := range table {
		switch table[i].Name {
		case "items":
			table[i].Fields[0].PlanRefusals = []fields.PlanRefusal{{Sends: `{"items":[{"sku":"x"},{"qty":2,"sku":"y"}]}`, Probe: "isolated/x_and_two_y", AccountCountry: "US", Message: "Not x beside two y."}}
		case "address":
			table[i].Fields[0].PlanRefusals = []fields.PlanRefusal{{Update: true, Sends: `{"address":{"line1":"Blocked"}}`, Holds: `{"items":[{"sku":{"$any":true}}],"kind":"a"}`,
				Probe: "isolated/blocked_line", AccountCountry: "US", Message: "Line blocked."}}
		}
	}
	return table
}

// A plan whose create or update would send a request Stripe refused, in the record, fails with
// Stripe's message, the request and the probe; any other request, one the configuration does not
// know yet and an update that is a replace, which creates, pass the update's refusal.
func TestRecordedRefusalsFailThePlan(t *testing.T) {
	r, _ := countryResource(t, "US", nil)
	itemType := r.objType.AttrTypes["items"].(basetypes.ListType).ElemType.(basetypes.ObjectType)
	addressType := r.objType.AttrTypes["address"].(basetypes.ObjectType)
	item := func(sku attr.Value, qty int64) attr.Value {
		q := types.Int64Null()
		if qty > 0 {
			q = types.Int64Value(qty)
		}
		return types.ObjectValueMust(itemType.AttrTypes, map[string]attr.Value{"sku": sku, "qty": q})
	}
	items := func(elems ...attr.Value) attr.Value { return types.ListValueMust(itemType, elems) }
	address := func(line1 string) attr.Value {
		return types.ObjectValueMust(addressType.AttrTypes, map[string]attr.Value{"line1": sv(line1), "zip": types.StringNull()})
	}
	none := types.ObjectNull(r.objType.AttrTypes)

	for name, tc := range map[string]struct {
		items  attr.Value
		refuse bool
	}{
		"the refused request":           {items(item(sv("x"), 0), item(sv("y"), 2)), true},
		"its elements reordered":        {items(item(sv("y"), 2), item(sv("x"), 0)), true},
		"another quantity":              {items(item(sv("x"), 0), item(sv("y"), 3)), false},
		"another element beside":        {items(item(sv("x"), 0), item(sv("y"), 2), item(sv("z"), 0)), false},
		"an element not known at plan":  {items(item(sv("x"), 0), item(types.StringUnknown(), 2)), false},
		"a member not known beside":     {items(types.ObjectValueMust(itemType.AttrTypes, map[string]attr.Value{"sku": sv("x"), "qty": types.Int64Unknown()}), item(sv("y"), 2)), false},
		"the elements not known at all": {types.ListUnknown(itemType), false},
	} {
		plan := r.fill(map[string]attr.Value{"name": sv("w"), "items": tc.items})
		resp := r.modifyPlan(t, plan, none)
		if resp.Diagnostics.HasError() != tc.refuse {
			t.Errorf("create, %s: refused %t, want %t: %v", name, resp.Diagnostics.HasError(), tc.refuse, resp.Diagnostics)
		}
	}
	resp := r.modifyPlan(t, r.fill(map[string]attr.Value{"name": sv("w"), "items": items(item(sv("x"), 0), item(sv("y"), 2))}), none)
	if detail := diagDetail(resp); !strings.Contains(detail, "`items[].sku = \"x\"`, `items[].qty = 2` and `items[].sku = \"y\"`") ||
		!strings.Contains(detail, `"Not x beside two y."`) || !strings.Contains(detail, "probe `isolated/x_and_two_y`") || !strings.Contains(detail, "Stripe account in US") {
		t.Errorf("the refusal's detail: %s", detail)
	}

	state := r.fill(map[string]attr.Value{"id": sv("wid_1"), "name": sv("w"), "kind": sv("a"), "items": items(item(sv("s"), 0)), "address": address("Main")})
	held := func(set map[string]attr.Value) basetypes.ObjectValue { return r.fill(with(state, set)) }
	for name, tc := range map[string]struct {
		state, plan basetypes.ObjectValue
		refuse      bool
	}{
		"the refused update":                  {state, held(map[string]attr.Value{"address": address("Blocked")}), true},
		"another line":                        {state, held(map[string]attr.Value{"address": address("High")}), false},
		"to a widget of another kind":         {held(map[string]attr.Value{"kind": sv("b")}), held(map[string]attr.Value{"kind": sv("b"), "address": address("Blocked")}), false},
		"to a widget with no items":           {held(map[string]attr.Value{"items": types.ListNull(itemType)}), held(map[string]attr.Value{"items": types.ListNull(itemType), "address": address("Blocked")}), false},
		"a replace, which creates the widget": {state, held(map[string]attr.Value{"kind": sv("b"), "address": address("Blocked")}), false},
	} {
		resp := r.modifyPlan(t, tc.plan, tc.state)
		if resp.Diagnostics.HasError() != tc.refuse {
			t.Errorf("update, %s: refused %t, want %t: %v", name, resp.Diagnostics.HasError(), tc.refuse, resp.Diagnostics)
		}
	}
	resp = r.modifyPlan(t, held(map[string]attr.Value{"address": address("Blocked")}), state)
	if detail := diagDetail(resp); !strings.Contains(detail, "The plan is an update that sends `address.line1 = \"Blocked\"` to a widget that holds `items[].sku` set and `kind = \"a\"`") {
		t.Errorf("the update's refusal: %s", detail)
	}
}

// countryClient answers GET /v1/account with an account in country, or err, counts the reads,
// and fails on any other request.
type countryClient struct {
	t       *testing.T
	country string
	err     error
	reads   atomic.Int32
}

func (c *countryClient) Do(_ context.Context, req stripeapi.Request) (*stripeapi.Response, error) {
	if req.Method != "GET" || req.Path != "/v1/account" {
		c.t.Errorf("unexpected request %s %s", req.Method, req.Path)
		return nil, errors.New("unexpected request")
	}
	c.reads.Add(1)
	if c.err != nil {
		return nil, c.err
	}
	return &stripeapi.Response{Status: 200, Body: wire.Object{{Name: "object", Value: wire.String("account")}, {Name: "country", Value: wire.String(c.country)}}}, nil
}

// countryResource is the refused fixture configured, as the provider configures it, with the
// country of an account in country, or a read that fails with err.
func countryResource(t *testing.T, country string, err error) (*Resource, *countryClient) {
	t.Helper()
	ctor, cerr := NewResource(fixtureDeclaration(), refusedTable())
	if cerr != nil {
		t.Fatal(cerr)
	}
	r := ctor().(*Resource)
	client := &countryClient{t: t, country: country, err: err}
	var resp resource.ConfigureResponse
	r.Configure(context.Background(), resource.ConfigureRequest{ProviderData: &ProviderData{Client: client, Country: NewAccountCountry(client)}}, &resp)
	noErrors(t, resp.Diagnostics)
	return r, client
}

// A plan that matches a recorded refusal is refused only where the provider's account is in the
// country the refusal was recorded in, which the provider reads once, then and only then: a plan
// that matches none asks Stripe nothing, and an account in another country, or one the key
// cannot read, is left to Stripe.
func TestRecordedRefusalsHoldInTheirCountry(t *testing.T) {
	refused := func(r *Resource) basetypes.ObjectValue {
		itemType := r.objType.AttrTypes["items"].(basetypes.ListType).ElemType.(basetypes.ObjectType)
		item := func(sku string, qty attr.Value) attr.Value {
			return types.ObjectValueMust(itemType.AttrTypes, map[string]attr.Value{"sku": sv(sku), "qty": qty})
		}
		return r.fill(map[string]attr.Value{"name": sv("w"), "items": types.ListValueMust(itemType, []attr.Value{item("x", types.Int64Null()), item("y", types.Int64Value(2))})})
	}
	for name, tc := range map[string]struct {
		country string
		err     error
		refuse  bool
	}{
		"the same country":             {"US", nil, true},
		"the same country, lower case": {"us", nil, true},
		"another country":              {"GB", nil, false},
		"an account it cannot read":    {"", &stripeapi.APIError{Status: 403, Message: "The provided key does not have the required permissions."}, false},
	} {
		r, client := countryResource(t, tc.country, tc.err)
		none := types.ObjectNull(r.objType.AttrTypes)
		if resp := r.modifyPlan(t, r.fill(map[string]attr.Value{"name": sv("w")}), none); resp.Diagnostics.HasError() || client.reads.Load() != 0 {
			t.Errorf("%s: a plan that matches nothing: %v, %d reads", name, resp.Diagnostics, client.reads.Load())
		}
		for range 2 {
			if resp := r.modifyPlan(t, refused(r), none); resp.Diagnostics.HasError() != tc.refuse {
				t.Errorf("%s: refused %t, want %t: %v", name, resp.Diagnostics.HasError(), tc.refuse, resp.Diagnostics)
			}
		}
		if got := client.reads.Load(); got != 1 {
			t.Errorf("%s: %d reads of the account, want 1", name, got)
		}
	}
	unconfigured := configured(t, fixtureDeclaration(), refusedTable(), noCalls(t))
	if resp := unconfigured.modifyPlan(t, refused(unconfigured), types.ObjectNull(unconfigured.objType.AttrTypes)); resp.Diagnostics.HasError() {
		t.Errorf("no country to read: %v", resp.Diagnostics)
	}
}

// The docs line of an argument with recorded refusals says what the plan refuses, in which
// country, with Stripe's message and the probe.
func TestRecordedRefusalsAreDocumented(t *testing.T) {
	r, _ := countryResource(t, "US", nil)
	doc := r.AttributeDocs()["address.line1"].PlanRefusal
	for _, want := range []string{
		"Where the provider's Stripe account is in `US`, the plan refuses an update that sends `address.line1 = \"Blocked\"` to a widget that holds `items[].sku` set and `kind = \"a\"`",
		`which Stripe refused there with "Line blocked." (conformance suite probe ` + "`isolated/blocked_line`).",
		"leaves the request to Stripe where the account is elsewhere or the key cannot read it.",
	} {
		if !strings.Contains(doc, want) {
			t.Errorf("the docs line %q lacks %q", doc, want)
		}
	}
	if doc := r.AttributeDocs()["address.zip"].PlanRefusal; doc != "" {
		t.Errorf("an argument with no refusal: %q", doc)
	}
}

// A refusal whose pattern is not a JSON object fails the declaration.
func TestRecordedRefusalsMustBeObjects(t *testing.T) {
	for name, refusal := range map[string]fields.PlanRefusal{
		"sends nothing":         {Sends: `{}`, Probe: "isolated/empty"},
		"sends a list":          {Sends: `[1]`, Probe: "isolated/list"},
		"holds a malformed one": {Sends: `{"name":"w"}`, Holds: `{`, Probe: "isolated/malformed"},
	} {
		table := fixtureTable()
		table[2].PlanRefusals = []fields.PlanRefusal{refusal}
		if _, err := NewResource(fixtureDeclaration(), table); err == nil || !strings.Contains(err.Error(), refusal.Probe) {
			t.Errorf("%s: %v", name, err)
		}
	}
}

// diagDetail joins the details of a response's errors.
func diagDetail(resp *resource.ModifyPlanResponse) string {
	var out []string
	for _, d := range resp.Diagnostics.Errors() {
		out = append(out, d.Detail())
	}
	return strings.Join(out, "\n")
}
