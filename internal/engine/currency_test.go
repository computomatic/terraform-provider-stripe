package engine

import (
	"context"
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

func currencyTable() []fields.Field {
	return []fields.Field{
		{Name: "id", Shape: fields.String, Response: true},
		{Name: "currency", Shape: fields.String, Required: true, Create: true, Response: true},
		{Name: "nickname", Shape: fields.String, Create: true, Update: true, Response: true},
		{Name: "currency_options", Shape: fields.ObjectMap, Create: true, Update: true, Response: true, Fields: []fields.Field{
			{Name: "unit_amount", Shape: fields.Int, Create: true, Update: true, Response: true},
			{Name: "amount_off", Shape: fields.Int, Create: true, Update: true, Response: true},
		}},
	}
}

func currencyDeclaration() Declaration {
	return Declaration{
		TypeName: "stripe_widget",
		Paths: Paths{
			Create:   "/v1/widgets",
			Retrieve: "/v1/widgets/{id}",
			Update:   "/v1/widgets/{id}",
			Delete:   "/v1/widgets/{id}",
		},
		Delete:          Delete,
		CurrencyOptions: "currency_options",
		ServerDefaulted: []string{"currency_options.unit_amount"},
	}
}

func optionsValue(r *Resource, amounts map[string]int64) basetypes.MapValue {
	elemType := r.objType.AttrTypes["currency_options"].(basetypes.MapType).ElemType
	elems := map[string]attr.Value{}
	for k, v := range amounts {
		elems[k] = object(elemType, map[string]attr.Value{"amount_off": types.Int64Value(v)})
	}
	return types.MapValueMust(elemType, elems)
}

// widgetBody is a response whose expanded options include the own currency usd.
func widgetBody(options wire.Object) wire.Object {
	return wire.Object{
		{Name: "id", Value: wire.String("wid_1")},
		{Name: "currency", Value: wire.String("usd")},
		{Name: "currency_options", Value: options},
	}
}

func option(currency string, amount int64) wire.Field {
	return wire.Field{Name: currency, Value: wire.Object{{Name: "amount_off", Value: wire.Int(amount)}}}
}

func TestCurrencyOptionsKeepTheOwnCurrencyOutOfState(t *testing.T) {
	r := configured(t, currencyDeclaration(), currencyTable(), clientFunc(func(_ context.Context, req stripeapi.Request) (*stripeapi.Response, error) {
		return &stripeapi.Response{Status: 200, Body: widgetBody(wire.Object{option("eur", 400), option("usd", 500)})}, nil
	}))
	plan := r.fill(map[string]attr.Value{
		"currency":         types.StringValue("usd"),
		"currency_options": optionsValue(r, map[string]int64{"eur": 400}),
	})
	state, _ := r.create(t, plan)
	if !state.Attributes()["currency_options"].Equal(optionsValue(r, map[string]int64{"eur": 400})) {
		t.Fatalf("the own currency reached state: %v", state.Attributes()["currency_options"])
	}
}

func TestCurrencyOptionsOnlyOwnCurrencyReadsAsNull(t *testing.T) {
	r := configured(t, currencyDeclaration(), currencyTable(), clientFunc(func(_ context.Context, req stripeapi.Request) (*stripeapi.Response, error) {
		return &stripeapi.Response{Status: 200, Body: widgetBody(wire.Object{option("usd", 500)})}, nil
	}))
	state, _ := r.create(t, r.fill(map[string]attr.Value{"currency": types.StringValue("usd")}))
	if !state.Attributes()["currency_options"].IsNull() {
		t.Fatalf("a price with no options must read back null, got %v", state.Attributes()["currency_options"])
	}
}

func TestCurrencyOptionsUpdateSendsOnlyNewCurrencies(t *testing.T) {
	var sent wire.Object
	r := configured(t, currencyDeclaration(), currencyTable(), clientFunc(func(_ context.Context, req stripeapi.Request) (*stripeapi.Response, error) {
		if req.Method == "POST" {
			sent = req.Params
		}
		return &stripeapi.Response{Status: 200, Body: widgetBody(wire.Object{option("eur", 400), option("gbp", 300), option("usd", 500)})}, nil
	}))
	state := r.fill(map[string]attr.Value{
		"id":               types.StringValue("wid_1"),
		"currency":         types.StringValue("usd"),
		"currency_options": optionsValue(r, map[string]int64{"eur": 400}),
	})
	plan := r.fill(map[string]attr.Value{
		"id":               types.StringValue("wid_1"),
		"currency":         types.StringValue("usd"),
		"currency_options": optionsValue(r, map[string]int64{"eur": 400, "gbp": 300}),
	})
	next := r.update(t, plan, state)
	if got := wireOf(t, sent); !strings.Contains(got, `"currency_options":{"gbp":{"amount_off":300}}`) || strings.Contains(got, "eur") {
		t.Fatalf("the update must send only the new currency, sent %s", got)
	}
	if !next.Attributes()["currency_options"].Equal(optionsValue(r, map[string]int64{"eur": 400, "gbp": 300})) {
		t.Fatalf("state after update: %v", next.Attributes()["currency_options"])
	}
}

func TestCurrencyOptionsChangedRequiresReplacement(t *testing.T) {
	r := configured(t, currencyDeclaration(), currencyTable(), noCalls(t))
	opts := func(amounts map[string]int64) basetypes.MapValue { return optionsValue(r, amounts) }
	elemType := r.objType.AttrTypes["currency_options"].(basetypes.MapType).ElemType
	for name, tc := range map[string]struct {
		config, state basetypes.MapValue
		replace       bool
	}{
		"unchanged":       {opts(map[string]int64{"eur": 400}), opts(map[string]int64{"eur": 400}), false},
		"added":           {opts(map[string]int64{"eur": 400, "gbp": 300}), opts(map[string]int64{"eur": 400}), false},
		"changed":         {opts(map[string]int64{"eur": 450}), opts(map[string]int64{"eur": 400}), true},
		"removed":         {opts(map[string]int64{"eur": 400}), opts(map[string]int64{"eur": 400, "gbp": 300}), true},
		"all removed":     {types.MapNull(elemType), opts(map[string]int64{"eur": 400}), true},
		"none before":     {opts(map[string]int64{"eur": 400}), types.MapNull(elemType), false},
		"unknown":         {types.MapUnknown(elemType), opts(map[string]int64{"eur": 400}), false},
		"an unknown leaf": {types.MapValueMust(elemType, map[string]attr.Value{"eur": object(elemType, map[string]attr.Value{"amount_off": types.Int64Unknown()})}), opts(map[string]int64{"eur": 400}), true},
		"an unset leaf":   {types.MapValueMust(elemType, map[string]attr.Value{"eur": object(elemType, nil)}), opts(map[string]int64{"eur": 400}), false},
	} {
		if got := currencyOptionsChanged(tc.config, tc.state); got != tc.replace {
			t.Errorf("%s: replace = %v, want %v", name, got, tc.replace)
		}
	}
}

func TestCurrencyOptionsValidateConfigRejectsTheOwnCurrency(t *testing.T) {
	r := configured(t, currencyDeclaration(), currencyTable(), noCalls(t))
	for name, tc := range map[string]struct {
		currency attr.Value
		options  map[string]int64
		wantErr  bool
	}{
		"own currency":        {types.StringValue("usd"), map[string]int64{"eur": 1, "usd": 2}, true},
		"own currency, cased": {types.StringValue("USD"), map[string]int64{"usd": 2}, true},
		"another currency":    {types.StringValue("usd"), map[string]int64{"eur": 1}, false},
		"unknown currency":    {types.StringUnknown(), map[string]int64{"usd": 2}, false},
		"no currency yet set": {types.StringNull(), map[string]int64{"usd": 2}, false},
	} {
		config := r.fill(map[string]attr.Value{"currency": tc.currency, "currency_options": optionsValue(r, tc.options)})
		var resp resource.ValidateConfigResponse
		r.ValidateConfig(context.Background(), resource.ValidateConfigRequest{Config: tfsdk.Config{Schema: r.schema, Raw: raw(t, config)}}, &resp)
		if resp.Diagnostics.HasError() != tc.wantErr {
			t.Errorf("%s: error = %v, want %v (%v)", name, resp.Diagnostics.HasError(), tc.wantErr, resp.Diagnostics)
		}
	}
}

func TestCurrencyOptionsNeedACurrencyField(t *testing.T) {
	table := currencyTable()
	table = append(table[:1:1], table[2:]...)
	if _, err := NewResource(currencyDeclaration(), table); err == nil {
		t.Fatal("a declaration with currency options and no `currency` field must not compile")
	}
}

func TestCurrencyOptionsUpdateWithNoNewCurrencySendsNone(t *testing.T) {
	var sent wire.Object
	r := configured(t, currencyDeclaration(), currencyTable(), clientFunc(func(_ context.Context, req stripeapi.Request) (*stripeapi.Response, error) {
		sent = req.Params
		return &stripeapi.Response{Status: 200, Body: widgetBody(wire.Object{option("eur", 400), option("usd", 500)})}, nil
	}))
	elemType := r.objType.AttrTypes["currency_options"].(basetypes.MapType).ElemType
	withUnitAmount := func(unitAmount attr.Value) basetypes.MapValue {
		return types.MapValueMust(elemType, map[string]attr.Value{"eur": object(elemType, map[string]attr.Value{
			"amount_off":  types.Int64Value(400),
			"unit_amount": unitAmount,
		})})
	}
	state := r.fill(map[string]attr.Value{
		"id":               types.StringValue("wid_1"),
		"currency":         types.StringValue("usd"),
		"nickname":         types.StringValue("old"),
		"currency_options": withUnitAmount(types.Int64Null()),
	})
	// The framework marks the computed nulls unknown when another attribute changes.
	plan := r.fill(map[string]attr.Value{
		"id":               types.StringValue("wid_1"),
		"currency":         types.StringValue("usd"),
		"nickname":         types.StringValue("new"),
		"currency_options": withUnitAmount(types.Int64Unknown()),
	})
	r.update(t, plan, state)
	if got := wireOf(t, sent); strings.Contains(got, "currency_options") || !strings.Contains(got, `"nickname":"new"`) {
		t.Fatalf("an update that adds no currency must not send currency_options, sent %s", got)
	}
}
