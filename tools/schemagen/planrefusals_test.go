package main

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/computomatic/terraform-provider-stripe/internal/fields"
	"github.com/computomatic/terraform-provider-stripe/internal/wire"
)

// gadgetRules is the fixture pinned with `color` verified on the create and the update, the
// minimal create `{"name":"Gadget"}`, and the rules given on `color`.
func gadgetRules(t *testing.T, res Resource, rules ...EvidenceRule) *Pinned {
	t.Helper()
	ev := pinned(t, res, map[string]string{"request/PostGadgets/color": ClassVerified, "request/PostGadgetsId/color": ClassVerified})
	object := ""
	for _, l := range ev.Attributes.Leaves {
		if l.Path == "color" && l.Side == SideRequest {
			object = l.Object
			ev.Evidence.Leaves = append(ev.Evidence.Leaves, EvidenceLeaf{Key: l.Key, Status: "proven", Tests: []string{"TestGadgetAttributes/leaves/" + l.Key.String()}})
		}
	}
	ev.creates = map[string][]minimalCreate{object + " PostGadgets": {
		{Object: object, Operation: "PostGadgets", Variant: "other", Status: 200, Params: recordObject{{Name: "name", Value: wire.String("Other")}}},
		{Object: object, Operation: "PostGadgets", Status: 200, Params: recordObject{{Name: "name", Value: wire.String("Gadget")}}},
	}}
	for _, r := range rules {
		r.Object, r.Path, r.Rule = object, "color", "Stripe's answer turns on the request"
		ev.Evidence.Rules = append(ev.Evidence.Rules, r)
	}
	return ev
}

// probe is an answer rule's observation: what a probe sent, to an object created with what,
// and Stripe's status with its message where it refused.
func probe(name, params, createdWith string, status int, message, code string) (o RuleObservation) {
	o.Condition.AccountCountry, o.Condition.Probe, o.Condition.Params = "US", name, json.RawMessage(params)
	if createdWith != "" {
		o.Condition.CreatedWith = json.RawMessage(createdWith)
	}
	o.Value = RuleValue{Status: status, Message: message, Code: code}
	return o
}

// A refused create is put to the plan as the whole request it sent, a value the suite generated
// standing for any, and a refused update also as the whole object it was created with, so that
// the refusal holds only where all of it matches.
func TestRefusedRequestsArePutToThePlan(t *testing.T) {
	ev := gadgetRules(t, gadget(),
		EvidenceRule{Operation: "PostGadgets", Observations: []RuleObservation{
			probe("isolated/blue", `{"name":"Gadget","color":"blue"}`, "", 200, "", ""),
			probe("isolated/red_for_owner", `{"name":"Gadget","color":"red","owner":{"email":{"$gen":"email"}}}`, "", 400, "Not red.", ""),
		}},
		EvidenceRule{Operation: "PostGadgetsId", Observations: []RuleObservation{
			probe("isolated/update_red", `{"color":"red"}`, `{"name":"Gadget"}`, 200, "", ""),
			probe("isolated/update_red_with_settings", `{"color":"red"}`, `{"name":"Gadget","settings":{"mode":"fast","limit":3}}`, 400, "Not red with settings.", ""),
		}})
	f := fieldAt(cover(t, gadget(), ev).table, "color")
	want := []fields.PlanRefusal{
		{Sends: `{"name":"Gadget","color":"red","owner":{"email":{"$any":true}}}`, Probe: "isolated/red_for_owner", AccountCountry: "US", Message: "Not red."},
		{Update: true, Sends: `{"color":"red"}`, Holds: `{"name":"Gadget","settings":{"mode":"fast","limit":3}}`, Probe: "isolated/update_red_with_settings", AccountCountry: "US", Message: "Not red with settings."},
	}
	if f == nil || len(f.PlanRefusals) != len(want) {
		t.Fatalf("color: %+v", f)
	}
	for i := range want {
		if f.PlanRefusals[i] != want[i] {
			t.Errorf("refusal %d:\n got %+v\nwant %+v", i, f.PlanRefusals[i], want[i])
		}
	}
}

// A recorded number reaches the pattern with its text: an integer beyond 2^53 is not rounded and
// an exponent keeps its form.
func TestPatternsKeepRecordedNumbers(t *testing.T) {
	ev := gadgetRules(t, gadget(), EvidenceRule{Operation: "PostGadgets", Observations: []RuleObservation{
		probe("isolated/numbers", `{"name":"Gadget","color":"red","tags":[9007199254740993,1e21]}`, "", 400, "Too large.", ""),
	}})
	f := fieldAt(cover(t, gadget(), ev).table, "color")
	if f == nil || len(f.PlanRefusals) != 1 || f.PlanRefusals[0].Sends != `{"name":"Gadget","color":"red","tags":[9007199254740993,1e21]}` {
		t.Errorf("color: %+v", f)
	}
}

// The account's country, which the suite generates as `account_country`, is the probe's own
// account country, in which alone the pattern holds, so it translates to that country exactly,
// in the refused request and in the requests Stripe took that a refusal is weighed against.
func TestAccountCountryIsTheProbesCountry(t *testing.T) {
	ev := gadgetRules(t, gadget(), EvidenceRule{Operation: "PostGadgets", Observations: []RuleObservation{
		probe("isolated/red_here", `{"name":"Gadget","color":"red","owner":{"email":{"$gen":"account_country"}}}`, "", 400, "Not red here.", ""),
		probe("isolated/blue_here", `{"name":"Gadget","color":"blue","owner":{"email":{"$gen":"account_country"}}}`, "", 200, "", ""),
	}})
	f := fieldAt(cover(t, gadget(), ev).table, "color")
	if f == nil || len(f.PlanRefusals) != 1 || f.PlanRefusals[0].Sends != `{"name":"Gadget","color":"red","owner":{"email":"US"}}` {
		t.Errorf("color: %+v", f)
	}
	ev = gadgetRules(t, gadget(), EvidenceRule{Operation: "PostGadgets", Observations: []RuleObservation{
		probe("isolated/red_here", `{"name":"Gadget","color":"red","owner":{"email":{"$gen":"account_country"}}}`, "", 400, "Not red here.", ""),
	}})
	ev.Evidence.Rules[0].Observations[0].Condition.AccountCountry = ""
	if _, err := coverTable(coverageSpec(t), gadget(), ev); err == nil || !strings.Contains(err.Error(), "names no account country") {
		t.Errorf("an account country with none recorded: %v", err)
	}
}

// A refused request the table cannot send, here with the omitted `confirm`, matches no plan
// and is left out.
func TestARequestTheTableCannotSendIsLeftOut(t *testing.T) {
	ev := gadgetRules(t, gadget(), EvidenceRule{Operation: "PostGadgets", Observations: []RuleObservation{
		probe("isolated/red_confirmed", `{"name":"Gadget","color":"red","confirm":true}`, "", 400, "Not confirmed.", ""),
	}})
	if f := fieldAt(cover(t, gadget(), ev).table, "color"); f == nil || len(f.PlanRefusals) != 0 {
		t.Errorf("color: %+v", f)
	}
}

// A rule the plan cannot evaluate fails generation, naming why, unless LeftToStripe lists it;
// an entry that names no such rule fails too.
func TestRulesThePlanCannotTellFailUnlessLeftToStripe(t *testing.T) {
	for name, tc := range map[string]struct {
		observations []RuleObservation
		want         string
	}{
		"another object's state": {[]RuleObservation{
			probe("isolated/expired", `{"name":"Gadget","color":{"$gen":"ref","arg":"color:expired"}}`, "", 400, "Expired.", ""),
		}, "another object in a state of its own"},
		"a shared object": {[]RuleObservation{
			probe("isolated/shared", `{"name":"Gadget","color":{"$gen":"shared","arg":"color:held","offset":1}}`, "", 400, "Held elsewhere.", ""),
		}, "another object in a state of its own"},
		"a value read from the object": {[]RuleObservation{
			probe("isolated/read", `{"name":"Gadget","color":{"$gen":"read","arg":"color"}}`, "", 400, "Read.", ""),
		}, "as read from the object"},
		"an object that does not exist": {[]RuleObservation{
			probe("isolated/missing", `{"name":"Gadget","color":"col_none"}`, "", 400, "No such color.", "resource_missing"),
		}, "does not exist"},
		"a request Stripe also took": {[]RuleObservation{
			probe("isolated/red", `{"name":"Gadget","color":"red"}`, "", 400, "Not red.", ""),
			probe("isolated/red_with_owner", `{"name":"Gadget","color":"red","owner":{"phone":"1"}}`, "", 200, "", ""),
		}, "cannot tell them apart"},
		"the minimal create, which Stripe took": {[]RuleObservation{
			probe("isolated/plain", `{"name":"Gadget"}`, "", 400, "Plain.", ""),
		}, "Stripe took the request of the minimal create"},
		"the account's currency": {[]RuleObservation{
			probe("isolated/currency", `{"name":"Gadget","color":{"$gen":"account_currency"}}`, "", 400, "Currency.", ""),
		}, "`$gen` `account_currency`"},
		"a time relative to the request": {[]RuleObservation{
			probe("isolated/later", `{"name":"Gadget","color":{"$gen":"now","offset":86400}}`, "", 400, "Later.", ""),
		}, "`$gen` `now`"},
		"a value computed from another": {[]RuleObservation{
			probe("isolated/one_ton", `{"name":"Gadget","color":{"$gen":"computed","arg":"one_ton_amount"}}`, "", 400, "Too little.", ""),
		}, "`$gen` `computed`"},
		"an empty hash, which the suite's form left out": {[]RuleObservation{
			probe("isolated/empty_owner", `{"name":"Gadget","color":"red","owner":{}}`, "", 400, "Empty owner.", ""),
		}, "`owner` is empty or null"},
		"a null at depth": {[]RuleObservation{
			probe("isolated/null_phone", `{"name":"Gadget","color":"red","owner":{"phone":null}}`, "", 400, "Null phone.", ""),
		}, "`owner.phone` is empty or null"},
		"a generated value of an unknown kind": {[]RuleObservation{
			probe("isolated/new", `{"name":"Gadget","color":{"$gen":"novel"}}`, "", 400, "New.", ""),
		}, "not one the generator knows"},
	} {
		t.Run(name, func(t *testing.T) {
			rule := EvidenceRule{Operation: "PostGadgets", Observations: tc.observations}
			if _, err := coverTable(coverageSpec(t), gadget(), gadgetRules(t, gadget(), rule)); err == nil || !strings.Contains(err.Error(), tc.want) || !strings.Contains(err.Error(), "LeftToStripe") {
				t.Errorf("got %v, want %q", err, tc.want)
			}
			res := gadget()
			res.LeftToStripe = map[string]string{"PostGadgets color": "Stripe's refusal turns on what the plan cannot know."}
			if f := fieldAt(cover(t, res, gadgetRules(t, res, rule)).table, "color"); f == nil || len(f.PlanRefusals) != 0 {
				t.Errorf("left to Stripe: %+v", f)
			}
		})
	}
	res := gadget()
	res.LeftToStripe = map[string]string{"PostGadgetsId color": "No such rule."}
	if _, err := coverTable(coverageSpec(t), res, gadgetRules(t, res)); err == nil || !strings.Contains(err.Error(), "names PostGadgetsId color, which is no rule") {
		t.Errorf("a stale entry: %v", err)
	}
}

// An entry that names a probe leaves that refused request alone to Stripe: the plan refuses the
// rule's others, and an entry naming a probe the rule did not refuse fails generation.
func TestLeftToStripeNamesOneRefusedRequest(t *testing.T) {
	rule := EvidenceRule{Operation: "PostGadgets", Observations: []RuleObservation{
		probe("isolated/red_later", `{"name":"Gadget","color":"red","owner":{"phone":{"$gen":"now","offset":86400}}}`, "", 400, "Not red later.", ""),
		probe("isolated/red", `{"name":"Gadget","color":"red"}`, "", 400, "Not red.", ""),
	}}
	res := gadget()
	res.LeftToStripe = map[string]string{"PostGadgets color isolated/red_later": "A time relative to the probe."}
	f := fieldAt(cover(t, res, gadgetRules(t, res, rule)).table, "color")
	if f == nil || len(f.PlanRefusals) != 1 || f.PlanRefusals[0].Probe != "isolated/red" {
		t.Errorf("color: %+v", f)
	}
	res.LeftToStripe = map[string]string{"PostGadgets color isolated/blue": "No such probe."}
	plain := EvidenceRule{Operation: "PostGadgets", Observations: rule.Observations[1:]}
	if _, err := coverTable(coverageSpec(t), res, gadgetRules(t, res, plain)); err == nil || !strings.Contains(err.Error(), "names PostGadgets color isolated/blue, which is no rule") {
		t.Errorf("a stale probe: %v", err)
	}
}
