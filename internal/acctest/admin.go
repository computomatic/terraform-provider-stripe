package acctest

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"

	"github.com/computomatic/terraform-provider-stripe/internal/wire"
)

// adminClient is typed to the five admin calls the harness needs.
type adminClient struct {
	baseURL string
	token   string
	http    http.Client
}

// Account is a fresh isolated account on the fake.
type Account struct {
	ID         string
	SecretKey  string
	APIVersion string
}

// Health is what `GET /_fake/health` reports.
type Health struct {
	FakeVersion string
	APIVersion  string
}

// LoggedRequest is one entry of an account's request log.
type LoggedRequest struct {
	Method         string
	Path           string
	Params         wire.Value
	IdempotencyKey string
	Status         int
}

func (a *adminClient) call(ctx context.Context, method, path string, body []byte) (wire.Object, error) {
	req, err := http.NewRequestWithContext(ctx, method, a.baseURL+path, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	if len(body) > 0 {
		req.Header.Set("Content-Type", "application/json")
	}
	if a.token != "" {
		req.Header.Set("Authorization", "Bearer "+a.token)
	}
	resp, err := a.http.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return nil, fmt.Errorf("%s %s: HTTP %d: %s", method, path, resp.StatusCode, raw)
	}
	if len(bytes.TrimSpace(raw)) == 0 {
		return nil, nil
	}
	v, err := wire.DecodeJSON(raw)
	if err != nil {
		return nil, fmt.Errorf("%s %s: %w", method, path, err)
	}
	obj, ok := v.(wire.Object)
	if !ok {
		return nil, fmt.Errorf("%s %s: the response is not a JSON object", method, path)
	}
	return obj, nil
}

// CreateAccount makes a fresh account with a generated key.
func (a *adminClient) CreateAccount(ctx context.Context) (Account, error) {
	obj, err := a.call(ctx, http.MethodPost, "/_fake/accounts", nil)
	if err != nil {
		return Account{}, err
	}
	acct := Account{}
	acct.ID, _ = obj.GetString("account_id")
	acct.SecretKey, _ = obj.GetString("secret_key")
	acct.APIVersion, _ = obj.GetString("api_version")
	if acct.ID == "" || acct.SecretKey == "" {
		return Account{}, fmt.Errorf("the fake's account response lacks account_id or secret_key")
	}
	return acct, nil
}

// DeleteAccount removes an account; its key stops working.
func (a *adminClient) DeleteAccount(ctx context.Context, id string) error {
	_, err := a.call(ctx, http.MethodDelete, "/_fake/accounts/"+url.PathEscape(id), nil)
	return err
}

// Requests returns the account's request log, newest first.
func (a *adminClient) Requests(ctx context.Context, id string) ([]LoggedRequest, error) {
	obj, err := a.call(ctx, http.MethodGet, "/_fake/accounts/"+url.PathEscape(id)+"/requests", nil)
	if err != nil {
		return nil, err
	}
	list, _ := obj.Get("requests")
	entries, ok := list.(wire.Array)
	if !ok {
		return nil, fmt.Errorf("the request log response has no requests array")
	}
	out := make([]LoggedRequest, 0, len(entries))
	for _, e := range entries {
		entry, ok := e.(wire.Object)
		if !ok {
			return nil, fmt.Errorf("a request log entry is not an object")
		}
		lr := LoggedRequest{}
		lr.Method, _ = entry.GetString("method")
		lr.Path, _ = entry.GetString("path")
		lr.Params, _ = entry.Get("params")
		lr.IdempotencyKey, _ = entry.GetString("idempotency_key")
		if status, ok := entry.Get("status"); ok {
			if n, ok := status.(wire.Int); ok {
				lr.Status = int(n)
			}
		}
		out = append(out, lr)
	}
	return out, nil
}

// Health reports the fake's versions.
func (a *adminClient) Health(ctx context.Context) (Health, error) {
	obj, err := a.call(ctx, http.MethodGet, "/_fake/health", nil)
	if err != nil {
		return Health{}, err
	}
	h := Health{}
	h.FakeVersion, _ = obj.GetString("fake_version")
	h.APIVersion, _ = obj.GetString("api_version")
	return h, nil
}

// Seed inserts whole objects, such as files and events that no API call can create, and
// returns the IDs the fake gave them, in order.
func (a *adminClient) Seed(ctx context.Context, accountID string, objects []wire.Object) ([]string, error) {
	items := make(wire.Array, len(objects))
	for i, o := range objects {
		items[i] = o
	}
	body, err := wire.EncodeJSON(wire.Object{{Name: "objects", Value: items}})
	if err != nil {
		return nil, err
	}
	obj, err := a.call(ctx, http.MethodPost, "/_fake/accounts/"+url.PathEscape(accountID)+"/seed", body)
	if err != nil {
		return nil, err
	}
	list, _ := obj.Get("objects")
	refs, ok := list.(wire.Array)
	if !ok || len(refs) != len(objects) {
		return nil, fmt.Errorf("the seed response does not list the %d objects", len(objects))
	}
	ids := make([]string, len(refs))
	for i, r := range refs {
		ref, _ := r.(wire.Object)
		if ids[i], _ = ref.GetString("id"); ids[i] == "" {
			return nil, fmt.Errorf("seeded object %d has no id in the response", i)
		}
	}
	return ids, nil
}
