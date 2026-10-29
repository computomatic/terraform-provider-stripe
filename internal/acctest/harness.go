// Package acctest is the acceptance-test harness. It gives every test a fresh, isolated
// account on a fake Stripe API, so tests share no state and can run fully in parallel.
//
// A package that uses it calls Main from its TestMain. The fake is either one already
// running (STRIPE_FAKE_URL, with STRIPE_FAKE_ADMIN_TOKEN when it needs one) or one the
// harness builds from a temporary copy of the source tree named by STRIPE_FAKE_SOURCE
// and starts on first use. With neither, tests that need an account are skipped.
//
// With TF_SMOKE=1 the harness runs against real Stripe instead: every test shares the one
// account the STRIPE_API_KEY belongs to. See the smoke run ADR.
package acctest

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/computomatic/terraform-provider-stripe/internal/stripeapi"
	"github.com/computomatic/terraform-provider-stripe/internal/wire"
)

const (
	// EnvFakeURL names a running fake to use.
	EnvFakeURL = "STRIPE_FAKE_URL"
	// EnvFakeAdminToken is the bearer token that fake's admin API requires, if any.
	EnvFakeAdminToken = "STRIPE_FAKE_ADMIN_TOKEN"
	// EnvFakeSource is the fake's source tree. It is copied, never modified.
	EnvFakeSource = "STRIPE_FAKE_SOURCE"
	// EnvSmoke switches the harness to real Stripe when it is "1".
	EnvSmoke = "TF_SMOKE"
	// EnvAPIKey is the sandbox key the provider and the harness's own client read in smoke
	// mode. It never reaches generated HCL.
	EnvAPIKey = "STRIPE_API_KEY"
	// EnvBaseURL overrides the API origin in smoke mode.
	EnvBaseURL = "STRIPE_BASE_URL"
	// EnvRunID names the smoke run in object names. CI sets it.
	EnvRunID = "GITHUB_RUN_ID"
	// EnvRunAttempt separates a re-run of the same run, which keeps its run id while the
	// first attempt's archived objects still hold their unique keys.
	EnvRunAttempt = "GITHUB_RUN_ATTEMPT"
)

const (
	// DefaultBaseURL is real Stripe.
	DefaultBaseURL = "https://api.stripe.com"
	// SmokeAccountID stands for the one account every smoke test shares.
	SmokeAccountID = "smoke"
	// SmokePrefix starts the name of everything a smoke run creates.
	SmokePrefix = "tfsmoke-"
	// sandboxKeyPrefix is the only kind of key smoke mode accepts.
	sandboxKeyPrefix = "sk_test_"
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
	if _, err := currentMode(); err != nil {
		fmt.Fprintln(os.Stderr, "acctest:", err)
		return 1
	}
	code := m.Run()
	if started != nil {
		started.stop()
	}
	return code
}

// mode is how the harness reaches Stripe.
type mode struct {
	smoke   bool
	key     string
	baseURL string
}

// resolveMode reads the mode from the environment. Smoke mode needs a sandbox key and
// excludes every fake setting, so a run cannot mix the two.
func resolveMode(getenv func(string) string) (mode, error) {
	if getenv(EnvSmoke) != "1" {
		return mode{}, nil
	}
	for _, name := range []string{EnvFakeURL, EnvFakeSource, EnvFakeAdminToken} {
		if getenv(name) != "" {
			return mode{}, fmt.Errorf("%s=1 runs against real Stripe and cannot be combined with %s, which selects the fake", EnvSmoke, name)
		}
	}
	key := getenv(EnvAPIKey)
	if key == "" {
		return mode{}, fmt.Errorf("%s=1 needs a sandbox key in %s: no key is set", EnvSmoke, EnvAPIKey)
	}
	if !strings.HasPrefix(key, sandboxKeyPrefix) {
		return mode{}, fmt.Errorf("%s is not a sandbox key: smoke mode only accepts keys that start with %s", EnvAPIKey, sandboxKeyPrefix)
	}
	baseURL := getenv(EnvBaseURL)
	if baseURL == "" {
		baseURL = DefaultBaseURL
	}
	return mode{smoke: true, key: key, baseURL: baseURL}, nil
}

var (
	modeOnce   sync.Once
	activeMode mode
	modeErr    error
)

func currentMode() (mode, error) {
	modeOnce.Do(func() { activeMode, modeErr = resolveMode(os.Getenv) })
	return activeMode, modeErr
}

var (
	runIDOnce sync.Once
	runID     string
)

// smokeRunID identifies this process's objects: the CI run id and attempt, else random
// characters.
func smokeRunID() string {
	runIDOnce.Do(func() {
		if runID = os.Getenv(EnvRunID); runID != "" {
			if attempt := os.Getenv(EnvRunAttempt); attempt != "" {
				runID += "-" + attempt
			}
			return
		}
		b := make([]byte, 4)
		if _, err := rand.Read(b); err != nil {
			panic(fmt.Sprintf("reading random bytes: %v", err))
		}
		runID = hex.EncodeToString(b)
	})
	return runID
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
	// Smoke is set when the account is the real sandbox. Its Key is empty: the provider
	// and Client read STRIPE_API_KEY from the environment, so the key is never written
	// into HCL or a working directory.
	Smoke bool
	// StripeAccount is a connected account of the test's account that calls act on, through
	// the `Stripe-Account` header. Empty means the account itself.
	StripeAccount string
	// t is the test the account was made for. It owns the unique values the account asks
	// for.
	t testing.TB
}

// ProviderBlock renders the provider block for a test's configuration. Without a key it
// leaves out api_key, so the provider reads it from the environment. In smoke mode it names
// Stripe's Files API host, which the provider would otherwise take from `base_url`.
func (c Config) ProviderBlock() string {
	if c.Key == "" {
		return fmt.Sprintf("provider \"stripe\" {\n  base_url = %q\n%s}\n", c.BaseURL, c.filesBaseURLLine("  files_base_url = "))
	}
	return fmt.Sprintf("provider \"stripe\" {\n  api_key  = %q\n  base_url = %q\n%s}\n", c.Key, c.BaseURL, c.filesBaseURLLine("  files_base_url = "))
}

// FilesBaseURL is the Files API host that uploads go to: Stripe's in smoke mode, the fake's
// own port otherwise.
func (c Config) FilesBaseURL() string {
	if c.Smoke {
		return stripeapi.DefaultFilesBaseURL
	}
	return c.BaseURL
}

// filesBaseURLLine is the `files_base_url` argument of a provider block, after the prefix that
// aligns it, in smoke mode, and "" against the fake, whose `base_url` serves files too.
func (c Config) filesBaseURLLine(prefix string) string {
	if !c.Smoke {
		return ""
	}
	return fmt.Sprintf("%s%q\n", prefix, c.FilesBaseURL())
}

// ProviderBlockWith renders ProviderBlock with more arguments, each one line such as
// `stripe_context = "acct_1"`.
func (c Config) ProviderBlockWith(arguments ...string) string {
	block := strings.TrimSuffix(c.ProviderBlock(), "}\n")
	for _, a := range arguments {
		block += "  " + a + "\n"
	}
	return block + "}\n"
}

// Connected returns the configuration for acting on a connected account of c's account.
func (c Config) Connected(account string) Config {
	c.StripeAccount = account
	return c
}

// ConnectedProviderBlock renders a second provider block, aliased `connected`, that acts on
// the connected account c names. Resources select it with `provider = stripe.connected`.
func (c Config) ConnectedProviderBlock() string {
	key := ""
	if c.Key != "" {
		key = fmt.Sprintf("  api_key        = %q\n", c.Key)
	}
	return fmt.Sprintf("provider \"stripe\" {\n  alias          = \"connected\"\n%s  base_url       = %q\n%s  stripe_account = %q\n}\n", key, c.BaseURL, c.filesBaseURLLine("  files_base_url = "), c.StripeAccount)
}

// APIKey is the key for the harness's own calls: the account's key against the fake, the
// environment's in smoke mode.
func (c Config) APIKey() string {
	if c.Key == "" {
		return os.Getenv(EnvAPIKey)
	}
	return c.Key
}

// Name returns base as the name to give an object. In smoke mode it is made unique to
// the run and recognisable to the sweep: tfsmoke-<run id>-<base>. Against the fake it is
// base unchanged.
func (c Config) Name(base string) string {
	if !c.Smoke {
		return base
	}
	return SmokePrefix + smokeRunID() + "-" + base
}

// ID returns base as a caller-chosen object ID, such as prod_priced for prefix "prod".
// In smoke mode the base part is made unique as Name does, because Stripe keeps the IDs
// of archived objects. An empty prefix leaves the ID without one, as coupons allow.
// In smoke mode it fails the test when another test already asked for the same ID.
func (c Config) ID(prefix, base string) string {
	id := c.Name(base)
	if prefix != "" {
		id = prefix + "_" + id
	}
	return c.claim(id)
}

// Unique returns base as a value Stripe keeps unique, such as a price lookup key or a
// billing meter event name. Name suits display names, which may repeat; a unique value
// may not, because archived and deleted objects can keep theirs. In smoke mode it is made
// unique to the run, and it fails the test when another test already asked for the same
// value: give each test a base of its own.
func (c Config) Unique(base string) string {
	return c.claim(c.Name(base))
}

var (
	claimsMu sync.Mutex
	claims   = map[string]string{}
)

// claim records that c's test owns value and fails the test when another test does. A test
// may ask for its own value again, as a multi-step configuration does.
func (c Config) claim(value string) string {
	if !c.Smoke {
		return value
	}
	if err := claimValue(value, c.t.Name()); err != nil {
		c.t.Helper()
		c.t.Fatal(err)
	}
	return value
}

func claimValue(value, owner string) error {
	claimsMu.Lock()
	defer claimsMu.Unlock()
	if first, taken := claims[value]; taken && first != owner {
		return fmt.Errorf("%q is a value Stripe keeps unique and %s already uses it: give %s a base of its own", value, first, owner)
	}
	claims[value] = owner
	return nil
}

func smokeAccount(m mode) Config {
	return Config{AccountID: SmokeAccountID, BaseURL: m.baseURL, Smoke: true}
}

// RequireFake skips the test in smoke mode. Call it first in a test that uses a fake-only
// aid (RequestLog, Seed, the admin API) or assumes a fresh account.
func RequireFake(t *testing.T) {
	t.Helper()
	if Smoke() {
		t.Skip("needs the fake: RequestLog, Seed and fresh accounts do not exist on real Stripe")
	}
}

// NewAccount creates a fresh account on the fake and removes it when the test ends. It
// skips the test when no fake is configured. In smoke mode it returns the one shared
// account and creates nothing.
func NewAccount(t *testing.T) Config {
	t.Helper()
	m, err := currentMode()
	if err != nil {
		t.Fatal(err)
	}
	if m.smoke {
		c := smokeAccount(m)
		c.t = t
		return c
	}
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
	requireFakeAccount(t, c)
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
// and events, and for the balance. Each object names its type in `object`. A singleton
// such as the balance has no ID, and its entry is empty.
func Seed(t *testing.T, c Config, objects ...wire.Object) []string {
	t.Helper()
	requireFakeAccount(t, c)
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

// requireFakeAccount fails a fake-only call made on the smoke account, so an unguarded
// test cannot reach for an admin API that real Stripe lacks.
func requireFakeAccount(t *testing.T, c Config) {
	t.Helper()
	if c.Smoke {
		t.Fatal(errors.New("a fake-only aid was used in smoke mode: call acctest.RequireFake first"))
	}
}

// Smoke reports whether the harness runs against real Stripe.
func Smoke() bool {
	m, err := currentMode()
	return err == nil && m.smoke
}
