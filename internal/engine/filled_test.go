package engine

import (
	"context"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-framework/types/basetypes"

	"github.com/computomatic/terraform-provider-stripe/internal/fields"
)

// caseTable is a dispute's shape: a required evidence hash with a reason and one hash of
// details per reason, and a list whose elements Stripe fills in.
func caseTable() []fields.Field {
	hash := func(name string) fields.Field {
		return fields.Field{Name: name, Shape: fields.Object, Create: true, Update: true, Response: true, Fields: []fields.Field{
			{Name: "explanation", Shape: fields.String, Create: true, Update: true, Response: true},
		}}
	}
	return []fields.Field{
		{Name: "id", Shape: fields.String, Response: true},
		{Name: "evidence", Shape: fields.Object, Required: true, Create: true, Update: true, Response: true, Fields: []fields.Field{
			{Name: "reason", Shape: fields.String, Required: true, Create: true, Update: true, Response: true},
			hash("fraudulent"),
			hash("other"),
		}},
		{Name: "notes", Shape: fields.ObjectList, Create: true, Update: true, Response: true, Fields: []fields.Field{
			{Name: "text", Shape: fields.String, Create: true, Update: true, Response: true, ServerDefaulted: true},
		}},
	}
}

func caseDeclaration() Declaration {
	hashes := []string{"evidence.fraudulent", "evidence.other"}
	return Declaration{
		TypeName:        "stripe_case",
		IDPrefix:        "case_",
		Description:     "A case.",
		Paths:           Paths{Create: "/v1/cases", Retrieve: "/v1/cases/{id}", Update: "/v1/cases/{id}", Delete: "/v1/cases/{id}"},
		Delete:          Delete,
		ServerDefaulted: hashes,
		FilledFrom:      map[string][]string{"evidence.reason": hashes},
	}
}

// A change of the source makes the fields Stripe fills in from it unknown, unless the
// configuration sets them; an unchanged source leaves the plan as the framework made it.
func TestFilledFromForgetsTheStateOnAChange(t *testing.T) {
	r := configured(t, caseDeclaration(), caseTable(), newStub())
	evidenceType := r.objType.AttrTypes["evidence"].(basetypes.ObjectType)
	hashType := evidenceType.AttrTypes["fraudulent"]
	explained := object(hashType, map[string]attr.Value{"explanation": types.StringValue("Not mine.")})
	evidence := func(reason string, fraudulent, other attr.Value) attr.Value {
		return object(evidenceType, map[string]attr.Value{"reason": types.StringValue(reason), "fraudulent": fraudulent, "other": other})
	}
	state := r.fill(map[string]attr.Value{"id": types.StringValue("case_1"), "evidence": evidence("fraudulent", explained, nullOf(hashType))})
	// The plan is what UseStateForUnknown leaves: the state's hashes where the configuration
	// sets none.
	modify := func(plan, config basetypes.ObjectValue) basetypes.ObjectValue {
		resp := &resource.ModifyPlanResponse{Plan: tfsdk.Plan{Schema: r.schema, Raw: raw(t, plan)}}
		r.ModifyPlan(context.Background(), resource.ModifyPlanRequest{
			Config: tfsdk.Config{Schema: r.schema, Raw: raw(t, config)},
			Plan:   tfsdk.Plan{Schema: r.schema, Raw: raw(t, plan)},
			State:  tfsdk.State{Schema: r.schema, Raw: raw(t, state)},
		}, resp)
		noErrors(t, resp.Diagnostics)
		var got basetypes.ObjectValue
		noErrors(t, resp.Plan.Get(context.Background(), &got))
		return got
	}
	hashes := func(v basetypes.ObjectValue) (fraudulent, other attr.Value) {
		e := v.Attributes()["evidence"].(basetypes.ObjectValue).Attributes()
		return e["fraudulent"], e["other"]
	}
	withID := func(e attr.Value) basetypes.ObjectValue {
		return r.fill(map[string]attr.Value{"id": types.StringValue("case_1"), "evidence": e})
	}
	configOf := func(e attr.Value) basetypes.ObjectValue {
		return r.fill(map[string]attr.Value{"id": types.StringNull(), "evidence": e})
	}

	fraudulent, other := hashes(modify(withID(evidence("other", explained, nullOf(hashType))), configOf(evidence("other", nullOf(hashType), nullOf(hashType)))))
	if !fraudulent.IsUnknown() || !other.IsUnknown() {
		t.Errorf("a new reason must leave the unset hashes to Stripe: fraudulent %v, other %v", fraudulent, other)
	}

	set := object(hashType, map[string]attr.Value{"explanation": types.StringValue("Returned.")})
	fraudulent, other = hashes(modify(withID(evidence("other", explained, set)), configOf(evidence("other", nullOf(hashType), set))))
	if !fraudulent.IsUnknown() || !other.Equal(set) {
		t.Errorf("a configured hash must keep its value: fraudulent %v, other %v", fraudulent, other)
	}

	changed := object(hashType, map[string]attr.Value{"explanation": types.StringValue("Still not mine.")})
	fraudulent, other = hashes(modify(withID(evidence("fraudulent", changed, nullOf(hashType))), configOf(evidence("fraudulent", changed, nullOf(hashType)))))
	if !fraudulent.Equal(changed) || !other.IsNull() {
		t.Errorf("an unchanged reason must leave the plan alone: fraudulent %v, other %v", fraudulent, other)
	}
}

// A read-only member, which an object the plan keeps would otherwise hold at the state's value,
// can be filled in too.
func TestFilledFromTakesAReadOnlyMember(t *testing.T) {
	table := caseTable()
	table[1].Fields = append(table[1].Fields, fields.Field{Name: "score", Shape: fields.String, Response: true})
	decl := caseDeclaration()
	decl.FilledFrom = map[string][]string{"evidence.reason": {"evidence.score"}}
	if _, err := NewResource(decl, table); err != nil {
		t.Error(err)
	}
}

func TestFilledFromMustNameComputedFields(t *testing.T) {
	for name, filled := range map[string]map[string][]string{
		"unknown source":         {"evidence.motive": {"evidence.other"}},
		"output-only source":     {"id": {"evidence.other"}},
		"unknown filled field":   {"evidence.reason": {"evidence.motive"}},
		"not server-defaulted":   {"evidence.reason": {"evidence.reason"}},
		"below a scalar":         {"evidence.reason.code": {"evidence.other"}},
		"source in a list":       {"notes.text": {"evidence.other"}},
		"filled field in a list": {"evidence.reason": {"notes.text"}},
		"filled required object": {"evidence.reason": {"evidence"}},
	} {
		decl := caseDeclaration()
		decl.FilledFrom = filled
		if _, err := NewResource(decl, caseTable()); err == nil {
			t.Errorf("%s: must not compile", name)
		}
	}
}
