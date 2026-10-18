// Package acctest is the acceptance-test harness. It gives every test a fresh, isolated
// account on a fake Stripe API, so tests share no state and can run fully in parallel.
//
// A package that uses it calls Main from its TestMain. The fake is either one already
// running (STRIPE_FAKE_URL, with STRIPE_FAKE_ADMIN_TOKEN when it needs one) or one the
// harness builds from a temporary copy of the source tree named by STRIPE_FAKE_SOURCE
// and starts on first use. With neither, tests that need an account are skipped.
package acctest

import (
	"context"
	"fmt"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/computomatic/terraform-provider-stripe/internal/wire"
)

const (
	// EnvFakeURL names a running fake to use.
	EnvFakeURL = "STRIPE_FAKE_URL"
	// EnvFakeAdminToken is the bearer token that fake's admin API requires, if any.
	EnvFakeAdminToken = "STRIPE_FAKE_ADMIN_TOKEN"
	// EnvFakeSource is the fake's source tree. It is copied, never modified.
	EnvFakeSource = "STRIPE_FAKE_SOURCE"
)

const adminTimeout = 30 * time.Second

var (
	startOnce sync.Once
	shared    *adminClient
	started   *fake
	startErr  error
)

// Main runs a package's tests and then stops any fake the harness started. Use it as
// `func TestMain(m *testing.M) { os.Exit(acctest.Main(m)) }`.
func Main(m *testing.M) int {
	code := m.Run()
	if started != nil {
		started.stop()
	}
	return code
}

func admin() (*adminClient, error) {
	startOnce.Do(func() {
		if url := os.Getenv(EnvFakeURL); url != "" {
			shared = &adminClient{baseURL: url, token: os.Getenv(EnvFakeAdminToken)}
			return
		}
		source := os.Getenv(EnvFakeSource)
		if source == "" {
			return
		}
		started, startErr = startFake(source)
		if startErr == nil {
			shared = &adminClient{baseURL: started.baseURL}
		}
	})
	return shared, startErr
}

// Config is what a test needs to point the provider at its own account.
type Config struct {
	AccountID string
	Key       string
	BaseURL   string
	// APIVersion is the version the fake serves.
	APIVersion string
}

// ProviderBlock renders the provider block for a test's configuration.
func (c Config) ProviderBlock() string {
	return fmt.Sprintf("provider \"stripe\" {\n  api_key  = %q\n  base_url = %q\n}\n", c.Key, c.BaseURL)
}

// NewAccount creates a fresh account on the fake and removes it when the test ends. It
// skips the test when no fake is configured.
func NewAccount(t *testing.T) Config {
	t.Helper()
	a, err := admin()
	if err != nil {
		t.Fatalf("starting the fake: %v", err)
	}
	if a == nil {
		t.Skipf("no fake Stripe API: set %s to a running fake or %s to its source tree", EnvFakeURL, EnvFakeSource)
	}
	ctx, cancel := context.WithTimeout(context.Background(), adminTimeout)
	defer cancel()
	acct, err := a.CreateAccount(ctx)
	if err != nil {
		t.Fatalf("creating an account: %v", err)
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), adminTimeout)
		defer cancel()
		if err := a.DeleteAccount(ctx, acct.ID); err != nil {
			t.Errorf("deleting account %s: %v", acct.ID, err)
		}
	})
	return Config{AccountID: acct.ID, Key: acct.SecretKey, BaseURL: a.baseURL, APIVersion: acct.APIVersion}
}

// RequestLog returns the account's request log from the fake, newest first. It is a
// fake-only assertion aid.
func RequestLog(t *testing.T, c Config) []LoggedRequest {
	t.Helper()
	a, err := admin()
	if err != nil || a == nil {
		t.Fatalf("no fake available: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), adminTimeout)
	defer cancel()
	log, err := a.Requests(ctx, c.AccountID)
	if err != nil {
		t.Fatalf("reading the request log: %v", err)
	}
	return log
}

// Seed inserts whole objects into the test's account through the fake's admin API and
// returns their IDs in order. It is for objects Stripe creates on its own, such as files
// and events. Each object names its type in `object`.
func Seed(t *testing.T, c Config, objects ...wire.Object) []string {
	t.Helper()
	a, err := admin()
	if err != nil || a == nil {
		t.Fatalf("no fake available: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), adminTimeout)
	defer cancel()
	ids, err := a.Seed(ctx, c.AccountID, objects)
	if err != nil {
		t.Fatalf("seeding the account: %v", err)
	}
	return ids
}
