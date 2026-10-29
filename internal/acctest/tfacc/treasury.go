package tfacc

import (
	"errors"
	"strings"
	"testing"

	"github.com/computomatic/terraform-provider-stripe/internal/acctest"
	"github.com/computomatic/terraform-provider-stripe/internal/stripeapi"
	"github.com/computomatic/terraform-provider-stripe/internal/wire"
)

const (
	// treasuryDestinationRole is the `metadata[tfacc_role]` of the second financial account that
	// outbound transfers send to. A connected account may hold three open financial accounts, so
	// smoke runs reuse it and never close it, which keeps the shared account at two.
	treasuryDestinationRole = "treasury_destination"
	// achCapability is the connected account capability a US bank account attached to the
	// account itself needs (suite: "A setup intent cannot attach a US bank account to a connected
	// account without that payment method").
	achCapability = "us_bank_account_ach_payments"
	// directChargesRefusal opens Stripe's refusal of a setup intent through `Stripe-Account` on a
	// Custom connected account, which it gives platforms it considers new (suite: "Direct charges
	// on a Custom connected account differ between sandboxes").
	directChargesRefusal = "Creating direct charges with type=express or type=custom is not supported for new platforms."
)

// TreasuryFeatures requests each named feature of the financial account that has not been
// requested, such as `intra_stripe_flows` or `outbound_payments.ach`, and waits until all are
// active, as each money movement needs its own (suite: "awaitTreasuryFeatures"). A feature still
// inactive after the wait skips the test.
func TreasuryFeatures(t *testing.T, conn acctest.Config, financialAccount string, features ...string) {
	t.Helper()
	path := "/v1/treasury/financial_accounts/" + financialAccount + "/features"
	var request wire.Object
	for _, f := range features {
		if featureStatus(get(t, conn, path), f) == "" {
			request = append(request, featureRequest(strings.Split(f, ".")))
		}
	}
	if len(request) > 0 {
		Post(t, conn, path, request)
	}
	var statuses wire.Object
	if !poll(func() bool {
		body := get(t, conn, path)
		statuses = nil
		active := true
		for _, f := range features {
			status := featureStatus(body, f)
			statuses = append(statuses, wire.Field{Name: f, Value: wire.String(status)})
			active = active && status == "active"
		}
		return active
	}) {
		t.Skipf("the features of financial account %s are not all active after %s: %v", financialAccount, activationLimit, statuses)
	}
}

// featureRequest is the `requested` flag of a feature at the dotted path names.
func featureRequest(names []string) wire.Field {
	if len(names) == 1 {
		return wire.Field{Name: names[0], Value: wire.Object{{Name: "requested", Value: wire.Bool(true)}}}
	}
	return wire.Field{Name: names[0], Value: wire.Object{featureRequest(names[1:])}}
}

// featureStatus is the status of the feature at the dotted path in a features object, or ""
// before it is requested.
func featureStatus(features wire.Object, feature string) string {
	obj := features
	for _, name := range strings.Split(feature, ".") {
		obj, _ = obj.GetObject(name)
	}
	status, _ := obj.GetString("status")
	return status
}

// FundedFinancialAccount returns IssuingFinancialAccount with the named features active and at
// least amount cents of cash, which a received credit adds when it holds less. Received credits
// cannot be removed, so in smoke mode the money stays for later runs.
func FundedFinancialAccount(t *testing.T, conn acctest.Config, amount int64, features ...string) string {
	t.Helper()
	fa := IssuingFinancialAccount(t, conn)
	TreasuryFeatures(t, conn, fa, features...)
	if cash(t, conn, fa) < amount {
		ReceivedCredit(t, conn, fa, amount)
		if !poll(func() bool { return cash(t, conn, fa) >= amount }) {
			t.Skipf("financial account %s holds less than %d cents after a received credit of that much", fa, amount)
		}
	}
	return fa
}

// cash is the financial account's `usd` cash balance in cents.
func cash(t *testing.T, conn acctest.Config, financialAccount string) int64 {
	t.Helper()
	balance, _ := get(t, conn, "/v1/treasury/financial_accounts/"+financialAccount).GetObject("balance")
	byCurrency, _ := balance.GetObject("cash")
	v, _ := byCurrency.Get("usd")
	n, _ := v.(wire.Int)
	return int64(n)
}

// DestinationFinancialAccount returns a second `usd` financial account of the connected account
// conn names, with `intra_stripe_flows` active, for outbound transfers to send to (suite:
// "create to a financial account").
func DestinationFinancialAccount(t *testing.T, conn acctest.Config) string {
	t.Helper()
	id := tagged(t, conn, "/v1/treasury/financial_accounts", treasuryDestinationRole)
	if id == "" {
		id = Create(t, conn, "/v1/treasury/financial_accounts", wire.Object{
			{Name: "supported_currencies", Value: wire.Array{wire.String("usd")}},
			{Name: "metadata", Value: wire.Object{{Name: "tfacc_role", Value: wire.String(treasuryDestinationRole)}}},
		})
	}
	TreasuryFeatures(t, conn, id, "intra_stripe_flows")
	return id
}

// SelfBankAccount returns a US bank account payment method attached to the connected account
// conn names, for inbound and outbound flows, which an inbound transfer pulls from and an
// outbound transfer can send to. acct is the platform, which requests the connected account's
// ACH capability. As the suite does ("setUpSelfBankAccount"), it turns `us_bank_account` on in
// the platform's payment method configuration for the account, creates a setup intent with
// `attach_to_self` for Stripe's test bank account and verifies its microdeposits. Smoke runs
// reuse a bank account an earlier run attached. A sandbox that refuses the setup intent to a new
// platform skips the test with Stripe's message.
func SelfBankAccount(t *testing.T, acct, conn acctest.Config) string {
	t.Helper()
	if pm := attachedBankAccount(t, conn); pm != "" {
		return pm
	}
	accountPath := "/v1/accounts/" + conn.StripeAccount
	capabilities, _ := get(t, acct, accountPath).GetObject("capabilities")
	if _, requested := capabilities.Get(achCapability); !requested {
		Post(t, acct, accountPath, wire.Object{{Name: "capabilities", Value: wire.Object{
			{Name: achCapability, Value: wire.Object{{Name: "requested", Value: wire.Bool(true)}}},
		}}})
	}
	awaitCapabilities(t, acct, conn.StripeAccount, achCapability)
	bankPaymentMethodConfiguration(t, conn)

	resp, err := Client(conn).Do(t.Context(), stripeapi.Request{Method: "POST", Path: "/v1/setup_intents", Params: wire.Object{
		{Name: "attach_to_self", Value: wire.Bool(true)},
		{Name: "flow_directions", Value: wire.Array{wire.String("inbound"), wire.String("outbound")}},
		{Name: "payment_method_data", Value: wire.Object{
			{Name: "type", Value: wire.String("us_bank_account")},
			{Name: "us_bank_account", Value: wire.Object{
				{Name: "routing_number", Value: wire.String("110000000")},
				{Name: "account_number", Value: wire.String("000123456789")},
				{Name: "account_holder_type", Value: wire.String("individual")},
			}},
			{Name: "billing_details", Value: wire.Object{{Name: "name", Value: wire.String("Jenny Rosen")}}},
		}},
		{Name: "payment_method_options", Value: wire.Object{{Name: "us_bank_account", Value: wire.Object{{Name: "verification_method", Value: wire.String("microdeposits")}}}}},
		{Name: "mandate_data", Value: wire.Object{{Name: "customer_acceptance", Value: wire.Object{{Name: "type", Value: wire.String("offline")}}}}},
		{Name: "automatic_payment_methods", Value: wire.Object{{Name: "enabled", Value: wire.Bool(true)}, {Name: "allow_redirects", Value: wire.String("never")}}},
		{Name: "confirm", Value: wire.Bool(true)},
	}})
	var apiErr *stripeapi.APIError
	if errors.As(err, &apiErr) && strings.HasPrefix(apiErr.Message, directChargesRefusal) && acctest.Smoke() {
		t.Skipf("this sandbox refuses to attach a bank account to the Custom connected account, so no inbound transfer can be made: %s", apiErr.Message)
	}
	if err != nil {
		t.Fatalf("POST /v1/setup_intents: %v", err)
	}
	intent := resp.Body.(wire.Object)
	id, _ := intent.GetString("id")
	if status, _ := intent.GetString("status"); status == "requires_action" {
		resp, err = Client(conn).Do(t.Context(), stripeapi.Request{Method: "POST", Path: "/v1/setup_intents/" + id + "/verify_microdeposits", Params: wire.Object{
			{Name: "amounts", Value: wire.Array{wire.Int(32), wire.Int(45)}},
		}})
		if err != nil {
			t.Fatalf("POST /v1/setup_intents/%s/verify_microdeposits: %v", id, err)
		}
		intent = resp.Body.(wire.Object)
	}
	pm, _ := intent.GetString("payment_method")
	if status, _ := intent.GetString("status"); status != "succeeded" || pm == "" {
		t.Fatalf("setup intent %s did not attach a bank account: %v", id, intent)
	}
	return pm
}

// attachedBankAccount returns the payment method of a succeeded setup intent with
// `attach_to_self` on the connected account, or "".
func attachedBankAccount(t *testing.T, conn acctest.Config) string {
	t.Helper()
	resp, err := Client(conn).Do(t.Context(), stripeapi.Request{Method: "GET", Path: "/v1/setup_intents", Params: wire.Object{
		{Name: "attach_to_self", Value: wire.Bool(true)},
		{Name: "limit", Value: wire.Int(100)},
	}})
	if err != nil {
		t.Fatalf("GET /v1/setup_intents: %v", err)
	}
	data, _ := resp.Body.(wire.Object).Get("data")
	items, _ := data.(wire.Array)
	for _, item := range items {
		intent, _ := item.(wire.Object)
		status, _ := intent.GetString("status")
		pm, _ := intent.GetString("payment_method")
		if status == "succeeded" && pm != "" {
			return pm
		}
	}
	return ""
}

// bankPaymentMethodConfiguration turns `us_bank_account` on in the platform's payment method
// configuration for the connected account conn names: the default one whose `application` is
// set. With it off, Stripe refuses the setup intent's payment method type.
func bankPaymentMethodConfiguration(t *testing.T, conn acctest.Config) {
	t.Helper()
	resp, err := Client(conn).Do(t.Context(), stripeapi.Request{Method: "GET", Path: "/v1/payment_method_configurations", Params: wire.Object{{Name: "limit", Value: wire.Int(100)}}})
	if err != nil {
		t.Fatalf("GET /v1/payment_method_configurations: %v", err)
	}
	data, _ := resp.Body.(wire.Object).Get("data")
	items, _ := data.(wire.Array)
	for _, item := range items {
		cfg, _ := item.(wire.Object)
		isDefault, _ := cfg.GetBool("is_default")
		if _, fromPlatform := cfg.GetString("application"); !isDefault || !fromPlatform {
			continue
		}
		id, _ := cfg.GetString("id")
		bank, _ := cfg.GetObject("us_bank_account")
		preference, _ := bank.GetObject("display_preference")
		if value, _ := preference.GetString("value"); value != "on" {
			Post(t, conn, "/v1/payment_method_configurations/"+id, wire.Object{{Name: "us_bank_account", Value: wire.Object{
				{Name: "display_preference", Value: wire.Object{{Name: "preference", Value: wire.String("on")}}},
			}}})
		}
		return
	}
	t.Fatalf("the connected account %s has no default payment method configuration from the platform: %v", conn.StripeAccount, resp.Body)
}
