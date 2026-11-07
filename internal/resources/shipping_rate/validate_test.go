package shipping_rate_test

import (
	"strings"
	"testing"

	"github.com/computomatic/terraform-provider-stripe/internal/acctest/tfacc"
	"github.com/computomatic/terraform-provider-stripe/internal/resources/shipping_rate"
)

// The rate's own currency as a key of `fixed_amount.currency_options` fails at plan: Stripe adds
// that currency to the map by itself and the provider leaves it out on read. Another currency
// passes.
func TestOwnCurrencyInCurrencyOptionsIsRefused(t *testing.T) {
	ctor, err := shipping_rate.NewResource()
	if err != nil {
		t.Fatal(err)
	}
	config := func(key string) map[string]any {
		return map[string]any{
			"display_name": "Ground",
			"type":         "fixed_amount",
			"fixed_amount": map[string]any{
				"amount":           500,
				"currency":         "usd",
				"currency_options": map[string]any{key: map[string]any{"amount": 7}},
			},
		}
	}
	if diags := tfacc.ValidateConfig(t, ctor, config("usd")); !diags.HasError() || !strings.Contains(diags.Errors()[0].Summary(), "Own currency") {
		t.Errorf("the own currency must fail: %v", diags)
	}
	if diags := tfacc.ValidateConfig(t, ctor, config("eur")); diags.HasError() {
		t.Errorf("another currency must pass: %v", diags)
	}
}
