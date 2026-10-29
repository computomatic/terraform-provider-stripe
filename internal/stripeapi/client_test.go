package stripeapi

import (
	"context"
	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"io"
	"mime"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"regexp"
	"slices"
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
	if _, err := c.Do(context.Background(), Request{Method: "POST", Path: "/v2/core/event_destinations", Params: params}); err != nil {
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

// A configured context goes verbatim on every request, whatever its path, method or host; an
// unset one sends no header.
func TestStripeContextOnEveryRequest(t *testing.T) {
	srv, seen := testServer(t, scripted{200, nil, okBody})
	c, _ := testClient(srv, 0)
	if _, err := c.Do(context.Background(), Request{Method: "GET", Path: "/v1/balance"}); err != nil {
		t.Fatal(err)
	}
	if got := seen()[0].header.Values("Stripe-Context"); len(got) != 0 {
		t.Fatalf("unset context sent %q", got)
	}

	c.cfg.StripeContext = "acct_org/acct_child"
	for _, req := range []Request{
		{Method: "GET", Path: "/v1/balance"},
		{Method: "POST", Path: "/v1/customers", Params: wire.Object{{Name: "name", Value: wire.String("A")}}},
		{Method: "DELETE", Path: "/v1/customers/cus_1"},
		{Method: "POST", Path: "/v2/core/event_destinations", Params: wire.Object{{Name: "name", Value: wire.String("A")}}},
		{Method: "POST", Path: "/v1/files", Host: FilesHost},
	} {
		if _, err := c.Do(context.Background(), req); err != nil {
			t.Fatal(err)
		}
	}
	for _, r := range seen()[1:] {
		if got := r.header.Values("Stripe-Context"); len(got) != 1 || got[0] != "acct_org/acct_child" {
			t.Errorf("%s %s: Stripe-Context %q", r.method, r.uri, got)
		}
		if got := r.header.Get("Stripe-Account"); got != "" {
			t.Errorf("%s %s: Stripe-Account %q", r.method, r.uri, got)
		}
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

// An upload is a multipart form: the params as text parts in bracket notation, then the file
// part, and a retry sends the same bytes under the same idempotency key.
func TestMultipartUpload(t *testing.T) {
	srv, seen := testServer(t, scripted{500, nil, `{"error":{"type":"api_error"}}`}, scripted{200, nil, `{"id":"file_1","object":"file"}`})
	c, _ := testClient(srv, 1)
	content := []byte("\x89PNG binary")
	_, err := c.Do(context.Background(), Request{
		Method: "POST", Path: "/v1/files", Host: FilesHost,
		Params: wire.Object{
			{Name: "purpose", Value: wire.String("dispute_evidence")},
			{Name: "file_link_data", Value: wire.Object{{Name: "create", Value: wire.Bool(true)}}},
		},
		File: &File{Part: "file", Name: "evidence.png", Content: content},
	})
	if err != nil {
		t.Fatal(err)
	}
	requests := seen()
	if len(requests) != 2 || requests[0].body != requests[1].body || requests[0].header.Get("Idempotency-Key") != requests[1].header.Get("Idempotency-Key") {
		t.Fatalf("the retry must resend the same upload: %+v", requests)
	}
	mediaType, params, err := mime.ParseMediaType(requests[0].header.Get("Content-Type"))
	if err != nil || mediaType != "multipart/form-data" {
		t.Fatalf("content type %q: %v", requests[0].header.Get("Content-Type"), err)
	}
	r := multipart.NewReader(strings.NewReader(requests[0].body), params["boundary"])
	var got []string
	for {
		part, err := r.NextPart()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatal(err)
		}
		data, _ := io.ReadAll(part)
		got = append(got, part.FormName()+"|"+part.FileName()+"|"+string(data))
	}
	want := []string{"purpose||dispute_evidence", "file_link_data[create]||true", "file|evidence.png|" + string(content)}
	if !slices.Equal(got, want) {
		t.Errorf("parts %q, want %q", got, want)
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
	AddToDiagnostics(&attached, err, existing(func(p path.Path) bool { return p.Equal(want) }))
	withPath, ok := attached[0].(diag.DiagnosticWithPath)
	if !ok || !withPath.Path().Equal(want) {
		t.Fatalf("expected a diagnostic at %s, got %#v", want, attached[0])
	}

	var general diag.Diagnostics
	AddToDiagnostics(&general, err, existing(func(path.Path) bool { return false }))
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

func TestV2GetSendsIndexedArrays(t *testing.T) {
	srv, seen := testServer(t, scripted{200, nil, okBody})
	c, _ := testClient(srv, 0)
	params := wire.Object{{Name: "include", Value: wire.Array{wire.String("webhook_endpoint.url")}}, {Name: "limit", Value: wire.Int(20)}}
	if _, err := c.Do(context.Background(), Request{Method: "GET", Path: "/v2/core/event_destinations", Params: params}); err != nil {
		t.Fatal(err)
	}
	if got := seen()[0].uri; got != "/v2/core/event_destinations?include%5B0%5D=webhook_endpoint.url&limit=20" {
		t.Fatalf("uri %q", got)
	}
}

func TestV2RequestWithoutParamsSendsNoBody(t *testing.T) {
	srv, seen := testServer(t, scripted{200, nil, okBody})
	c, _ := testClient(srv, 0)
	for _, method := range []string{"POST", "DELETE"} {
		if _, err := c.Do(context.Background(), Request{Method: method, Path: "/v2/core/event_destinations/ed_1"}); err != nil {
			t.Fatal(err)
		}
	}
	for _, got := range seen() {
		if got.body != "" || got.header.Get("Content-Type") != "" {
			t.Fatalf("%s: body %q type %q", got.method, got.body, got.header.Get("Content-Type"))
		}
	}
}

func TestV2ErrorKeepsInvalidFieldsAndContext(t *testing.T) {
	body := `{"error":{"code":"invalid_fields","message":"Invalid fields","invalid_fields":[` +
		`{"field":"webhook_endpoint.url","message":"Required request field missing"},` +
		`{"field":"enabled_events.0","message":"Unknown field."},{"field":"nowhere","message":"Unknown field."},"odd"]}}`
	srv, _ := testServer(t, scripted{400, map[string]string{"Request-Id": "req_v2"}, body})
	c, _ := testClient(srv, 0)
	_, err := c.Do(context.Background(), Request{Method: "POST", Path: "/v2/core/event_destinations", Params: wire.Object{}})
	apiErr, ok := err.(*APIError)
	if !ok || len(apiErr.InvalidFields) != 3 || apiErr.InvalidFields[0] != (InvalidField{"webhook_endpoint.url", "Required request field missing"}) {
		t.Fatalf("got %#v", err)
	}
	if encoded, _ := wire.EncodeJSON(apiErr.Context); string(encoded) != `{"invalid_fields":["odd"]}` {
		t.Fatalf("context %s", encoded)
	}

	known := map[string]bool{"webhook_endpoint": true, "webhook_endpoint.url": true, "enabled_events": true}
	var diags diag.Diagnostics
	AddToDiagnostics(&diags, err, existing(func(p path.Path) bool { return known[p.String()] }))
	if len(diags) != 3 {
		t.Fatalf("want 3 diagnostics, got %v", diags)
	}
	paths := []string{}
	for _, d := range diags {
		if wp, ok := d.(diag.DiagnosticWithPath); ok {
			paths = append(paths, wp.Path().String())
		} else {
			paths = append(paths, "-")
		}
	}
	if strings.Join(paths, ",") != "webhook_endpoint.url,enabled_events,-" {
		t.Fatalf("paths %v", paths)
	}
	if !strings.Contains(diags[2].Detail(), "nowhere: Unknown field.") || !strings.Contains(diags[2].Detail(), "Request-Id: req_v2") {
		t.Fatalf("general detail %q", diags[2].Detail())
	}
}

func TestV2ErrorContextFields(t *testing.T) {
	body := `{"error":{"message":"You passed invalid Event type(s)","event_type":["v1.not.an.event"],"request_log_url":"https://x"}}`
	srv, _ := testServer(t, scripted{400, nil, body})
	c, _ := testClient(srv, 0)
	_, err := c.Do(context.Background(), Request{Method: "POST", Path: "/v2/core/event_destinations"})
	var diags diag.Diagnostics
	AddToDiagnostics(&diags, err, nil)
	if diags[0].Summary() != "Stripe API error: HTTP 400" || !strings.Contains(diags[0].Detail(), `{"event_type":["v1.not.an.event"]}`) {
		t.Fatalf("%q %q", diags[0].Summary(), diags[0].Detail())
	}
}

func TestV2NotFoundIsNotFound(t *testing.T) {
	srv, _ := testServer(t, scripted{404, nil, `{"error":{"code":"not_found","message":"No such event_destination: 'ed_x'"}}`})
	c, _ := testClient(srv, 0)
	_, err := c.Do(context.Background(), Request{Method: "GET", Path: "/v2/core/event_destinations/ed_x"})
	if !IsNotFound(err) {
		t.Fatalf("got %v", err)
	}
}

// existing adapts a yes-or-no schema check to the lookup AddToDiagnostics takes: every
// attribute that exists is a string.
func existing(has func(path.Path) bool) AttributeType {
	return func(p path.Path) (attr.Type, bool) {
		if !has(p) {
			return nil, false
		}
		return types.StringType, true
	}
}

// A /v1 error carries members such as `payment_intent` that hold objects and secrets. None
// of them reaches the diagnostic, which stays what it was before /v2 support.
func TestV1ErrorContextNeverReachesTheDiagnostic(t *testing.T) {
	body := `{"error":{"type":"card_error","code":"card_declined","message":"Your card was declined.","decline_code":"generic_decline",` +
		`"charge":"ch_1","payment_intent":{"id":"pi_1","client_secret":"pi_1_secret_abc","status":"requires_payment_method"},` +
		`"source":{"id":"src_1"},"invalid_fields":[{"field":"x","message":"y"}]}}`
	srv, _ := testServer(t, scripted{402, map[string]string{"Request-Id": "req_card"}, body})
	c, _ := testClient(srv, 0)
	_, err := c.Do(context.Background(), Request{Method: "POST", Path: "/v1/payment_intents", Params: wire.Object{}})
	apiErr, ok := err.(*APIError)
	if !ok || apiErr.Context != nil || apiErr.InvalidFields != nil {
		t.Fatalf("a /v1 error must carry no context: %#v", err)
	}
	var diags diag.Diagnostics
	AddToDiagnostics(&diags, err, nil)
	if len(diags) != 1 {
		t.Fatalf("diagnostics %v", diags)
	}
	if want := "Your card was declined.\n\nRequest-Id: req_card"; diags[0].Detail() != want {
		t.Errorf("detail %q, want %q", diags[0].Detail(), want)
	}
	for _, leaked := range []string{"pi_1_secret_abc", "client_secret", "decline_code", "ch_1", "src_1"} {
		if strings.Contains(diags[0].Detail(), leaked) || strings.Contains(diags[0].Summary(), leaked) {
			t.Errorf("the diagnostic leaks %q", leaked)
		}
	}
}

// Even on /v2 only scalars and arrays of scalars are kept as context; an object never is.
func TestV2ErrorContextKeepsOnlyScalars(t *testing.T) {
	body := `{"error":{"message":"bad","field":"webhook_endpoint","event_type":["a"],"count":3,"flag":true,` +
		`"payment_intent":{"client_secret":"pi_secret"},"nested":["ok",{"client_secret":"x"}],"invalid_fields":[{"field":"a","message":"m"},{"client_secret":"x"},"plain"]}}`
	srv, _ := testServer(t, scripted{400, nil, body})
	c, _ := testClient(srv, 0)
	_, err := c.Do(context.Background(), Request{Method: "POST", Path: "/v2/core/event_destinations", Params: wire.Object{}})
	apiErr := err.(*APIError)
	encoded, _ := wire.EncodeJSON(apiErr.Context)
	if want := `{"field":"webhook_endpoint","event_type":["a"],"count":3,"flag":true,"invalid_fields":["plain"]}`; string(encoded) != want {
		t.Errorf("context %s, want %s", encoded, want)
	}
	if strings.Contains(string(encoded), "secret") {
		t.Errorf("context leaks a secret: %s", encoded)
	}
}

// An element of a set has no stable index and a map key is data, so an error about one
// attaches to the set or map itself. A list index is kept.
func TestInvalidFieldAttachesToSetAndMapButKeepsListIndexes(t *testing.T) {
	schema := map[string]attr.Type{
		"enabled_events":    types.SetType{ElemType: types.StringType},
		"enabled_events[0]": types.StringType,
		"labels":            types.MapType{ElemType: types.StringType},
		"labels.env":        types.StringType,
		"tags":              types.ListType{ElemType: types.StringType},
		"tags[2]":           types.StringType,
		"items":             types.ListType{ElemType: types.ObjectType{AttrTypes: map[string]attr.Type{"sku": types.StringType}}},
		"items[1]":          types.ObjectType{AttrTypes: map[string]attr.Type{"sku": types.StringType}},
		"items[1].sku":      types.StringType,
		"groups":            types.SetType{ElemType: types.ObjectType{AttrTypes: map[string]attr.Type{"id": types.StringType}}},
		"groups[0]":         types.ObjectType{AttrTypes: map[string]attr.Type{"id": types.StringType}},
		"groups[0].id":      types.StringType,
	}
	typeAt := func(p path.Path) (attr.Type, bool) {
		typ, ok := schema[p.String()]
		return typ, ok
	}
	for field, want := range map[string]string{
		"enabled_events.0": "enabled_events",
		"labels.env":       "labels",
		"labels.nope":      "labels",
		"tags.2":           "tags[2]",
		"items.1.sku":      "items[1].sku",
		"items.1.zip":      "items[1]",
		"groups.0.id":      "groups",
		"enabled_events":   "enabled_events",
	} {
		p, ok := attributeFor(field, typeAt)
		if !ok || p.String() != want {
			t.Errorf("%s attaches to %q (%v), want %q", field, p, ok, want)
		}
	}
	if _, ok := attributeFor("missing.0", typeAt); ok {
		t.Error("a field with no attribute must not attach")
	}
}
