package engine

import (
	"bytes"
	"context"
	"math/big"
	"strings"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-framework/types/basetypes"
	"github.com/hashicorp/terraform-plugin-go/tfprotov6"
	"github.com/hashicorp/terraform-plugin-go/tftypes"
	"github.com/hashicorp/terraform-plugin-log/tflogtest"

	"github.com/computomatic/terraform-provider-stripe/internal/fields"
	"github.com/computomatic/terraform-provider-stripe/internal/wire"
)

// secretsTable holds sensitive values inside a list, an object and a set.
func secretsTable() []fields.Field {
	return []fields.Field{
		{Name: "id", Shape: fields.String, Response: true},
		{Name: "name", Shape: fields.String, Required: true, Create: true, Update: true, Response: true},
		{Name: "id_numbers", Shape: fields.ObjectList, Create: true, Update: true, Response: true, Fields: []fields.Field{
			{Name: "type", Shape: fields.String, Required: true, Create: true, Update: true, Response: true},
			{Name: "value", Shape: fields.String, Create: true, Update: true, Response: true, Sensitive: true},
		}},
		{Name: "company", Shape: fields.Object, Create: true, Update: true, Response: true, Fields: []fields.Field{
			{Name: "name", Shape: fields.String, Create: true, Update: true, Response: true},
			{Name: "tax_id", Shape: fields.String, Create: true, Update: true, Response: true, Sensitive: true},
		}},
		{Name: "vault", Shape: fields.Object, Create: true, Update: true, Response: true, Fields: []fields.Field{
			{Name: "codes", Shape: fields.ScalarList, Elem: fields.String, Create: true, Update: true, Response: true},
		}},
		{Name: "pins", Shape: fields.ScalarList, Elem: fields.String, Create: true, Update: true, Response: true},
	}
}

// secrets are the sensitive values the test configures; none may reach a diagnostic or a log.
var secrets = []string{"111-22-3333", "444-55-6666", "987654321", "code-1", "code-2", "pin-1", "pin-2"}

// E4 never shows a sensitive value, also where the difference is at a list or an object that
// holds one, or at a sensitive set: in the warning, the strict-mode error and the DEBUG line.
func TestApplyDifferenceHidesSensitiveValuesInsideContainers(t *testing.T) {
	decl := planDecl()
	decl.Sensitive, decl.Sets = []string{"vault.codes", "pins"}, []string{"vault.codes", "pins"}
	body := wire.Object{
		{Name: "id", Value: wire.String("wid_1")},
		{Name: "name", Value: wire.String("w")},
		// Stripe kept one of the two ID numbers and one of the two pins, and dropped the company
		// and the vault with its set of codes.
		{Name: "id_numbers", Value: wire.Array{wire.Object{{Name: "type", Value: wire.String("us_ssn")}, {Name: "value", Value: wire.String("111-22-3333")}}}},
		{Name: "pins", Value: wire.Array{wire.String("pin-1")}},
	}
	for _, mode := range []string{"warning", "strict", "unconfigured"} {
		r := configured(t, decl, secretsTable(), answering(&body))
		r.api.strict = mode == "strict"
		idType := r.objType.AttrTypes["id_numbers"].(basetypes.ListType).ElemType.(basetypes.ObjectType)
		id := func(typ, value string) attr.Value {
			return types.ObjectValueMust(idType.AttrTypes, map[string]attr.Value{"type": types.StringValue(typ), "value": types.StringValue(value)})
		}
		companyType := r.objType.AttrTypes["company"].(basetypes.ObjectType)
		vaultType := r.objType.AttrTypes["vault"].(basetypes.ObjectType)
		plan := r.fill(map[string]attr.Value{
			"name":       types.StringValue("w"),
			"id_numbers": types.ListValueMust(idType, []attr.Value{id("us_ssn", "111-22-3333"), id("us_itin", "444-55-6666")}),
			"company":    types.ObjectValueMust(companyType.AttrTypes, map[string]attr.Value{"name": types.StringValue("Co"), "tax_id": types.StringValue("987654321")}),
			"vault": types.ObjectValueMust(vaultType.AttrTypes, map[string]attr.Value{
				"codes": types.SetValueMust(types.StringType, []attr.Value{types.StringValue("code-1"), types.StringValue("code-2")})}),
			"pins": types.SetValueMust(types.StringType, []attr.Value{types.StringValue("pin-1"), types.StringValue("pin-2")}),
		})
		config := configOf(t, plan)
		if mode == "unconfigured" {
			config = tftypes.NewValue(config.Type(), nil)
		}
		var logs bytes.Buffer
		ctx := tflogtest.RootLogger(context.Background(), &logs)
		resp := resource.CreateResponse{State: tfsdk.State{Schema: r.schema, Raw: tftypes.NewValue(r.schema.Type().TerraformType(ctx), nil)}}
		r.Create(ctx, resource.CreateRequest{Plan: tfsdk.Plan{Schema: r.schema, Raw: raw(t, plan)}, Config: tfsdk.Config{Schema: r.schema, Raw: config}}, &resp)

		var shown []string
		for _, d := range resp.Diagnostics {
			shown = append(shown, d.Summary()+": "+d.Detail())
		}
		want := map[string]int{"warning": 4, "strict": 4, "unconfigured": 0}[mode]
		if got := len(diagsOf(resp.Diagnostics, mode == "strict")); got != want {
			t.Errorf("%s: %d diagnostics, want %d: %q", mode, got, want, shown)
		}
		if mode == "unconfigured" && strings.Count(logs.String(), "not as planned") != 4 {
			t.Errorf("unconfigured: want four DEBUG lines, got %s", logs.String())
		}
		for _, s := range secrets {
			if text := strings.Join(shown, "\n") + logs.String(); strings.Contains(text, s) {
				t.Errorf("%s: a sensitive value is shown: %s", mode, text)
			}
		}
	}
}

// diagsOf returns the errors, or the warnings.
func diagsOf(diags diag.Diagnostics, errs bool) diag.Diagnostics {
	if errs {
		return diags.Errors()
	}
	return diags.Warnings()
}

// fromTerraform is a number as Terraform sends it: parsed from its decimal text at 512 bits.
func fromTerraform(t *testing.T, s string) tftypes.Value {
	t.Helper()
	f, _, err := big.ParseFloat(s, 10, 512, big.ToNearestEven)
	if err != nil {
		t.Fatal(err)
	}
	return tftypes.NewValue(tftypes.Number, f)
}

// E4 compares a fractional number as the decoder reads it: Terraform's 512-bit `33.3` and the
// float64 Stripe's `33.3` decodes to are the same value, so an apply neither warns nor, in strict
// mode, fails.
func TestApplyKeepsAFractionalNumberWithoutADifference(t *testing.T) {
	for _, value := range []string{"33.3", "0.1"} {
		body := wire.Object{{Name: "id", Value: wire.String("wid_1")}, {Name: "name", Value: wire.String("w")}, {Name: "ratio", Value: wire.Decimal(value)}, {Name: "metadata", Value: wire.Object{}}}
		for _, strict := range []bool{false, true} {
			r := configured(t, fixtureDeclaration(), fixtureTable(), answering(&body))
			r.api.strict = strict
			plan, err := tftypes.Transform(raw(t, r.fill(map[string]attr.Value{"name": types.StringValue("w")})), func(p *tftypes.AttributePath, v tftypes.Value) (tftypes.Value, error) {
				if p.Equal(tftypes.NewAttributePath().WithAttributeName("ratio")) {
					return fromTerraform(t, value), nil
				}
				return v, nil
			})
			if err != nil {
				t.Fatal(err)
			}
			ctx := context.Background()
			config, err := tftypes.Transform(plan, func(_ *tftypes.AttributePath, v tftypes.Value) (tftypes.Value, error) {
				if !v.IsKnown() {
					return tftypes.NewValue(v.Type(), nil), nil
				}
				return v, nil
			})
			if err != nil {
				t.Fatal(err)
			}
			resp := resource.CreateResponse{State: tfsdk.State{Schema: r.schema, Raw: tftypes.NewValue(r.schema.Type().TerraformType(ctx), nil)}}
			r.Create(ctx, resource.CreateRequest{Plan: tfsdk.Plan{Schema: r.schema, Raw: plan}, Config: tfsdk.Config{Schema: r.schema, Raw: config}}, &resp)
			if len(resp.Diagnostics) != 0 {
				t.Errorf("ratio %s, strict %v: %v", value, strict, resp.Diagnostics)
			}
		}
	}
	// A number Stripe really changed still differs.
	if sameValue(fromTerraform(t, "33.3"), tftypes.NewValue(tftypes.Number, big.NewFloat(33.4))) {
		t.Error("33.3 and 33.4 compare the same")
	}
	big1 := tftypes.NewValue(tftypes.Number, new(big.Float).SetInt64(1<<62))
	big2 := tftypes.NewValue(tftypes.Number, new(big.Float).SetInt64(1<<62+1))
	if sameValue(big1, big2) {
		t.Error("integers beyond float64's precision compare the same")
	}
}

// E4 keeps the planned number itself where Stripe's answer matches it: the state the provider
// sends back, through msgpack as on the wire, holds Terraform's 512-bit value exactly, for a
// top-level number and one in a list element.
func TestApplyKeepsThePlannedNumberExactly(t *testing.T) {
	table := append(fixtureTable(), fields.Field{Name: "rates", Shape: fields.ObjectList, Create: true, Update: true, Response: true, Fields: []fields.Field{
		{Name: "share", Shape: fields.Float, Create: true, Update: true, Response: true},
	}})
	ratio := tftypes.NewAttributePath().WithAttributeName("ratio")
	share := tftypes.NewAttributePath().WithAttributeName("rates").WithElementKeyInt(0).WithAttributeName("share")
	for _, value := range []string{"33.3", "0.1"} {
		body := wire.Object{{Name: "id", Value: wire.String("wid_1")}, {Name: "name", Value: wire.String("w")}, {Name: "metadata", Value: wire.Object{}},
			{Name: "ratio", Value: wire.Decimal(value)}, {Name: "rates", Value: wire.Array{wire.Object{{Name: "share", Value: wire.Decimal(value)}}}}}
		r := configured(t, fixtureDeclaration(), table, answering(&body))
		r.api.strict = true
		rateType := r.objType.AttrTypes["rates"].(basetypes.ListType).ElemType.(basetypes.ObjectType)
		rates := types.ListValueMust(rateType, []attr.Value{types.ObjectValueMust(rateType.AttrTypes, map[string]attr.Value{"share": types.Float64Value(0)})})
		plan, err := tftypes.Transform(raw(t, r.fill(map[string]attr.Value{"name": types.StringValue("w"), "rates": rates})), func(p *tftypes.AttributePath, v tftypes.Value) (tftypes.Value, error) {
			if p.Equal(ratio) || p.Equal(share) {
				return fromTerraform(t, value), nil
			}
			return v, nil
		})
		if err != nil {
			t.Fatal(err)
		}
		config, err := tftypes.Transform(plan, func(_ *tftypes.AttributePath, v tftypes.Value) (tftypes.Value, error) {
			if !v.IsKnown() {
				return tftypes.NewValue(v.Type(), nil), nil
			}
			return v, nil
		})
		if err != nil {
			t.Fatal(err)
		}
		ctx := context.Background()
		resp := resource.CreateResponse{State: tfsdk.State{Schema: r.schema, Raw: tftypes.NewValue(r.schema.Type().TerraformType(ctx), nil)}}
		r.Create(ctx, resource.CreateRequest{Plan: tfsdk.Plan{Schema: r.schema, Raw: plan}, Config: tfsdk.Config{Schema: r.schema, Raw: config}}, &resp)
		noErrors(t, resp.Diagnostics)

		dv, err := tfprotov6.NewDynamicValue(resp.State.Raw.Type(), resp.State.Raw)
		if err != nil {
			t.Fatal(err)
		}
		sent, err := dv.Unmarshal(resp.State.Raw.Type())
		if err != nil {
			t.Fatal(err)
		}
		want := new(big.Float)
		if err := fromTerraform(t, value).As(&want); err != nil {
			t.Fatal(err)
		}
		for _, p := range []*tftypes.AttributePath{ratio, share} {
			v, _, err := tftypes.WalkAttributePath(sent, p)
			if err != nil {
				t.Fatal(err)
			}
			got := new(big.Float)
			if err := v.(tftypes.Value).As(&got); err != nil {
				t.Fatal(err)
			}
			if got.Cmp(want) != 0 {
				t.Errorf("%s %s: the state sends %s, not the planned %s", value, p, got.Text('g', 30), want.Text('g', 30))
			}
		}
	}
}
