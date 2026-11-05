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

// A family's per-hash facts are read strictly, a hash's update from a closed set, and a matcher
// holds the days of a `time_within`.
func TestEvidenceReaderTakesTheFormsOfTheFourthPin(t *testing.T) {
	record := func(update string) []byte {
		return []byte(fmt.Sprintf(`{"format_version": 1, "families": [{"family": "v1-payments", "objects": ["charge"], "nested_hash_update": "siblings_cleared",
			"hashes": [{"object": "charge", "hash": "shipping.address", "update": %q}], "array_update": "replaced", "map_key_removal": "empty_string", "scalar_unset": "empty_string", "tests": []}],
			"rules": [{"object": "charge", "operation": "PostCharges", "path": "x", "rule": "a rule no leaf cites", "tests": [],
				"observations": [{"condition": {"account_country": "US", "variant": ""}, "value": {"kind": "time_within", "days": 1}}]}]}`, update))
	}
	e, err := decodeEvidence(record(HashSiblingsCleared))
	if err != nil {
		t.Fatal(err)
	}
	if h := e.Families[0].Hashes[0]; h.Hash != "shipping.address" || h.Update != HashSiblingsCleared {
		t.Errorf("the hash: %+v", h)
	}
	if o := e.Rules[0].Observations[0].Value; o.Days == nil || *o.Days != 1 {
		t.Errorf("the observation: %+v", o)
	}
	if _, err := decodeEvidence(record("sometimes")); err == nil || !strings.Contains(err.Error(), `hash update "sometimes"`) {
		t.Errorf("an unknown hash update: %v", err)
	}
}

// A hash's answer belongs to one object of its family and may be `refused`, and a proven request
// leaf may carry the values Stripe fails on every attempt; the reader refuses what the generator
// cannot use of either.
func TestEvidenceReaderTakesTheFormsOfTheSeventhPin(t *testing.T) {
	record := func(hashes, unreachable string) []byte {
		return []byte(`{"format_version": 1, "families": [{"family": "v1-payments", "objects": ["payment_link", "transfer"], "nested_hash_update": "mixed",
			"hashes": ` + hashes + `, "array_update": "replaced", "map_key_removal": "empty_string", "scalar_unset": "empty_string", "tests": []}],
			"leaves": [{"object": "transfer", "side": "request", "operation": "PostTransfers", "path": "source_type", "status": "proven", "tests": [],
				"enum_values": [{"accepted": true, "value": "card"}], "unreachable_values": ` + unreachable + `}]}`)
	}
	const refused = `[{"object": "payment_link", "hash": "after_completion", "update": "refused"}]`
	const fpx = `[{"value": "fpx", "reason": "Stripe answers 500 to every attempt"}]`
	e, err := decodeEvidence(record(refused, fpx))
	if err != nil {
		t.Fatal(err)
	}
	if h := e.Families[0].Hashes[0]; h.Object != "payment_link" || h.Hash != "after_completion" || h.Update != HashRefused {
		t.Errorf("the hash: %+v", h)
	}
	if u := e.Leaves[0].Unreachable; len(u) != 1 || u[0].Value != "fpx" || u[0].Reason == "" {
		t.Errorf("the unreachable values: %+v", u)
	}
	for _, tc := range []struct{ name, hashes, unreachable, want string }{
		{"a hash of another family's object", `[{"object": "charge", "hash": "shipping", "update": "siblings_kept"}]`, fpx, "not one of its objects"},
		{"a hash given twice", `[{"object": "payment_link", "hash": "x", "update": "refused"}, {"object": "payment_link", "hash": "x", "update": "siblings_kept"}]`, fpx, "twice"},
		{"a hash with no member", `[{"object": "payment_link", "hash": "x", "update": "refused", "since": "now"}]`, fpx, "is a member the format does not define"},
		{"an unreachable value with no reason", refused, `[{"value": "fpx", "reason": " "}]`, "has no reason"},
		{"an unreachable value listed twice", refused, `[{"value": "fpx", "reason": "a"}, {"value": "fpx", "reason": "b"}]`, "listed twice"},
		{"an unreachable value the suite sent", refused, `[{"value": "card", "reason": "a"}]`, "among the values the suite sent"},
	} {
		if _, err := decodeEvidence(record(tc.hashes, tc.unreachable)); err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Errorf("%s: %v, want %q", tc.name, err, tc.want)
		}
	}
	onResponse := bytes.Replace(record(refused, fpx), []byte(`"side": "request"`), []byte(`"side": "response"`), 1)
	if _, err := decodeEvidence(onResponse); err == nil || !strings.Contains(err.Error(), "not a proven request leaf") {
		t.Errorf("unreachable values on a response leaf: %v", err)
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
		{"an unknown matcher kind", `{"condition": {"variant": ""}, "value": {"kind": "derived"}}`, `observation: kind "derived"`},
		{"an unknown matcher member", `{"condition": {"variant": ""}, "value": {"kind": "value", "value": 1, "unit": "cents"}}`, `rules[0].observations[0].value.unit is a member the format does not define`},
		{"an unknown condition", `{"condition": {"variant": "", "customer.currency": "eur"}, "value": {"kind": "absent"}}`, `rules[0].observations[0].condition.customer.currency is a member the format does not define`},
	} {
		if _, err := decodeEvidence(withObservation(tc.observation)); err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Errorf("%s: %v, want %q", tc.name, err, tc.want)
		}
	}
}

// A rule whose observations are Stripe's answers to probes, each the request it sent and, on an
// update, what the object was created with, reads with its status and its read-back or refusal; the
// reader refuses an answer it cannot weigh, and a default that names such a rule.
func TestEvidenceReaderReadsAnswers(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("testdata", "evidence", "run-40926522642.json"))
	if err != nil {
		t.Fatal(err)
	}
	const (
		took    = `{"condition": {"account_country": "US", "probe": "isolated/beside", "params": {"name": "A", "suffix": "B"}, "created_with": {}}, "value": {"status": 200, "read": {"kind": "sent", "path": "name"}}}`
		refused = `{"condition": {"account_country": "US", "probe": "isolated/alone", "params": {"name": "A"}}, "value": {"status": 400, "type": "invalid_request_error", "code": "c", "param": "name", "message": "No."}}`
	)
	withObservations := func(observations ...string) []byte {
		rule := `"rules": [{"object": "coupon", "operation": "PostCoupons", "path": "name", "rule": "Stripe's answer turns on the request", "observations": [` + strings.Join(observations, ", ") + `], "tests": ["a"]}],`
		return bytes.Replace(data, []byte(`"rules": [],`), []byte(rule), 1)
	}
	e, err := decodeEvidence(withObservations(took, refused))
	if err != nil {
		t.Fatal(err)
	}
	if r := e.Rules[0]; !r.answer() || r.Observations[0].Value.Read == nil || r.Observations[1].Value.Message != "No." || r.Observations[1].Condition.Probe != "isolated/alone" {
		t.Errorf("the answers: %+v", r)
	}
	for _, tc := range []struct{ name, observation, want string }{
		{"no status", `{"condition": {"probe": "p", "params": {}}, "value": {"message": "No."}}`, "has status 0"},
		{"a refusal with no message", `{"condition": {"probe": "p", "params": {}}, "value": {"status": 400}}`, "refuses with no message"},
		{"an acceptance with a message", `{"condition": {"probe": "p", "params": {}}, "value": {"status": 200, "message": "No."}}`, "accepts and refuses"},
		{"no params", `{"condition": {"probe": "p"}, "value": {"status": 200}}`, "holds no params"},
		{"an unknown read-back", `{"condition": {"probe": "p", "params": {}}, "value": {"status": 200, "read": {"kind": "derived"}}}`, `reads back kind "derived"`},
		{"a default's matcher", `{"condition": {"probe": "p", "params": {}}, "value": {"status": 200, "kind": "value", "value": 1}}`, "holds a default's members"},
		{"a mix", `{"condition": {"variant": ""}, "value": {"kind": "absent"}}`, "mixes answers"},
		{"an unknown member", `{"condition": {"probe": "p", "params": {}}, "value": {"status": 200, "headers": {}}}`, "value.headers is a member the format does not define"},
	} {
		if _, err := decodeEvidence(withObservations(took, tc.observation)); err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Errorf("%s: %v, want %q", tc.name, err, tc.want)
		}
	}
	if _, err := decodeEvidence(withObservations(`{"condition": {"variant": ""}, "value": {"kind": "absent", "status": 200}}`)); err == nil || !strings.Contains(err.Error(), "a default's observation holds an answer's members") {
		t.Errorf("a default's observation with a status: %v", err)
	}
	pointed := bytes.Replace(withObservations(took, refused), []byte(`"leaves": [`), []byte(`"leaves": [{"object": "coupon", "side": "request", "operation": "PostCoupons", "path": "name", "status": "proven", "tests": ["a"], "default": {"kind": "rule", "rule_ref": 0}},`), 1)
	if _, err := decodeEvidence(pointed); err == nil || !strings.Contains(err.Error(), "answers probes and words no default") {
		t.Errorf("a default naming an answer rule: %v", err)
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

// The ninth pin's forms: a map's update on an update leaf, a discount's list, response path and
// holder, a Beside rule with a leaf's `returned.rule_ref` naming it, and what a side effect carried
// beside its triggers. The reader refuses what the generator cannot use of each.
func TestEvidenceReaderTakesTheFormsOfTheNinthPin(t *testing.T) {
	const (
		mapLeaf  = `{"object": "coupon", "side": "request", "operation": "PostCouponsCoupon", "path": "metadata", "status": "proven", "on": ["update"], "tests": [], "map": {"update": "merged", "empty_value": {"by": "empty_string", "result": "removed"}}}`
		discount = `{"object": "quote", "operation": "PostQuotes", "path": "line_items.discounts", "form": "coupon", "response_path": "line_items.data.discounts.discount", "returned": "discount_object", "expanded": "discount", "relation": "coupon", "tests": []}`
		clock    = `{"object": "test_helpers.test_clock", "side": "request", "operation": "PostTestHelpersTestClocks", "path": "frozen_time", "status": "proven", "tests": [], "returned": {"form": "as_sent", "response_path": "frozen_time", "rule_ref": 0}}`
		beside   = `{"object": "test_helpers.test_clock", "operation": "PostTestHelpersTestClocks", "path": "frozen_time", "beside": "customer", "rule": "the clock's ` + "`created`" + `, on a create with a ` + "`customer`" + `", "tests": [], "observations": [
			{"condition": {"account_country": "US", "probe": "minimal/customer", "params": {"customer": {"$gen": "fresh"}, "frozen_time": 1}}, "value": {"kind": "from", "path": "created"}},
			{"condition": {"account_country": "US", "probe": "minimal", "params": {"frozen_time": 1}}, "value": {"kind": "sent", "path": "frozen_time"}}]}`
		effect = `{"object": "quote", "operation": "PostQuotesQuote", "probe": "update/1", "triggers": ["line_items.quantity"], "carried": {"line_items.id": ["li_1"]}, "changed": [{"path": "amount_total", "before": {"kind": "value", "value": 1}, "after": {"kind": "value", "value": 2}}], "tests": []}`
	)
	record := func(leaves, discounts, rules, effects string) []byte {
		return []byte(`{"format_version": 1, "leaves": [` + leaves + `], "discounts": [` + discounts + `], "rules": [` + rules + `], "side_effects": [` + effects + `]}`)
	}
	e, err := decodeEvidence(record(mapLeaf+", "+clock, discount, beside, effect))
	if err != nil {
		t.Fatal(err)
	}
	if m := e.Leaves[0].Map; m == nil || m.Update != MapMerged || m.EmptyValue == nil || m.EmptyValue.Result != MapRemoved {
		t.Errorf("the map: %+v", m)
	}
	if d := e.Discounts[0]; d.Path != "line_items.discounts" || d.ResponsePath != "line_items.data.discounts.discount" || d.Returned != DiscountObject {
		t.Errorf("the discount: %+v", d)
	}
	if r := e.Rules[0]; r.Beside != "customer" || r.answer() {
		t.Errorf("the beside rule: %+v", r)
	}
	if ref := e.Leaves[1].returnedRule(); ref == nil || *ref != 0 {
		t.Errorf("the returned rule: %v", ref)
	}
	if c := e.SideEffects[0].Carried; len(c) != 1 || c["line_items.id"] == nil {
		t.Errorf("the carried leaves: %v", c)
	}
	replace := func(s, old, new string) string {
		t.Helper()
		if !strings.Contains(s, old) {
			t.Fatalf("no %s in %s", old, s)
		}
		return strings.Replace(s, old, new, 1)
	}
	for _, tc := range []struct{ name, leaves, discounts, rules, effects, want string }{
		{"a map on a create leaf", replace(mapLeaf, `["update"]`, `["create"]`), discount, beside, effect, "not an update's"},
		{"an unknown map update", replace(mapLeaf, `"merged"`, `"patched"`), discount, beside, effect, `map update "patched"`},
		{"an unknown empty value", replace(mapLeaf, `"removed"`, `"nulled"`), discount, beside, effect, `map empty value result "nulled"`},
		{"an unknown discount return", mapLeaf, replace(discount, `"discount_object"`, `"discount_ref"`), beside, effect, `discounts returned "discount_ref"`},
		{"a relation of another form", mapLeaf, replace(discount, `"relation": "coupon"`, `"relation": "promotion_code"`), beside, effect, "relation"},
		{"a discount with no list", mapLeaf, replace(discount, `"line_items.discounts"`, `"line_items"`), beside, effect, "names no list of discounts"},
		{"a beside rule always beside", mapLeaf + ", " + clock, discount, replace(beside, `"params": {"frozen_time": 1}`, `"params": {"customer": 1, "frozen_time": 1}`), effect, "both with and without"},
		{"a returned rule that is no beside rule", mapLeaf + ", " + clock, discount, replace(beside, `"beside": "customer", `, ``), effect, "not a beside rule of the leaf"},
		{"a carried trigger", mapLeaf, discount, beside, replace(effect, `"line_items.id"`, `"line_items.quantity"`), "one of its triggers"},
	} {
		if _, err := decodeEvidence(record(tc.leaves, tc.discounts, tc.rules, tc.effects)); err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Errorf("%s: %v, want %q", tc.name, err, tc.want)
		}
	}
}

// A map's update becomes the table's MapUpdate: merged or replaced, and whether a key sent empty
// stays, which a merged map whose record does not say is taken to do. A record that sends the
// empty key otherwise than the engine sends an unset on the table's API fails.
func TestMapUpdateFollowsTheRecord(t *testing.T) {
	leaf := func(update, by, result string) *EvidenceLeaf {
		m := &EvidenceMap{Update: update}
		if by != "" {
			m.EmptyValue = &struct {
				By     string `json:"by"`
				Result string `json:"result"`
			}{by, result}
		}
		return &EvidenceLeaf{Map: m}
	}
	for name, tc := range map[string]struct {
		leaf *EvidenceLeaf
		want *fields.MapUpdate
	}{
		"merged, removed":        {leaf("merged", "empty_string", "removed"), &fields.MapUpdate{}},
		"merged, stored":         {leaf("merged", "empty_string", "stored"), &fields.MapUpdate{EmptyKept: true}},
		"replaced, stored":       {leaf("replaced", "empty_string", "stored"), &fields.MapUpdate{Replaced: true, EmptyKept: true}},
		"merged, no empty value": {leaf("merged", "", ""), &fields.MapUpdate{EmptyKept: true}},
		"no update":              {leaf("", "empty_string", "removed"), nil},
		"no map":                 {&EvidenceLeaf{}, nil},
	} {
		got, err := mapUpdate(Key{Path: "metadata"}, tc.leaf, "empty_string")
		if err != nil || !reflect.DeepEqual(got, tc.want) {
			t.Errorf("%s: %+v, %v, want %+v", name, got, err, tc.want)
		}
	}
	if _, err := mapUpdate(Key{Path: "metadata"}, leaf("merged", "empty_string", "removed"), "null"); err == nil || !strings.Contains(err.Error(), "sends an unset on this API as null") {
		t.Errorf("a /v2 map the record empties with a string: %v", err)
	}
}

// A default that holds only beside another argument names it: a record's Beside rule that does
// not has no statement until a declaration words it, and a declaration's wording that leaves the
// argument out fails.
func TestBesideDefaultsNameTheirArgument(t *testing.T) {
	rule := 0
	key := Key{Object: "gadget", Side: SideRequest, Operation: "PostGadgets", Path: "color"}
	p := &Pinned{Evidence: &Evidence{
		Leaves: []EvidenceLeaf{{Key: key, Status: "proven", Default: &EvidenceDefault{Kind: "rule", RuleRef: &rule}}},
		Rules:  []EvidenceRule{{Path: "color", Beside: "owner.email", Rule: "the owner's colour"}},
	}}
	if d := p.leafDefault(key); d == nil || d.Kind != fields.DefaultVaries {
		t.Errorf("a beside rule that does not name its argument: %+v", d)
	}
	p.Evidence.Rules[0].Rule, p.leaves = "the colour of `owner.email`'s account", nil
	if d := p.leafDefault(key); d == nil || d.Kind != fields.DefaultRule {
		t.Errorf("a beside rule that names its argument: %+v", d)
	}
	tc := &tableCoverage{
		leaves:  []Leaf{{Key: key, At: "color"}},
		records: []Record{{Table: "gadget", Key: key, State: StateExposed, Class: ClassVerified, Default: &DefaultRecord{Source: "evidence", Varies: true}}},
		table:   []fields.Field{{Name: "color", Default: &fields.Default{Kind: fields.DefaultVaries}}},
	}
	if err := tc.applyDeclaredDefaults(map[string]string{"color": "blue"}, p); err == nil || !strings.Contains(err.Error(), "only beside `owner.email`") {
		t.Errorf("a declared default that leaves the argument out: %v", err)
	}
	if err := tc.applyDeclaredDefaults(map[string]string{"color": "blue, or the colour `owner.email` names"}, p); err != nil {
		t.Errorf("a declared default that names the argument: %v", err)
	}
}

// A table may not expose a leaf whose returned form holds only without another leaf beside that
// leaf; nor keep an accepted-only discount form the record does not show returned where the
// engine reads it back.
func TestBesideReturnsAndKeptDiscountsFollowTheRecord(t *testing.T) {
	rule := json.RawMessage(`{"form": "as_sent", "response_path": "frozen_time", "rule_ref": 0}`)
	frozen := Key{Object: "test_helpers.test_clock", Side: SideRequest, Operation: "PostTestHelpersTestClocks", Path: "frozen_time"}
	customer := frozen
	customer.Path = "customer"
	p := &Pinned{Evidence: &Evidence{
		Leaves: []EvidenceLeaf{{Key: frozen, Status: "proven", Returned: rule}},
		Rules:  []EvidenceRule{{Object: frozen.Object, Operation: frozen.Operation, Path: "frozen_time", Beside: "customer", Rule: "the clock's `created`"}},
	}}
	leaves := []Leaf{{Key: frozen}, {Key: customer}}
	exposedBoth := []Record{{State: StateExposed}, {State: StateExposed}}
	if err := checkBesideReturns("test_helpers_test_clock", leaves, exposedBoth, p); err == nil || !strings.Contains(err.Error(), "reads back as sent only without `customer`") {
		t.Errorf("both exposed: %v", err)
	}
	if err := checkBesideReturns("test_helpers_test_clock", leaves, []Record{{State: StateExposed}, {State: StateExcluded}}, p); err != nil {
		t.Errorf("the beside leaf left out: %v", err)
	}

	line := Key{Object: "quote", Side: SideRequest, Operation: "PostQuotes", Path: "line_items.discounts.coupon"}
	top := line
	top.Path = "discounts.coupon"
	kept := []Record{{State: StateExposed, Class: ClassAcceptedOnly}, {State: StateExposed, Class: ClassAcceptedOnly}}
	p.Evidence.Discounts = []Discount{
		{Object: "quote", Operation: "PostQuotes", Path: "discounts", Form: "coupon", ResponsePath: "discounts", Returned: DiscountID},
		{Object: "quote", Operation: "PostQuotes", Path: "line_items.discounts", Form: "coupon", ResponsePath: "line_items.data.discounts.discount", Returned: DiscountObject},
	}
	if err := checkKeptDiscounts(Resource{Package: "quote"}, []Leaf{{Key: line}, {Key: top}}, kept, p); err != nil {
		t.Errorf("forms read back as the record shows: %v", err)
	}
	p.Evidence.Discounts[1].HeldBy = "invoiceitem"
	if err := checkKeptDiscounts(Resource{Package: "quote"}, []Leaf{{Key: line}, {Key: top}}, kept, p); err == nil || !strings.Contains(err.Error(), "line_items.discounts.coupon") {
		t.Errorf("a line's discount another object holds: %v", err)
	}
	p.Evidence.Discounts = p.Evidence.Discounts[:1]
	p.Evidence.Discounts[0].Returned = "discount_hash"
	if err := checkKeptDiscounts(Resource{Package: "quote"}, []Leaf{{Key: top}}, kept[:1], p); err == nil {
		t.Error("a discount returned as a hash is kept")
	}
}
