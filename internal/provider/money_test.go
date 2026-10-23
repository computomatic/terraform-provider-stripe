package provider

import (
	"context"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/providerserver"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-go/tfprotov6"
	"github.com/hashicorp/terraform-plugin-go/tftypes"
)

// The resources that move money, which the live-mode guard covers (ADR
// 2026-10-22-money-moving-resources, D6). A resource that moves money without the flag, or a
// flag on another resource, fails here.
func TestMoneyMovingResources(t *testing.T) {
	want := []string{
		"stripe_application_fee_refund", "stripe_checkout_session", "stripe_climate_order", "stripe_payment_intent", "stripe_refund",
		"stripe_topup", "stripe_transfer", "stripe_transfer_reversal",
	}
	ctx := context.Background()
	var got []string
	for _, ctor := range New("test")().Resources(ctx) {
		r := ctor()
		noted, ok := r.(interface{ MoneyNote() string })
		if !ok || noted.MoneyNote() == "" {
			continue
		}
		var meta resource.MetadataResponse
		r.Metadata(ctx, resource.MetadataRequest{ProviderTypeName: "stripe"}, &meta)
		got = append(got, meta.TypeName)
	}
	slices.Sort(got)
	if !slices.Equal(got, want) {
		t.Errorf("money-moving resources:\n got %v\nwant %v", got, want)
	}
}

func TestConfigureRejectsUnknownAllowLiveMoneyMovement(t *testing.T) {
	t.Setenv("STRIPE_API_KEY", "sk_test_x")
	diags := configureProvider(t, map[string]tftypes.Value{
		"allow_live_money_movement": tftypes.NewValue(tftypes.Bool, tftypes.UnknownValue),
	})
	if len(diags) != 1 || diags[0].Summary != "Unknown value" {
		t.Fatalf("want one unknown-value diagnostic, got %v", diags)
	}
}

// planTopupInLiveMode configures the provider against a Stripe stand-in whose balance is in
// live mode and plans a top-up's create. It returns the plan's diagnostics and how many
// requests other than the balance reached the stand-in.
func planTopupInLiveMode(t *testing.T, provider map[string]tftypes.Value) ([]*tfprotov6.Diagnostic, int) {
	t.Helper()
	ctx := context.Background()
	others := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.Method == http.MethodGet && r.URL.Path == "/v1/balance" {
			_, _ = w.Write([]byte(`{"object":"balance","livemode":true,"available":[],"pending":[]}`))
			return
		}
		others++
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte(`{"error":{"type":"invalid_request_error","message":"unexpected"}}`))
	}))
	defer srv.Close()

	server := providerserver.NewProtocol6(New("test")())()
	schemas, err := server.GetProviderSchema(ctx, &tfprotov6.GetProviderSchemaRequest{})
	if err != nil {
		t.Fatal(err)
	}
	set := map[string]tftypes.Value{
		"api_key":  tftypes.NewValue(tftypes.String, "sk_test_x"),
		"base_url": tftypes.NewValue(tftypes.String, srv.URL),
	}
	for k, v := range provider {
		set[k] = v
	}
	cfg := dynamic(t, schemas.Provider.ValueType(), set)
	configured, err := server.ConfigureProvider(ctx, &tfprotov6.ConfigureProviderRequest{Config: &cfg})
	if err != nil || len(configured.Diagnostics) > 0 {
		t.Fatalf("configure: %v %v", err, configured.Diagnostics)
	}

	typ := schemas.ResourceSchemas["stripe_topup"].ValueType()
	config := dynamic(t, typ, map[string]tftypes.Value{
		"amount":   tftypes.NewValue(tftypes.Number, 1000),
		"currency": tftypes.NewValue(tftypes.String, "usd"),
	})
	prior, err := tfprotov6.NewDynamicValue(typ, tftypes.NewValue(typ, nil))
	if err != nil {
		t.Fatal(err)
	}
	resp, err := server.PlanResourceChange(ctx, &tfprotov6.PlanResourceChangeRequest{
		TypeName: "stripe_topup", PriorState: &prior, ProposedNewState: &config, Config: &config,
	})
	if err != nil {
		t.Fatal(err)
	}
	return resp.Diagnostics, others
}

// dynamic is an object of typ with the given attributes and every other one null.
func dynamic(t *testing.T, typ tftypes.Type, set map[string]tftypes.Value) tfprotov6.DynamicValue {
	t.Helper()
	vals := map[string]tftypes.Value{}
	for name, at := range typ.(tftypes.Object).AttributeTypes {
		if v, ok := set[name]; ok {
			vals[name] = v
		} else {
			vals[name] = tftypes.NewValue(at, nil)
		}
	}
	dv, err := tfprotov6.NewDynamicValue(typ, tftypes.NewValue(typ, vals))
	if err != nil {
		t.Fatal(err)
	}
	return dv
}

// The provider hands its resources the switch: without it a live account refuses the plan,
// with it the plan goes ahead, and neither sends a request beyond the balance.
func TestLiveMoneyMovementFollowsTheSwitch(t *testing.T) {
	t.Setenv("STRIPE_API_KEY", "")
	diags, others := planTopupInLiveMode(t, nil)
	if len(diags) != 1 || diags[0].Summary != "Live-mode money movement is not allowed" || !strings.Contains(diags[0].Detail, "a topup") {
		t.Errorf("without the switch: %v", diags)
	}
	diags, more := planTopupInLiveMode(t, map[string]tftypes.Value{"allow_live_money_movement": tftypes.NewValue(tftypes.Bool, true)})
	if len(diags) != 0 {
		t.Errorf("with the switch: %v", diags)
	}
	if others+more != 0 {
		t.Errorf("plans sent %d requests besides the balance", others+more)
	}
}
