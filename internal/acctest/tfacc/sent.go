package tfacc

import (
	"fmt"
	"strconv"
	"strings"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/terraform"

	"github.com/computomatic/terraform-provider-stripe/internal/acctest"
	"github.com/computomatic/terraform-provider-stripe/internal/wire"
)

// Sent returns a check that the newest request the fake logged for c's account with method and
// the path *path sent each dotted path of want, a list element by its index, with the value
// written as text. It proves what reached Stripe for an argument the state keeps from
// configuration, which the state alone cannot show. It checks nothing in smoke mode or for a
// connected account, whose requests the log does not hold: there a successful apply is what
// Stripe took.
func Sent(t *testing.T, c acctest.Config, method string, path *string, want map[string]string) resource.TestCheckFunc {
	return func(*terraform.State) error {
		if c.Smoke || c.StripeAccount != "" {
			return nil
		}
		for _, r := range acctest.RequestLog(t, c) {
			if r.Method != method || r.Path != *path {
				continue
			}
			for p, v := range want {
				if got, ok := textAt(r.Params, p); !ok || got != v {
					return fmt.Errorf("%s %s sent %s = %q (present: %t), want %q", method, *path, p, got, ok, v)
				}
			}
			return nil
		}
		return fmt.Errorf("the fake logged no %s %s", method, *path)
	}
}

// textAt returns the scalar at a dotted path of a logged request's parameters as text.
func textAt(v wire.Value, path string) (string, bool) {
	for _, step := range strings.Split(path, ".") {
		switch c := v.(type) {
		case wire.Object:
			next, ok := c.Get(step)
			if !ok {
				return "", false
			}
			v = next
		case wire.Array:
			i, err := strconv.Atoi(step)
			if err != nil || i < 0 || i >= len(c) {
				return "", false
			}
			v = c[i]
		default:
			return "", false
		}
	}
	switch s := v.(type) {
	case wire.String:
		return string(s), true
	case wire.Int:
		return strconv.FormatInt(int64(s), 10), true
	case wire.Decimal:
		return string(s), true
	case wire.Bool:
		return strconv.FormatBool(bool(s)), true
	}
	return "", false
}
