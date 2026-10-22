package tfacc

import (
	"slices"
	"testing"
	"time"

	"github.com/computomatic/terraform-provider-stripe/internal/acctest"
	"github.com/computomatic/terraform-provider-stripe/internal/stripeapi"
	"github.com/computomatic/terraform-provider-stripe/internal/wire"
)

const (
	// issuingRole is the `metadata[tfacc_role]` value of the connected account and the
	// financial account the Issuing and Treasury tests share. Stripe verifies a new connected
	// account for longer than a test waits, so smoke runs reuse them and never delete them, as
	// the conformance suite does (suite: "A new Issuing connected account is verified
	// asynchronously"). Against the fake every test's account is fresh, so each test makes its
	// own.
	issuingRole = "issuing"
	// activationLimit bounds each wait for Stripe to activate a capability, a feature or a
	// cardholder.
	activationLimit = 5 * time.Minute
)

// IssuingAccount returns the configuration for a US Custom connected account of c's account
// whose `card_issuing` and `treasury` capabilities are active: Stripe issues cards only for a
// platform's connected accounts, and those cards draw on a Treasury financial account (suite:
// "Issuing is for connected accounts on this platform", "Issuing cards on a connected account
// need a Treasury financial account"). The account carries the onboarding values Stripe
// documents as always verifying, which the suite sends ("issuingAccountForm"); the fake
// activates both capabilities at once. A capability still verifying after the wait skips the
// test; one with requirements due fails it.
func IssuingAccount(t *testing.T, c acctest.Config) acctest.Config {
	t.Helper()
	id := tagged(t, c, "/v1/accounts")
	if id == "" {
		id = Create(t, c, "/v1/accounts", issuingAccountParams(c))
	}
	for _, capability := range []string{"card_issuing", "treasury"} {
		var account wire.Object
		active := poll(func() bool {
			account = get(t, c, "/v1/accounts/"+id)
			capabilities, _ := account.GetObject("capabilities")
			status, _ := capabilities.GetString(capability)
			return status == "active"
		})
		if !active {
			requirements, _ := account.GetObject("requirements")
			if reason, _ := requirements.GetString("disabled_reason"); reason != "" || !emptyList(requirements, "currently_due") || !emptyList(requirements, "past_due") {
				t.Fatalf("connected account %s cannot activate %s, requirements: %v", id, capability, requirements)
			}
			t.Skipf("connected account %s is still verifying %s after %s, requirements: %v", id, capability, activationLimit, requirements)
		}
	}
	return c.Connected(id)
}

func issuingAccountParams(c acctest.Config) wire.Object {
	terms := wire.Object{{Name: "date", Value: wire.Int(1609459200)}, {Name: "ip", Value: wire.String("8.8.8.8")}}
	requested := wire.Object{{Name: "requested", Value: wire.Bool(true)}}
	return wire.Object{
		{Name: "type", Value: wire.String("custom")},
		{Name: "country", Value: wire.String("US")},
		{Name: "business_type", Value: wire.String("individual")},
		{Name: "email", Value: wire.String(c.Name("issuing") + "@example.com")},
		{Name: "capabilities", Value: wire.Object{
			{Name: "card_issuing", Value: requested},
			{Name: "transfers", Value: requested},
			{Name: "treasury", Value: requested},
		}},
		{Name: "business_profile", Value: wire.Object{
			{Name: "mcc", Value: wire.String("5734")},
			{Name: "url", Value: wire.String("https://accessible.stripe.com")},
			{Name: "product_description", Value: wire.String("Acceptance test issuing programme")},
			{Name: "annual_revenue", Value: wire.Object{
				{Name: "amount", Value: wire.Int(10000000)},
				{Name: "currency", Value: wire.String("usd")},
				{Name: "fiscal_year_end", Value: wire.String("2025-12-31")},
			}},
			{Name: "estimated_worker_count", Value: wire.Int(1)},
		}},
		{Name: "individual", Value: wire.Object{
			{Name: "first_name", Value: wire.String("Jenny")},
			{Name: "last_name", Value: wire.String("Rosen")},
			{Name: "email", Value: wire.String(c.Name("issuing-individual") + "@example.com")},
			{Name: "phone", Value: wire.String("0000000000")},
			{Name: "id_number", Value: wire.String("000000000")},
			{Name: "ssn_last_4", Value: wire.String("0000")},
			{Name: "dob", Value: wire.Object{{Name: "day", Value: wire.Int(1)}, {Name: "month", Value: wire.Int(1)}, {Name: "year", Value: wire.Int(1901)}}},
			{Name: "address", Value: wire.Object{
				{Name: "line1", Value: wire.String("address_full_match")},
				{Name: "city", Value: wire.String("South San Francisco")},
				{Name: "state", Value: wire.String("CA")},
				{Name: "postal_code", Value: wire.String("94080")},
			}},
			{Name: "verification", Value: wire.Object{{Name: "document", Value: wire.Object{{Name: "front", Value: wire.String("file_identity_document_success")}}}}},
		}},
		{Name: "external_account", Value: wire.String("btok_us_verified")},
		{Name: "tos_acceptance", Value: terms},
		{Name: "settings", Value: wire.Object{
			{Name: "card_issuing", Value: wire.Object{{Name: "tos_acceptance", Value: terms}}},
			{Name: "treasury", Value: wire.Object{{Name: "tos_acceptance", Value: terms}}},
		}},
		{Name: "metadata", Value: wire.Object{{Name: "tfacc_role", Value: wire.String(issuingRole)}}},
	}
}

// IssuingFinancialAccount returns a `usd` Treasury financial account of the connected account
// conn names, with its `card_issuing` feature active, for cards to draw on. The fake activates
// the feature at once (suite: "Issuing answers from the first card runs").
func IssuingFinancialAccount(t *testing.T, conn acctest.Config) string {
	t.Helper()
	id := tagged(t, conn, "/v1/treasury/financial_accounts")
	if id == "" {
		id = Create(t, conn, "/v1/treasury/financial_accounts", wire.Object{
			{Name: "supported_currencies", Value: wire.Array{wire.String("usd")}},
			{Name: "features", Value: wire.Object{{Name: "card_issuing", Value: wire.Object{{Name: "requested", Value: wire.Bool(true)}}}}},
			{Name: "metadata", Value: wire.Object{{Name: "tfacc_role", Value: wire.String(issuingRole)}}},
		})
	}
	if !poll(func() bool {
		return listed(get(t, conn, "/v1/treasury/financial_accounts/"+id), "active_features", "card_issuing")
	}) {
		t.Skipf("financial account %s has no active card_issuing feature after %s", id, activationLimit)
	}
	return id
}

// ReceivedCredit makes a received credit of amount cents into the financial account with
// Stripe's test helper and returns its ID and its transaction's. The helper needs the
// account's `financial_addresses.aba` feature, which this requests and waits for (suite:
// "Treasury test helpers need `financial_addresses.aba`").
func ReceivedCredit(t *testing.T, conn acctest.Config, financialAccount string, amount int64) (id, transaction string) {
	t.Helper()
	return received(t, conn, "received_credits", financialAccount, amount)
}

// ReceivedDebit is ReceivedCredit for a received debit.
func ReceivedDebit(t *testing.T, conn acctest.Config, financialAccount string, amount int64) (id, transaction string) {
	t.Helper()
	return received(t, conn, "received_debits", financialAccount, amount)
}

func received(t *testing.T, conn acctest.Config, kind, financialAccount string, amount int64) (id, transaction string) {
	t.Helper()
	features := "/v1/treasury/financial_accounts/" + financialAccount + "/features"
	if abaStatus(t, conn, features) == "" {
		Post(t, conn, features, wire.Object{{Name: "financial_addresses", Value: wire.Object{{Name: "aba", Value: wire.Object{{Name: "requested", Value: wire.Bool(true)}}}}}})
	}
	if !poll(func() bool { return abaStatus(t, conn, features) == "active" }) {
		t.Skipf("the financial_addresses.aba feature of %s is not active after %s", financialAccount, activationLimit)
	}
	resp, err := Client(conn).Do(t.Context(), stripeapi.Request{Method: "POST", Path: "/v1/test_helpers/treasury/" + kind, Params: wire.Object{
		{Name: "financial_account", Value: wire.String(financialAccount)},
		{Name: "amount", Value: wire.Int(amount)},
		{Name: "currency", Value: wire.String("usd")},
		{Name: "network", Value: wire.String("ach")},
		{Name: "description", Value: wire.String(conn.Name("acceptance"))},
	}})
	if err != nil {
		t.Fatalf("POST /v1/test_helpers/treasury/%s: %v", kind, err)
	}
	body := resp.Body.(wire.Object)
	id, _ = body.GetString("id")
	transaction, _ = body.GetString("transaction")
	return id, transaction
}

// abaStatus is the status of the financial account's `financial_addresses.aba` feature, or ""
// before it is requested.
func abaStatus(t *testing.T, conn acctest.Config, features string) string {
	t.Helper()
	addresses, _ := get(t, conn, features).GetObject("financial_addresses")
	aba, _ := addresses.GetObject("aba")
	status, _ := aba.GetString("status")
	return status
}

// AwaitCardholderReview waits until Stripe has reviewed a new cardholder, whose
// `requirements.disabled_reason` reads `under_review` until then: activating a card needs it
// (suite: "New Issuing cardholders start `under_review`"). The fake finishes the review at once.
// A review still running after the wait skips the test.
func AwaitCardholderReview(t *testing.T, conn acctest.Config, cardholder string) {
	t.Helper()
	cleared := poll(func() bool {
		requirements, _ := get(t, conn, "/v1/issuing/cardholders/"+cardholder).GetObject("requirements")
		reason, _ := requirements.GetString("disabled_reason")
		return reason == ""
	})
	if !cleared {
		t.Skipf("cardholder %s is still under review after %s", cardholder, activationLimit)
	}
}

// tagged returns the ID of the first object of the list path whose `metadata[tfacc_role]` is
// issuingRole, following pages, or "".
func tagged(t *testing.T, c acctest.Config, path string) string {
	t.Helper()
	params := wire.Object{{Name: "limit", Value: wire.Int(100)}}
	for range 20 {
		resp, err := Client(c).Do(t.Context(), stripeapi.Request{Method: "GET", Path: path, Params: params})
		if err != nil {
			t.Fatalf("GET %s: %v", path, err)
		}
		body := resp.Body.(wire.Object)
		data, _ := body.Get("data")
		items, _ := data.(wire.Array)
		var last string
		for _, item := range items {
			obj, _ := item.(wire.Object)
			last, _ = obj.GetString("id")
			metadata, _ := obj.GetObject("metadata")
			if role, _ := metadata.GetString("tfacc_role"); role == issuingRole {
				return last
			}
		}
		if more, _ := body.GetBool("has_more"); !more || last == "" {
			return ""
		}
		params = wire.Object{{Name: "limit", Value: wire.Int(100)}, {Name: "starting_after", Value: wire.String(last)}}
	}
	return ""
}

func get(t *testing.T, c acctest.Config, path string) wire.Object {
	t.Helper()
	resp, err := Client(c).Do(t.Context(), stripeapi.Request{Method: "GET", Path: path})
	if err != nil {
		t.Fatalf("GET %s: %v", path, err)
	}
	return resp.Body.(wire.Object)
}

// poll calls done until it reports true or activationLimit passes, and returns its last answer.
func poll(done func() bool) bool {
	deadline := time.Now().Add(activationLimit)
	for !done() {
		if time.Now().After(deadline) {
			return false
		}
		time.Sleep(2 * time.Second)
	}
	return true
}

// listed reports whether the object's string array field holds value.
func listed(obj wire.Object, field, value string) bool {
	v, _ := obj.Get(field)
	items, _ := v.(wire.Array)
	return slices.ContainsFunc(items, func(item wire.Value) bool { s, ok := item.(wire.String); return ok && string(s) == value })
}

func emptyList(obj wire.Object, field string) bool {
	v, _ := obj.Get(field)
	items, _ := v.(wire.Array)
	return len(items) == 0
}
