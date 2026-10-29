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
