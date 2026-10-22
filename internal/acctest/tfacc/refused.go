package tfacc

import (
	"fmt"
	"regexp"
	"strings"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/terraform"

	"github.com/computomatic/terraform-provider-stripe/internal/acctest"
	"github.com/computomatic/terraform-provider-stripe/internal/stripeapi"
	"github.com/computomatic/terraform-provider-stripe/internal/wire"
)

// RefusedDestroy and RefusedReplace match the engine's refusal of a destroy and of a
// replacement of an object Stripe can neither delete nor archive.
var (
	RefusedDestroy = regexp.MustCompile(`Cannot destroy stripe_`)
	RefusedReplace = regexp.MustCompile(`Cannot replace stripe_`)
)

// LogMark returns a check that records in n how many requests the fake has logged for c's
// account, for Untouched to look only at what came after. It records nothing in smoke mode
// or for a connected account, whose requests the log does not hold.
func LogMark(t *testing.T, c acctest.Config, n *int) resource.TestCheckFunc {
	return func(*terraform.State) error {
		if !c.Smoke && c.StripeAccount == "" {
			*n = len(acctest.RequestLog(t, c))
		}
		return nil
	}
}

// Holds returns a check that the object at *path is in the Stripe account with field holding
// want.
func Holds(t *testing.T, c acctest.Config, path *string, field string, want wire.Value) resource.TestCheckFunc {
	return func(*terraform.State) error {
		resp, err := Client(c).Do(t.Context(), stripeapi.Request{Method: "GET", Path: *path})
		if err != nil {
			return fmt.Errorf("retrieving %s: %w", *path, err)
		}
		body, _ := resp.Body.(wire.Object)
		got, _ := body.Get(field)
		have, _ := wire.EncodeJSON(got)
		wanted, _ := wire.EncodeJSON(want)
		if string(have) != string(wanted) {
			return fmt.Errorf("%s of %s is %s, want %s", field, *path, have, wanted)
		}
		return nil
	}
}

// Untouched returns the check that ends a `_delete` test of a resource whose destroy is
// refused: the object at *path is still in the Stripe account with field holding want, and,
// against the fake, no request logged after the *since that LogMark counted changed it or called one of
// its actions, so the refused destroy sent nothing. The fake's log does not hold requests made
// through `Stripe-Account`, so a connected account's object is checked by its field only.
func Untouched(t *testing.T, c acctest.Config, path *string, since *int, field string, want wire.Value) resource.TestCheckFunc {
	holds := Holds(t, c, path, field, want)
	return func(s *terraform.State) error {
		if err := holds(s); err != nil {
			return fmt.Errorf("after the refused destroy: %w", err)
		}
		if c.Smoke || c.StripeAccount != "" {
			return nil
		}
		// The log is newest first, so what came after the mark leads it.
		log := acctest.RequestLog(t, c)
		for _, r := range log[:max(len(log)-*since, 0)] {
			if r.Method != "GET" && (r.Path == *path || strings.HasPrefix(r.Path, *path+"/")) {
				return fmt.Errorf("the refused destroy sent %s %s", r.Method, r.Path)
			}
		}
		return nil
	}
}
