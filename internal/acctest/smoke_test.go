package acctest

import (
	"fmt"
	"strings"
	"sync"
	"testing"
)

func env(vars map[string]string) func(string) string {
	return func(name string) string { return vars[name] }
}

func TestResolveMode(t *testing.T) {
	const key = "sk_test_abc123"
	cases := []struct {
		name    string
		vars    map[string]string
		smoke   bool
		baseURL string
		wantErr string
	}{
		{"fake mode is the default", map[string]string{EnvFakeURL: "http://x"}, false, "", ""},
		{"smoke needs the switch to be exactly 1", map[string]string{EnvSmoke: "true", EnvAPIKey: "sk_live_x"}, false, "", ""},
		{"smoke with a sandbox key", map[string]string{EnvSmoke: "1", EnvAPIKey: key}, true, DefaultBaseURL, ""},
		{"smoke with a base URL", map[string]string{EnvSmoke: "1", EnvAPIKey: key, EnvBaseURL: "http://localhost:1"}, true, "http://localhost:1", ""},
		{"no key", map[string]string{EnvSmoke: "1"}, false, "", "no key"},
		{"a live key", map[string]string{EnvSmoke: "1", EnvAPIKey: "sk_live_secret"}, false, "", "not a sandbox key"},
		{"a restricted key", map[string]string{EnvSmoke: "1", EnvAPIKey: "rk_test_secret"}, false, "", "not a sandbox key"},
		{"with a fake URL", map[string]string{EnvSmoke: "1", EnvAPIKey: key, EnvFakeURL: "http://x"}, false, "", "cannot be combined"},
		{"with a fake source", map[string]string{EnvSmoke: "1", EnvAPIKey: key, EnvFakeSource: "/src"}, false, "", "cannot be combined"},
		{"with a fake admin token", map[string]string{EnvSmoke: "1", EnvAPIKey: key, EnvFakeAdminToken: "t"}, false, "", "cannot be combined"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			m, err := resolveMode(env(c.vars))
			if c.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), c.wantErr) {
					t.Fatalf("err = %v, want one containing %q", err, c.wantErr)
				}
				if secret := c.vars[EnvAPIKey]; secret != "" && strings.Contains(err.Error(), secret) {
					t.Fatalf("the error leaks the key: %v", err)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if m.smoke != c.smoke || m.baseURL != c.baseURL {
				t.Fatalf("mode = %+v", m)
			}
		})
	}
}

func TestSmokeAccountHasNoKeyInHCL(t *testing.T) {
	const key = "sk_test_abc123"
	t.Setenv(EnvAPIKey, key)
	m, err := resolveMode(env(map[string]string{EnvSmoke: "1", EnvAPIKey: key, EnvBaseURL: "http://127.0.0.1:1"}))
	if err != nil {
		t.Fatal(err)
	}
	c := smokeAccount(m)
	if !c.Smoke || c.Key != "" || c.AccountID != SmokeAccountID {
		t.Fatalf("account = %+v", c)
	}
	hcl := c.ProviderBlock()
	if strings.Contains(hcl, "api_key") || strings.Contains(hcl, key) {
		t.Fatalf("the provider block carries the key: %q", hcl)
	}
	if want := "provider \"stripe\" {\n  base_url = \"http://127.0.0.1:1\"\n}\n"; hcl != want {
		t.Fatalf("got %q", hcl)
	}
	if c.APIKey() != key {
		t.Fatal("the harness's own client must read the key from the environment")
	}
}

func TestNameAndID(t *testing.T) {
	t.Setenv(EnvRunID, "") // the run id is fixed once per process, so only its shape is checked
	fake := Config{Key: "sk_test_k"}
	if got := fake.Name("Gold plan"); got != "Gold plan" {
		t.Fatalf("fake Name = %q", got)
	}
	if got := fake.ID("prod", "priced"); got != "prod_priced" {
		t.Fatalf("fake ID = %q", got)
	}
	if got := fake.ID("", "second"); got != "second" {
		t.Fatalf("fake ID without a prefix = %q", got)
	}
	smoke := Config{Smoke: true, t: &recorder{name: "TestNameAndID"}}
	wantName := SmokePrefix + smokeRunID() + "-Gold plan"
	if got := smoke.Name("Gold plan"); got != wantName {
		t.Fatalf("smoke Name = %q, want %q", got, wantName)
	}
	if got := smoke.ID("prod", "priced"); got != "prod_"+SmokePrefix+smokeRunID()+"-priced" {
		t.Fatalf("smoke ID = %q", got)
	}
	if got := smoke.ID("", "second"); got != SmokePrefix+smokeRunID()+"-second" {
		t.Fatalf("smoke ID without a prefix = %q", got)
	}
	if smoke.Name("a") != smoke.Name("a") || smoke.Name("a") == smoke.Name("b") {
		t.Fatal("names must be stable per base and distinct across bases")
	}
}

// recorder stands in for a test so a refusal can be observed without failing this one.
type recorder struct {
	testing.TB
	name   string
	failed []string
}

func (r *recorder) Name() string      { return r.name }
func (r *recorder) Helper()           {}
func (r *recorder) Fatal(args ...any) { r.failed = append(r.failed, fmt.Sprint(args...)) }

func TestUniqueValuesRefuseASecondTest(t *testing.T) {
	a, b := &recorder{name: "TestA"}, &recorder{name: "TestB"}
	ca, cb := Config{Smoke: true, t: a}, Config{Smoke: true, t: b}

	for _, ask := range []struct {
		what string
		get  func(Config) string
	}{
		{"ID", func(c Config) string { return c.ID("coupon", "launch") }},
		{"Unique", func(c Config) string { return c.Unique("api_calls") }},
	} {
		first := ask.get(ca)
		if again := ask.get(ca); again != first || len(a.failed) != 0 {
			t.Fatalf("%s: a test must be able to ask for its own value again (failed: %v)", ask.what, a.failed)
		}
		if got := ask.get(cb); got != first || len(b.failed) != 1 {
			t.Fatalf("%s: the second test got %q and failed %v, want a failure", ask.what, got, b.failed)
		}
		b.failed = nil
	}

	cb.Name("Gold plan")
	ca.Name("Gold plan")
	if len(a.failed)+len(b.failed) != 0 {
		t.Fatalf("display names may repeat: %v %v", a.failed, b.failed)
	}
	if got := ca.ID("prod", "x"); got == ca.ID("prod", "y") || got == cb.ID("prod", "y") {
		t.Fatal("distinct bases must give distinct values")
	}

	fake := Config{t: b}
	b.failed = nil
	if fake.Unique("api_calls") != "api_calls" || fake.Unique("api_calls") != "api_calls" || len(b.failed) != 0 {
		t.Fatal("fake mode never claims")
	}
}

func TestRunIDIncludesTheAttempt(t *testing.T) {
	// smokeRunID is fixed once per process, so the attempt is checked on a fresh read.
	t.Setenv(EnvRunID, "42")
	t.Setenv(EnvRunAttempt, "2")
	runIDOnce, runID = sync.Once{}, ""
	defer func() { runIDOnce, runID = sync.Once{}, "" }()
	if got := smokeRunID(); got != "42-2" {
		t.Fatalf("run id = %q", got)
	}
}
