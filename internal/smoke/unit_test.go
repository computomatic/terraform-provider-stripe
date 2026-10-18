package smoke_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/computomatic/terraform-provider-stripe/internal/smoke"
	"github.com/computomatic/terraform-provider-stripe/internal/stripeapi"
)

// stripeStub serves canned lists and records every change request.
type stripeStub struct {
	mu      sync.Mutex
	lists   map[string]string // path, or path#product for prices of one product -> JSON body
	failing map[string]int    // "METHOD path" -> status
	calls   []string          // "METHOD path" of every request
	queries map[string]string // path -> raw query of its last GET
}

func (s *stripeStub) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	defer s.mu.Unlock()
	key := r.Method + " " + r.URL.Path
	s.calls = append(s.calls, key)
	w.Header().Set("Content-Type", "application/json")
	if status, ok := s.failing[key]; ok {
		w.WriteHeader(status)
		_ = json.NewEncoder(w).Encode(map[string]any{"error": map[string]any{"type": "invalid_request_error", "message": "stub says no"}})
		return
	}
	if r.Method == http.MethodGet {
		s.queries[r.URL.Path] = r.URL.RawQuery
		body, ok := s.lists[r.URL.Path+"#"+r.URL.Query().Get("product")]
		if !ok {
			body, ok = s.lists[r.URL.Path]
		}
		if !ok {
			body = `{"object":"list","data":[],"has_more":false}`
		}
		_, _ = w.Write([]byte(body))
		return
	}
	_, _ = w.Write([]byte(`{"id":"x"}`))
}

func (s *stripeStub) changes() []string {
	var out []string
	for _, c := range s.calls {
		if !strings.HasPrefix(c, "GET ") {
			out = append(out, c)
		}
	}
	return out
}

func newStub(t *testing.T, lists map[string]string) (*stripeStub, *smoke.Sweeper) {
	t.Helper()
	stub := &stripeStub{lists: lists, failing: map[string]int{}, queries: map[string]string{}}
	srv := httptest.NewServer(stub)
	t.Cleanup(srv.Close)
	client := stripeapi.New(stripeapi.Config{APIKey: "sk_test_stub", BaseURL: srv.URL}, stripeapi.Options{})
	return stub, &smoke.Sweeper{
		Client: client,
		Now:    func() time.Time { return time.Unix(1_000_000_000, 0) },
		Log:    t.Logf,
	}
}

func list(items ...string) string {
	return `{"object":"list","data":[` + strings.Join(items, ",") + `],"has_more":false}`
}

// unsafeNames must never match, whatever else the sweep does.
var unsafeNames = []string{"x-tfsmoke-run", "TFSMOKE-run", "Tfsmoke-run", " tfsmoke-run", "tfsmoke", "tfsmoke_run", "my tfsmoke-run", ""}

// onlySafe runs the sweep over customers whose names are the unsafe ones and one that is
// ours, and returns the ids it deleted.
func onlySafe(t *testing.T, match func(string) bool) []string {
	var items []string
	for i, name := range unsafeNames {
		items = append(items, `{"id":"cus_unsafe`+string(rune('a'+i))+`","name":"`+name+`"}`)
	}
	items = append(items, `{"id":"cus_ours","name":"tfsmoke-run-1"}`, `{"id":"cus_nameless","name":null}`)
	stub, sw := newStub(t, map[string]string{"/v1/customers": list(items...)})
	sw.Match = match
	if _, err := sw.Run(t.Context()); err != nil {
		t.Fatal(err)
	}
	var deleted []string
	for _, c := range stub.changes() {
		deleted = append(deleted, strings.TrimPrefix(c, "DELETE /v1/customers/"))
	}
	return deleted
}

func TestNamesThatOnlyContainThePrefixAreNeverTouched(t *testing.T) {
	deleted := onlySafe(t, nil)
	if len(deleted) != 1 || deleted[0] != "cus_ours" {
		t.Fatalf("deleted %v, want only cus_ours", deleted)
	}
}

// TestPrefixMatchMutations shows the safety test above would catch a weaker match: each
// mutant lets an unsafe name through.
func TestPrefixMatchMutations(t *testing.T) {
	mutants := map[string]func(string) bool{
		"contains":       func(n string) bool { return strings.Contains(n, smoke.Prefix) },
		"case folded":    func(n string) bool { return strings.HasPrefix(strings.ToLower(n), smoke.Prefix) },
		"suffix":         func(n string) bool { return strings.HasSuffix(n, smoke.Prefix) },
		"trimmed":        func(n string) bool { return strings.HasPrefix(strings.TrimSpace(n), smoke.Prefix) },
		"short prefix":   func(n string) bool { return strings.HasPrefix(n, "tfsmoke") },
		"everything":     func(string) bool { return true },
		"hyphen dropped": func(n string) bool { return strings.HasPrefix(n, "tfsmoke-") || strings.HasPrefix(n, "tfsmoke_") },
	}
	for name, match := range mutants {
		t.Run(name, func(t *testing.T) {
			if deleted := onlySafe(t, match); len(deleted) == 1 && deleted[0] == "cus_ours" {
				t.Fatalf("mutant %q passes the safety test", name)
			}
		})
	}
}

func TestNotFoundIsSuccessAndOtherErrorsFailAfterTheRest(t *testing.T) {
	stub, sw := newStub(t, map[string]string{
		"/v1/customers": list(`{"id":"cus_gone","name":"tfsmoke-a"}`, `{"id":"cus_broken","name":"tfsmoke-b"}`, `{"id":"cus_fine","name":"tfsmoke-c"}`),
		"/v1/coupons":   list(`{"id":"cpn_fine","name":"tfsmoke-d"}`),
	})
	stub.failing["DELETE /v1/customers/cus_gone"] = http.StatusNotFound
	stub.failing["DELETE /v1/customers/cus_broken"] = http.StatusBadRequest
	swept, err := sw.Run(t.Context())
	if err == nil || !strings.Contains(err.Error(), "cus_broken") || !strings.Contains(err.Error(), "stub says no") {
		t.Fatalf("err = %v, want the id and Stripe's message", err)
	}
	if strings.Contains(err.Error(), "cus_gone") {
		t.Fatalf("a 404 failed the sweep: %v", err)
	}
	var ids []string
	for _, s := range swept {
		ids = append(ids, s.ID)
	}
	if got := strings.Join(ids, ","); got != "cus_gone,cus_fine,cpn_fine" {
		t.Fatalf("swept %s", got)
	}
}

func TestCreatedFilterOnlyWhereTheTableSaysSo(t *testing.T) {
	stub, sw := newStub(t, nil)
	if _, err := sw.Run(t.Context()); err != nil {
		t.Fatal(err)
	}
	want := map[string]bool{"/v1/customers": true, "/v1/products": true, "/v1/coupons": true, "/v1/webhook_endpoints": false, "/v1/billing/meters": false}
	for path, filtered := range want {
		q := stub.queries[path]
		if has := strings.Contains(q, "created%5Bgte%5D=998790400"); has != filtered {
			t.Errorf("%s query %q: created filter = %v, want %v", path, q, has, filtered)
		}
	}
}

func TestInactiveObjectsAreSkippedAndProductsWithoutPricesDeleted(t *testing.T) {
	stub, sw := newStub(t, map[string]string{
		"/v1/products": list(`{"id":"prod_old","name":"tfsmoke-a","active":false}`, `{"id":"prod_bare","name":"tfsmoke-b","active":true}`,
			`{"id":"prod_priced","name":"tfsmoke-c","active":true}`),
		"/v1/prices#prod_old":    list(`{"id":"price_old","active":false,"product":"prod_old"}`),
		"/v1/prices#prod_priced": list(`{"id":"price_1","active":true,"product":"prod_priced"}`, `{"id":"price_2","active":false,"product":"prod_priced"}`),
		"/v1/billing/meters": list(`{"id":"mtr_off","display_name":"tfsmoke-m","status":"inactive"}`,
			`{"id":"mtr_on","display_name":"tfsmoke-n","status":"active"}`),
	})
	if _, err := sw.Run(t.Context()); err != nil {
		t.Fatal(err)
	}
	got := strings.Join(stub.changes(), ";")
	for _, want := range []string{"POST /v1/billing/meters/mtr_on/deactivate", "DELETE /v1/products/prod_bare", "POST /v1/prices/price_1", "POST /v1/products/prod_priced"} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q in %s", want, got)
		}
	}
	for _, unwanted := range []string{"prod_old", "mtr_off", "price_2"} {
		if strings.Contains(got, unwanted) {
			t.Errorf("%s was touched: %s", unwanted, got)
		}
	}
}

// pagedStub serves /v1/customers in pages keyed by the starting_after cursor that must
// reach it, answers anything else 400, and records the cursor of every list request.
type pagedStub struct {
	mu      sync.Mutex
	pages   map[string]string // cursor -> page body
	cursors []string
	deleted []string
}

func (p *pagedStub) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	p.mu.Lock()
	defer p.mu.Unlock()
	w.Header().Set("Content-Type", "application/json")
	if r.Method == http.MethodDelete {
		p.deleted = append(p.deleted, strings.TrimPrefix(r.URL.Path, "/v1/customers/"))
		_, _ = w.Write([]byte(`{"deleted":true}`))
		return
	}
	if r.URL.Path != "/v1/customers" {
		_, _ = w.Write([]byte(list()))
		return
	}
	cursor := r.URL.Query().Get("starting_after")
	p.cursors = append(p.cursors, cursor)
	body, ok := p.pages[cursor]
	if !ok {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"error":{"message":"no page after that cursor"}}`))
		return
	}
	_, _ = w.Write([]byte(body))
}

func page(more bool, ids ...string) string {
	var items []string
	for _, id := range ids {
		items = append(items, `{"id":"`+id+`","name":"tfsmoke-`+id+`"}`)
	}
	tail := "false"
	if more {
		tail = "true"
	}
	return `{"object":"list","data":[` + strings.Join(items, ",") + `],"has_more":` + tail + `}`
}

// rewriteCursor is a mutant transport: it changes the starting_after of every list request.
type rewriteCursor func(prev string, current string) string

func (f rewriteCursor) RoundTrip(r *http.Request) (*http.Response, error) {
	q := r.URL.Query()
	if cur := q.Get("starting_after"); cur != "" {
		q.Set("starting_after", f("", cur))
		r.URL.RawQuery = q.Encode()
	}
	return http.DefaultTransport.RoundTrip(r)
}

// walk sweeps the customers of stub through transport (nil for none) and returns the
// swept ids and the error.
func walk(t *testing.T, stub *pagedStub, transport http.RoundTripper) ([]string, error) {
	srv := httptest.NewServer(stub)
	t.Cleanup(srv.Close)
	opts := stripeapi.Options{}
	if transport != nil {
		opts.HTTPClient = &http.Client{Transport: transport}
	}
	sw := &smoke.Sweeper{
		Client: stripeapi.New(stripeapi.Config{APIKey: "sk_test_stub", BaseURL: srv.URL}, opts),
		Log:    t.Logf,
	}
	swept, err := sw.Run(t.Context())
	var ids []string
	for _, s := range swept {
		ids = append(ids, s.ID)
	}
	return ids, err
}

func TestPaginationFollowsTheLastIDOfEachPage(t *testing.T) {
	cases := map[string]map[string]string{
		"ends on has_more false": {
			"":   page(true, "c1", "c2"),
			"c2": page(true, "c3", "c4"),
			"c4": page(false, "c5"),
		},
		"ends on an empty page": {
			"":   page(true, "c1", "c2"),
			"c2": page(true, "c3", "c4"),
			"c4": page(true, "c5"),
			"c5": page(true),
		},
	}
	for name, pages := range cases {
		t.Run(name, func(t *testing.T) {
			stub := &pagedStub{pages: pages}
			ids, err := walk(t, stub, nil)
			if err != nil {
				t.Fatal(err)
			}
			if got := strings.Join(stub.deleted, ","); got != "c1,c2,c3,c4,c5" {
				t.Errorf("deleted %s, want an object from every page", got)
			}
			if len(ids) != 5 {
				t.Errorf("swept %v", ids)
			}
			want := "|c2|c4"
			if name == "ends on an empty page" {
				want += "|c5"
			}
			if got := strings.Join(stub.cursors, "|"); got != want {
				t.Errorf("cursors %q, want %q: each must be the previous page's last id", got, want)
			}
		})
	}
}

// TestPaginationCursorMutations shows the test above would catch a wrong cursor: each
// mutant rewrites what the sweep sends, and the walk must then fail or miss objects.
func TestPaginationCursorMutations(t *testing.T) {
	mutants := map[string]func(prev, cur string) string{
		"first id of the page": func(_, cur string) string { return map[string]string{"c2": "c1", "c4": "c3"}[cur] },
		"stale cursor":         func(_, _ string) string { return "c2" },
		"unknown cursor":       func(_, cur string) string { return cur + "x" },
	}
	for name, mutate := range mutants {
		t.Run(name, func(t *testing.T) {
			stub := &pagedStub{pages: map[string]string{
				"":   page(true, "c1", "c2"),
				"c2": page(true, "c3", "c4"),
				"c4": page(false, "c5"),
			}}
			done := make(chan struct{})
			var err error
			go func() {
				defer close(done)
				_, err = walk(t, stub, rewriteCursor(mutate))
			}()
			select {
			case <-done:
			case <-time.After(3 * time.Second):
				return // a walk that never ends is caught too
			}
			if err == nil && strings.Join(stub.deleted, ",") == "c1,c2,c3,c4,c5" {
				t.Fatalf("mutant %q passes the pagination test", name)
			}
		})
	}
}

func TestPriceOfAnotherProductIsLeftAlone(t *testing.T) {
	stub, sw := newStub(t, map[string]string{
		"/v1/products":         list(`{"id":"prod_ours","name":"tfsmoke-a","active":true}`),
		"/v1/prices#prod_ours": list(`{"id":"price_ok","active":true,"product":"prod_ours"}`, `{"id":"price_foreign","active":true,"product":"prod_other"}`),
	})
	_, err := sw.Run(t.Context())
	if err == nil || !strings.Contains(err.Error(), "price_foreign") {
		t.Fatalf("err = %v, want a failure naming price_foreign", err)
	}
	got := strings.Join(stub.changes(), ";")
	if strings.Contains(got, "price_foreign") || strings.Contains(got, "POST /v1/products/prod_ours") {
		t.Errorf("touched a foreign price or archived the product: %s", got)
	}
	if !strings.Contains(got, "POST /v1/prices/price_ok") {
		t.Errorf("the product's own price was not archived: %s", got)
	}
}

func TestArchivedProductKeepsItsActivePricesSweepable(t *testing.T) {
	stub, sw := newStub(t, map[string]string{
		"/v1/products":        list(`{"id":"prod_off","name":"tfsmoke-a","active":false}`),
		"/v1/prices#prod_off": list(`{"id":"price_live","active":true,"product":"prod_off"}`),
	})
	if _, err := sw.Run(t.Context()); err != nil {
		t.Fatal(err)
	}
	if got := strings.Join(stub.changes(), ";"); got != "POST /v1/prices/price_live" {
		t.Errorf("changes = %s, want only the price archived", got)
	}
}
