package acctest

import (
	"context"
	"io"
	"net/http"
	"os"
	"testing"
)

func TestMain(m *testing.M) { os.Exit(Main(m)) }

func getBalance(t *testing.T, c Config, key string) int {
	t.Helper()
	req, err := http.NewRequestWithContext(context.Background(), http.MethodGet, c.BaseURL+"/v1/balance", nil)
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
