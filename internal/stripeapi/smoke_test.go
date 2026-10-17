package stripeapi_test

import (
	"context"
	"os"
	"testing"

	"github.com/computomatic/terraform-provider-stripe/internal/acctest"
	"github.com/computomatic/terraform-provider-stripe/internal/stripeapi"
	"github.com/computomatic/terraform-provider-stripe/internal/wire"
)

func TestMain(m *testing.M) { os.Exit(acctest.Main(m)) }

// A balance call against the fake with a fresh account succeeds, and a wrong key is
// reported as a decoded Stripe error.
func TestSmokeBalanceAgainstFake(t *testing.T) {
	acct := acctest.NewAccount(t)
	client := stripeapi.New(stripeapi.Config{APIKey: acct.Key, BaseURL: acct.BaseURL, FilesBaseURL: acct.BaseURL, MaxRetries: 3},
		stripeapi.Options{Version: "test"})

	resp, err := client.Do(context.Background(), stripeapi.Request{Method: "GET", Path: "/v1/balance"})
	if err != nil {
		t.Fatal(err)
	}
	body, ok := resp.Body.(wire.Object)
	if !ok {
		t.Fatalf("body %#v", resp.Body)
	}
	if object, _ := body.GetString("object"); object != "balance" {
		t.Fatalf("object = %q", object)
	}
	if resp.RequestID == "" {
		t.Error("no Request-Id")
	}

	bad := stripeapi.New(stripeapi.Config{APIKey: "sk_test_wrong", BaseURL: acct.BaseURL, MaxRetries: 3}, stripeapi.Options{})
	_, err = bad.Do(context.Background(), stripeapi.Request{Method: "GET", Path: "/v1/balance"})
	apiErr, ok := err.(*stripeapi.APIError)
	if !ok || apiErr.Status != 401 {
		t.Fatalf("expected a 401 APIError, got %#v", err)
	}
}

// A POST reaches the fake with an idempotency key and form-encoded nested parameters.
func TestSmokePostAgainstFake(t *testing.T) {
	acct := acctest.NewAccount(t)
	client := stripeapi.New(stripeapi.Config{APIKey: acct.Key, BaseURL: acct.BaseURL, MaxRetries: 3}, stripeapi.Options{})

	params := wire.Object{
		{Name: "name", Value: wire.String("Gold plan")},
		{Name: "metadata", Value: wire.Object{{Name: "team", Value: wire.String("a")}}},
	}
	resp, err := client.Do(context.Background(), stripeapi.Request{Method: "POST", Path: "/v1/products", Params: params})
	if err != nil {
		t.Fatal(err)
	}
	if id, _ := resp.Body.(wire.Object).GetString("id"); id == "" {
		t.Fatalf("no product id in %#v", resp.Body)
	}
	log := acctest.RequestLog(t, acct)
	if len(log) != 1 || log[0].IdempotencyKey == "" {
		t.Fatalf("request log %+v", log)
	}
	if md, ok := log[0].Params.(wire.Object); !ok {
		t.Fatalf("params %#v", log[0].Params)
	} else if meta, _ := md.GetObject("metadata"); len(meta) != 1 {
		t.Fatalf("metadata did not arrive nested: %#v", log[0].Params)
	}
}
