package main

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"maps"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
)

// EvidencePin is spec/EVIDENCE_PIN.json (the ADR's `spec/EVIDENCE.json`, renamed because it
// is the same file as spec/evidence.json on a case-insensitive file system): the fake release whose `attributes.json` the provider
// vendors as spec/attributes.json, the conformance suite's commit and evidence run that file
// names, and the size and SHA-256 of both vendored files (ADR
// 2026-10-29-full-attribute-coverage, "The evidence gate").
type EvidencePin struct {
	FakeVersion string     `json:"fake_version"`
	SuiteRef    string     `json:"suite_ref"`
	RunID       int64      `json:"run_id"`
	Attributes  PinnedFile `json:"attributes"`
	Evidence    PinnedFile `json:"evidence"`
}

// PinnedFile is the size and SHA-256 of one vendored file.
type PinnedFile struct {
	Bytes  int    `json:"bytes"`
	SHA256 string `json:"sha256"`
}

// Classes of a leaf in a fake release's `attributes.json` (fake ADR
// 2026-10-29-attribute-parity, A7). Only ClassVerified exposes a leaf.
const (
	ClassVerified           = "verified"
	ClassServedFailing      = "served_failing"
	ClassServedUnverified   = "served_unverified"
	ClassGapProvenNotServed = "gap_proven_not_served"
	ClassGapStripeOnly      = "gap_stripe_only"
	ClassGapPlaceholderNull = "gap_placeholder_null"
	ClassUnproven           = "unproven"
)

var classes = []string{ClassVerified, ClassServedFailing, ClassServedUnverified, ClassGapProvenNotServed, ClassGapStripeOnly, ClassGapPlaceholderNull, ClassUnproven}

// Attributes is the part of a fake release's `attributes.json` the generator reads: the
// header that ties it to a suite run and the spec, and each leaf's key and class. Other
// members are ignored.
type Attributes struct {
	FakeVersion string          `json:"fake_version"`
	SuiteRef    string          `json:"suite_ref"`
	RunID       int64           `json:"run_id"`
	APIVersion  string          `json:"api_version"`
	SpecSHA256  string          `json:"spec_sha256"`
	Leaves      []AttributeLeaf `json:"leaves"`
}

// AttributeLeaf is one leaf of `attributes.json`.
type AttributeLeaf struct {
	Key
	Class string `json:"class"`
}

// Evidence is the suite's evidence record, format version 1 (suite ADR
// 2026-10-29-attribute-coverage, A7; its JSON Schema is `internal/evidence/schema/v1.json` in
// the suite). Every member the schema defines is declared, so that a member the generator does
// not know fails the decode; those it does not read stay raw.
type Evidence struct {
	FormatVersion  int             `json:"format_version"`
	SuiteCommit    string          `json:"suite_commit"`
	APIVersion     string          `json:"api_version"`
	SpecSHA256     string          `json:"spec_sha256"`
	StripeRun      StripeRun       `json:"stripe_run"`
	Scope          json.RawMessage `json:"scope,omitempty"`
	Leaves         []EvidenceLeaf  `json:"leaves"`
	MinimalCreates json.RawMessage `json:"minimal_creates,omitempty"`
	Rules          []EvidenceRule  `json:"rules"`
	SideEffects    []SideEffect    `json:"side_effects"`
	FilledFrom     []FilledFrom    `json:"filled_from"`
	Families       json.RawMessage `json:"families,omitempty"`
	EmbeddedCopies json.RawMessage `json:"embedded_copies,omitempty"`
	PagedLists     json.RawMessage `json:"paged_lists,omitempty"`
	Discounts      []Discount      `json:"discounts"`
	Countries      json.RawMessage `json:"countries,omitempty"`
}

// StripeRun is the suite's run against Stripe that produced the record.
type StripeRun struct {
	ID              int64  `json:"id"`
	Event           string `json:"event,omitempty"`
	Time            string `json:"time,omitempty"`
	Sandbox         string `json:"sandbox,omitempty"`
	PlatformCountry string `json:"platform_country,omitempty"`
	Passed          bool   `json:"passed,omitempty"`
}

// EvidenceLeaf is one leaf of the evidence record.
type EvidenceLeaf struct {
	Key
	Status  string           `json:"status"`
	Reason  string           `json:"reason,omitempty"`
	Targets []string         `json:"targets,omitempty"`
	On      []string         `json:"on,omitempty"`
	Tests   []string         `json:"tests"`
	Default *EvidenceDefault `json:"default,omitempty"`
	// Unset is, on an update leaf, what an update that unsets the leaf did (P4).
	Unset      *EvidenceUnset  `json:"unset,omitempty"`
	Returned   json.RawMessage `json:"returned,omitempty"`
	EnumValues json.RawMessage `json:"enum_values,omitempty"`
	WriteOnly  json.RawMessage `json:"write_only,omitempty"`
	AcceptedIn json.RawMessage `json:"accepted_in,omitempty"`
	Refusals   json.RawMessage `json:"refusals,omitempty"`
	Initial    json.RawMessage `json:"initial,omitempty"`
	Nullable   json.RawMessage `json:"nullable,omitempty"`
	Failure    json.RawMessage `json:"failure,omitempty"`
}

// EvidenceDefault is what Stripe set on a minimal create that left the leaf out: a value,
// null, nothing, or a rule; and per variant of the minimal create (D-2), where they differ.
type EvidenceDefault struct {
	Kind     string           `json:"kind"`
	Value    json.RawMessage  `json:"value,omitempty"`
	RuleRef  *int             `json:"rule_ref,omitempty"`
	Variants []DefaultVariant `json:"variants,omitempty"`
}

// DefaultVariant is a leaf's default in one variant of the minimal create.
type DefaultVariant struct {
	Variant string          `json:"variant"`
	Default EvidenceDefault `json:"default"`
}

// none reports a default of null or nothing, in the base create and every variant.
func (d *EvidenceDefault) none() bool {
	if d == nil || d.Kind != "null" && d.Kind != "absent" {
		return false
	}
	for _, v := range d.Variants {
		if !v.Default.none() {
			return false
		}
	}
	return true
}

// EvidenceUnset is how an update unset a leaf (`by`) and what Stripe then held (`result`), or
// the error with which it refused.
type EvidenceUnset struct {
	By      string          `json:"by"`
	Result  string          `json:"result"`
	Matcher json.RawMessage `json:"matcher,omitempty"`
	Error   json.RawMessage `json:"error,omitempty"`
}

// EvidenceRule is a default that depends on the account or another argument.
type EvidenceRule struct {
	Object       string          `json:"object"`
	Operation    string          `json:"operation"`
	Path         string          `json:"path"`
	Rule         string          `json:"rule"`
	Observations json.RawMessage `json:"observations,omitempty"`
	Tests        []string        `json:"tests"`
}

// SideEffect is one update of an update probe and the paths it did not name whose literal
// value changed (P5).
type SideEffect struct {
	Object    string        `json:"object"`
	Operation string        `json:"operation"`
	Probe     string        `json:"probe"`
	Changed   []ChangedPath `json:"changed"`
	Tests     []string      `json:"tests"`
}

// ChangedPath is one path a side effect changed, with matchers of the values before and after.
type ChangedPath struct {
	Path   string          `json:"path"`
	Before json.RawMessage `json:"before"`
	After  json.RawMessage `json:"after"`
}

// FilledFrom is a field Stripe fills from another (P5).
type FilledFrom struct {
	Object string   `json:"object"`
	Path   string   `json:"path"`
	From   string   `json:"from"`
	Tests  []string `json:"tests"`
}

// Discount is what one form of a `discounts` element sent to one operation returned (P11).
type Discount struct {
	Object    string   `json:"object"`
	Operation string   `json:"operation"`
	Form      string   `json:"form"`
	Returned  string   `json:"returned"`
	Expanded  string   `json:"expanded"`
	Relation  string   `json:"relation"`
	Tests     []string `json:"tests"`
}

// The values of the record's closed sets that the generator reads. A value outside them is a
// format change the generator does not know, and fails the load.
var (
	leafStatuses  = []string{"proven", "failed", "not_reached", "unproven", "unreachable", "out_of_scope"}
	leafTargets   = []string{"fake", "stripe"}
	leafOn        = []string{"create", "update"}
	defaultKinds  = []string{"value", "null", "absent", "rule"}
	unsetBy       = []string{"empty_string", "null", "shorter_array"}
	unsetResults  = []string{"null", "absent", "as_sent", "unchanged", "value", "refused"}
	discountForms = []string{"coupon", "discount", "promotion_code"}
)

// Pinned is the vendored evidence, read and checked against its pin and the spec's.
type Pinned struct {
	Pin        EvidencePin
	Attributes *Attributes
	Evidence   *Evidence
	// classes and leaves index the two files by key, built on first use.
	classes map[Key]string
	leaves  map[Key]*EvidenceLeaf
}

const evidenceFormat = 1

// LoadEvidence reads spec/EVIDENCE_PIN.json, spec/attributes.json and spec/evidence.json under
// root. It returns nil when there is no pin: until a fake release attaches `attributes.json`,
// nothing is verified and every leaf not exposed today awaits proof. Either file is refused
// when it differs from the pin, when its API version or spec hash differs from spec/PIN.json,
// or when the two disagree on the suite commit and run.
func LoadEvidence(root string, spec Pin) (*Pinned, error) {
	pinData, err := os.ReadFile(filepath.Join(root, "spec", "EVIDENCE_PIN.json"))
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var p Pinned
	d := json.NewDecoder(bytes.NewReader(pinData))
	d.DisallowUnknownFields()
	if err := d.Decode(&p.Pin); err != nil {
		return nil, fmt.Errorf("decode spec/EVIDENCE_PIN.json: %w", err)
	}
	if err := namesRun("spec/EVIDENCE_PIN.json", p.Pin.SuiteRef, p.Pin.RunID); err != nil {
		return nil, err
	}
	fake, err := os.ReadFile(filepath.Join(root, ".fake-version"))
	if err != nil {
		return nil, err
	}
	if v := strings.TrimSpace(string(fake)); v != p.Pin.FakeVersion {
		return nil, fmt.Errorf("spec/EVIDENCE_PIN.json pins fake %s, .fake-version is %s: re-pin both together", p.Pin.FakeVersion, v)
	}

	attrData, err := readPinned(root, "attributes.json", p.Pin.Attributes)
	if err != nil {
		return nil, err
	}
	p.Attributes = new(Attributes)
	if err := json.Unmarshal(attrData, p.Attributes); err != nil {
		return nil, fmt.Errorf("decode spec/attributes.json: %w", err)
	}
	a := p.Attributes
	if err := namesRun("spec/attributes.json", a.SuiteRef, a.RunID); err != nil {
		return nil, err
	}
	switch {
	case a.FakeVersion != p.Pin.FakeVersion:
		return nil, fmt.Errorf("spec/attributes.json is from fake %s, spec/EVIDENCE_PIN.json pins %s", a.FakeVersion, p.Pin.FakeVersion)
	case a.SuiteRef != p.Pin.SuiteRef || a.RunID != p.Pin.RunID:
		return nil, fmt.Errorf("spec/attributes.json names suite run %s/%d, spec/EVIDENCE_PIN.json pins %s/%d", a.SuiteRef, a.RunID, p.Pin.SuiteRef, p.Pin.RunID)
	}
	if err := sameSpec("spec/attributes.json", a.APIVersion, a.SpecSHA256, spec); err != nil {
		return nil, err
	}
	seen := map[Key]bool{}
	for _, l := range a.Leaves {
		if !slices.Contains(classes, l.Class) {
			return nil, fmt.Errorf("spec/attributes.json: %s has the unknown class %q", l.Key, l.Class)
		}
		if seen[l.Key] {
			return nil, fmt.Errorf("spec/attributes.json lists %s twice", l.Key)
		}
		seen[l.Key] = true
	}

	evData, err := readPinned(root, "evidence.json", p.Pin.Evidence)
	if err != nil {
		return nil, err
	}
	if p.Evidence, err = decodeEvidence(evData); err != nil {
		return nil, fmt.Errorf("spec/evidence.json: %w", err)
	}
	e := p.Evidence
	if err := namesRun("spec/evidence.json", e.SuiteCommit, e.StripeRun.ID); err != nil {
		return nil, err
	}
	if e.SuiteCommit != a.SuiteRef || e.StripeRun.ID != a.RunID {
		return nil, fmt.Errorf("spec/evidence.json is suite run %s/%d, spec/attributes.json names %s/%d", e.SuiteCommit, e.StripeRun.ID, a.SuiteRef, a.RunID)
	}
	if err := sameSpec("spec/evidence.json", e.APIVersion, e.SpecSHA256, spec); err != nil {
		return nil, err
	}
	proven := map[Key]bool{}
	for _, l := range e.Leaves {
		if _, dup := proven[l.Key]; dup {
			return nil, fmt.Errorf("spec/evidence.json lists %s twice", l.Key)
		}
		proven[l.Key] = l.Status == "proven"
	}
	// The fake verifies only what the record proves; a verified leaf the pinned record does
	// not prove means the two files do not belong together.
	var unproven []string
	for _, l := range a.Leaves {
		if l.Class == ClassVerified && !proven[l.Key] {
			unproven = append(unproven, l.Key.String())
		}
	}
	if len(unproven) > 0 {
		return nil, fmt.Errorf("spec/attributes.json verifies leaves that spec/evidence.json does not prove: %s", strings.Join(unproven, ", "))
	}
	return &p, nil
}

// decodeEvidence decodes an evidence record, refusing a member the format does not define and a
// value outside a closed set the generator reads.
func decodeEvidence(data []byte) (*Evidence, error) {
	var version struct {
		FormatVersion int `json:"format_version"`
	}
	if err := json.Unmarshal(data, &version); err != nil {
		return nil, fmt.Errorf("decode: %w", err)
	}
	if version.FormatVersion != evidenceFormat {
		return nil, fmt.Errorf("format version %d; the generator reads %d", version.FormatVersion, evidenceFormat)
	}
	e := new(Evidence)
	d := json.NewDecoder(bytes.NewReader(data))
	d.DisallowUnknownFields()
	if err := d.Decode(e); err != nil {
		if where := unknownMember(data, reflect.TypeFor[Evidence](), ""); where != "" {
			return nil, fmt.Errorf("decode: %s is a member the format does not define: %w", where, err)
		}
		return nil, fmt.Errorf("decode: %w", err)
	}
	if err := e.check(); err != nil {
		return nil, err
	}
	return e, nil
}

// unknownMember returns where in data the first member that t does not declare is, as a path
// from the record's root such as `leaves[12] (request/PostCoupons/name).unset.since`, naming the
// leaf an element of a list is, or "" when t declares every member.
func unknownMember(data json.RawMessage, t reflect.Type, at string) string {
	for t.Kind() == reflect.Pointer {
		t = t.Elem()
	}
	switch t.Kind() {
	case reflect.Struct:
		var members map[string]json.RawMessage
		if json.Unmarshal(data, &members) != nil {
			return ""
		}
		declared := jsonMembers(t)
		names := make([]string, 0, len(members))
		for name := range members {
			names = append(names, name)
		}
		slices.Sort(names)
		for _, name := range names {
			inner := name
			if at != "" {
				inner = at + "." + name
			}
			ft, ok := declared[name]
			if !ok {
				return inner
			}
			if where := unknownMember(members[name], ft, inner); where != "" {
				return where
			}
		}
	case reflect.Slice:
		var elems []json.RawMessage
		if t.Elem() == reflect.TypeFor[byte]() || json.Unmarshal(data, &elems) != nil {
			return ""
		}
		for i, elem := range elems {
			inner := fmt.Sprintf("%s[%d]", at, i)
			var k Key
			if json.Unmarshal(elem, &k) == nil && k.Side != "" {
				inner += " (" + k.String() + ")"
			}
			if where := unknownMember(elem, t.Elem(), inner); where != "" {
				return where
			}
		}
	}
	return ""
}

// jsonMembers maps the JSON member names a struct declares, those of embedded structs
// included, to their types.
func jsonMembers(t reflect.Type) map[string]reflect.Type {
	out := map[string]reflect.Type{}
	for f := range t.Fields() {
		name, _, _ := strings.Cut(f.Tag.Get("json"), ",")
		switch {
		case name == "-":
		case f.Anonymous && name == "":
			maps.Copy(out, jsonMembers(f.Type))
		case name == "":
			out[f.Name] = f.Type
		default:
			out[name] = f.Type
		}
	}
	return out
}

// check refuses a value outside a closed set the generator reads, and a rule default with no
// rule.
func (e *Evidence) check() error {
	unknown := func(what, value string, known []string) error {
		if slices.Contains(known, value) {
			return nil
		}
		return fmt.Errorf("%s %q is not one the generator knows (%s)", what, value, strings.Join(known, ", "))
	}
	var checkDefault func(k Key, d *EvidenceDefault) error
	checkDefault = func(k Key, d *EvidenceDefault) error {
		if err := unknown(k.String()+": default kind", d.Kind, defaultKinds); err != nil {
			return err
		}
		if d.Kind == "rule" && (d.RuleRef == nil || *d.RuleRef < 0 || *d.RuleRef >= len(e.Rules)) {
			return fmt.Errorf("%s has a rule default with no rule", k)
		}
		for _, v := range d.Variants {
			if err := checkDefault(k, &v.Default); err != nil {
				return err
			}
		}
		return nil
	}
	for _, l := range e.Leaves {
		errs := []error{
			unknown(l.Key.String()+": side", string(l.Side), []string{string(SideRequest), string(SideResponse)}),
			unknown(l.Key.String()+": status", l.Status, leafStatuses),
		}
		for _, t := range l.Targets {
			errs = append(errs, unknown(l.Key.String()+": target", t, leafTargets))
		}
		for _, o := range l.On {
			errs = append(errs, unknown(l.Key.String()+": on", o, leafOn))
		}
		if l.Default != nil {
			errs = append(errs, checkDefault(l.Key, l.Default))
		}
		if u := l.Unset; u != nil {
			errs = append(errs, unknown(l.Key.String()+": unset by", u.By, unsetBy), unknown(l.Key.String()+": unset result", u.Result, unsetResults))
		}
		if err := errors.Join(errs...); err != nil {
			return err
		}
	}
	for _, d := range e.Discounts {
		if err := unknown(d.Object+" "+d.Operation+": discounts form", d.Form, discountForms); err != nil {
			return err
		}
	}
	return nil
}

// namesRun refuses a file that names no suite commit or run.
func namesRun(file, suiteRef string, runID int64) error {
	if strings.TrimSpace(suiteRef) == "" || runID == 0 {
		return fmt.Errorf("%s names no suite run (suite commit %q, run %d)", file, suiteRef, runID)
	}
	return nil
}

func readPinned(root, name string, want PinnedFile) ([]byte, error) {
	data, err := os.ReadFile(filepath.Join(root, "spec", name))
	if err != nil {
		return nil, err
	}
	sum := sha256.Sum256(data)
	if got := hex.EncodeToString(sum[:]); got != want.SHA256 || len(data) != want.Bytes {
		return nil, fmt.Errorf("spec/%s is %d bytes with sha256 %s, spec/EVIDENCE_PIN.json pins %d bytes with %s", name, len(data), got, want.Bytes, want.SHA256)
	}
	return data, nil
}

func sameSpec(file, apiVersion, specSHA string, spec Pin) error {
	if apiVersion != spec.APIVersion {
		return fmt.Errorf("%s is for API version %s, spec/PIN.json pins %s", file, apiVersion, spec.APIVersion)
	}
	if specSHA != spec.SHA256 {
		return fmt.Errorf("%s is for the spec with sha256 %s, spec/PIN.json pins %s", file, specSHA, spec.SHA256)
	}
	return nil
}
