package fields

import "testing"

// A literal shows as a code span that holds it whole: a value with a backtick takes a longer
// fence, padded so that a backtick at either end stays in the value.
func TestStatementCodeSpans(t *testing.T) {
	for value, want := range map[string]string{
		"once":   "Default (Stripe): `once`.",
		"":       "Default (Stripe): `\"\"`.",
		"a`b":    "Default (Stripe): `` a`b ``.",
		"`x`":    "Default (Stripe): `` `x` ``.",
		"a``b`c": "Default (Stripe): ``` a``b`c ```.",
	} {
		if got := (Default{Kind: DefaultValue, Value: value}).Statement(); got != want {
			t.Errorf("%q: %s, want %s", value, got, want)
		}
	}
}
