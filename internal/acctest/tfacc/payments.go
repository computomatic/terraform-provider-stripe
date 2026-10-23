package tfacc

import (
	"testing"

	"github.com/computomatic/terraform-provider-stripe/internal/acctest"
	"github.com/computomatic/terraform-provider-stripe/internal/stripeapi"
	"github.com/computomatic/terraform-provider-stripe/internal/wire"
)

// Create posts to Stripe behind the provider's back and returns the new object's ID. Tests
// use it for objects the provider has no resource for.
func Create(t *testing.T, c acctest.Config, path string, params wire.Object) string {
	t.Helper()
	resp, err := Client(c).Do(t.Context(), stripeapi.Request{Method: "POST", Path: path, Params: params})
	if err != nil {
		t.Fatalf("POST %s: %v", path, err)
	}
	id, ok := resp.Body.(wire.Object).GetString("id")
	if !ok {
		t.Fatalf("POST %s: no id in %v", path, resp.Body)
	}
	return id
}

// SucceededPaymentIntent creates and confirms a 2000 cent payment intent with the Stripe test
// payment method paymentMethod, as the suite does ("createSucceededPaymentIntent"), and
// returns the intent's ID and its charge's.
func SucceededPaymentIntent(t *testing.T, c acctest.Config, paymentMethod string) (intent, charge string) {
	t.Helper()
	return succeededIntent(t, c, wire.Object{
		{Name: "amount", Value: wire.Int(2000)},
		{Name: "currency", Value: wire.String("usd")},
	}, paymentMethod)
}

// succeededIntent creates and confirms a payment intent of params with paymentMethod, and
// returns the intent's ID and its charge's.
func succeededIntent(t *testing.T, c acctest.Config, params wire.Object, paymentMethod string) (intent, charge string) {
	t.Helper()
	params = append(params,
		wire.Field{Name: "payment_method", Value: wire.String(paymentMethod)},
		wire.Field{Name: "confirm", Value: wire.Bool(true)},
		wire.Field{Name: "automatic_payment_methods", Value: wire.Object{
			{Name: "enabled", Value: wire.Bool(true)},
			{Name: "allow_redirects", Value: wire.String("never")},
		}},
	)
	resp, err := Client(c).Do(t.Context(), stripeapi.Request{Method: "POST", Path: "/v1/payment_intents", Params: params})
	if err != nil {
		t.Fatalf("creating a payment intent: %v", err)
	}
	body := resp.Body.(wire.Object)
	intent, _ = body.GetString("id")
	charge, _ = body.GetString("latest_charge")
	if charge == "" {
		t.Fatalf("payment intent %s has no charge: %v", intent, body)
	}
	return intent, charge
}
