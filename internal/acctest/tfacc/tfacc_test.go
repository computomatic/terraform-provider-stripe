package tfacc

import (
	"errors"
	"testing"
)

func TestSkipWhenFakeLacks(t *testing.T) {
	cases := []struct {
		name    string
		err     error
		skipped bool
	}{
		{"not implemented", errors.New("Stripe API error: fake_not_implemented: parameter wifi is not implemented"), true},
		{"unproven", errors.New("fake_unproven: parameter wifi"), true},
		{"a fake crash", errors.New("fake_internal_error: wifi: a request handler panicked"), false},
		{"another parameter", errors.New("fake_not_implemented: parameter tipping"), false},
		{"a real error", errors.New("parameter_unknown: wifi"), false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			var inner *testing.T
			t.Run("check", func(t *testing.T) {
				inner = t
				if got := SkipWhenFakeLacks(t, "wifi")(c.err); got == nil {
					t.Error("an error must be returned when the test is not skipped")
				}
			})
			if inner.Skipped() != c.skipped {
				t.Errorf("skipped = %v, want %v", inner.Skipped(), c.skipped)
			}
		})
	}
	if err := SkipWhenFakeLacks(t, "wifi")(nil); err != nil {
		t.Errorf("no error stays no error: %v", err)
	}
}
