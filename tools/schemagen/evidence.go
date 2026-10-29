package main

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
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

// Evidence is the part of the suite's evidence record, format version 1, that the generator
// reads (suite ADR 2026-10-29-attribute-coverage, A7). Other members are ignored.
type Evidence struct {
	FormatVersion int    `json:"format_version"`
	SuiteCommit   string `json:"suite_commit"`
	APIVersion    string `json:"api_version"`
	SpecSHA256    string `json:"spec_sha256"`
	StripeRun     struct {
		ID int64 `json:"id"`
	} `json:"stripe_run"`
	Leaves []EvidenceLeaf `json:"leaves"`
	Rules  []EvidenceRule `json:"rules"`
}

// EvidenceLeaf is one leaf of the evidence record.
type EvidenceLeaf struct {
	Key
	Status  string           `json:"status"`
	Targets []string         `json:"targets"`
	Tests   []string         `json:"tests"`
	Default *EvidenceDefault `json:"default"`
}

// EvidenceDefault is what Stripe set on a minimal create that left the leaf out: a value,
// null, nothing, or a rule.
type EvidenceDefault struct {
	Kind    string          `json:"kind"`
	Value   json.RawMessage `json:"value"`
	RuleRef *int            `json:"rule_ref"`
}

// EvidenceRule is a default that depends on the account or another argument.
type EvidenceRule struct {
	Rule string `json:"rule"`
}

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
	p.Evidence = new(Evidence)
	if err := json.Unmarshal(evData, p.Evidence); err != nil {
		return nil, fmt.Errorf("decode spec/evidence.json: %w", err)
	}
	e := p.Evidence
	if err := namesRun("spec/evidence.json", e.SuiteCommit, e.StripeRun.ID); err != nil {
		return nil, err
	}
	switch {
	case e.FormatVersion != evidenceFormat:
		return nil, fmt.Errorf("spec/evidence.json has format version %d; the generator reads %d", e.FormatVersion, evidenceFormat)
	case e.SuiteCommit != a.SuiteRef || e.StripeRun.ID != a.RunID:
		return nil, fmt.Errorf("spec/evidence.json is suite run %s/%d, spec/attributes.json names %s/%d", e.SuiteCommit, e.StripeRun.ID, a.SuiteRef, a.RunID)
	}
	if err := sameSpec("spec/evidence.json", e.APIVersion, e.SpecSHA256, spec); err != nil {
		return nil, err
	}
	proven := map[Key]bool{}
	for _, l := range e.Leaves {
		if d := l.Default; d != nil && d.Kind == "rule" && (d.RuleRef == nil || *d.RuleRef < 0 || *d.RuleRef >= len(e.Rules)) {
			return nil, fmt.Errorf("spec/evidence.json: %s has a rule default with no rule", l.Key)
		}
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
