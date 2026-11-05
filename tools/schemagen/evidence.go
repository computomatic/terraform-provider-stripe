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
	"net/http"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"

	"github.com/computomatic/terraform-provider-stripe/tools/internal/evidencecheck"
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
	// CreateProbes is spec/create_probes.json, the digest of the suite's observations at SuiteRef.
	CreateProbes PinnedFile `json:"create_probes"`
}

// PinnedFile is the size and SHA-256 of one vendored file.
type PinnedFile struct {
	Bytes  int    `json:"bytes"`
	SHA256 string `json:"sha256"`
}

// Classes of a leaf in a fake release's `attributes.json` (fake ADR
// 2026-10-29-attribute-parity, A7, and the fake's `architecture/ci.md`, "Attribute fidelity",
// which adds `served_broken` and `proven_by_test`). Only ClassVerified exposes a leaf.
const (
	ClassVerified = "verified"
	// ClassAcceptedOnly is a leaf Stripe takes and never returns, so that a user could neither
	// read it back nor see drift: rule X6 leaves it out unless Resource.AcceptedOnly allows it.
	ClassAcceptedOnly       = "verified_accepted_only"
	ClassServedFailing      = "served_failing"
	ClassServedBroken       = "served_broken"
	ClassServedUnverified   = "served_unverified"
	ClassGapProvenNotServed = "gap_proven_not_served"
	ClassGapStripeOnly      = "gap_stripe_only"
	ClassGapPlaceholderNull = "gap_placeholder_null"
	ClassProvenByTest       = "proven_by_test"
	ClassUnproven           = "unproven"
)

var classes = []string{ClassVerified, ClassAcceptedOnly, ClassServedFailing, ClassServedBroken, ClassServedUnverified, ClassGapProvenNotServed, ClassGapStripeOnly, ClassGapPlaceholderNull, ClassProvenByTest, ClassUnproven}

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
	// Reason qualifies the class: ReasonRefusedOnly on a verified leaf says Stripe refused every
	// request that sent it, so that only its refusal is proven.
	Reason string `json:"reason,omitempty"`
}

// ReasonRefusedOnly is the reason of a leaf Stripe has only ever refused.
const ReasonRefusedOnly = "refused_only"

// ReasonAcceptedIgnored is the reason of a leaf the fake serves whose request Stripe accepted
// with no effect the suite could observe. It qualifies ClassUnproven alone, so such a leaf awaits
// proof like any other unproven leaf.
const ReasonAcceptedIgnored = "accepted_ignored"

// attributeReasons is the closed set of reasons `attributes.json` gives. A reason outside it can
// change what a class means, so a later fake's new one fails generation until the generator reads
// it.
var attributeReasons = []string{ReasonRefusedOnly, ReasonAcceptedIgnored, "accepted_only", "country_profile_missing", "needs_declaration", "not_proven", "operation_not_registered", "stripe_only"}

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
	Families       []Family        `json:"families,omitempty"`
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
	Unset *EvidenceUnset `json:"unset,omitempty"`
	// Map is, on an update leaf of a map, how the update treats the keys it leaves out and a key
	// it sends empty.
	Map        *EvidenceMap    `json:"map,omitempty"`
	Returned   json.RawMessage `json:"returned,omitempty"`
	EnumValues json.RawMessage `json:"enum_values,omitempty"`
	WriteOnly  json.RawMessage `json:"write_only,omitempty"`
	AcceptedIn json.RawMessage `json:"accepted_in,omitempty"`
	Refusals   []Refusal       `json:"refusals,omitempty"`
	// Unreachable is, on a proven request leaf, the values Stripe fails on every attempt, which the
	// suite therefore never sends.
	Unreachable []UnreachableValue `json:"unreachable_values,omitempty"`
	Initial     json.RawMessage    `json:"initial,omitempty"`
	Nullable    json.RawMessage    `json:"nullable,omitempty"`
	Failure     json.RawMessage    `json:"failure,omitempty"`
}

// UnreachableValue is a value of a request leaf that Stripe fails on every attempt, with the
// suite's reason.
type UnreachableValue struct {
	Value  string `json:"value"`
	Reason string `json:"reason"`
}

// Refusal is one refusal of a request that sent the leaf, on an account of a country. The message
// holds IDs and times as the placeholders `<id:prefix>` and `<time>`.
type Refusal struct {
	AccountCountry string   `json:"account_country"`
	Status         int      `json:"status"`
	Type           string   `json:"type"`
	Code           string   `json:"code"`
	Param          string   `json:"param"`
	Message        string   `json:"message"`
	Tests          []string `json:"tests"`
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

// EvidenceMap is what an update of a map does: `merged` keeps the keys it leaves out and
// `replaced` drops them, and a key sent empty (`by`) is then `removed` or `stored` as sent.
// Either is absent where no probe showed it.
type EvidenceMap struct {
	Update     string `json:"update,omitempty"`
	EmptyValue *struct {
		By     string `json:"by"`
		Result string `json:"result"`
	} `json:"empty_value,omitempty"`
}

// The values of EvidenceMap.
const (
	MapMerged   = "merged"
	MapReplaced = "replaced"
	MapRemoved  = "removed"
)

// EvidenceRule is a default that depends on the account or another argument.
type EvidenceRule struct {
	Object    string `json:"object"`
	Operation string `json:"operation"`
	Path      string `json:"path"`
	Rule      string `json:"rule"`
	// Beside is, on a rule that holds only beside another create leaf, that leaf: a create that
	// sends it reads the rule's value at Path, and one that does not reads as the others.
	Beside       string            `json:"beside,omitempty"`
	Observations []RuleObservation `json:"observations,omitempty"`
	Tests        []string          `json:"tests"`
}

// RuleObservation is what one probe of a rule held. The record's schema leaves the condition
// open, so a member other than these fails the decode, rather than a condition the generator
// cannot weigh passing for the plain create.
type RuleObservation struct {
	Condition struct {
		AccountCountry string `json:"account_country"`
		// Variant is the minimal create's variant, "" for the create with nothing else set.
		Variant string `json:"variant"`
		// Probe, on an answer (an observation of an answer rule), is the probe that sent Params,
		// to an object created with CreatedWith where the rule is an update's.
		Probe       string          `json:"probe,omitempty"`
		Params      json.RawMessage `json:"params,omitempty"`
		CreatedWith json.RawMessage `json:"created_with,omitempty"`
	} `json:"condition"`
	Value RuleValue `json:"value"`
}

// RuleValue is what a rule's observation held: a matcher of the response path on a default, or,
// on an answer, Stripe's status with what it read back (`read`) or its refusal.
type RuleValue struct {
	Matcher
	Status  int      `json:"status,omitempty"`
	Read    *Matcher `json:"read,omitempty"`
	Type    string   `json:"type,omitempty"`
	Code    string   `json:"code,omitempty"`
	Param   string   `json:"param,omitempty"`
	Message string   `json:"message,omitempty"`
}

// answer reports a rule whose observations are Stripe's answers to the requests its probes sent,
// which says when Stripe takes an argument rather than what it defaults to. A Beside rule's
// observations name their probes too, but hold what the response path held.
func (r EvidenceRule) answer() bool {
	return r.Beside == "" && len(r.Observations) > 0 && r.Observations[0].Condition.Probe != ""
}

// Matcher is what a response path held, in every form the format defines. Each kind carries the
// members it needs: `value` a literal, `sent` and `fingerprint` the request path, `from` the
// path, under `ref` where a probe's parent holds it, and `id`, `created` and `secret` a prefix.
type Matcher struct {
	Kind     string          `json:"kind"`
	Value    json.RawMessage `json:"value,omitempty"`
	Path     string          `json:"path,omitempty"`
	Prefix   *string         `json:"prefix,omitempty"`
	Pattern  string          `json:"pattern,omitempty"`
	Field    string          `json:"field,omitempty"`
	Ref      string          `json:"ref,omitempty"`
	Name     string          `json:"name,omitempty"`
	Returned json.RawMessage `json:"returned,omitempty"`
	Element  *Matcher        `json:"element,omitempty"`
	Count    *int            `json:"count,omitempty"`
	HasMore  *bool           `json:"has_more,omitempty"`
	// Days is how many days after the response's created a `day_within` or `time_within` lies.
	Days *int `json:"days,omitempty"`
}

// SideEffect is one update of an update probe and the paths it did not name whose literal
// value changed (P5).
type SideEffect struct {
	Object    string `json:"object"`
	Operation string `json:"operation"`
	Probe     string `json:"probe"`
	// Triggers are the request leaves the update sent, which the effect is known to follow.
	Triggers []string `json:"triggers"`
	// Carried are the leaves the update sent beside the triggers, with the values sent: the
	// effect is known to follow the triggers only with these beside them.
	Carried map[string]json.RawMessage `json:"carried,omitempty"`
	Changed []ChangedPath              `json:"changed"`
	Tests   []string                   `json:"tests"`
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

// Family is what a partial update of the family's objects does (P4). Hashes, where given, says
// per object and hash whether it keeps the siblings a partial update leaves out; a hash it does
// not list for an object is unknown, whatever NestedHashUpdate says of the family.
type Family struct {
	Family           string       `json:"family"`
	Objects          []string     `json:"objects"`
	NestedHashUpdate string       `json:"nested_hash_update"`
	Hashes           []HashUpdate `json:"hashes,omitempty"`
	ArrayUpdate      string       `json:"array_update"`
	MapKeyRemoval    string       `json:"map_key_removal"`
	ScalarUnset      string       `json:"scalar_unset"`
	Tests            []string     `json:"tests"`
}

// HashUpdate is what a partial update of one hash of one object did to the members it left out.
type HashUpdate struct {
	Object string `json:"object"`
	Hash   string `json:"hash"`
	Update string `json:"update"`
}

// The values of Discount.Returned the engine reads back: a discount ID at the top-level
// `discounts`, and the discount whole at a list element's.
const (
	DiscountID     = "discount_id"
	DiscountObject = "discount_object"
)

// The values of HashUpdate.Update. HashRefused is a hash Stripe refused to update in part, for a
// member the update left out.
const (
	HashSiblingsKept    = "siblings_kept"
	HashSiblingsCleared = "siblings_cleared"
	HashRefused         = "refused"
)

// Discount is what one form of a `discounts` element sent to one operation returned (P11): Path
// is the request's list of discounts the form is a member of, the top-level one or a line's, and
// ResponsePath where the response holds what Returned names, in the object HeldBy names where
// another object holds the discount.
type Discount struct {
	Object       string   `json:"object"`
	Operation    string   `json:"operation"`
	Path         string   `json:"path"`
	Form         string   `json:"form"`
	ResponsePath string   `json:"response_path"`
	HeldBy       string   `json:"held_by,omitempty"`
	Returned     string   `json:"returned"`
	Expanded     string   `json:"expanded"`
	Relation     string   `json:"relation"`
	Tests        []string `json:"tests"`
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
	// discountReturns are what a discount form returns: the discount's ID, the discount as a
	// hash in the holder's own shape, or the discount whole.
	discountReturns = []string{DiscountID, "discount_hash", DiscountObject}
	// discountRelations gives, by form, what the discount Stripe returns was made from.
	discountRelations = map[string]string{"coupon": "coupon", "discount": "existing_discount", "promotion_code": "promotion_code"}
	mapUpdates        = []string{MapMerged, MapReplaced}
	mapEmptyBy        = []string{"empty_string", "null"}
	mapEmptyResults   = []string{MapRemoved, "stored"}
	matcherKinds      = []string{"sent", "normalised", "value", "null", "absent", "id", "created", "ref", "account_id", "account", "from", "timestamp", "url", "secret", "list", "unchanged", "fingerprint", "day_within", "time_within"}
	hashUpdates       = []string{HashSiblingsKept, HashSiblingsCleared, HashRefused}
)

// minimalCreate is one variant of a minimal create in the record: what it sent and what each
// response path then held. Unlike a leaf's `default.variants`, which holds only the variants with
// a literal or none, it gives a value Stripe generates or copies from another argument.
type minimalCreate struct {
	Object    string             `json:"object"`
	Operation string             `json:"operation"`
	Status    int                `json:"status"`
	Params    any                `json:"params"`
	Response  map[string]Matcher `json:"response"`
}

// Pinned is the vendored evidence, read and checked against its pin and the spec's.
type Pinned struct {
	Pin        EvidencePin
	Attributes *Attributes
	Evidence   *Evidence
	// creates holds the record's minimal creates by object and operation.
	creates map[string][]minimalCreate
	// probed holds what the suite's create probes show of a leaf they left out, by key.
	probed map[Key][]evidencecheck.ProbedValue
	// classes, refused and leaves index the two files by key, built on first use.
	classes map[Key]string
	refused map[Key]bool
	leaves  map[Key]*EvidenceLeaf
}

const evidenceFormat = 1

// LoadEvidence reads spec/EVIDENCE_PIN.json, spec/attributes.json and spec/evidence.json under
// root. It returns nil when there is no pin: nothing is then verified, and every leaf not
// exposed today awaits proof. Either file is refused when it differs from the pin, when its API
// version or spec hash differs from spec/PIN.json, or when the two disagree on the suite commit
// and run, and the record is refused when its Stripe run did not pass (evidencecheck, which
// tools/evidencepin applies too).
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
	if err := evidencecheck.NamesRun("spec/EVIDENCE_PIN.json", p.Pin.SuiteRef, p.Pin.RunID); err != nil {
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
	pinnedSpec := evidencecheck.Spec{APIVersion: spec.APIVersion, SHA256: spec.SHA256}
	if err := evidencecheck.CheckAttributes(a.header(), pinnedSpec); err != nil {
		return nil, err
	}
	switch {
	case a.FakeVersion != p.Pin.FakeVersion:
		return nil, fmt.Errorf("spec/attributes.json is from fake %s, spec/EVIDENCE_PIN.json pins %s", a.FakeVersion, p.Pin.FakeVersion)
	case a.SuiteRef != p.Pin.SuiteRef || a.RunID != p.Pin.RunID:
		return nil, fmt.Errorf("spec/attributes.json names suite run %s/%d, spec/EVIDENCE_PIN.json pins %s/%d", a.SuiteRef, a.RunID, p.Pin.SuiteRef, p.Pin.RunID)
	}
	if err := a.check(); err != nil {
		return nil, err
	}

	evData, err := readPinned(root, "evidence.json", p.Pin.Evidence)
	if err != nil {
		return nil, err
	}
	if p.Evidence, err = decodeEvidence(evData); err != nil {
		return nil, fmt.Errorf("spec/evidence.json: %w", err)
	}
	e := p.Evidence
	if err := evidencecheck.CheckEvidence(e.header(), a.header(), pinnedSpec); err != nil {
		return nil, err
	}
	var creates []minimalCreate
	if len(e.MinimalCreates) > 0 {
		if err := json.Unmarshal(e.MinimalCreates, &creates); err != nil {
			return nil, fmt.Errorf("spec/evidence.json: minimal_creates: %w", err)
		}
	}
	p.creates = map[string][]minimalCreate{}
	for _, c := range creates {
		p.creates[c.Object+" "+c.Operation] = append(p.creates[c.Object+" "+c.Operation], c)
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
	if err := p.loadCreateProbes(root); err != nil {
		return nil, err
	}
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

// loadCreateProbes reads the digest of the suite's create probes, which must be of the pinned
// suite commit. It is not optional: without it nothing checks a default statement against them.
func (p *Pinned) loadCreateProbes(root string) error {
	data, err := readPinned(root, "create_probes.json", p.Pin.CreateProbes)
	if err != nil {
		return err
	}
	var digest evidencecheck.CreateProbes
	if err := json.Unmarshal(data, &digest); err != nil {
		return fmt.Errorf("decode spec/create_probes.json: %w", err)
	}
	if digest.SuiteRef != p.Pin.SuiteRef {
		return fmt.Errorf("spec/create_probes.json is of suite commit %s, spec/EVIDENCE_PIN.json pins %s", digest.SuiteRef, p.Pin.SuiteRef)
	}
	p.probed = map[Key][]evidencecheck.ProbedValue{}
	for _, l := range digest.Leaves {
		p.probed[Key{Object: l.Object, Side: SideRequest, Operation: l.Operation, Path: l.Path}] = l.Held
	}
	return nil
}

// check refuses a class or a reason outside its closed set, and a key listed twice.
func (a *Attributes) check() error {
	seen := map[Key]bool{}
	for _, l := range a.Leaves {
		if !slices.Contains(classes, l.Class) {
			return fmt.Errorf("spec/attributes.json: %s has the unknown class %q", l.Key, l.Class)
		}
		if l.Reason != "" && !slices.Contains(attributeReasons, l.Reason) {
			return fmt.Errorf("spec/attributes.json: %s has the unknown reason %q", l.Key, l.Reason)
		}
		if l.Reason == ReasonAcceptedIgnored && l.Class != ClassUnproven {
			return fmt.Errorf("spec/attributes.json: %s has the reason %q on the class %q; the generator reads it only on %q", l.Key, l.Reason, l.Class, ClassUnproven)
		}
		if seen[l.Key] {
			return fmt.Errorf("spec/attributes.json lists %s twice", l.Key)
		}
		seen[l.Key] = true
	}
	return nil
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
		if d.Kind == "rule" && e.Rules[*d.RuleRef].answer() {
			return fmt.Errorf("%s has for its default rule %d, which answers probes and words no default", k, *d.RuleRef)
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
		if m := l.Map; m != nil {
			switch {
			case l.Side != SideRequest || l.Operation == "" || !slices.Contains(l.On, "update"):
				errs = append(errs, fmt.Errorf("%s: a map's update on a leaf that is not an update's", l.Key))
			case m.Update != "":
				errs = append(errs, unknown(l.Key.String()+": map update", m.Update, mapUpdates))
			}
			if v := m.EmptyValue; v != nil {
				errs = append(errs, unknown(l.Key.String()+": map empty value by", v.By, mapEmptyBy), unknown(l.Key.String()+": map empty value result", v.Result, mapEmptyResults))
			}
		}
		errs = append(errs, l.checkUnreachable(), e.checkReturnedRule(l))
		if err := errors.Join(errs...); err != nil {
			return err
		}
	}
	for _, s := range e.SideEffects {
		for leaf, v := range s.Carried {
			if leaf == "" || len(v) == 0 || slices.Contains(s.Triggers, leaf) {
				return fmt.Errorf("side effect %s %s %s: carried %q is empty or one of its triggers", s.Object, s.Operation, s.Probe, leaf)
			}
		}
	}
	for _, r := range e.Rules {
		if err := r.checkBeside(); err != nil {
			return fmt.Errorf("%s %s %s: %w", r.Object, r.Operation, r.Path, err)
		}
		for _, o := range r.Observations {
			if err := r.checkObservation(o); err != nil {
				return fmt.Errorf("%s %s %s: observation: %w", r.Object, r.Operation, r.Path, err)
			}
		}
	}
	for _, f := range e.Families {
		seen := map[[2]string]bool{}
		for _, h := range f.Hashes {
			if err := unknown(f.Family+" "+h.Object+" "+h.Hash+": hash update", h.Update, hashUpdates); err != nil {
				return err
			}
			switch {
			case !slices.Contains(f.Objects, h.Object):
				return fmt.Errorf("family %s gives hash %s of %q, which is not one of its objects", f.Family, h.Hash, h.Object)
			case h.Hash == "":
				return fmt.Errorf("family %s gives a hash of %s with no path", f.Family, h.Object)
			case seen[[2]string{h.Object, h.Hash}]:
				return fmt.Errorf("family %s gives hash %s of %s twice", f.Family, h.Hash, h.Object)
			}
			seen[[2]string{h.Object, h.Hash}] = true
		}
	}
	for _, d := range e.Discounts {
		at := d.Object + " " + d.Operation + " " + d.Path
		if err := errors.Join(unknown(at+": discounts form", d.Form, discountForms), unknown(at+": discounts returned", d.Returned, discountReturns)); err != nil {
			return err
		}
		switch {
		case d.Relation != discountRelations[d.Form]:
			return fmt.Errorf("%s: the form %s returns a discount of relation %q, not %q", at, d.Form, d.Relation, discountRelations[d.Form])
		case d.Path != "discounts" && !strings.HasSuffix(d.Path, ".discounts") || d.ResponsePath == "":
			return fmt.Errorf("%s: a discounts entry names no list of discounts, or no response path", at)
		}
	}
	return nil
}

// checkReturnedRule refuses a leaf's `returned.rule_ref` that names no Beside rule of its own
// object, operation and path: the form it gives holds only for a create without the rule's
// beside leaf.
func (e *Evidence) checkReturnedRule(l EvidenceLeaf) error {
	ref := l.returnedRule()
	if ref == nil {
		return nil
	}
	if *ref < 0 || *ref >= len(e.Rules) {
		return fmt.Errorf("%s: returned names rule %d, which the record does not hold", l.Key, *ref)
	}
	if r := e.Rules[*ref]; r.Beside == "" || r.Object != l.Object || r.Operation != l.Operation || r.Path != l.Path {
		return fmt.Errorf("%s: returned names rule %d, which is not a beside rule of the leaf", l.Key, *ref)
	}
	return nil
}

// returnedRule is the index of the Beside rule a leaf's `returned` names, or nil.
func (l EvidenceLeaf) returnedRule() *int {
	var returned struct {
		RuleRef *int `json:"rule_ref"`
	}
	if l.Returned == nil || json.Unmarshal(l.Returned, &returned) != nil {
		return nil
	}
	return returned.RuleRef
}

// checkBeside refuses a Beside rule none of whose observations sends its beside leaf, or all of
// which do: the rule says what a create reads beside the leaf and what one without it reads.
func (r EvidenceRule) checkBeside() error {
	if r.Beside == "" {
		return nil
	}
	with := 0
	for _, o := range r.Observations {
		var params any
		if err := json.Unmarshal(o.Condition.Params, &params); err != nil {
			return fmt.Errorf("beside rule observation %s: params: %w", o.Condition.Probe, err)
		}
		leaf, _, _ := strings.Cut(r.Beside, "@")
		flat, _ := params.(map[string]any)
		if _, named := flat[leaf]; named || sends(params, strings.Split(leaf, ".")) {
			with++
		}
	}
	if with == 0 || with == len(r.Observations) {
		return fmt.Errorf("beside rule on %q has no observation both with and without it", r.Beside)
	}
	return nil
}

// checkUnreachable refuses unreachable values the generator cannot use: on a leaf other than a
// proven request leaf, with no reason, listed twice, or among the values the suite sent.
func (l EvidenceLeaf) checkUnreachable() error {
	if len(l.Unreachable) == 0 {
		return nil
	}
	if l.Side != SideRequest || l.Status != "proven" {
		return fmt.Errorf("%s: unreachable values on a leaf that is not a proven request leaf", l.Key)
	}
	var sent []struct {
		Value json.RawMessage `json:"value"`
	}
	if len(l.EnumValues) > 0 {
		if err := json.Unmarshal(l.EnumValues, &sent); err != nil {
			return fmt.Errorf("%s: enum_values: %w", l.Key, err)
		}
	}
	sentValues := map[string]bool{}
	for _, e := range sent {
		sentValues[string(e.Value)] = true
	}
	seen := map[string]bool{}
	for _, u := range l.Unreachable {
		quoted, err := json.Marshal(u.Value)
		if err != nil {
			return err
		}
		switch {
		case strings.TrimSpace(u.Reason) == "":
			return fmt.Errorf("%s: unreachable value %q has no reason", l.Key, u.Value)
		case seen[u.Value]:
			return fmt.Errorf("%s: unreachable value %q is listed twice", l.Key, u.Value)
		case sentValues[string(quoted)]:
			return fmt.Errorf("%s: unreachable value %q is among the values the suite sent", l.Key, u.Value)
		}
		seen[u.Value] = true
	}
	return nil
}

// checkObservation refuses an observation of a form the generator cannot weigh: answers beside a
// default's observations in one rule, either holding the other's members, an answer with no
// params or with another status than a success or a refusal, a refusal with no message, an
// acceptance that also refuses, and a matcher kind outside the format's.
func (r EvidenceRule) checkObservation(o RuleObservation) error {
	c, v := o.Condition, o.Value
	if r.Beside != "" {
		switch {
		case c.Probe == "" || c.Params == nil:
			return errors.New("a beside rule's observation names no probe or params")
		case c.Variant != "" || c.CreatedWith != nil || v.Status != 0 || v.Read != nil || v.Message != "" || v.Type != "" || v.Code != "" || v.Param != "":
			return fmt.Errorf("beside rule observation %s holds an answer's members", c.Probe)
		case !slices.Contains(matcherKinds, v.Kind):
			return fmt.Errorf("kind %q is not one the generator knows (%s)", v.Kind, strings.Join(matcherKinds, ", "))
		}
		return nil
	}
	if (c.Probe != "") != r.answer() {
		return errors.New("a rule mixes answers to probes with a default's observations")
	}
	if c.Probe == "" {
		if c.Params != nil || c.CreatedWith != nil || v.Status != 0 || v.Read != nil || v.Message != "" || v.Type != "" || v.Code != "" || v.Param != "" {
			return errors.New("a default's observation holds an answer's members")
		}
		if !slices.Contains(matcherKinds, v.Kind) {
			return fmt.Errorf("kind %q is not one the generator knows (%s)", v.Kind, strings.Join(matcherKinds, ", "))
		}
		return nil
	}
	switch {
	case c.Variant != "" || v.Kind != "":
		return fmt.Errorf("answer %s holds a default's members", c.Probe)
	case c.Params == nil:
		return fmt.Errorf("answer %s holds no params", c.Probe)
	case v.Status == http.StatusOK:
		if v.Message != "" || v.Type != "" || v.Code != "" || v.Param != "" {
			return fmt.Errorf("answer %s accepts and refuses", c.Probe)
		}
		if v.Read != nil && !slices.Contains(matcherKinds, v.Read.Kind) {
			return fmt.Errorf("answer %s reads back kind %q, not one the generator knows (%s)", c.Probe, v.Read.Kind, strings.Join(matcherKinds, ", "))
		}
	case v.Status >= 400 && v.Status < 500:
		if v.Message == "" || v.Read != nil {
			return fmt.Errorf("answer %s refuses with no message", c.Probe)
		}
	default:
		return fmt.Errorf("answer %s has status %d", c.Probe, v.Status)
	}
	return nil
}

// header is the part of the file evidencecheck reads.
func (a *Attributes) header() evidencecheck.Attributes {
	return evidencecheck.Attributes{FakeVersion: a.FakeVersion, SuiteRef: a.SuiteRef, RunID: a.RunID, APIVersion: a.APIVersion, SpecSHA256: a.SpecSHA256}
}

// header is the part of the record evidencecheck reads.
func (e *Evidence) header() evidencecheck.Evidence {
	h := evidencecheck.Evidence{SuiteCommit: e.SuiteCommit, APIVersion: e.APIVersion, SpecSHA256: e.SpecSHA256}
	h.StripeRun.ID, h.StripeRun.Passed = e.StripeRun.ID, e.StripeRun.Passed
	return h
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
