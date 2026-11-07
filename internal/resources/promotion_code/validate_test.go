package promotion_code_test

import (
	"strings"
	"testing"

	"github.com/computomatic/terraform-provider-stripe/internal/acctest/tfacc"
	"github.com/computomatic/terraform-provider-stripe/internal/resources/promotion_code"
)

// The minimum amount's currency as a key of `restrictions.currency_options` fails at plan: Stripe
// adds that currency to the map by itself and the provider leaves it out on read. Another
// currency passes.
func TestOwnCurrencyInCurrencyOptionsIsRefused(t *testing.T) {
	ctor, err := promotion_code.NewResource()
	if err != nil {
		t.Fatal(err)
	}
	config := func(key string) map[string]any {
		return map[string]any{
			"code":      "OPTIONS",
			"promotion": map[string]any{"type": "coupon", "coupon": "co_1"},
			"restrictions": map[string]any{
				"minimum_amount":          2000,
				"minimum_amount_currency": "usd",
				"currency_options":        map[string]any{key: map[string]any{"minimum_amount": 1800}},
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
