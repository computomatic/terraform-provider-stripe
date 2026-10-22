package acctest

import (
	"context"
	"io"
	"net/http"
	"os"
	"strings"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"

	"github.com/computomatic/terraform-provider-stripe/internal/wire"
)

func TestMain(m *testing.M) { os.Exit(Main(m)) }

func getBalance(t *testing.T, c Config, key string) int {
	t.Helper()
	return getStatusWithKey(t, c, "/v1/balance", key)
}

func getStatus(t *testing.T, c Config, path string) int {
	t.Helper()
	return getStatusWithKey(t, c, path, c.Key)
}

func getStatusWithKey(t *testing.T, c Config, path, key string) int {
	t.Helper()
	req, err := http.NewRequestWithContext(context.Background(), http.MethodGet, c.BaseURL+path, nil)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Authorization", "Bearer "+key)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, resp.Body)
	return resp.StatusCode
}

func TestEachTestGetsAnIsolatedAccount(t *testing.T) {
	a := NewAccount(t)
	b := NewAccount(t)
	if a.AccountID == b.AccountID || a.Key == b.Key {
		t.Fatal("two accounts share an identity")
	}
	if got := getBalance(t, a, a.Key); got != http.StatusOK {
		t.Fatalf("balance with the account's own key: HTTP %d", got)
	}
	if got := getBalance(t, a, "sk_test_not_a_key"); got == http.StatusOK {
		t.Fatal("an unknown key was accepted")
	}

	log := RequestLog(t, a)
	if len(log) != 1 || log[0].Method != "GET" || log[0].Path != "/v1/balance" || log[0].Status != 200 {
		t.Fatalf("request log of a: %+v", log)
	}
	if other := RequestLog(t, b); len(other) != 0 {
		t.Fatalf("b's log holds a's requests: %+v", other)
	}
}

func TestParallelAccounts(t *testing.T) {
	for _, name := range []string{"one", "two", "three"} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			c := NewAccount(t)
			if got := getBalance(t, c, c.Key); got != http.StatusOK {
				t.Fatalf("HTTP %d", got)
			}
		})
	}
}

func TestCleanupDeletesTheAccount(t *testing.T) {
	var cfg Config
	t.Run("create", func(t *testing.T) { cfg = NewAccount(t) })
	if cfg.Key == "" {
		t.Skip("no fake Stripe API")
	}
	if got := getBalance(t, cfg, cfg.Key); got == http.StatusOK {
		t.Fatal("the account's key still works after the test ended")
	}
}

func TestProviderBlock(t *testing.T) {
	c := Config{Key: "sk_test_k", BaseURL: "http://127.0.0.1:1"}
	want := "provider \"stripe\" {\n  api_key  = \"sk_test_k\"\n  base_url = \"http://127.0.0.1:1\"\n}\n"
	if got := c.ProviderBlock(); got != want {
		t.Fatalf("got %q", got)
	}
}

func TestRemoved(t *testing.T) {
	want := "removed {\n  from = stripe_quote.a\n\n  lifecycle {\n    destroy = false\n  }\n}\n" +
		"removed {\n  from = stripe_quote.b\n\n  lifecycle {\n    destroy = false\n  }\n}\n"
	if got := Removed("stripe_quote.a", "stripe_quote.b"); got != want {
		t.Fatalf("got %q", got)
	}
}

// Forgetting a type drops each block of it and every block that refers to one, keeps the
// rest, and removes each from state without a destroy.
func TestForgetDropsTheResourceAndWhatRefersToIt(t *testing.T) {
	config := `provider "stripe" {}

resource "stripe_customer" "test" {
  name = "c"
}

resource "stripe_quote" "test" {
  customer = stripe_customer.test.id
}

resource "stripe_quote" "test_other" {
  customer = stripe_customer.test.id
}

data "stripe_quote" "test" {
  id = stripe_quote.test.id
}

output "quote_status" {
  value = data.stripe_quote.test.status
}
`
	steps := Forget(t, []resource.TestStep{{Config: config}, {ImportState: true, ResourceName: "stripe_quote.test"}}, nil, "stripe_quote")
	if len(steps) != 3 {
		t.Fatalf("want one step appended: %d", len(steps))
	}
	got := steps[2].Config
	for _, keep := range []string{`provider "stripe"`, `resource "stripe_customer" "test"`, Removed("stripe_quote.test", "stripe_quote.test_other")} {
		if !strings.Contains(got, keep) {
			t.Errorf("lacks %q:\n%s", keep, got)
		}
	}
	for _, drop := range []string{`resource "stripe_quote"`, `data "stripe_quote"`, `output "quote_status"`} {
		if strings.Contains(got, drop) {
			t.Errorf("keeps %q:\n%s", drop, got)
		}
	}
	if strings.Count(got, "removed {") != 2 {
		t.Errorf("a removed block per quote:\n%s", got)
	}
}

func TestConnectedProviderBlock(t *testing.T) {
	c := Config{Key: "sk_test_k", BaseURL: "http://127.0.0.1:1"}.Connected("acct_1")
	want := "provider \"stripe\" {\n  alias          = \"connected\"\n  api_key        = \"sk_test_k\"\n  base_url       = \"http://127.0.0.1:1\"\n  stripe_account = \"acct_1\"\n}\n"
	if got := c.ConnectedProviderBlock(); got != want {
		t.Fatalf("got %q", got)
	}
	// In smoke mode the key stays in the environment.
	c.Key = ""
	if got := c.ConnectedProviderBlock(); strings.Contains(got, "api_key") {
		t.Fatalf("the provider block carries a key: %q", got)
	}
}

func TestSeedInsertsAnObjectTheAPICannotCreate(t *testing.T) {
	c := NewAccount(t)
	ids := Seed(t, c, wire.Object{
		{Name: "object", Value: wire.String("file")},
		{Name: "filename", Value: wire.String("seed.png")},
		{Name: "purpose", Value: wire.String("dispute_evidence")},
		{Name: "size", Value: wire.Int(70)},
		{Name: "type", Value: wire.String("png")},
		{Name: "title", Value: wire.Null{}},
		{Name: "url", Value: wire.Null{}},
		{Name: "expires_at", Value: wire.Null{}},
	})
	if len(ids) != 1 || !strings.HasPrefix(ids[0], "file_") {
		t.Fatalf("seeded ids: %v", ids)
	}
	if got := getStatus(t, c, "/v1/files/"+ids[0]); got != http.StatusOK {
		t.Fatalf("GET of the seeded file: HTTP %d", got)
	}
}

// The balance has no ID, so seeding it returns an empty entry.
func TestSeedInsertsTheBalanceWhichHasNoID(t *testing.T) {
	c := NewAccount(t)
	ids := Seed(t, c, wire.Object{
		{Name: "object", Value: wire.String("balance")},
		{Name: "livemode", Value: wire.Bool(false)},
		{Name: "available", Value: wire.Array{wire.Object{{Name: "amount", Value: wire.Int(1000)}, {Name: "currency", Value: wire.String("cad")}}}},
		{Name: "pending", Value: wire.Array{wire.Object{{Name: "amount", Value: wire.Int(0)}, {Name: "currency", Value: wire.String("cad")}}}},
	})
	if len(ids) != 1 || ids[0] != "" {
		t.Fatalf("seeded ids: %q", ids)
	}
}
