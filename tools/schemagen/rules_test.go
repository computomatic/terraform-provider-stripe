package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/computomatic/terraform-provider-stripe/internal/fields"
)

// suiteFixture reads the suite's fixture evidence record, cut to two objects
// (testdata/evidence/README.md).
func suiteFixture(t *testing.T) []byte {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("testdata", "evidence", "evidence.json"))
	if err != nil {
		t.Fatal(err)
	}
	return data
}

// The reader takes the suite's own fixture record whole, and refuses a member the format does
// not define, naming where in the record it is, and a value outside a closed set it reads.
func TestEvidenceReaderTakesTheSuiteFixtureAndRefusesTheUnknown(t *testing.T) {
	data := suiteFixture(t)
	e, err := decodeEvidence(data)
	if err != nil {
		t.Fatalf("the suite's fixture: %v", err)
	}
	var unset *EvidenceUnset
	for _, l := range e.Leaves {
		if l.Object == "coupon" && l.Operation == "PostCouponsCoupon" && l.Path == "name" {
			unset = l.Unset
		}
	}
	if unset == nil || unset.By != "empty_string" || unset.Result != "refused" {
		t.Errorf("coupon name's unset: %+v", unset)
	}
	for _, tc := range []struct {
		name, old, new, want string
	}{
		{"an unknown top-level member", `"format_version": 1,`, `"format_version": 1, "verdicts": [],`, `decode: verdicts is a member the format does not define`},
		{"an unknown leaf member", `"status": "proven",`, `"status": "proven", "since": "2026-10-01",`, `decode: leaves[0] (request/PostCoupons/amount_off).since is a member the format does not define`},
		{"an unknown member of a leaf's fact", `"result": "refused"`, `"result": "refused", "retried": true`, `decode: leaves[14] (request/PostCouponsCoupon/name).unset.retried is a member the format does not define`},
		{"an unknown unset result", `"result": "refused"`, `"result": "cleared"`, `unset result "cleared"`},
		{"an unknown default kind", `"kind": "null"`, `"kind": "empty"`, `default kind "empty"`},
		{"an unknown status", `"status": "unreachable"`, `"status": "skipped"`, `status "skipped"`},
		{"another format", `"format_version": 1`, `"format_version": 2`, "format version 2"},
	} {
		changed := bytes.Replace(data, []byte(tc.old), []byte(tc.new), 1)
		if bytes.Equal(changed, data) {
			t.Fatalf("%s: the fixture has no %s", tc.name, tc.old)
		}
		if _, err := decodeEvidence(changed); err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Errorf("%s: %v, want %q", tc.name, err, tc.want)
		}
	}
}

// A rule's observation is the one matcher the generator reads, so the reader refuses a kind the
// format does not define and a condition it cannot weigh, and reads every kind the record holds.
func TestEvidenceReaderReadsRuleObservationsStrictly(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("testdata", "evidence", "run-40926522642.json"))
	if err != nil {
		t.Fatal(err)
	}
	withObservation := func(observation string) []byte {
		rule := `"rules": [{"object": "coupon", "operation": "PostCoupons", "path": "name", "rule": "a rule", "observations": [` + observation + `], "tests": ["a"]}],`
		return bytes.Replace(data, []byte(`"rules": [],`), []byte(rule), 1)
	}
	for _, m := range []string{
		`{"kind": "value", "value": false}`, `{"kind": "absent"}`, `{"kind": "null"}`, `{"kind": "sent", "path": "name"}`,
		`{"kind": "created", "prefix": "arn:"}`, `{"kind": "fingerprint", "path": "account_number"}`,
		`{"kind": "from", "path": "currency", "ref": "application_fee"}`, `{"kind": "normalised", "path": "amount_off", "returned": {"usd": 500}}`,
	} {
		if _, err := decodeEvidence(withObservation(`{"condition": {"account_country": "US", "variant": ""}, "value": ` + m + `}`)); err != nil {
			t.Errorf("%s: %v", m, err)
		}
	}
	for _, tc := range []struct{ name, observation, want string }{
		{"an unknown matcher kind", `{"condition": {"variant": ""}, "value": {"kind": "derived"}}`, `observation "derived"`},
		{"an unknown matcher member", `{"condition": {"variant": ""}, "value": {"kind": "value", "value": 1, "unit": "cents"}}`, `rules[0].observations[0].value.unit is a member the format does not define`},
		{"an unknown condition", `{"condition": {"variant": "", "customer.currency": "eur"}, "value": {"kind": "absent"}}`, `rules[0].observations[0].condition.customer.currency is a member the format does not define`},
	} {
		if _, err := decodeEvidence(withObservation(tc.observation)); err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Errorf("%s: %v, want %q", tc.name, err, tc.want)
		}
	}
}

// proven adds a proven evidence leaf for the key, with its facts.
func proven(p *Pinned, key string, l EvidenceLeaf) {
	side, rest, _ := strings.Cut(key, "/")
	owner, path, _ := strings.Cut(rest, "/")
	l.Key = Key{Object: "gadget", Side: Side(side), Path: path}
	if l.Side == SideRequest {
		l.Operation = owner
	}
	l.Status, l.Targets, l.Tests = "proven", []string{"fake", "stripe"}, []string{"TestGadgetAttributes/leaves/" + key}
	p.Evidence.Leaves = append(p.Evidence.Leaves, l)
}

// color is the fixture's trimmed `color` as the gate exposes it with the given evidence facts.
func color(t *testing.T, create, update EvidenceLeaf, effects ...SideEffect) fields.Field {
	t.Helper()
	res := trimmed()
	p := pinned(t, res, map[string]string{
		"request/PostGadgets/color":   ClassVerified,
		"request/PostGadgetsId/color": ClassVerified,
		"response/gadget/color":       ClassVerified,
	})
	proven(p, "request/PostGadgets/color", create)
	proven(p, "request/PostGadgetsId/color", update)
	proven(p, "response/gadget/color", EvidenceLeaf{})
	p.Evidence.SideEffects = effects
	f, ok := fields.Find(cover(t, res, p).table, "color")
	if !ok {
		t.Fatal("the gate did not expose color")
	}
	return f
}

// E2: a request field the gate exposes that the response returns is Optional and Computed
// unless the evidence shows Stripe leaves it null on a minimal create and an update unsets it
// with `""` on /v1.
func TestGateMakesAnExposedArgumentComputedUnlessTheEvidenceShowsItClears(t *testing.T) {
	none := &EvidenceDefault{Kind: "null"}
	clears := &EvidenceUnset{By: "empty_string", Result: "null", Matcher: json.RawMessage(`{"kind":"null"}`)}
	for _, tc := range []struct {
		name     string
		create   EvidenceLeaf
		update   EvidenceLeaf
		computed bool
	}{
		{"null and unset", EvidenceLeaf{Default: none}, EvidenceLeaf{Unset: clears}, false},
		{"absent and unset to absent", EvidenceLeaf{Default: &EvidenceDefault{Kind: "absent"}}, EvidenceLeaf{Unset: &EvidenceUnset{By: "empty_string", Result: "absent"}}, false},
		{"a default value", EvidenceLeaf{Default: &EvidenceDefault{Kind: "value", Value: json.RawMessage(`"red"`)}}, EvidenceLeaf{Unset: clears}, true},
		{"a default value in a variant", EvidenceLeaf{Default: &EvidenceDefault{Kind: "null", Variants: []DefaultVariant{{Variant: "v", Default: EvidenceDefault{Kind: "value", Value: json.RawMessage(`"red"`)}}}}}, EvidenceLeaf{Unset: clears}, true},
		{"no default recorded", EvidenceLeaf{}, EvidenceLeaf{Unset: clears}, true},
		{"an unset Stripe refuses", EvidenceLeaf{Default: none}, EvidenceLeaf{Unset: &EvidenceUnset{By: "empty_string", Result: "refused"}}, true},
		{"an unset Stripe ignores", EvidenceLeaf{Default: none}, EvidenceLeaf{Unset: &EvidenceUnset{By: "empty_string", Result: "unchanged"}}, true},
		{"an unset by null on /v1", EvidenceLeaf{Default: none}, EvidenceLeaf{Unset: &EvidenceUnset{By: "null", Result: "null"}}, true},
		{"no unset recorded", EvidenceLeaf{Default: none}, EvidenceLeaf{}, true},
	} {
		f := color(t, tc.create, tc.update)
		if !f.Create || !f.Update || !f.Response {
			t.Fatalf("%s: color is %+v", tc.name, f)
		}
		if f.ServerDefaulted != tc.computed {
			t.Errorf("%s: ServerDefaulted %v, want %v", tc.name, f.ServerDefaulted, tc.computed)
		}
	}
}

// E5: a field the evidence verifies is stable when the suite proved an update of the object and
// no update changed it as a side effect.
func TestGateMarksAFieldStableUnlessAnUpdateChangedIt(t *testing.T) {
	update := EvidenceLeaf{On: []string{"update"}}
	if f := color(t, EvidenceLeaf{}, update); !f.Stable {
		t.Error("no side effect: color is not stable")
	}
	effect := func(path string) SideEffect {
		return SideEffect{Object: "gadget", Operation: "PostGadgetsId", Probe: "update/1",
			Changed: []ChangedPath{{Path: path, Before: json.RawMessage(`{"kind":"null"}`), After: json.RawMessage(`{"kind":"value","value":"red"}`)}}}
	}
	if f := color(t, EvidenceLeaf{}, update, effect("color")); f.Stable {
		t.Error("an update changed color, which is still stable")
	}
	if f := color(t, EvidenceLeaf{}, update, effect("name")); !f.Stable {
		t.Error("an update changed another field, and color is not stable")
	}

	// Without a proven update leaf, no update was observed.
	res := trimmed()
	p := pinned(t, res, map[string]string{"request/PostGadgets/color": ClassVerified, "response/gadget/color": ClassVerified})
	proven(p, "request/PostGadgets/color", EvidenceLeaf{})
	proven(p, "response/gadget/color", EvidenceLeaf{})
	if f, _ := fields.Find(cover(t, res, p).table, "color"); f.Stable {
		t.Error("no update observed, and color is stable")
	}
}

// The suite's fixture evidence, with every leaf it proves on both targets verified, builds the
// coupon and v2 event destination tables of the pinned spec. It changes today's fields only by
// E5, on the optional arguments an update takes, and exposes the one leaf today's table trims.
func TestSuiteFixtureBuildsTheRealTables(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("..", "..", "spec", "openapi.json"))
	if err != nil {
		t.Fatal(err)
	}
	spec, err := ParseSpec(data)
	if err != nil {
		t.Fatal(err)
	}
	e, err := decodeEvidence(suiteFixture(t))
	if err != nil {
		t.Fatal(err)
	}
	p := &Pinned{Attributes: &Attributes{}, Evidence: e}
	for _, l := range e.Leaves {
		if l.Status == "proven" && slices.Contains(l.Targets, "fake") {
			p.Attributes.Leaves = append(p.Attributes.Leaves, AttributeLeaf{Key: l.Key, Class: ClassVerified})
		}
	}
	for pkg, want := range map[string][]string{
		"coupon":               {"~ metadata stable", "~ name stable"},
		"event_destination_v2": {"~ description stable", "~ metadata stable"},
	} {
		i := slices.IndexFunc(Resources, func(r Resource) bool { return r.Package == pkg })
		tc, err := coverTable(spec, Resources[i], p)
		if err != nil {
			t.Fatalf("%s: %v", pkg, err)
		}
		today, err := Build(spec, Resources[i])
		if err != nil {
			t.Fatal(err)
		}
		if got := tableDiff(today, tc.table, ""); !slices.Equal(got, want) {
			t.Errorf("%s: %q, want %q", pkg, got, want)
		}
	}
}

// tableDiff lists the fields b adds to a, and those whose only difference is Stable. What the
// docs line says (Default, Copy, PagedBy) is not compared.
func tableDiff(a, b []fields.Field, prefix string) []string {
	var out []string
	for _, f := range b {
		f.Default, f.Copy, f.PagedBy = nil, "", ""
		g, ok := fields.Find(a, f.Name)
		g.Default, g.Copy, g.PagedBy = nil, "", ""
		switch {
		case !ok:
			out = append(out, "+ "+prefix+f.AttrName())
			continue
		case f.Stable && !g.Stable:
			g.Stable = true
			out = append(out, "~ "+prefix+f.AttrName()+" stable")
		}
		kids, todayKids := f.Fields, g.Fields
		f.Fields, g.Fields = nil, nil
		if !reflect.DeepEqual(f, g) {
			out = append(out, fmt.Sprintf("~ %s%s: %+v, today %+v", prefix, f.AttrName(), f, g))
		}
		out = append(out, tableDiff(todayKids, kids, prefix+f.Name+".")...)
	}
	return out
}

// The suite's first real run (testdata/evidence/run-40926522642.json), with every leaf it proves
// on both targets verified, gives the real tables their default statements: a string, an empty
// string, a list and a map Stripe sets, and none. A verified optional argument the minimal
// creates set, or whose hash they leave out, has no statement, and the coverage report names it.
func TestSuiteRunGivesTheRealTablesTheirDefaults(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("..", "..", "spec", "openapi.json"))
	if err != nil {
		t.Fatal(err)
	}
	spec, err := ParseSpec(data)
	if err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(filepath.Join("testdata", "evidence", "run-40926522642.json"))
	if err != nil {
		t.Fatal(err)
	}
	e, err := decodeEvidence(raw)
	if err != nil {
		t.Fatal(err)
	}
	p := &Pinned{Attributes: &Attributes{}, Evidence: e}
	for _, l := range e.Leaves {
		if l.Status == "proven" && slices.Contains(l.Targets, "fake") {
			p.Attributes.Leaves = append(p.Attributes.Leaves, AttributeLeaf{Key: l.Key, Class: ClassVerified})
		}
	}
	for pkg, want := range map[string]map[string]string{
		"coupon": {
			"duration":    "Default (Stripe): `once`.",
			"metadata":    "Default (Stripe): `{}`.",
			"redeem_by":   "Default: none.",
			"percent_off": "",
			"id":          "",
		},
		"event_destination_v2": {
			"description":          "Default (Stripe): `\"\"`.",
			"events_from":          "Default (Stripe): `[\"@self\"]`.",
			"snapshot_api_version": "Default: none.",
		},
		"terminal_location": {
			"display_name_kana": "Default: none.",
			"address_kana.city": "",
		},
	} {
		i := slices.IndexFunc(Resources, func(r Resource) bool { return r.Package == pkg })
		tc, err := coverTable(spec, Resources[i], p)
		if err != nil {
			t.Fatalf("%s: %v", pkg, err)
		}
		for path, statement := range want {
			f := fieldAt(tc.table, path)
			verified := slices.ContainsFunc(tc.records, func(r Record) bool {
				return r.Side == SideRequest && r.Path == path && r.State == StateExposed && r.Class == ClassVerified
			})
			if f == nil || !verified {
				t.Errorf("%s %s: %+v, want a verified field", pkg, path, f)
				continue
			}
			got := ""
			if f.Default != nil {
				got = f.Default.Statement()
			}
			if got != statement {
				t.Errorf("%s %s: %q, want %q", pkg, path, got, statement)
			}
		}
		for _, r := range tc.records {
			if r.SpecDefault != "" {
				t.Errorf("%s %s: the spec's default %q disagrees with the run", pkg, r.Key, r.SpecDefault)
			}
		}
	}
}

// A rule default carries the index of its rule (the format's schema requires `rule_ref` with kind
// `rule`), in the base create and in every variant: the reader refuses one without it, or with
// an index past the rules, so the generator never compares or words a rule it does not have.
func TestEvidenceReaderRefusesARuleDefaultWithoutItsRule(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join("testdata", "evidence", "run-40926522642.json"))
	if err != nil {
		t.Fatal(err)
	}
	for name, set := range map[string]func(d map[string]any){
		"base without rule_ref":    func(d map[string]any) { d["kind"] = "rule"; delete(d, "value") },
		"variant without rule_ref": func(d map[string]any) { vd := variantDefault(d); vd["kind"] = "rule"; delete(vd, "value") },
		"base and variant without": func(d map[string]any) {
			d["kind"] = "rule"
			delete(d, "value")
			vd := variantDefault(d)
			vd["kind"] = "rule"
			delete(vd, "value")
		},
		"variant past the rules": func(d map[string]any) {
			vd := variantDefault(d)
			vd["kind"] = "rule"
			delete(vd, "value")
			vd["rule_ref"] = 0
		},
	} {
		var record map[string]any
		if err := json.Unmarshal(raw, &record); err != nil {
			t.Fatal(err)
		}
		for _, l := range record["leaves"].([]any) {
			leaf := l.(map[string]any)
			if leaf["object"] == "coupon" && leaf["operation"] == "PostCoupons" && leaf["path"] == "duration" {
				set(leaf["default"].(map[string]any))
			}
		}
		changed, err := json.Marshal(record)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := decodeEvidence(changed); err == nil || !strings.Contains(err.Error(), "request/PostCoupons/duration has a rule default with no rule") {
			t.Errorf("%s: err = %v", name, err)
		}
	}
}

// variantDefault is the default of a recorded default's first variant.
func variantDefault(d map[string]any) map[string]any {
	return d["variants"].([]any)[0].(map[string]any)["default"].(map[string]any)
}
