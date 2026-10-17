package provider

import (
	"context"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/providerserver"
	"github.com/hashicorp/terraform-plugin-go/tfprotov6"
	"github.com/hashicorp/terraform-plugin-go/tftypes"
)

func configureProvider(t *testing.T, attrs map[string]tftypes.Value) []*tfprotov6.Diagnostic {
	t.Helper()
	ctx := context.Background()
	server := providerserver.NewProtocol6(New("test")())()

	schemaResp, err := server.GetProviderSchema(ctx, &tfprotov6.GetProviderSchemaRequest{})
	if err != nil {
		t.Fatal(err)
	}
	objType, ok := schemaResp.Provider.ValueType().(tftypes.Object)
	if !ok {
		t.Fatalf("provider schema is not an object type")
	}
	values := map[string]tftypes.Value{}
	for name, attrType := range objType.AttributeTypes {
		values[name] = tftypes.NewValue(attrType, nil)
	}
	for name, v := range attrs {
		values[name] = v
	}
	cfg, err := tfprotov6.NewDynamicValue(objType, tftypes.NewValue(objType, values))
	if err != nil {
		t.Fatal(err)
	}
	resp, err := server.ConfigureProvider(ctx, &tfprotov6.ConfigureProviderRequest{Config: &cfg})
	if err != nil {
		t.Fatal(err)
	}
	return resp.Diagnostics
}

func TestConfigureRejectsMissingKey(t *testing.T) {
	t.Setenv("STRIPE_API_KEY", "")
	if diags := configureProvider(t, nil); len(diags) == 0 {
		t.Fatal("expected a diagnostic for the missing api_key")
	}
}

func TestConfigureAcceptsKeyAndBaseURL(t *testing.T) {
	t.Setenv("STRIPE_API_KEY", "")
	diags := configureProvider(t, map[string]tftypes.Value{
		"api_key":  tftypes.NewValue(tftypes.String, "sk_test_x"),
		"base_url": tftypes.NewValue(tftypes.String, "http://127.0.0.1:12111"),
	})
	if len(diags) != 0 {
		t.Fatalf("unexpected diagnostics: %v", diags[0].Detail)
	}
}

func TestConfigureRejectsUnknownKey(t *testing.T) {
	if diags := configureProvider(t, map[string]tftypes.Value{
		"api_key": tftypes.NewValue(tftypes.String, tftypes.UnknownValue),
	}); len(diags) == 0 {
		t.Fatal("expected a diagnostic for an unknown api_key")
	}
}
