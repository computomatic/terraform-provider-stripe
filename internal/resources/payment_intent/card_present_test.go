package payment_intent_test

import (
	"fmt"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"

	"github.com/computomatic/terraform-provider-stripe/internal/acctest"
)

// A card-present intent's options change in place, each on an intent of its own, as the suite's
// probes `update/53`, `update/54` and `update/55` change them on an intent that takes card-present
// payments only, captured manually.
func TestAccPaymentIntent_cardPresentOptions(t *testing.T) {
	for name, c := range map[string]struct{ created, changed, attr, before, after string }{
		"capture_method":     {`capture_method = "manual"`, `capture_method = "manual_preferred"`, "capture_method", "manual", "manual_preferred"},
		"extended":           {`request_extended_authorization = true`, `request_extended_authorization = false`, "request_extended_authorization", "true", "false"},
		"incremental_signal": {`request_incremental_authorization_support = true`, `request_incremental_authorization_support = false`, "request_incremental_authorization_support", "true", "false"},
	} {
		t.Run(name, func(t *testing.T) {
			acct := acctest.NewAccount(t)
			options := func(member string) string {
				return fmt.Sprintf(`  allowed_payment_method_types = ["card_present"]
  capture_method               = "manual"
  payment_method_options       = { card_present = { %s } }
`, member)
			}
			at := "payment_method_options.card_present." + c.attr
			inPlace(t, acct, options(c.created), options(c.changed),
				resource.TestCheckResourceAttr(address, at, c.before),
				resource.TestCheckResourceAttr(address, at, c.after))
		})
	}
}
