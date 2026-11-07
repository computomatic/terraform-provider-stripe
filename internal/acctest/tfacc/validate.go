package tfacc

import (
	"fmt"
	"math/big"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/hashicorp/terraform-plugin-go/tftypes"
)

// ValidateConfig runs a resource's configuration validation, with no provider, Terraform or
// Stripe, on a configuration that sets what config holds and leaves the rest null. config maps
// attribute names to a string, an int, a bool, or a map[string]any for an object or a map.
func ValidateConfig(t *testing.T, newResource func() resource.Resource, config map[string]any) diag.Diagnostics {
	t.Helper()
	r := newResource()
	var schema resource.SchemaResponse
	r.Schema(t.Context(), resource.SchemaRequest{}, &schema)
	if schema.Diagnostics.HasError() {
		t.Fatalf("schema: %v", schema.Diagnostics)
	}
	v, ok := r.(resource.ResourceWithValidateConfig)
	if !ok {
		t.Fatal("the resource validates no configuration")
	}
	raw, err := configValue(schema.Schema.Type().TerraformType(t.Context()), config)
	if err != nil {
		t.Fatal(err)
	}
	var resp resource.ValidateConfigResponse
	v.ValidateConfig(t.Context(), resource.ValidateConfigRequest{Config: tfsdk.Config{Schema: schema.Schema, Raw: raw}}, &resp)
	return resp.Diagnostics
}

// configValue is v as a value of typ, null where v is nil.
func configValue(typ tftypes.Type, v any) (tftypes.Value, error) {
	if v == nil {
		return tftypes.NewValue(typ, nil), nil
	}
	switch tt := typ.(type) {
	case tftypes.Object:
		m, ok := v.(map[string]any)
		if !ok {
			return tftypes.Value{}, fmt.Errorf("%T for an object", v)
		}
		attrs := map[string]tftypes.Value{}
		for name, at := range tt.AttributeTypes {
			val, err := configValue(at, m[name])
			if err != nil {
				return tftypes.Value{}, fmt.Errorf("%s: %w", name, err)
			}
			attrs[name] = val
		}
		for name := range m {
			if _, ok := tt.AttributeTypes[name]; !ok {
				return tftypes.Value{}, fmt.Errorf("no attribute %s", name)
			}
		}
		return tftypes.NewValue(tt, attrs), nil
	case tftypes.Map:
		m, ok := v.(map[string]any)
		if !ok {
			return tftypes.Value{}, fmt.Errorf("%T for a map", v)
		}
		elems := map[string]tftypes.Value{}
		for key, e := range m {
			val, err := configValue(tt.ElementType, e)
			if err != nil {
				return tftypes.Value{}, fmt.Errorf("%s: %w", key, err)
			}
			elems[key] = val
		}
		return tftypes.NewValue(tt, elems), nil
	}
	switch x := v.(type) {
	case int:
		return tftypes.NewValue(typ, big.NewFloat(float64(x))), nil
	case string, bool:
		return tftypes.NewValue(typ, x), nil
	}
	return tftypes.Value{}, fmt.Errorf("%T is not a value the helper builds", v)
}
