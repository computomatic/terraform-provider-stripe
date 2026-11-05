package main

import (
	"context"
	"strings"
	"testing"

	"github.com/computomatic/terraform-provider-stripe/internal/engine"
)

// A sentence after a description that already ends a sentence inside a closing bracket or
// quote adds no second full stop; one after a description with none adds one.
func TestStatementAfterADescriptionEndsOneSentence(t *testing.T) {
	p := &page{markdown: &markdown{}, resource: true}
	for desc, want := range map[string]string{
		"Pick one (the default.)":   "Pick one (the default.) Default: none.",
		`Stripe says "done."`:       `Stripe says "done." Default: none.`,
		"A list of product IDs":     "A list of product IDs. Default: none.",
		"See the guide (on Stripe)": "See the guide (on Stripe). Default: none.",
		"Ends here.":                "Ends here. Default: none.",
	} {
		got := p.line(&attr{Name: "x", Description: desc, Optional: true, Default: "Default: none."}, "x", true)
		if !strings.HasSuffix(got, want) {
			t.Errorf("%q: %s, want it to end %s", desc, got, want)
		}
	}
}

// A field another argument makes stays Optional alone, so that removing it clears it: no
// declaration makes it Computed, which the hash forms of token-or-hash parameters are.
func TestMadeByFieldIsNotComputed(t *testing.T) {
	s, err := loadSchemas(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"stripe_payment_intent", "stripe_setup_intent"} {
		a := at(byType(s.Resources, name).Attrs, "payment_method")
		if a == nil || !a.Optional || a.Computed {
			t.Errorf("%s.payment_method: %+v, want Optional alone", name, a)
		}
	}
}

// Stripe's recorded refusal follows the description and comes before the default statement, on a
// required argument too.
func TestRefusalPrecedesTheDefaultStatement(t *testing.T) {
	p := &page{resource: true}
	refused := `Stripe refused it on a US account on create: "No".`
	got := p.line(&attr{Name: "x", Description: "A thing", Optional: true, Default: "Default: none.", Refused: refused}, "x", true)
	if want := `A thing. ` + refused + ` Default: none.`; !strings.HasSuffix(got, want) {
		t.Errorf("%s, want it to end %s", got, want)
	}
	if got := p.line(&attr{Name: "x", Description: "A thing", Required: true, Refused: refused}, "x", true); !strings.HasSuffix(got, refused) {
		t.Errorf("a required argument: %s", got)
	}
}

// Where Stripe takes an argument it refuses in other requests follows the description and comes
// before the default statement.
func TestAnswerPrecedesTheDefaultStatement(t *testing.T) {
	p := &page{resource: true}
	answer := `Stripe takes it beside a suffix. Elsewhere Stripe refused it on a US account on create: "No".`
	got := p.line(&attr{Name: "x", Description: "A thing", Optional: true, Default: "Default: none.", Answer: answer}, "x", true)
	if want := `A thing. ` + answer + ` Default: none.`; !strings.HasSuffix(got, want) {
		t.Errorf("%s, want it to end %s", got, want)
	}
}

// A value Stripe fails on every attempt is named after the description and before the default
// statement.
func TestUnreachableValuePrecedesTheDefaultStatement(t *testing.T) {
	p := &page{resource: true}
	unreachable := "Stripe fails `fpx` on every attempt, so the conformance suite never sends it: \"Stripe answers 500\"."
	got := p.line(&attr{Name: "x", Description: "A thing", Optional: true, Default: "Default (Stripe): `card`.", Unreachable: unreachable}, "x", true)
	if want := `A thing. ` + unreachable + " Default (Stripe): `card`."; !strings.HasSuffix(got, want) {
		t.Errorf("%s, want it to end %s", got, want)
	}
}

// Inside a list element the engine keeps no prior value (an insert or a reorder would pair it
// with another element), so a subscription item's Optional and Computed `price` and `quantity`
// say that Stripe sets them when the list is next sent, not that the value stays.
func TestRemovalSentenceFollowsTheEngine(t *testing.T) {
	s, err := loadSchemas(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	r := byType(s.Resources, "stripe_subscription")
	for path, want := range map[string]string{
		"items.price":    engine.RemovalInList,
		"items.quantity": engine.RemovalInList,
		"items.id":       engine.RemovalInList,
	} {
		a := at(r.Attrs, path)
		if a == nil || !a.Optional || !a.Computed {
			t.Fatalf("%s: %+v, want an Optional and Computed argument", path, a)
		}
		got := (&page{markdown: &markdown{}, resource: true}).line(a, path, true)
		if !strings.HasSuffix(got, want) || strings.Contains(got, engine.RemovalKeeps) {
			t.Errorf("%s: %s, want it to end %q", path, got, want)
		}
	}
}
