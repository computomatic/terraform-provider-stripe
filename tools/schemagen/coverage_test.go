package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/computomatic/terraform-provider-stripe/internal/engine"
	"github.com/computomatic/terraform-provider-stripe/internal/fields"
	"github.com/computomatic/terraform-provider-stripe/internal/resources/payment_intent"
	"github.com/computomatic/terraform-provider-stripe/internal/resources/setup_intent"
)

const coverageSpecFile = "coverage_spec.json"

func coverageSpec(t *testing.T) *Spec {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("testdata", coverageSpecFile))
	if err != nil {
		t.Fatal(err)
	}
	spec, err := ParseSpec(data)
	if err != nil {
		t.Fatal(err)
	}
	return spec
}

// gadget is the fixture table: `confirm`, which `status` does, and a `payment` union and a
// `parent` recursion, which the generator cannot hold yet, are omitted.
func gadget() Resource {
	return Resource{Package: "gadget", CreatePath: "/v1/gadgets", UpdatePath: "/v1/gadgets/{id}", StatusConfirms: true, Omit: []string{"confirm", "payment", "parent"},
		PagedLists: map[string]string{"parts": "stripe_gadget"}}
}

func cover(t *testing.T, res Resource, ev *Pinned) *tableCoverage {
	t.Helper()
	tc, err := coverTable(coverageSpec(t), res, ev)
	if err != nil {
		t.Fatal(err)
	}
	return tc
}

func byKey(tc *tableCoverage) map[string]Record {
	out := map[string]Record{}
	for _, r := range tc.records {
		out[r.Key.String()] = r
	}
	return out
}

// The walk keys leaves as the suite and the fake do, and gives every leaf a state: the rules
// X1 to X5 by name, a nested object the request writes as a hash walked into its leaves, one
// written as an ID kept as one embedded-copy leaf, a list envelope as one leaf, unions by
// branch and the response's own `object` left out.
func TestCoverageWalkKeysRulesAndStates(t *testing.T) {
	type want struct {
		state    State
		rule     string
		shape    string
		required bool
	}
	got := byKey(cover(t, gadget(), nil))
	expect := map[string]want{
		"request/PostGadgets/name":                       {StateExposed, "", "string", true},
		"request/PostGadgets/color":                      {StateExposed, "", "string", false},
		"request/PostGadgets/confirm":                    {StateExcluded, RuleConfirm, "boolean", false},
		"request/PostGadgets/expand":                     {StateExcluded, RuleControl, "list of string", false},
		"request/PostGadgets/owner.email":                {StateExposed, "", "string", false},
		"request/PostGadgets/price":                      {StateExposed, "", "string", false},
		"request/PostGadgets/source@string":              {StateExposed, "", "string", false},
		"request/PostGadgets/source.number@object":       {StateAwaiting, "", "string", true},
		"request/PostGadgets/source.cvc@object":          {StateAwaiting, "", "string", false},
		"request/PostGadgets/settings.mode":              {StateExposed, "", "string", true},
		"request/PostGadgets/tags":                       {StateExposed, "", "list of string", false},
		"request/PostGadgets/options.amount":             {StateExposed, "", "integer", false},
		"request/PostGadgetsId/settings.limit":           {StateExposed, "", "integer", false},
		"request/PostGadgetsId/expand":                   {StateExcluded, RuleControl, "list of string", false},
		"response/gadget/id":                             {StateExposed, "", "string", false},
		"response/gadget/owner.verified":                 {StateExposed, "", "boolean", false},
		"response/gadget/price":                          {StateExposed, "", "embedded", false},
		"response/gadget/customer":                       {StateExposed, "", "reference", false},
		"response/gadget/parts":                          {StateExposed, "", "paged_list", false},
		"response/gadget/extras":                         {StateExposed, RulePagedList, "paged_list", false},
		"response/gadget/marker":                         {StateExcluded, RuleTypeMarker, "empty_hash", false},
		"response/gadget/parent":                         {StateAwaiting, "", "recursion", false},
		"response/gadget/payment.brand@card":             {StateAwaiting, "", "string", false},
		"response/gadget/payment.bank_name@bank_account": {StateAwaiting, "", "string", false},
		"response/gadget/options.amount":                 {StateExposed, "", "integer", false},
	}
	for k, w := range expect {
		r, ok := got[k]
		if !ok {
			t.Errorf("%s: not walked", k)
			continue
		}
		if r.State != w.state || r.Rule != w.rule || r.Shape != w.shape || r.Required != w.required {
			t.Errorf("%s: %s %q %s required=%v, want %s %q %s required=%v", k, r.State, r.Rule, r.Shape, r.Required, w.state, w.rule, w.shape, w.required)
		}
	}
	for _, k := range []string{"response/gadget/object", "response/gadget/price.unit_amount", "response/gadget/parts.has_more", "request/PostGadgets/source"} {
		if _, ok := got[k]; ok {
			t.Errorf("%s: walked, want it left out or held by a leaf above it", k)
		}
	}
	if len(got) != 41 {
		t.Errorf("%d leaves, want 41", len(got))
	}
}

// pinned is in-memory evidence that lists every leaf of the table as unproven, then the
// classes given.
func pinned(t *testing.T, res Resource, set map[string]string) *Pinned {
	t.Helper()
	return pinnedIn(t, coverageSpec(t), res, set)
}

// pinnedIn is pinned for a table of another fixture spec.
func pinnedIn(t *testing.T, spec *Spec, res Resource, set map[string]string) *Pinned {
	t.Helper()
	_, leaves, err := walkTable(spec, res)
	if err != nil {
		t.Fatal(err)
	}
	p := &Pinned{Attributes: &Attributes{}, Evidence: &Evidence{FormatVersion: 1}}
	seen := map[string]bool{}
	for _, l := range leaves {
		class := ClassUnproven
		if c, ok := set[l.Key.String()]; ok {
			class = c
			seen[l.Key.String()] = true
		}
		p.Attributes.Leaves = append(p.Attributes.Leaves, AttributeLeaf{Key: l.Key, Class: class})
	}
	for k := range set {
		if !seen[k] {
			t.Fatalf("%s is not a leaf of the fixture", k)
		}
	}
	return p
}

// trimmed is the fixture with today's trims on the leaves the gate tests verify.
func trimmed() Resource {
	res := gadget()
	res.Omit = append(res.Omit, "color")
	res.ReadOnly = []string{"settings"}
	res.NoResponse = []string{"owner"}
	return res
}

// With nothing verified, the gate changes no field table.
func TestGateWithoutVerifiedLeavesChangesNoTable(t *testing.T) {
	res := trimmed()
	want, err := Build(coverageSpec(t), res)
	if err != nil {
		t.Fatal(err)
	}
	if got := cover(t, res, pinned(t, res, nil)).table; !reflect.DeepEqual(got, want) {
		t.Error("the gate changed the table with no verified leaf")
	}
}

// A leaf `verified` in attributes.json is exposed on its own side through Omit, ReadOnly and
// NoResponse, and its trimmed siblings stay trimmed.
func TestGateExposesAVerifiedLeaf(t *testing.T) {
	res := trimmed()
	tc := cover(t, res, pinned(t, res, map[string]string{
		"request/PostGadgets/color":            ClassVerified,
		"response/gadget/color":                ClassGapStripeOnly,
		"request/PostGadgetsId/settings.limit": ClassVerified,
		"response/gadget/owner.verified":       ClassVerified,
	}))
	h := index(tc.table).request
	check := func(path string, want held) {
		t.Helper()
		got := h[path]
		got.nested, got.shape = false, 0
		if got != want {
			t.Errorf("%s: %+v, want %+v", path, got, want)
		}
	}
	check("color", held{create: true})
	check("settings.limit", held{update: true, response: true})
	check("settings.mode", held{response: true})
	check("owner.verified", held{response: true})
	check("owner.email", held{create: true})
	got := byKey(tc)
	for k, s := range map[string]State{
		"request/PostGadgets/color":            StateExposed,
		"response/gadget/color":                StateAwaiting,
		"request/PostGadgetsId/color":          StateAwaiting,
		"request/PostGadgetsId/settings.limit": StateExposed,
		"request/PostGadgets/settings.limit":   StateAwaiting,
		"response/gadget/owner.verified":       StateExposed,
		"response/gadget/owner.email":          StateAwaiting,
	} {
		if got[k].State != s {
			t.Errorf("%s: %s, want %s", k, got[k].State, s)
		}
	}
}

// A leaf in any class other than `verified` is not exposed.
func TestGateExposesNoOtherClass(t *testing.T) {
	res := trimmed()
	for _, class := range classes {
		if class == ClassVerified {
			continue
		}
		tc := cover(t, res, pinned(t, res, map[string]string{"request/PostGadgets/color": class}))
		if _, ok := index(tc.table).request["color"]; ok {
			t.Errorf("%s: color is in the table", class)
		}
		if r := byKey(tc)["request/PostGadgets/color"]; r.State != StateAwaiting || r.Class != class {
			t.Errorf("%s: %s with class %s, want awaiting proof", class, r.State, r.Class)
		}
	}
}

// An excluded leaf stays out even when verified.
func TestGateLeavesAnExcludedLeafOut(t *testing.T) {
	res := gadget()
	res.Exclude = map[string]string{"source.number": "Fixture: excluded."}
	tc := cover(t, res, pinned(t, res, map[string]string{"request/PostGadgets/source.number@object": ClassVerified}))
	if r := byKey(tc)["request/PostGadgets/source.number@object"]; r.State != StateExcluded || r.Rule != RuleRegister || r.Reason != "Fixture: excluded." {
		t.Errorf("source.number: %+v", r)
	}
}

// A verified leaf that the table cannot hold, such as the token form of a hash the table
// spreads into its top level, fails generation, naming it.
func TestGateFailsOnAVerifiedLeafTheTableCannotHold(t *testing.T) {
	res := gadget()
	res.CreateHash = "source"
	_, err := coverTable(coverageSpec(t), res, pinned(t, res, map[string]string{"request/PostGadgets/source@string": ClassVerified}))
	if err == nil || !strings.Contains(err.Error(), "request/PostGadgets/source@string is verified, but the generator cannot hold it") {
		t.Errorf("err = %v, want it to name the leaf", err)
	}
}

// A leaf with no state fails, naming it: with attributes.json pinned, one it does not list.
func TestALeafWithNoStateFails(t *testing.T) {
	err := checkStates([]Record{{Table: "widget", Key: Key{Object: "widget", Side: SideResponse, Path: "colour"}}})
	if err == nil || !strings.Contains(err.Error(), "widget response/widget/colour") {
		t.Errorf("err = %v, want it to name the leaf", err)
	}

	res := gadget()
	ev := pinned(t, res, nil)
	for i, l := range ev.Attributes.Leaves {
		if l.Key.String() == "request/PostGadgets/source.cvc@object" {
			ev.Attributes.Leaves = append(ev.Attributes.Leaves[:i], ev.Attributes.Leaves[i+1:]...)
			break
		}
	}
	err = checkStates(cover(t, res, ev).records)
	if err == nil || !strings.Contains(err.Error(), "request/PostGadgets/source.cvc@object") {
		t.Errorf("err = %v, want it to name the unlisted leaf", err)
	}
}

// A verified leaf of a table's object that the walk does not have fails, so that a
// difference of path language shows; the response's own `object` field is not one.
func TestAVerifiedLeafTheWalkLacksFails(t *testing.T) {
	res := gadget()
	tc := cover(t, res, nil)
	ev := pinned(t, res, nil)
	ev.Attributes.Leaves = append(ev.Attributes.Leaves,
		AttributeLeaf{Key: Key{Object: "gadget", Side: SideResponse, Path: "object"}, Class: ClassVerified},
		AttributeLeaf{Key: Key{Object: "gadget", Side: SideRequest, Operation: "PostGadgetsGadgetConfirm", Path: "return_url"}, Class: ClassVerified})
	if err := checkVerifiedKeys(ev, []*tableCoverage{tc}); err != nil {
		t.Errorf("err = %v, want none", err)
	}
	ev.Attributes.Leaves = append(ev.Attributes.Leaves, AttributeLeaf{Key: Key{Object: "gadget", Side: SideResponse, Path: "owner.verified_at"}, Class: ClassVerified})
	if err := checkVerifiedKeys(ev, []*tableCoverage{tc}); err == nil || !strings.Contains(err.Error(), "response/gadget/owner.verified_at") {
		t.Errorf("err = %v, want it to name the leaf", err)
	}
}

// A new Required leaf inside an object exposed today fails unless the register approves it.
func TestANewRequiredLeafFailsUnlessTheRegisterApprovesIt(t *testing.T) {
	res := gadget()
	res.Omit = append(res.Omit, "settings.mode")
	_, err := coverTable(coverageSpec(t), res, nil)
	if err == nil || !strings.Contains(err.Error(), "request/PostGadgets/settings.mode") || !strings.Contains(err.Error(), "request/PostGadgetsId/settings.mode") {
		t.Errorf("err = %v, want it to name both leaves", err)
	}
	res.Exclude = map[string]string{"settings.mode": "Fixture: approved."}
	if _, err := coverTable(coverageSpec(t), res, nil); err != nil {
		t.Errorf("with a register entry: %v", err)
	}
}

func TestRegisterEntriesAreChecked(t *testing.T) {
	for name, exclude := range map[string]map[string]string{
		"names no leaf":                {"colour": "Fixture."},
		"removes a leaf exposed today": {"color": "Fixture."},
		"has no reason":                {"source.cvc": " "},
	} {
		res := gadget()
		res.Exclude = exclude
		if _, err := coverTable(coverageSpec(t), res, nil); err == nil || !strings.Contains(err.Error(), name) {
			t.Errorf("%s: err = %v", name, err)
		}
	}
}

// The default statement of a verified leaf comes from the evidence: a value, none, a rule with
// the value observed on the suite's account, or `not sent` for an argument only the update
// takes. Variants that disagree leave the declaration to word the rule. The table's fields
// carry the statement, and the report lists where the spec's "Defaults to" text disagrees.
func TestDefaultStatementsComeFromTheEvidence(t *testing.T) {
	res := gadget()
	verified := map[string]string{}
	for _, k := range []string{"request/PostGadgets/color", "request/PostGadgets/tags", "request/PostGadgets/price", "request/PostGadgets/owner.email", "request/PostGadgetsId/color"} {
		verified[k] = ClassVerified
	}
	ev := pinned(t, res, verified)
	rule := 0
	ev.Evidence.StripeRun.PlatformCountry = "US"
	ev.Evidence.Rules = []EvidenceRule{{Rule: "the customer's currency, else the account's default currency",
		Observations: json.RawMessage(`[{"condition":{"customer.currency":"eur"},"value":"eur"},{"condition":{},"value":"usd"}]`)}}
	leaf := func(key string, d *EvidenceDefault) EvidenceLeaf {
		for _, l := range ev.Attributes.Leaves {
			if l.Key.String() == key {
				return EvidenceLeaf{Key: l.Key, Status: "proven", Tests: []string{"TestGadgetAttributes/leaves/" + key}, Default: d}
			}
		}
		t.Fatalf("%s is not a leaf", key)
		return EvidenceLeaf{}
	}
	ev.Evidence.Leaves = []EvidenceLeaf{
		leaf("request/PostGadgets/color", &EvidenceDefault{Kind: "value", Value: json.RawMessage(`"blue"`),
			Variants: []DefaultVariant{{Variant: "with_price", Default: EvidenceDefault{Kind: "value", Value: json.RawMessage(`"blue"`)}}}}),
		leaf("request/PostGadgets/tags", &EvidenceDefault{Kind: "null"}),
		leaf("request/PostGadgets/price", &EvidenceDefault{Kind: "rule", RuleRef: &rule}),
		leaf("request/PostGadgets/owner.email", &EvidenceDefault{Kind: "absent",
			Variants: []DefaultVariant{{Variant: "with_owner", Default: EvidenceDefault{Kind: "value", Value: json.RawMessage(`"a@example.com"`)}}}}),
		leaf("request/PostGadgetsId/color", nil),
		leaf("request/PostGadgetsId/settings.limit", nil),
	}
	tc := cover(t, res, ev)
	got := byKey(tc)
	for k, want := range map[string]DefaultRecord{
		"request/PostGadgets/color":       {Statement: "Default (Stripe): `blue`.", Source: "evidence"},
		"request/PostGadgets/tags":        {Statement: "Default: none.", Source: "evidence"},
		"request/PostGadgets/price":       {Statement: "Default (Stripe): the customer's currency, else the account's default currency (`usd` on a US account).", Source: "evidence"},
		"request/PostGadgets/owner.email": {Source: "evidence", Varies: true},
	} {
		if d := got[k].Default; d == nil || *d != want {
			t.Errorf("%s: %+v, want %+v", k, d, want)
		}
		if len(got[k].Tests) != 1 {
			t.Errorf("%s: tests %v", k, got[k].Tests)
		}
	}
	if d := got["request/PostGadgetsId/color"].Default; d != nil {
		t.Errorf("an update leaf the create also takes has the statement %+v", d)
	}
	if d := got["request/PostGadgets/name"].Default; d != nil {
		t.Errorf("an unverified leaf has the statement %+v", d)
	}
	// The fixture's update takes nothing the create does not, so the update-only case reads the
	// update's leaf without the create's. It needs no evidence: an unverified leaf, with nothing
	// pinned, is `not sent` too, but one not exposed has no statement.
	update := Leaf{Key: Key{Object: "gadget", Side: SideRequest, Operation: "PostGadgetsId", Path: "settings.limit"}}
	for _, p := range []*Pinned{ev, nil} {
		if d := defaultOf(update, Record{State: StateExposed}, "PostGadgets", nil, p); d == nil || d.Statement() != "Default: not sent." {
			t.Errorf("an argument only the update takes: %+v", d)
		}
	}
	if d := defaultOf(update, Record{State: StateAwaiting}, "PostGadgets", nil, nil); d != nil {
		t.Errorf("an update-only leaf awaiting proof has the statement %+v", d)
	}

	// The spec's "Defaults to `red`" disagrees with the evidence's `blue`, and its "Defaults to
	// `sale`" with none; neither is a source.
	if s := got["request/PostGadgets/color"].SpecDefault; s != "red" {
		t.Errorf("color: spec default %q, want red", s)
	}
	if s := got["request/PostGadgets/tags"].SpecDefault; s != "sale" {
		t.Errorf("tags: spec default %q, want sale", s)
	}

	for path, want := range map[string]*fields.Default{
		"color":       {Kind: fields.DefaultValue, Value: "blue"},
		"tags":        {Kind: fields.DefaultNone},
		"owner.email": {Kind: fields.DefaultVaries},
		"name":        nil,
	} {
		f := fieldAt(tc.table, path)
		if f == nil {
			t.Fatalf("no field %s", path)
		}
		if !reflect.DeepEqual(f.Default, want) || f.Verified != (want != nil) {
			t.Errorf("%s: default %+v, verified %v, want %+v", path, f.Default, f.Verified, want)
		}
	}
	if f := fieldAt(tc.table, "parts"); f == nil || f.PagedBy != "stripe_gadget" {
		t.Errorf("parts: %+v, want it read by stripe_gadget", f)
	}
}

// The report counts, per table, the optional arguments exposed without a default statement, and
// lists the spec's disagreements.
func TestReportCountsDefaultsNotYetRecorded(t *testing.T) {
	records := []Record{
		{Table: "gadget", Key: Key{Object: "gadget", Side: SideRequest, Operation: "PostGadgets", Path: "color"}, State: StateExposed, Default: &DefaultRecord{Statement: "Default (Stripe): `blue`.", Source: "evidence"}, SpecDefault: "red"},
		{Table: "gadget", Key: Key{Object: "gadget", Side: SideRequest, Operation: "PostGadgetsId", Path: "color"}, State: StateExposed},
		{Table: "gadget", Key: Key{Object: "gadget", Side: SideRequest, Operation: "PostGadgets", Path: "tags"}, State: StateExposed},
		{Table: "gadget", Key: Key{Object: "gadget", Side: SideRequest, Operation: "PostGadgetsId", Path: "tags"}, State: StateExposed},
		{Table: "gadget", Key: Key{Object: "gadget", Side: SideRequest, Operation: "PostGadgets", Path: "name"}, State: StateExposed, Required: true},
		{Table: "gadget", Key: Key{Object: "gadget", Side: SideRequest, Operation: "PostGadgetsId", Path: "name"}, State: StateExposed},
		{Table: "gadget", Key: Key{Object: "gadget", Side: SideRequest, Operation: "PostGadgets", Path: "owner.email"}, State: StateExposed, Default: &DefaultRecord{Source: "evidence", Varies: true}},
		{Table: "gadget", Key: Key{Object: "gadget", Side: SideResponse, Path: "status"}, State: StateExposed},
	}
	md := string(RenderCoverageMarkdown(coverageHeader{}, records))
	for _, want := range []string{
		"|`gadget`|`gadget`|7|0|0|1|0|0|2|",
		"- `gadget` PostGadgets `color` (): the spec says `red`; the evidence says Default (Stripe): `blue`.",
	} {
		if !strings.Contains(md, want) {
			t.Errorf("coverage.md lacks %q:\n%s", want, md)
		}
	}
}

// fixtureRoot writes a repository root for Generate: the fixture spec and its pin, and,
// when attributes is set, the fake version, the evidence pin and both vendored files.
func fixtureRoot(t *testing.T, attributes *Attributes, evidence *Evidence) string {
	t.Helper()
	root := t.TempDir()
	write := func(rel string, data []byte) PinnedFile {
		t.Helper()
		path := filepath.Join(root, rel)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, data, 0o644); err != nil {
			t.Fatal(err)
		}
		sum := sha256.Sum256(data)
		return PinnedFile{Bytes: len(data), SHA256: hex.EncodeToString(sum[:])}
	}
	specData, err := os.ReadFile(filepath.Join("testdata", coverageSpecFile))
	if err != nil {
		t.Fatal(err)
	}
	spec := write("spec/openapi.json", specData)
	write("spec/PIN.json", must(json.Marshal(Pin{APIVersion: "2000-01-01.fixture", Tag: "fixture", SHA256: spec.SHA256, Bytes: spec.Bytes})))
	if attributes == nil {
		return root
	}
	write(".fake-version", []byte("v9.9.9\n"))
	pin := EvidencePin{FakeVersion: "v9.9.9", SuiteRef: "abc123", RunID: 42}
	pin.Attributes = write("spec/attributes.json", must(json.Marshal(attributes)))
	pin.Evidence = write("spec/evidence.json", must(json.Marshal(evidence)))
	write("spec/EVIDENCE_PIN.json", must(json.Marshal(pin)))
	return root
}

// fixtureEvidence is a matching attributes.json and evidence record for the fixture spec,
// with the leaf `request/PostGadgets/color` verified.
func fixtureEvidence(t *testing.T) (*Attributes, *Evidence) {
	t.Helper()
	sum := sha256.Sum256(must(os.ReadFile(filepath.Join("testdata", coverageSpecFile))))
	a := pinned(t, trimmed(), map[string]string{"request/PostGadgets/color": ClassVerified}).Attributes
	a.FakeVersion, a.SuiteRef, a.RunID, a.APIVersion, a.SpecSHA256 = "v9.9.9", "abc123", 42, "2000-01-01.fixture", hex.EncodeToString(sum[:])
	e := &Evidence{FormatVersion: 1, SuiteCommit: "abc123", APIVersion: a.APIVersion, SpecSHA256: a.SpecSHA256,
		Leaves: []EvidenceLeaf{{Key: Key{Object: "gadget", Side: SideRequest, Operation: "PostGadgets", Path: "color"}, Status: "proven", Targets: []string{"fake", "stripe"},
			Tests: []string{"TestGadgetAttributes/leaves/request/PostGadgets/color"}}}}
	e.StripeRun.ID = 42
	return a, e
}

// must returns data, and panics on an error reading or encoding a fixture.
func must(data []byte, err error) []byte {
	if err != nil {
		panic(err)
	}
	return data
}

// go generate writes coverage.json and coverage.md, and the pinned evidence exposes its
// verified leaf.
func TestGenerateWritesTheCoverageReport(t *testing.T) {
	a, e := fixtureEvidence(t)
	for _, tc := range []struct {
		name     string
		root     string
		exposed  bool
		evidence string
	}{
		{"no evidence pinned", fixtureRoot(t, nil, nil), false, "No evidence is pinned"},
		{"evidence pinned", fixtureRoot(t, a, e), true, "Evidence: fake `v9.9.9`, suite commit `abc123`, run `42`."},
	} {
		files, err := Generate(tc.root, []Resource{trimmed()})
		if err != nil {
			t.Fatalf("%s: %v", tc.name, err)
		}
		got := map[string]string{}
		for _, f := range files {
			got[f.Path] = string(f.Content)
		}
		var report struct {
			Header coverageHeader `json:"header"`
			Leaves []Record       `json:"leaves"`
		}
		if err := json.Unmarshal([]byte(got["internal/generated/coverage.json"]), &report); err != nil {
			t.Fatalf("%s: coverage.json: %v", tc.name, err)
		}
		for _, r := range report.Leaves {
			if r.Key.String() == "request/PostGadgets/color" && (r.State == StateExposed) != tc.exposed {
				t.Errorf("%s: color is %s", tc.name, r.State)
			}
		}
		if strings.Contains(got["internal/generated/gadget/fields.go"], `Name: "color"`) != tc.exposed {
			t.Errorf("%s: the table's color field does not follow the evidence", tc.name)
		}
		if !strings.Contains(got["internal/generated/coverage.md"], tc.evidence) {
			t.Errorf("%s: coverage.md does not say %q", tc.name, tc.evidence)
		}
	}
}

// The generator refuses evidence for another API version or spec, and evidence whose run
// differs from the one attributes.json names, or that differs from its pin.
func TestGenerateRefusesMismatchedEvidence(t *testing.T) {
	for _, tc := range []struct {
		name  string
		edit  func(*Attributes, *Evidence)
		error string
	}{
		{"evidence for another API version", func(_ *Attributes, e *Evidence) { e.APIVersion = "2001-01-01.other" }, "spec/evidence.json is for API version 2001-01-01.other"},
		{"evidence for another spec", func(_ *Attributes, e *Evidence) { e.SpecSHA256 = "0000" }, "spec/evidence.json is for the spec with sha256 0000"},
		{"attributes for another API version", func(a *Attributes, _ *Evidence) { a.APIVersion = "2001-01-01.other" }, "spec/attributes.json is for API version 2001-01-01.other"},
		{"evidence of another run", func(_ *Attributes, e *Evidence) { e.StripeRun.ID = 43 }, "spec/evidence.json is suite run abc123/43, spec/attributes.json names abc123/42"},
		{"evidence of another commit", func(_ *Attributes, e *Evidence) { e.SuiteCommit = "def456" }, "spec/evidence.json is suite run def456/42"},
		{"attributes of another fake", func(a *Attributes, _ *Evidence) { a.FakeVersion = "v9.9.8" }, "spec/attributes.json is from fake v9.9.8"},
		{"another evidence format", func(_ *Attributes, e *Evidence) { e.FormatVersion = 2 }, "format version 2"},
		{"an unknown class", func(a *Attributes, _ *Evidence) { a.Leaves[0].Class = "proven" }, `unknown class "proven"`},
		{"a duplicate key in attributes.json", func(a *Attributes, _ *Evidence) {
			a.Leaves = append(a.Leaves, AttributeLeaf{Key: Key{Object: "gadget", Side: SideRequest, Operation: "PostGadgets", Path: "color"}, Class: ClassUnproven})
		}, "spec/attributes.json lists request/PostGadgets/color twice"},
		{"a duplicate key in the evidence", func(_ *Attributes, e *Evidence) { e.Leaves = append(e.Leaves, e.Leaves[0]) }, "spec/evidence.json lists request/PostGadgets/color twice"},
		{"a verified leaf the evidence does not prove", func(_ *Attributes, e *Evidence) { e.Leaves[0].Status = "failed" }, "spec/attributes.json verifies leaves that spec/evidence.json does not prove: request/PostGadgets/color"},
		{"a verified leaf the evidence lacks", func(_ *Attributes, e *Evidence) { e.Leaves = nil }, "does not prove: request/PostGadgets/color"},
		{"attributes with no suite commit", func(a *Attributes, _ *Evidence) { a.SuiteRef = "" }, `spec/attributes.json names no suite run (suite commit "", run 42)`},
		{"attributes with no run", func(a *Attributes, _ *Evidence) { a.RunID = 0 }, `spec/attributes.json names no suite run (suite commit "abc123", run 0)`},
		{"evidence with no suite commit", func(_ *Attributes, e *Evidence) { e.SuiteCommit = " " }, `spec/evidence.json names no suite run`},
		{"evidence with no run", func(_ *Attributes, e *Evidence) { e.StripeRun.ID = 0 }, `spec/evidence.json names no suite run (suite commit "abc123", run 0)`},
	} {
		a, e := fixtureEvidence(t)
		tc.edit(a, e)
		if _, err := Generate(fixtureRoot(t, a, e), []Resource{trimmed()}); err == nil || !strings.Contains(err.Error(), tc.error) {
			t.Errorf("%s: err = %v, want %q", tc.name, err, tc.error)
		}
	}

	a, e := fixtureEvidence(t)
	root := fixtureRoot(t, a, e)
	if err := os.WriteFile(filepath.Join(root, "spec", "evidence.json"), []byte(`{}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := Generate(root, []Resource{trimmed()}); err == nil || !strings.Contains(err.Error(), "spec/evidence.json is 2 bytes") {
		t.Errorf("a file that differs from its pin: err = %v", err)
	}
	root = fixtureRoot(t, a, e)
	if err := os.WriteFile(filepath.Join(root, ".fake-version"), []byte("v9.9.10\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := Generate(root, []Resource{trimmed()}); err == nil || !strings.Contains(err.Error(), ".fake-version is v9.9.10") {
		t.Errorf("a pin for another fake release: err = %v", err)
	}

	for name, edit := range map[string]func(*EvidencePin){
		`spec/EVIDENCE_PIN.json names no suite run (suite commit "", run 42)`:      func(p *EvidencePin) { p.SuiteRef = "" },
		`spec/EVIDENCE_PIN.json names no suite run (suite commit "abc123", run 0)`: func(p *EvidencePin) { p.RunID = 0 },
	} {
		root := fixtureRoot(t, a, e)
		path := filepath.Join(root, "spec", "EVIDENCE_PIN.json")
		var pin EvidencePin
		if err := json.Unmarshal(must(os.ReadFile(path)), &pin); err != nil {
			t.Fatal(err)
		}
		edit(&pin)
		if err := os.WriteFile(path, must(json.Marshal(pin)), 0o644); err != nil {
			t.Fatal(err)
		}
		if _, err := Generate(root, []Resource{trimmed()}); err == nil || !strings.Contains(err.Error(), name) {
			t.Errorf("err = %v, want %q", err, name)
		}
	}
}

// Rule X5 applies where the declaration's `status` transition confirms. Every table whose
// request takes `confirm` is listed here with its declaration.
func TestStatusConfirmsFollowsTheDeclarations(t *testing.T) {
	declarations := map[string]engine.Declaration{"payment_intent": payment_intent.Declaration, "setup_intent": setup_intent.Declaration}
	data, err := os.ReadFile(filepath.Join(repoRoot, "spec", "openapi.json"))
	if err != nil {
		t.Fatal(err)
	}
	spec, err := ParseSpec(data)
	if err != nil {
		t.Fatal(err)
	}
	for _, res := range Resources {
		_, leaves, err := walkTable(spec, res)
		if err != nil {
			t.Fatal(err)
		}
		takesConfirm := false
		for _, l := range leaves {
			takesConfirm = takesConfirm || l.Side == SideRequest && l.Path == "confirm"
		}
		if !takesConfirm {
			if res.StatusConfirms {
				t.Errorf("%s: StatusConfirms, but its request takes no `confirm`", res.Package)
			}
			continue
		}
		decl, ok := declarations[res.Package]
		if !ok {
			t.Errorf("%s takes `confirm`: add its declaration to this test", res.Package)
			continue
		}
		confirms := false
		for _, tr := range decl.Transitions {
			confirms = confirms || tr.Field == "status" && strings.HasSuffix(tr.Path, "/confirm")
		}
		if confirms != res.StatusConfirms {
			t.Errorf("%s: StatusConfirms is %v, but its declaration's status transition confirms: %v", res.Package, res.StatusConfirms, confirms)
		}
	}
}

// Every table whose own response repeats the parent ID that the engine adds as the
// declaration's Parent attribute excludes that copy in the register: by the parent's name, or
// for a parent `<name>_id`, by `<name>`.
func TestEveryParentCopyHasItsRegisterEntry(t *testing.T) {
	data, err := os.ReadFile(filepath.Join(repoRoot, "spec", "openapi.json"))
	if err != nil {
		t.Fatal(err)
	}
	spec, err := ParseSpec(data)
	if err != nil {
		t.Fatal(err)
	}
	parents := declaredParents(t)
	if len(parents) == 0 {
		t.Fatal("no declaration has a parent: the source scan is broken")
	}
	for _, res := range Resources {
		parent, ok := parents[res.Package]
		if !ok {
			continue
		}
		delete(parents, res.Package)
		_, leaves, err := walkTable(spec, res)
		if err != nil {
			t.Fatal(err)
		}
		for _, name := range []string{parent, strings.TrimSuffix(parent, "_id")} {
			copied := slices.ContainsFunc(leaves, func(l Leaf) bool { return l.Side == SideResponse && l.Path == name })
			if copied && res.Exclude[name] != parentHolds {
				t.Errorf("%s: its response's `%s` is the parent `%s`; add the register entry %q", res.Package, name, parent, parentHolds)
			}
		}
	}
	for pkg := range parents {
		t.Errorf("internal/resources/%s declares a parent but has no table", pkg)
	}
}

// declaredParents reads the Parent of every declaration under internal/resources, by package.
func declaredParents(t *testing.T) map[string]string {
	t.Helper()
	files, err := filepath.Glob(filepath.Join(repoRoot, "internal", "resources", "*", "*.go"))
	if err != nil {
		t.Fatal(err)
	}
	out := map[string]string{}
	fset := token.NewFileSet()
	for _, path := range files {
		if strings.HasSuffix(path, "_test.go") {
			continue
		}
		f, err := parser.ParseFile(fset, path, nil, 0)
		if err != nil {
			t.Fatal(err)
		}
		ast.Inspect(f, func(n ast.Node) bool {
			kv, ok := n.(*ast.KeyValueExpr)
			if !ok {
				return true
			}
			key, isIdent := kv.Key.(*ast.Ident)
			value, isLit := kv.Value.(*ast.BasicLit)
			if isIdent && isLit && key.Name == "Parent" && value.Kind == token.STRING {
				parent, err := strconv.Unquote(value.Value)
				if err != nil {
					t.Fatal(err)
				}
				out[filepath.Base(filepath.Dir(path))] = parent
			}
			return true
		})
	}
	return out
}
