package tfacc

import (
	"context"
	"testing"

	"github.com/computomatic/terraform-provider-stripe/internal/acctest"
	"github.com/computomatic/terraform-provider-stripe/internal/stripeapi"
	"github.com/computomatic/terraform-provider-stripe/internal/wire"
)

// transfersRole is the `metadata[tfacc_role]` value of the connected account that transfers
// and destination charges pay. Smoke runs reuse it and never delete it, as the conformance
// suite does (suite: "A transfers-only connected account activates at once").
const transfersRole = "transfers"

// TransfersAccount returns the ID of a US Custom connected account of c's account whose
// `transfers` capability is active, so that it can receive transfers and destination charges.
// It requests only that capability, with the onboarding values Stripe documents as always
// verifying, which the suite sends ("transfersAccount"). The fake activates it at once.
func TransfersAccount(t *testing.T, c acctest.Config) string {
	t.Helper()
	id := tagged(t, c, "/v1/accounts", transfersRole)
	if id == "" {
		id = Create(t, c, "/v1/accounts", wire.Object{
			{Name: "type", Value: wire.String("custom")},
			{Name: "country", Value: wire.String("US")},
			{Name: "business_type", Value: wire.String("individual")},
			{Name: "email", Value: wire.String(c.Name("transfers") + "@example.com")},
			{Name: "capabilities", Value: wire.Object{{Name: "transfers", Value: wire.Object{{Name: "requested", Value: wire.Bool(true)}}}}},
			{Name: "business_profile", Value: wire.Object{
				{Name: "mcc", Value: wire.String("5734")},
				{Name: "url", Value: wire.String("https://accessible.stripe.com")},
				{Name: "product_description", Value: wire.String("Acceptance test transfers")},
			}},
			{Name: "individual", Value: verifiedIndividual(c, "transfers-individual")},
			{Name: "external_account", Value: wire.String("btok_us_verified")},
			{Name: "tos_acceptance", Value: wire.Object{{Name: "date", Value: wire.Int(1609459200)}, {Name: "ip", Value: wire.String("8.8.8.8")}}},
			{Name: "metadata", Value: wire.Object{{Name: "tfacc_role", Value: wire.String(transfersRole)}}},
		})
	}
	awaitCapabilities(t, c, id, "transfers")
	return id
}

// FundAvailableBalance makes sure c's account has at least need available in currency, which a
// transfer draws on. When it has less, it charges the shortfall, with the card fee on top, to
// `pm_card_bypassPending`, whose funds are available at once, as the suite does
// ("fundAvailableBalance").
func FundAvailableBalance(t *testing.T, c acctest.Config, currency string, need int64) {
	t.Helper()
	have := availableBalance(t, c, currency)
	if have >= need {
		return
	}
	// The card fee is 2.9% and 30 cents, so the charge is enough that what it nets covers the
	// shortfall, with a margin for rounding.
	succeededIntent(t, c, wire.Object{
		{Name: "amount", Value: wire.Int(max((need-have+30)*100/97+20, 1000))},
		{Name: "currency", Value: wire.String(currency)},
	}, "pm_card_bypassPending")
	if !poll(func() bool { have = availableBalance(t, c, currency); return have >= need }) {
		t.Fatalf("the available balance is %d %s after %s, want %d", have, currency, activationLimit, need)
	}
}

func availableBalance(t *testing.T, c acctest.Config, currency string) int64 {
	t.Helper()
	available, _ := get(t, c, "/v1/balance").Get("available")
	items, _ := available.(wire.Array)
	for _, item := range items {
		obj, _ := item.(wire.Object)
		if cur, _ := obj.GetString("currency"); cur == currency {
			amount, _ := obj.Get("amount")
			if n, ok := amount.(wire.Int); ok {
				return int64(n)
			}
		}
	}
	return 0
}

// Transfer funds c's account and transfers amount in currency from its available balance to
// the connected account destination, and returns the transfer's ID. Transfers cannot be
// deleted, so in smoke mode a cleanup reverses what remains of it, which returns the money to
// the platform, as the suite does ("cleanupReverseTransfer").
func Transfer(t *testing.T, c acctest.Config, destination, currency string, amount int64) string {
	t.Helper()
	FundAvailableBalance(t, c, currency, amount)
	id := Create(t, c, "/v1/transfers", wire.Object{
		{Name: "amount", Value: wire.Int(amount)},
		{Name: "currency", Value: wire.String(currency)},
		{Name: "destination", Value: wire.String(destination)},
	})
	if acctest.Smoke() {
		t.Cleanup(func() {
			// The test's context is done by the time cleanups run.
			if _, err := Client(c).Do(context.Background(), stripeapi.Request{Method: "POST", Path: "/v1/transfers/" + id + "/reversals"}); err != nil {
				t.Logf("reversing what remains of transfer %s: %v", id, err)
			}
		})
	}
	return id
}

// DestinationCharge makes a succeeded charge of amount in currency on c's account that sends
// the funds to the connected account destination less an application fee of fee, and returns
// the charge's ID and the fee's (suite: "destinationCharge"). The fee appears on the charge a
// moment after it succeeds. Application fees cannot be deleted, so callers keep amounts small.
func DestinationCharge(t *testing.T, c acctest.Config, destination, currency string, amount, fee int64) (charge, applicationFee string) {
	t.Helper()
	_, charge = succeededIntent(t, c, wire.Object{
		{Name: "amount", Value: wire.Int(amount)},
		{Name: "currency", Value: wire.String(currency)},
		{Name: "application_fee_amount", Value: wire.Int(fee)},
		{Name: "transfer_data", Value: wire.Object{{Name: "destination", Value: wire.String(destination)}}},
	}, "pm_card_bypassPending")
	if !poll(func() bool {
		applicationFee, _ = get(t, c, "/v1/charges/"+charge).GetString("application_fee")
		return applicationFee != ""
	}) {
		t.Fatalf("charge %s has no application fee after %s", charge, activationLimit)
	}
	return charge, applicationFee
}
