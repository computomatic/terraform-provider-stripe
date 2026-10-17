package stripeapi

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/path"

	"github.com/computomatic/terraform-provider-stripe/internal/wire"
)

// scripted is one canned response.
type scripted struct {
	status int
	header map[string]string
	body   string
}

type recorded struct {
	method string
	uri    string
	header http.Header
	body   string
}

// testServer replays responses in order and records every request. The last response
// repeats when the script runs out.
func testServer(t *testing.T, script ...scripted) (*httptest.Server, func() []recorded) {
	t.Helper()
	var mu sync.Mutex
	var seen []recorded
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		mu.Lock()
		i := min(len(seen), len(script)-1)
		seen = append(seen, recorded{r.Method, r.URL.RequestURI(), r.Header.Clone(), string(body)})
		mu.Unlock()
		for k, v := range script[i].header {
			w.Header().Set(k, v)
		}
		w.WriteHeader(script[i].status)
		_, _ = io.WriteString(w, script[i].body)
	}))
	t.Cleanup(srv.Close)
	return srv, func() []recorded {
		mu.Lock()
		defer mu.Unlock()
		return append([]recorded(nil), seen...)
	}
}

// testClient builds a Client with no real waiting. waits collects every backoff sleep and
// jitter always picks the top of the window, so waits equal the window sizes.
func testClient(srv *httptest.Server, maxRetries int64) (*Client, *[]time.Duration) {
	c := New(Config{APIKey: "sk_test_secret", BaseURL: srv.URL, FilesBaseURL: srv.URL, MaxRetries: maxRetries},
		Options{Version: "1.2.3"})
	var waits []time.Duration
	c.sleep = func(_ context.Context, d time.Duration) error { waits = append(waits, d); return nil }
	c.lim = newLimiter(DefaultMaxInFlight, 0, c.now, c.sleep)
	c.jitter = func(max time.Duration) time.Duration { return max }
	return c, &waits
}

const okBody = `{"id":"prod_1","object":"product"}`

func TestRetryTable(t *testing.T) {
	errBody := `{"error":{"type":"api_error","message":"boom"}}`
	tests := []struct {
		name         string
		script       []scripted
		method       string
		wantRequests int
		wantStatus   int // 0 means success
	}{
		{"429 without rate-limit reason (lock_timeout) retries", []scripted{{429, nil, errBody}, {200, nil, okBody}}, "GET", 2, 0},
		{"429 with rate-limit reason retries", []scripted{{429, map[string]string{"Stripe-Rate-Limited-Reason": "global-rate"}, errBody}, {200, nil, okBody}}, "GET", 2, 0},
		{"500 retries", []scripted{{500, nil, errBody}, {200, nil, okBody}}, "GET", 2, 0},
		{"503 retries then gives up", []scripted{{503, nil, errBody}}, "GET", 4, 503},
		{"400 never retries", []scripted{{400, nil, errBody}, {200, nil, okBody}}, "GET", 1, 400},
		{"404 never retries", []scripted{{404, nil, errBody}}, "GET", 1, 404},
		{"409 never retries", []scripted{{409, nil, errBody}}, "POST", 1, 409},
		{"Should-Retry false overrides 500", []scripted{{500, map[string]string{"Stripe-Should-Retry": "false"}, errBody}, {200, nil, okBody}}, "GET", 1, 500},
		{"Should-Retry true overrides 409", []scripted{{409, map[string]string{"Stripe-Should-Retry": "true"}, errBody}, {200, nil, okBody}}, "POST", 2, 0},
		{"Should-Retry false overrides 429", []scripted{{429, map[string]string{"Stripe-Should-Retry": "false"}, errBody}, {200, nil, okBody}}, "GET", 1, 429},
		{"non-JSON 502 body retries", []scripted{{502, nil, "<html>bad gateway</html>"}, {200, nil, okBody}}, "GET", 2, 0},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			srv, seen := testServer(t, tc.script...)
			c, _ := testClient(srv, 3)
			resp, err := c.Do(context.Background(), Request{Method: tc.method, Path: "/v1/products/prod_1"})
			if got := len(seen()); got != tc.wantRequests {
				t.Fatalf("%d requests, want %d", got, tc.wantRequests)
			}
			if tc.wantStatus == 0 {
				if err != nil || resp.Status != 200 {
					t.Fatalf("expected success, got %v", err)
				}
				return
			}
			apiErr, ok := err.(*APIError)
			if !ok || apiErr.Status != tc.wantStatus {
				t.Fatalf("expected APIError %d, got %#v", tc.wantStatus, err)
			}
		})
	}
}

func TestTransportErrorRetries(t *testing.T) {
	srv, seen := testServer(t, scripted{200, nil, okBody})
	c, _ := testClient(srv, 2)
	calls := 0
	c.http = &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		calls++
		if calls < 3 {
			return nil, io.ErrUnexpectedEOF
		}
		return http.DefaultTransport.RoundTrip(r)
	})}
	if _, err := c.Do(context.Background(), Request{Method: "GET", Path: "/v1/balance"}); err != nil {
		t.Fatal(err)
	}
	if calls != 3 || len(seen()) != 1 {
		t.Fatalf("calls=%d served=%d", calls, len(seen()))
	}
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestBackoffScheduleAndRetryAfter(t *testing.T) {
	srv, _ := testServer(t,
		scripted{500, nil, `{}`}, scripted{500, nil, `{}`}, scripted{500, nil, `{}`},
		scripted{429, map[string]string{"Retry-After": "2"}, `{}`}, scripted{200, nil, okBody})
	c, waits := testClient(srv, 10)
	if _, err := c.Do(context.Background(), Request{Method: "GET", Path: "/v1/balance"}); err != nil {
		t.Fatal(err)
	}
	want := []time.Duration{250 * time.Millisecond, 500 * time.Millisecond, time.Second, 2 * time.Second}
	if len(*waits) != len(want) {
		t.Fatalf("waits %v, want %v", *waits, want)
	}
	for i := range want {
		if (*waits)[i] != want[i] {
			t.Fatalf("waits %v, want %v", *waits, want)
		}
	}
}

func TestBackoffIsCappedAndJittered(t *testing.T) {
	var windows []time.Duration
	for attempt := range 9 {
		backoff(attempt, func(max time.Duration) time.Duration { windows = append(windows, max); return 0 })
	}
	if windows[0] != 250*time.Millisecond || windows[5] != 8*time.Second || windows[8] != 8*time.Second {
		t.Fatalf("windows %v", windows)
	}
}

func TestRetryAfterParsing(t *testing.T) {
	now := time.Date(2026, 10, 17, 12, 0, 0, 0, time.UTC)
	tests := map[string]struct {
		value string
		want  time.Duration
		ok    bool
	}{
		"seconds":   {"3", 3 * time.Second, true},
		"fraction":  {"0.5", 500 * time.Millisecond, true},
		"date":      {now.Add(5 * time.Second).Format(http.TimeFormat), 5 * time.Second, true},
		"past date": {now.Add(-time.Hour).Format(http.TimeFormat), 0, true},
		"capped":    {"3600", maxRetryAfter, true},
		"garbage":   {"soon", 0, false},
		"absent":    {"", 0, false},
	}
	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			h := http.Header{}
			if tc.value != "" {
				h.Set("Retry-After", tc.value)
			}
			got, ok := retryAfter(h, now)
			if got != tc.want || ok != tc.ok {
				t.Fatalf("got %v %v, want %v %v", got, ok, tc.want, tc.ok)
			}
		})
	}
}

func TestRetriesStopAtDeadline(t *testing.T) {
	srv, seen := testServer(t, scripted{429, map[string]string{"Retry-After": "30"}, `{"error":{"message":"slow down"}}`})
	c, waits := testClient(srv, 5)
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	_, err := c.Do(ctx, Request{Method: "GET", Path: "/v1/balance"})
	if apiErr, ok := err.(*APIError); !ok || apiErr.Status != 429 {
		t.Fatalf("expected the 429 error, got %v", err)
	}
	if len(seen()) != 1 || len(*waits) != 0 {
		t.Fatalf("served %d, waited %v", len(seen()), *waits)
	}
}

func TestMaxRetriesZeroSendsOnce(t *testing.T) {
	srv, seen := testServer(t, scripted{500, nil, `{}`})
	c, _ := testClient(srv, 0)
	if _, err := c.Do(context.Background(), Request{Method: "GET", Path: "/v1/balance"}); err == nil {
		t.Fatal("expected an error")
	}
	if len(seen()) != 1 {
		t.Fatalf("%d requests", len(seen()))
	}
}

var uuidV4 = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-4[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$`)

func TestIdempotencyKeyRules(t *testing.T) {
	srv, seen := testServer(t, scripted{500, nil, `{}`}, scripted{500, nil, `{}`}, scripted{200, nil, okBody})
	c, _ := testClient(srv, 3)
	params := wire.Object{{Name: "name", Value: wire.String("x")}}

	if _, err := c.Do(context.Background(), Request{Method: "POST", Path: "/v1/products", Params: params}); err != nil {
		t.Fatal(err)
	}
	first := seen()
	if len(first) != 3 {
		t.Fatalf("%d requests", len(first))
	}
	key := first[0].header.Get("Idempotency-Key")
	if !uuidV4.MatchString(key) || len(key) > 255 {
		t.Fatalf("key %q is not a v4 UUID", key)
	}
	for _, r := range first {
		if r.header.Get("Idempotency-Key") != key {
			t.Fatal("a retry changed the idempotency key")
		}
	}

	// A second logical operation gets a new key.
	if _, err := c.Do(context.Background(), Request{Method: "POST", Path: "/v1/products", Params: params}); err != nil {
		t.Fatal(err)
	}
	if second := seen()[3].header.Get("Idempotency-Key"); second == "" || second == key {
		t.Fatalf("second operation key %q reuses %q", second, key)
	}

	// GET and DELETE carry none.
	for _, m := range []string{"GET", "DELETE"} {
		if _, err := c.Do(context.Background(), Request{Method: m, Path: "/v1/products/prod_1"}); err != nil {
			t.Fatal(err)
		}
		if got := seen()[len(seen())-1].header.Get("Idempotency-Key"); got != "" {
			t.Fatalf("%s sent key %q", m, got)
		}
	}
}

func TestRequestShape(t *testing.T) {
	srv, seen := testServer(t, scripted{200, map[string]string{"Request-Id": "req_9"}, okBody})
	c, _ := testClient(srv, 0)
	c.cfg.StripeAccount = "acct_1"
	params := wire.Object{{Name: "metadata", Value: wire.Object{{Name: "k", Value: wire.String("v")}}}}

	resp, err := c.Do(context.Background(), Request{Method: "POST", Path: "/v1/products", Params: params})
	if err != nil {
		t.Fatal(err)
	}
	if resp.RequestID != "req_9" {
		t.Fatalf("request id %q", resp.RequestID)
	}
	if id, _ := resp.Body.(wire.Object).GetString("id"); id != "prod_1" {
		t.Fatalf("body %#v", resp.Body)
	}
	if _, err := c.Do(context.Background(), Request{Method: "GET", Path: "/v1/products", Params: wire.Object{{Name: "limit", Value: wire.Int(100)}}}); err != nil {
		t.Fatal(err)
	}
	if _, err := c.Do(context.Background(), Request{Method: "POST", Path: "/v2/core/event_destinations", Encoding: JSONEncoding, Params: params}); err != nil {
		t.Fatal(err)
	}

	got := seen()
	post := got[0]
	if post.body != "metadata%5Bk%5D=v" || post.header.Get("Content-Type") != "application/x-www-form-urlencoded" {
		t.Fatalf("form post: %q %q", post.body, post.header.Get("Content-Type"))
	}
	for header, want := range map[string]string{
		"Authorization":  "Bearer sk_test_secret",
		"Stripe-Version": Version,
		"Stripe-Account": "acct_1",
		"User-Agent":     "terraform-provider-stripe/1.2.3",
	} {
		if post.header.Get(header) != want {
			t.Errorf("%s = %q, want %q", header, post.header.Get(header), want)
		}
	}
	if !strings.Contains(post.header.Get("X-Stripe-Client-User-Agent"), "terraform-provider-stripe") {
		t.Error("missing client user agent")
	}
	if got[1].uri != "/v1/products?limit=100" || got[1].body != "" {
		t.Fatalf("get: %q %q", got[1].uri, got[1].body)
	}
	if got[2].body != `{"metadata":{"k":"v"}}` || got[2].header.Get("Content-Type") != "application/json" {
		t.Fatalf("json post: %q", got[2].body)
	}
}

func TestFilesHost(t *testing.T) {
	apiSrv, apiSeen := testServer(t, scripted{200, nil, okBody})
	filesSrv, filesSeen := testServer(t, scripted{200, nil, okBody})
	c, _ := testClient(apiSrv, 0)
	c.cfg.FilesBaseURL = filesSrv.URL
	if _, err := c.Do(context.Background(), Request{Method: "POST", Path: "/v1/files", Host: FilesHost}); err != nil {
		t.Fatal(err)
	}
	if len(apiSeen()) != 0 || len(filesSeen()) != 1 {
		t.Fatalf("api %d, files %d", len(apiSeen()), len(filesSeen()))
	}
}

func TestErrorDecoding(t *testing.T) {
	v1 := `{"error":{"type":"invalid_request_error","code":"parameter_unknown","message":"Received unknown parameter: foo","param":"foo"}}`
	v2 := `{"error":{"type":"invalid_request_error","code":"invalid_fields","message":"bad","invalid_fields":[{"field":"url"}]}}`
	bare := `{"error":{"type":"invalid_request_error","message":"No such product"}}`
	for name, tc := range map[string]struct {
		body              string
		wantCode, wantSum string
	}{
		"v1 with code":    {v1, "parameter_unknown", "Stripe API error: parameter_unknown"},
		"v2 fields":       {v2, "invalid_fields", "Stripe API error: invalid_fields"},
		"type only":       {bare, "", "Stripe API error: invalid_request_error"},
		"not an envelope": {`<html>`, "", "Stripe API error: HTTP 400"},
	} {
		t.Run(name, func(t *testing.T) {
			srv, _ := testServer(t, scripted{400, map[string]string{"Request-Id": "req_7"}, tc.body})
			c, _ := testClient(srv, 0)
			_, err := c.Do(context.Background(), Request{Method: "GET", Path: "/v1/products/x"})
			apiErr, ok := err.(*APIError)
			if !ok || apiErr.Code != tc.wantCode || apiErr.RequestID != "req_7" {
				t.Fatalf("got %#v", err)
			}
			var diags diag.Diagnostics
			AddToDiagnostics(&diags, err, nil)
			if len(diags) != 1 || diags[0].Summary() != tc.wantSum || !strings.Contains(diags[0].Detail(), "Request-Id: req_7") {
				t.Fatalf("diagnostic %v", diags)
			}
		})
	}
}

func TestDiagnosticAttachesToExistingAttribute(t *testing.T) {
	err := &APIError{Status: 400, Code: "parameter_invalid_integer", Message: "bad", Param: "tiers[1][up_to]"}
	want := path.Root("tiers").AtListIndex(1).AtName("up_to")

	var attached diag.Diagnostics
	AddToDiagnostics(&attached, err, func(p path.Path) bool { return p.Equal(want) })
	withPath, ok := attached[0].(diag.DiagnosticWithPath)
	if !ok || !withPath.Path().Equal(want) {
		t.Fatalf("expected a diagnostic at %s, got %#v", want, attached[0])
	}

	var general diag.Diagnostics
	AddToDiagnostics(&general, err, func(path.Path) bool { return false })
	if _, ok := general[0].(diag.DiagnosticWithPath); ok {
		t.Fatal("diagnostic should not carry a path when the attribute does not exist")
	}

	var plain diag.Diagnostics
	AddToDiagnostics(&plain, io.ErrUnexpectedEOF, nil)
	if plain[0].Summary() != "Stripe request failed" {
		t.Fatalf("%v", plain)
	}
}

func TestParamPath(t *testing.T) {
	if _, ok := ParamPath(""); ok {
		t.Fatal("empty param")
	}
	if _, ok := ParamPath("a[b"); ok {
		t.Fatal("unterminated bracket")
	}
	p, ok := ParamPath("metadata[team]")
	if !ok || p.String() != "metadata.team" {
		t.Fatalf("%v %v", p, ok)
	}
}

func TestSensitiveFieldsAreMasked(t *testing.T) {
	v := wire.Object{{Name: "secret", Value: wire.String("whsec_1")}, {Name: "list", Value: wire.Array{wire.Object{{Name: "secret", Value: wire.String("x")}, {Name: "n", Value: wire.Int(1)}}}}}
	got := renderBody(v, []string{"secret"})
	if strings.Contains(got, "whsec_1") || strings.Contains(got, `"x"`) || !strings.Contains(got, `"n":1`) {
		t.Fatalf("rendered %s", got)
	}
}

func TestTransportErrorDoesNotLeakKey(t *testing.T) {
	c := New(Config{APIKey: "sk_test_secret", BaseURL: "http://127.0.0.1:1", MaxRetries: 0}, Options{})
	c.http = &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
		return nil, io.EOF
	})}
	_, err := c.Do(context.Background(), Request{Method: "GET", Path: "/v1/balance"})
	if err == nil || strings.Contains(err.Error(), "sk_test_secret") {
		t.Fatalf("err %v", err)
	}
}

func TestLimiterSpacesAndCapsInFlight(t *testing.T) {
	now := time.Date(2026, 10, 17, 0, 0, 0, 0, time.UTC)
	var waits []time.Duration
	l := newLimiter(2, 50*time.Millisecond, func() time.Time { return now },
		func(_ context.Context, d time.Duration) error { waits = append(waits, d); return nil })

	var releases []func()
	for range 2 {
		r, err := l.acquire(context.Background())
		if err != nil {
			t.Fatal(err)
		}
		releases = append(releases, r)
	}
	if len(waits) != 1 || waits[0] != 50*time.Millisecond {
		t.Fatalf("waits %v: the second start should wait one spacing", waits)
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := l.acquire(ctx); err == nil {
		t.Fatal("a full limiter must block until the context is done")
	}
	releases[0]()
	if _, err := l.acquire(context.Background()); err != nil {
		t.Fatal("a freed slot should be usable")
	}
}

func TestOperationDeadlineCancelsWait(t *testing.T) {
	srv, _ := testServer(t, scripted{500, nil, `{}`})
	c, _ := testClient(srv, 3)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := c.Do(ctx, Request{Method: "GET", Path: "/v1/balance"}); err != context.Canceled {
		t.Fatalf("expected context.Canceled, got %v", err)
	}
}
