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
		StripeContext: types.StringNull(),
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
	emptyContext := baseModel()
	emptyContext.APIKey = types.StringValue("sk_test_x")
	emptyContext.StripeContext = types.StringValue("")
	cases["empty stripe_context"] = emptyContext
	both := baseModel()
	both.APIKey = types.StringValue("sk_test_x")
	both.StripeAccount = types.StringValue("acct_1")
	both.StripeContext = types.StringValue("acct_1")
	cases["stripe_account and stripe_context"] = both

	for name, m := range cases {
		if _, diags := resolveConfig(m, env(nil)); !diags.HasError() {
			t.Errorf("%s: expected an error", name)
		}
	}
}

// stripe_context, from the attribute or STRIPE_CONTEXT, goes to the client unchanged, and
// names the target account just as stripe_account does, so the two never combine, whichever
// of them comes from the environment.
func TestResolveConfigStripeContext(t *testing.T) {
	m := baseModel()
	m.APIKey = types.StringValue("sk_org_x")
	m.StripeContext = types.StringValue("acct_attr")
	cfg, diags := resolveConfig(m, env(map[string]string{"STRIPE_CONTEXT": "acct_env"}))
	if diags.HasError() || cfg.StripeContext != "acct_attr" || cfg.StripeAccount != "" {
		t.Fatalf("attribute: %+v %v", cfg, diags)
	}
	cfg, diags = resolveConfig(baseModel(), env(map[string]string{"STRIPE_API_KEY": "sk_test_x", "STRIPE_CONTEXT": "acct_org/acct_child"}))
	if diags.HasError() || cfg.StripeContext != "acct_org/acct_child" {
		t.Fatalf("environment: %+v %v", cfg, diags)
	}

	for name, tc := range map[string]struct {
		account, context types.String
		env              map[string]string
		want             string
	}{
		"both from the environment": {types.StringNull(), types.StringNull(), map[string]string{"STRIPE_ACCOUNT": "acct_1", "STRIPE_CONTEXT": "acct_1"},
			"stripe_account (from the STRIPE_ACCOUNT environment variable) and stripe_context (from the STRIPE_CONTEXT environment variable)"},
		"context from the environment": {types.StringValue("acct_1"), types.StringNull(), map[string]string{"STRIPE_CONTEXT": "acct_1"},
			"stripe_account and stripe_context (from the STRIPE_CONTEXT environment variable)"},
		"account from the environment": {types.StringNull(), types.StringValue("acct_1"), map[string]string{"STRIPE_ACCOUNT": "acct_1"},
			"stripe_account (from the STRIPE_ACCOUNT environment variable) and stripe_context both"},
	} {
		m := baseModel()
		m.APIKey = types.StringValue("sk_test_x")
		m.StripeAccount, m.StripeContext = tc.account, tc.context
		_, diags := resolveConfig(m, env(tc.env))
		if len(diags) != 1 || !strings.Contains(diags[0].Detail(), tc.want) {
			t.Errorf("%s: %v", name, diags)
		}
	}

	// An empty stripe_account clears STRIPE_ACCOUNT, so it does not conflict.
	m = baseModel()
	m.APIKey = types.StringValue("sk_test_x")
	m.StripeAccount = types.StringValue("")
	m.StripeContext = types.StringValue("acct_1")
	if cfg, diags := resolveConfig(m, env(map[string]string{"STRIPE_ACCOUNT": "acct_2"})); diags.HasError() || cfg.StripeAccount != "" || cfg.StripeContext != "acct_1" {
		t.Fatalf("cleared account: %+v %v", cfg, diags)
	}
}

func TestEveryDeclarationBuilds(t *testing.T) {
	p := &stripeProvider{}
	if got := len(p.Resources(t.Context())); got != 68 {
		t.Errorf("%d resources, want 68", got)
	}
	if got := len(p.DataSources(t.Context())); got != 121 {
		t.Errorf("%d data sources, want 121", got)
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
