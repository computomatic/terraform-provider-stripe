package provider

import (
	"os"
	"strings"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/computomatic/terraform-provider-stripe/internal/stripeapi"
)

func env(values map[string]string) func(string) string {
	return func(name string) string { return values[name] }
}

func baseModel() stripeProviderModel {
	return stripeProviderModel{
		APIKey:        types.StringNull(),
		BaseURL:       types.StringNull(),
		FilesBaseURL:  types.StringNull(),
		StripeAccount: types.StringNull(),
		MaxRetries:    types.Int64Null(),
	}
}

func TestResolveConfigDefaults(t *testing.T) {
	m := baseModel()
	m.APIKey = types.StringValue("sk_test_x")
	cfg, diags := resolveConfig(m, env(nil))
	if diags.HasError() {
		t.Fatalf("unexpected diagnostics: %v", diags)
	}
	if cfg.BaseURL != "https://api.stripe.com" || cfg.FilesBaseURL != "https://files.stripe.com" || cfg.MaxRetries != 3 {
		t.Fatalf("unexpected defaults: %+v", cfg)
	}
}

func TestResolveConfigEnvAndFilesFollowBase(t *testing.T) {
	cfg, diags := resolveConfig(baseModel(), env(map[string]string{
		"STRIPE_API_KEY":  "rk_test_x",
		"STRIPE_API_BASE": "http://127.0.0.1:12111/",
		"STRIPE_ACCOUNT":  "acct_1",
	}))
	if diags.HasError() {
		t.Fatalf("unexpected diagnostics: %v", diags)
	}
	if cfg.APIKey != "rk_test_x" || cfg.BaseURL != "http://127.0.0.1:12111" || cfg.FilesBaseURL != "http://127.0.0.1:12111" || cfg.StripeAccount != "acct_1" {
		t.Fatalf("unexpected config: %+v", cfg)
	}
}

func TestResolveConfigAttributeBeatsEnv(t *testing.T) {
	m := baseModel()
	m.APIKey = types.StringValue("sk_test_attr")
	cfg, _ := resolveConfig(m, env(map[string]string{"STRIPE_API_KEY": "sk_test_env"}))
	if cfg.APIKey != "sk_test_attr" {
		t.Fatalf("got %q", cfg.APIKey)
	}
}

func TestResolveConfigErrors(t *testing.T) {
	cases := map[string]stripeProviderModel{}
	missing := baseModel()
	cases["missing key"] = missing
	pk := baseModel()
	pk.APIKey = types.StringValue("pk_test_x")
	cases["publishable key"] = pk
	path := baseModel()
	path.APIKey = types.StringValue("sk_test_x")
	path.BaseURL = types.StringValue("http://127.0.0.1:1/v1")
	cases["base url path"] = path
	scheme := baseModel()
	scheme.APIKey = types.StringValue("sk_test_x")
	scheme.FilesBaseURL = types.StringValue("ftp://x")
	cases["files scheme"] = scheme
	retries := baseModel()
	retries.APIKey = types.StringValue("sk_test_x")
	retries.MaxRetries = types.Int64Value(-1)
	cases["negative retries"] = retries

	for name, m := range cases {
		if _, diags := resolveConfig(m, env(nil)); !diags.HasError() {
			t.Errorf("%s: expected an error", name)
		}
	}
}

func TestEveryDeclarationBuilds(t *testing.T) {
	p := &stripeProvider{}
	if got := len(p.Resources(t.Context())); got != 34 {
		t.Errorf("%d resources, want 34", got)
	}
	if got := len(p.DataSources(t.Context())); got != 51 {
		t.Errorf("%d data sources, want 51", got)
	}
}

// The README tells users which Stripe API version the provider pins. It must follow the
// constant.
func TestReadmeNamesThePinnedAPIVersion(t *testing.T) {
	readme, err := os.ReadFile("../../README.md")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(readme), stripeapi.Version) {
		t.Errorf("README.md does not name the pinned Stripe API version %s", stripeapi.Version)
	}
}
