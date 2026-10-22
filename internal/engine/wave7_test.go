package engine

import (
	"context"
	"strings"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/computomatic/terraform-provider-stripe/internal/stripeapi"
	"github.com/computomatic/terraform-provider-stripe/internal/wire"
)

func oneOfDeclaration() Declaration {
	decl := fixtureDeclaration()
	decl.ExactlyOneOf = [][]string{{"kind", "quantity"}}
	return decl
}

// A group of alternatives refuses a configuration that sets none or several of them, and
// leaves an unknown value for apply.
func TestExactlyOneOfRefusesNoneAndSeveral(t *testing.T) {
	r := configured(t, oneOfDeclaration(), fixtureTable(), noCalls(t))
	for name, tc := range map[string]struct {
		kind, quantity attr.Value
		wantErr        bool
	}{
		"one":             {types.StringValue("a"), types.Int64Null(), false},
		"the other":       {types.StringNull(), types.Int64Value(2), false},
		"none":            {types.StringNull(), types.Int64Null(), true},
		"both":            {types.StringValue("a"), types.Int64Value(2), true},
		"unknown":         {types.StringUnknown(), types.Int64Null(), false},
		"unknown and set": {types.StringUnknown(), types.Int64Value(2), false},
	} {
		config := r.fill(map[string]attr.Value{"name": types.StringValue("w"), "kind": tc.kind, "quantity": tc.quantity})
		var resp resource.ValidateConfigResponse
		r.ValidateConfig(context.Background(), resource.ValidateConfigRequest{Config: tfsdk.Config{Schema: r.schema, Raw: raw(t, config)}}, &resp)
		if resp.Diagnostics.HasError() != tc.wantErr {
			t.Errorf("%s: error = %v, want %v (%v)", name, resp.Diagnostics.HasError(), tc.wantErr, resp.Diagnostics)
		}
	}
}

func TestExactlyOneOfMustNameOptionalFields(t *testing.T) {
	for name, group := range map[string][]string{
		"one field":     {"kind"},
		"unknown field": {"kind", "nothing"},
		"required":      {"kind", "name"},
		"output only":   {"kind", "created"},
	} {
		decl := fixtureDeclaration()
		decl.ExactlyOneOf = [][]string{group}
		if _, err := NewResource(decl, fixtureTable()); err == nil {
			t.Errorf("%s: the group %v must not compile", name, group)
		}
	}
}

func cancelDeclaration() Declaration {
	decl := fixtureDeclaration()
	decl.Delete = Archive
	decl.Archive = &ArchiveAction{Field: "active", Value: wire.Bool(false), Path: "/v1/widgets/{id}/cancel"}
	decl.RefuseWhen = &RefuseWhen{Field: "is_default", Value: wire.Bool(true), Reason: "Stripe refuses to cancel the default widget."}
	return decl
}

// canceledStripe refuses the cancel action endpoint and answers every retrieve with the
// stub's object plus is_default set to protected.
func canceledStripe(stub *stubStripe, protected bool) clientFunc {
	return func(ctx context.Context, req stripeapi.Request) (*stripeapi.Response, error) {
		if req.Method == "POST" && strings.HasSuffix(req.Path, "/cancel") {
			return nil, archiveRefusal
		}
		resp, err := stub.Do(ctx, req)
		if err != nil || req.Method != "GET" {
			return resp, err
		}
		body, _ := resp.Body.(wire.Object)
		return &stripeapi.Response{Status: resp.Status, Body: put(body, "is_default", wire.Bool(protected))}, nil
	}
}

// An archive by an action endpoint refuses a provably protected object like an archive by
// update does, and fails with Stripe's error alone for any other object.
func TestArchiveByActionRefusesTheProtectedObject(t *testing.T) {
	stub := newStub()
	r := configured(t, cancelDeclaration(), defaultTable(), canceledStripe(stub, true))
	state, _ := r.create(t, createPlan(r))
	if diags := r.destroy(t, state); !refusedDestroy(diags) {
		t.Errorf("want the refusal: %v", diags)
	}

	stub = newStub()
	r = configured(t, cancelDeclaration(), defaultTable(), canceledStripe(stub, false))
	state, _ = r.create(t, createPlan(r))
	if diags := r.destroy(t, state); !diags.HasError() || refusedDestroy(diags) {
		t.Fatalf("a refused cancel of an ordinary object fails with Stripe's error only: %v", diags)
	}
}
