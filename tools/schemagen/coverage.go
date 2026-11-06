package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"regexp"
	"slices"
	"sort"
	"strings"

	"github.com/computomatic/terraform-provider-stripe/internal/fields"
	"github.com/computomatic/terraform-provider-stripe/tools/internal/evidencecheck"
)

// State is where a spec leaf stands in the provider (ADR 2026-10-29-full-attribute-coverage,
// D1, "The coverage report").
type State string

const (
	StateExposed  State = "exposed"
	StateExcluded State = "excluded"
	// StateAwaiting is a leaf that waits for the pinned fake release to verify it.
	StateAwaiting State = "awaiting_proof"
)

// The exclusion rules of D1, and the register.
const (
	RuleControl    = "X1"
	RuleTypeMarker = "X2"
	RulePagedList  = "X3"
	RuleRecursion  = "X4"
	RuleConfirm    = "X5"
	// RuleAcceptedOnly leaves out a leaf Stripe accepts and never returns.
	RuleAcceptedOnly = "X6"
	RuleRegister     = "register"
)

var ruleText = map[string]string{
	RuleControl:      "request controls: top-level `expand` and `include`",
	RuleTypeMarker:   "type markers: a response hash with no keys that no request writes",
	RulePagedList:    "paged lists returned only when expanded: a list envelope the object does not require and no request writes",
	RuleRecursion:    "the recursion cut: a schema met again on its own path, where it has no `id` to hold",
	RuleConfirm:      "`confirm` where the declaration's `status` transition confirms",
	RuleAcceptedOnly: "accepted only: Stripe takes it and never returns it, so a user could neither read it back nor see drift",
}

// Record is one spec leaf of one table in internal/generated/coverage.json.
type Record struct {
	Table string `json:"table"`
	Key
	Shape    string `json:"shape"`
	Target   string `json:"target,omitempty"`
	Required bool   `json:"required,omitempty"`
	State    State  `json:"state"`
	// Rule is the exclusion rule or `register`. An exposed leaf with a rule is one exposed
	// today that the rule removes before v0.1.0 (D6, Step 6).
	Rule string `json:"rule,omitempty"`
	// Reason is the register entry's reason, or the AcceptedOnly entry's that keeps a leaf X6
	// would leave out.
	Reason string `json:"reason,omitempty"`
	// Class is the leaf's class in the pinned spec/attributes.json.
	Class string `json:"class,omitempty"`
	// PagedBy is the type that reads every element of a list Stripe returns a page at a time
	// (Resource.PagedLists).
	PagedBy string `json:"paged_by,omitempty"`
	// Refused says that Stripe refused every request that sent the leaf, whose refusal the
	// field's docs line states.
	Refused bool `json:"refused,omitempty"`
	// Tests are the suite's leaf tests that prove it, from the pinned spec/evidence.json.
	Tests   []string       `json:"tests,omitempty"`
	Default *DefaultRecord `json:"default,omitempty"`
	// SpecDefault is the value the spec's "Defaults to" sentence names where the evidence's
	// default differs, for the suite to check; the spec's text is never a source (D4).
	SpecDefault string `json:"spec_default,omitempty"`
}

// DefaultRecord is an optional argument's default statement (D4) and where it comes from: the
// evidence, the provider for an argument only the update takes, which it never sends on
// create, or the declaration that words its rule (Declaration.Defaults). Varies marks a default
// the variants of the minimal create give differently, which has no statement until the
// declaration words it.
type DefaultRecord struct {
	Statement string `json:"statement,omitempty"`
	Source    string `json:"source"`
	Varies    bool   `json:"varies,omitempty"`
}

// defaultRecord is the report's record of a default.
func defaultRecord(d *fields.Default) *DefaultRecord {
	if d == nil {
		return nil
	}
	source := "evidence"
	if d.Kind == fields.DefaultNotSent {
		source = "provider"
	}
	return &DefaultRecord{Statement: d.Statement(), Source: source, Varies: d.Kind == fields.DefaultVaries}
}

// tableCoverage is what one table contributes to the report.
type tableCoverage struct {
	object  string
	ops     []string
	records []Record
	table   []fields.Field
	leaves  []Leaf
}

// coverTable walks one table, applies the rules and the register, builds the table through
// the gate and gives every leaf its state.
func coverTable(spec *Spec, res Resource, ev *Pinned) (*tableCoverage, error) {
	object, leaves, err := walkTable(spec, res)
	if err != nil {
		return nil, err
	}
	tc := &tableCoverage{object: object, leaves: leaves}
	var create, update string
	for _, o := range []struct {
		path string
		id   *string
	}{{res.CreatePath, &create}, {res.UpdatePath, &update}} {
		if o.path == "" {
			continue
		}
		op, err := spec.operation("POST", o.path)
		if err != nil {
			return nil, err
		}
		*o.id = op.OperationID
		tc.ops = append(tc.ops, op.OperationID)
	}

	written := map[string]bool{}
	for _, l := range leaves {
		if l.Side == SideRequest {
			for p := l.Path; p != ""; p = parentPath(p) {
				written[p] = true
			}
		}
	}
	register := map[string]bool{}
	for p, reason := range res.Exclude {
		if strings.TrimSpace(reason) == "" {
			return nil, fmt.Errorf("%s: register entry %q has no reason", res.Package, p)
		}
		register[p] = false
	}
	for p, reason := range res.AcceptedOnly {
		if strings.TrimSpace(reason) == "" {
			return nil, fmt.Errorf("%s: accepted-only entry %q has no reason", res.Package, p)
		}
	}

	records := make([]Record, len(leaves))
	g := &gate{}
	for i, l := range leaves {
		r := Record{Table: res.Package, Key: l.Key, Shape: l.Shape(), Target: l.Target, Required: l.Required && l.Side == SideRequest}
		r.Rule = rule(spec, res, l, written)
		for p := l.Path; p != "" && r.Rule == ""; p = parentPath(p) {
			for _, entry := range []string{p, l.Operation + " " + p} {
				if _, ok := register[entry]; ok && r.Rule == "" {
					register[entry] = true
					r.Rule, r.Reason = RuleRegister, res.Exclude[entry]
				}
			}
		}
		if ev != nil {
			r.Class = ev.class(l.Key)
			if r.Class == "" && l.Kind == KindEmbedded {
				r.Class = ev.copyClass(l.Key)
			}
			if e := ev.leaf(l.Key); e != nil && e.Status == "proven" {
				r.Tests = e.Tests
			}
			r.Refused = ev.refusedOnly(l.Key)
		}
		if r.Class == ClassAcceptedOnly && r.Rule == "" {
			if reason, ok := res.AcceptedOnly[l.Path]; ok {
				r.Reason = reason
			} else if reason, ok := res.AcceptedOnly[l.Operation+" "+l.Path]; ok {
				r.Reason = reason
			} else {
				r.Rule = RuleAcceptedOnly
			}
		}
		if exposable(r.Class) && r.Rule == "" {
			switch l.Operation {
			case "":
				g.response = append(g.response, l.At)
			case create:
				g.create = append(g.create, l.At)
			default:
				g.update = append(g.update, l.At)
			}
		}
		records[i] = r
	}
	for p, met := range register {
		if !met {
			return nil, fmt.Errorf("%s: register entry %q names no leaf", res.Package, p)
		}
	}

	if ev != nil {
		ev.facts(res, object, create, update, leaves, g)
	}
	g.dropCreate, g.dropUpdate = dropped(leaves, records, create), dropped(leaves, records, update)
	baseline, err := Build(spec, res)
	if err != nil {
		return nil, err
	}
	tc.table = baseline
	if len(g.create)+len(g.update)+len(g.response)+len(g.dropCreate)+len(g.dropUpdate) > 0 {
		if tc.table, err = buildGated(spec, res, g); err != nil {
			return nil, err
		}
	}
	before, after := index(baseline), index(tc.table)
	if err := coversTable(res, create, update, leaves, after); err != nil {
		return nil, err
	}
	if err := pagedLists(res, leaves, records); err != nil {
		return nil, err
	}

	for i, l := range leaves {
		r := &records[i]
		switch {
		case exposed(create, l, after):
			r.State = StateExposed
			if r.Rule == RuleRegister {
				return nil, fmt.Errorf("%s: register entry for %s removes a leaf exposed today", res.Package, l.Key)
			}
		case r.Rule != "":
			r.State = StateExcluded
		case exposable(r.Class):
			return nil, fmt.Errorf("%s: %s is verified, but the generator cannot hold it in the table yet", res.Package, l.Key)
		case ev == nil || r.Class != "":
			r.State = StateAwaiting
		}
	}
	if err := checkBesideReturns(res.Package, leaves, records, ev); err != nil {
		return nil, err
	}
	if err := checkKeptDiscounts(res, leaves, records, ev); err != nil {
		return nil, err
	}
	if err := checkHashUnsets(res, object, update, tc.table, ev); err != nil {
		return nil, err
	}
	if err := markTogether(res, object, create, update, tc.table, leaves, records, ev); err != nil {
		return nil, err
	}
	for i, l := range leaves {
		r := &records[i]
		if d := defaultOf(l, *r, create, leaves, records, ev); d != nil {
			r.Default = defaultRecord(d)
			if f := fieldAt(tc.table, l.At); f != nil {
				r.SpecDefault = disagreement(f.Description, d)
			}
		}
	}
	if err := annotate(tc.table, leaves, records, create, unsetFor(res), ev); err != nil {
		return nil, fmt.Errorf("%s: %w", res.Package, err)
	}
	if err := newRequired(create, leaves, records, before); err != nil {
		return nil, fmt.Errorf("%s: %w", res.Package, err)
	}
	tc.records = records
	return tc, nil
}

// fieldAt returns the field of a table at a dotted path of attribute names, or nil.
func fieldAt(table []fields.Field, path string) *fields.Field {
	name, rest, nested := strings.Cut(path, ".")
	for i := range table {
		if table[i].AttrName() != name {
			continue
		}
		if !nested {
			return &table[i]
		}
		return fieldAt(table[i].Fields, rest)
	}
	return nil
}

// exposable reports a class the gate lets through: verified, or accepted only where the rule that
// leaves those out has not, because Resource.AcceptedOnly allows the leaf.
func exposable(class string) bool {
	return class == ClassVerified || class == ClassAcceptedOnly
}

// dropped is the table paths that rule X6 takes out of the request of one operation: those with
// a leaf the rule leaves out and no other on or below them.
func dropped(leaves []Leaf, records []Record, operation string) map[string]bool {
	out, stays := map[string]bool{}, map[string]bool{}
	for i, l := range leaves {
		if l.Side != SideRequest || l.Operation != operation {
			continue
		}
		into := stays
		if records[i].Rule == RuleAcceptedOnly {
			into = out
		}
		for p := l.At; p != ""; p = parentPath(p) {
			into[p] = true
		}
	}
	for p := range stays {
		delete(out, p)
	}
	return out
}

// annotate records on the table's fields the default of each optional argument an exposed
// request leaf holds, for its docs line (D4), Stripe's refusal of each argument it has only
// ever refused, which it fails on where the record holds none, the values of an argument
// Stripe fails on every attempt, and how an update of a map treats the keys it leaves out and a
// key sent empty (the record's `map`). The create's default comes before an update's `not sent`.
func annotate(table []fields.Field, leaves []Leaf, records []Record, create, unsetBy string, ev *Pinned) error {
	refused, answered := map[string][]refusedOn{}, map[string][]refusedOn{}
	unreachable := map[string][]UnreachableValue{}
	var paths, answeredPaths, unreachablePaths []string
	for i, l := range leaves {
		r := records[i]
		if l.Side != SideRequest || r.State != StateExposed {
			continue
		}
		f := fieldAt(table, l.At)
		if f == nil {
			continue
		}
		if f.Shape == fields.StringMap && l.Operation != create {
			if e := ev.leaf(l.Key); e != nil && e.Status == "proven" {
				m, err := mapUpdate(l.Key, e, unsetBy)
				if err != nil {
					return err
				}
				f.MapUpdate = m
			}
		}
		if r.Refused {
			on := "update"
			if l.Operation == create {
				on = "create"
			}
			e := ev.leaf(l.Key)
			if len(e.Refusals) == 0 {
				return fmt.Errorf("%s has the reason %s, but the record holds no refusal of it", l.Key, ReasonRefusedOnly)
			}
			if _, seen := refused[l.At]; !seen {
				paths = append(paths, l.At)
			}
			for _, x := range e.Refusals {
				refused[l.At] = append(refused[l.At], refusedOn{on: on, country: x.AccountCountry, message: x.Message})
			}
		}
		if e := ev.leaf(l.Key); e != nil {
			for _, u := range e.Unreachable {
				if !slices.Contains(f.Enum, u.Value) {
					return fmt.Errorf("%s: the record marks %q unreachable, which is not one of the argument's documented values", l.Key, u.Value)
				}
				if _, seen := unreachable[l.At]; !seen {
					unreachablePaths = append(unreachablePaths, l.At)
				}
				if !slices.Contains(unreachable[l.At], u) {
					unreachable[l.At] = append(unreachable[l.At], u)
				}
			}
		}
		if obs := ev.answers(l.Key); len(obs) > 0 {
			on := "update"
			if l.Operation == create {
				on = "create"
			}
			if _, seen := answered[l.At]; !seen {
				answeredPaths = append(answeredPaths, l.At)
			}
			for _, o := range obs {
				if o.Value.Status != http.StatusOK {
					answered[l.At] = append(answered[l.At], refusedOn{on: on, country: o.Condition.AccountCountry, message: o.Value.Message})
				}
			}
		}
		if f.Required {
			continue
		}
		if d := defaultOf(l, r, create, leaves, records, ev); d != nil && (f.Default == nil || l.Operation == create) {
			f.Default = d
		}
	}
	for _, at := range paths {
		s, err := refusedSentence("Stripe refused it", refused[at])
		if err != nil {
			return fmt.Errorf("%s: %w", at, err)
		}
		fieldAt(table, at).Refused = s
	}
	for _, at := range answeredPaths {
		if len(answered[at]) == 0 {
			return fmt.Errorf("%s: the record's answers to it hold no refusal", at)
		}
		s, err := refusedSentence("Elsewhere Stripe refused it", answered[at])
		if err != nil {
			return fmt.Errorf("%s: %w", at, err)
		}
		fieldAt(table, at).Answer = s
	}
	for _, at := range unreachablePaths {
		sentences := make([]string, len(unreachable[at]))
		for i, u := range unreachable[at] {
			sentences[i] = fmt.Sprintf("Stripe fails %s on every attempt, so the conformance suite never sends it: %q.", fields.Code(u.Value), strings.TrimSuffix(u.Reason, "."))
		}
		fieldAt(table, at).Unreachable = strings.Join(sentences, " ")
	}
	return nil
}

// refusedOn is one recorded refusal of an argument: on which request, on an account of which
// country, and Stripe's message.
type refusedOn struct{ on, country, message string }

// refusedSentence words an argument's recorded refusals for its docs line, one sentence per
// message, in the order the record gives them, each led by lead.
func refusedSentence(lead string, all []refusedOn) (string, error) {
	type condition struct{ on, countries []string }
	conditions := map[string]*condition{}
	var messages []string
	for _, r := range all {
		m, err := refusalMessage(r.message)
		if err != nil {
			return "", err
		}
		c := conditions[m]
		if c == nil {
			c = &condition{}
			conditions[m] = c
			messages = append(messages, m)
		}
		if !slices.Contains(c.on, r.on) {
			c.on = append(c.on, r.on)
		}
		if !slices.Contains(c.countries, r.country) {
			c.countries = append(c.countries, r.country)
		}
	}
	sentences := make([]string, len(messages))
	for i, m := range messages {
		c := conditions[m]
		accounts := make([]string, len(c.countries))
		for j, country := range c.countries {
			accounts[j] = articleFor(country) + " " + country
		}
		sentences[i] = fmt.Sprintf("%s on %s account on %s: %q.", lead, strings.Join(accounts, " and "), strings.Join(c.on, " and "), m)
	}
	return strings.Join(sentences, " "), nil
}

// refusalMessage is a recorded message as the docs quote it, with its placeholders in words. A
// placeholder the record's format does not define fails, so that the docs never show one.
func refusalMessage(m string) (string, error) {
	m = strings.ReplaceAll(idPlaceholder.ReplaceAllString(m, "$1..."), "<time>", "a time")
	if p := anyPlaceholder.FindString(m); p != "" {
		return "", fmt.Errorf("the refusal %q holds the placeholder %s, which the generator cannot word", m, p)
	}
	return m, nil
}

var (
	idPlaceholder  = regexp.MustCompile(`<id:([a-zA-Z0-9_]+)>`)
	anyPlaceholder = regexp.MustCompile(`<[^ >]+>`)
)

// defaultOf is the default of an exposed optional request leaf (D4): `not sent` for one only
// the update takes, which needs no evidence, or else what the pinned evidence records for a
// verified one. A leaf only the update takes is an optional argument even where its hash in the
// update requires it, because the create never sends it, and so is one whose create leaf is not
// exposed: rule X6 leaves it out, or the gate exposes only the update's.
func defaultOf(l Leaf, r Record, create string, leaves []Leaf, records []Record, ev *Pinned) *fields.Default {
	if l.Side != SideRequest || r.State != StateExposed {
		return nil
	}
	if l.Operation != create {
		onCreate := false
		for i, c := range leaves {
			if c.Operation == create && c.At == l.At && records[i].State == StateExposed {
				onCreate = true
			}
		}
		if create == "" || !onCreate {
			return &fields.Default{Kind: fields.DefaultNotSent}
		}
		return nil
	}
	if r.Required || !exposable(r.Class) {
		return nil
	}
	return ev.leafDefault(l.Key, l.Lists)
}

// elementValue is the value every element of a value given per element of lists nested lists
// holds, or false where the elements differ or there are none.
func elementValue(v json.RawMessage, lists int) (json.RawMessage, bool) {
	for range lists {
		var items []json.RawMessage
		if json.Unmarshal(v, &items) != nil || len(items) == 0 {
			return nil, false
		}
		for _, item := range items[1:] {
			if !bytes.Equal(compact(item), compact(items[0])) {
				return nil, false
			}
		}
		v = items[0]
	}
	return v, true
}

// compact is v without insignificant space, or v where it is not JSON.
func compact(v json.RawMessage) []byte {
	var b bytes.Buffer
	if json.Compact(&b, v) != nil {
		return v
	}
	return b.Bytes()
}

// annotateCopies records on each embedded copy the table holds in full the data source type
// that reads the copied object: the one table whose object it is. A copy of an object no table,
// or several, holds has none, and so has one the table holds as its ID.
func annotateCopies(tc *tableCoverage, readers map[string][]string) {
	for i, l := range tc.leaves {
		if l.Kind != KindEmbedded || l.Side != SideResponse || tc.records[i].State != StateExposed || len(readers[l.Target]) != 1 {
			continue
		}
		if f := fieldAt(tc.table, l.At); f != nil && (f.Shape == fields.Object || f.Shape == fields.ObjectList || f.Shape == fields.ObjectMap) {
			f.Copy = readers[l.Target][0]
		}
	}
}

// rule returns the exclusion rule that leaves a leaf out, or "". The recursion cut leaves out
// only a repeat with no `id`: one with an ID is held as it (D3).
func rule(spec *Spec, res Resource, l Leaf, written map[string]bool) string {
	switch {
	case l.Side == SideRequest && (l.Path == "expand" || l.Path == "include"):
		return RuleControl
	case l.Side == SideResponse && l.Kind == KindEmptyHash && !written[l.Path]:
		return RuleTypeMarker
	case l.Side == SideResponse && l.Kind == KindPagedList && !l.Required && !written[l.Path]:
		return RulePagedList
	case l.Kind == KindRecursion && prop(spec.Components.Schemas[l.Target], "id") == nil:
		return RuleRecursion
	case l.Side == SideRequest && res.StatusConfirms && l.Path == "confirm":
		return RuleConfirm
	}
	return ""
}

// tableIndex is a table's fields by dotted attribute path: as the resource holds them, and as
// the data source reads them, which takes the response's shape where it differs (D3).
type tableIndex struct {
	request, response map[string]held
}

// held is one field of a table.
type held struct {
	create, update, response bool
	shape                    fields.Shape
	nested                   bool
}

func index(table []fields.Field) tableIndex {
	flat := func(table []fields.Field) map[string]held {
		out := map[string]held{}
		var walk func([]fields.Field, string)
		walk = func(fs []fields.Field, prefix string) {
			for _, f := range fs {
				p := join(prefix, f.AttrName())
				out[p] = held{create: f.Create, update: f.Update, response: f.Response, shape: f.Shape, nested: len(f.Fields) > 0}
				walk(f.Fields, p)
			}
		}
		walk(table, "")
		return out
	}
	return tableIndex{request: flat(table), response: flat(fields.ForDataSource(table))}
}

// whole reports a leaf that stands for everything below it: a copy, a list, a recursion or
// a reference.
func whole(k Kind) bool {
	return k == KindEmbedded || k == KindPagedList || k == KindRecursion || k == KindReference
}

// exposed reports whether the table holds a leaf on its side and in its union branch.
func exposed(create string, l Leaf, idx tableIndex) bool {
	table := idx.request
	if l.Side == SideResponse {
		table = idx.response
	}
	h, ok := table[l.At]
	// A request hash with no members is held by an object, whose members, if any, are the
	// response's.
	if !ok || h.nested && !whole(l.Kind) && l.Kind != KindEmptyHash {
		return false
	}
	switch {
	case l.Operation == "" && !h.response, l.Operation != "" && l.Operation == create && !h.create, l.Operation != "" && l.Operation != create && !h.update:
		return false
	}
	return holdsBranch(l, table)
}

// holdsBranch reports whether the field at a leaf's union holds the leaf's branch: a string,
// number or boolean field the scalar branches it can carry, an object the hash branch or a
// response's merged variants, and `<name>_data` the hash form of a token-or-hash parameter.
func holdsBranch(l Leaf, table map[string]held) bool {
	switch {
	case l.Branch == "":
		return true
	case l.PointAt == "":
		// The hash of Resource.CreateHash, spread into the top level.
		return l.Branch == "object"
	case strings.HasPrefix(l.At, l.PointAt+"_data."):
		return true
	}
	p, ok := table[l.PointAt]
	if !ok {
		return false
	}
	switch l.Branch {
	case "string":
		return p.shape == fields.String
	case "integer":
		return p.shape == fields.Int || p.shape == fields.String
	case "number":
		return p.shape == fields.Float || p.shape == fields.String
	case "boolean":
		return p.shape == fields.Bool || p.shape == fields.String
	case "array":
		return p.shape == fields.ObjectList || p.shape == fields.ScalarList || p.shape == fields.ScalarSet
	}
	// The hash branch of a request, or a response variant named by its `object` or schema.
	return p.shape == fields.Object
}

// coversTable checks that every field the table holds is a leaf of the walk, or below a leaf
// that stands for its whole subtree, so that the report cannot miss an attribute.
func coversTable(res Resource, create, update string, leaves []Leaf, idx tableIndex) error {
	at := map[string]Kind{}
	for _, l := range leaves {
		at[string(l.Side)+" "+l.Operation+" "+l.At] = l.Kind
	}
	found := func(op, p string) bool {
		side := SideRequest
		if op == "" {
			side = SideResponse
		}
		if _, ok := at[string(side)+" "+op+" "+p]; ok {
			return true
		}
		for q := parentPath(p); q != ""; q = parentPath(q) {
			if k, ok := at[string(side)+" "+op+" "+q]; ok && whole(k) {
				return true
			}
		}
		return false
	}
	check := func(table map[string]held, sides func(held) []struct {
		on bool
		op string
	}) error {
		paths := make([]string, 0, len(table))
		for p := range table {
			paths = append(paths, p)
		}
		sort.Strings(paths)
		for _, p := range paths {
			h := table[p]
			if h.nested {
				continue
			}
			for _, side := range sides(h) {
				if side.on && !found(side.op, p) {
					return fmt.Errorf("%s: the table holds %s (operation %q), which the leaf walk does not reach", res.Package, p, side.op)
				}
			}
		}
		return nil
	}
	type side = struct {
		on bool
		op string
	}
	if err := check(idx.request, func(h held) []side { return []side{{h.create, create}, {h.update, update}} }); err != nil {
		return err
	}
	return check(idx.response, func(h held) []side { return []side{{h.response, ""}} })
}

// pagedLists checks Resource.PagedLists (D3): every list envelope the object requires, which
// Stripe returns a page at a time on every read, names the type that reads every element, and
// every entry names such a list. The report records the type.
func pagedLists(res Resource, leaves []Leaf, records []Record) error {
	met := map[string]bool{}
	var missing []string
	for i, l := range leaves {
		if l.Side != SideResponse || l.Kind != KindPagedList || !l.Required {
			continue
		}
		reader, ok := res.PagedLists[l.Path]
		if !ok {
			missing = append(missing, l.Path)
			continue
		}
		met[l.Path] = true
		records[i].PagedBy = reader
	}
	if len(missing) > 0 {
		return fmt.Errorf("%s: name in PagedLists the type that reads every element of each list Stripe returns a page at a time: %s", res.Package, strings.Join(missing, ", "))
	}
	for p := range res.PagedLists {
		if !met[p] {
			return fmt.Errorf("%s: paged-lists entry %q names no list the object requires", res.Package, p)
		}
	}
	return nil
}

// checkPagedReaders fails on a PagedLists entry that names no table's type.
func checkPagedReaders(resources []Resource) error {
	types := map[string]bool{}
	for _, res := range resources {
		types["stripe_"+res.Package] = true
	}
	for _, res := range resources {
		for _, p := range sortedKeys(res.PagedLists) {
			if !types[res.PagedLists[p]] {
				return fmt.Errorf("%s: paged-lists entry %q names %s, which is no table's type", res.Package, p, res.PagedLists[p])
			}
		}
	}
	return nil
}

func sortedKeys(m map[string]string) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// newRequired fails on a Required request leaf, not exposed today, inside a hash that is
// exposed today (or at the top level of a request whose resource exists): exposing it would
// break the configurations that set the hash (D6). The register approves one by excluding it.
func newRequired(create string, leaves []Leaf, records []Record, today tableIndex) error {
	exposedToday := map[string]bool{}
	for _, l := range leaves {
		if l.Side == SideRequest && exposed(create, l, today) {
			exposedToday[l.Operation+" "+l.Branch+" "+l.Path] = true
			for p := parentPath(l.Path); ; p = parentPath(p) {
				exposedToday[l.Operation+" "+l.Branch+" "+p+"."] = true
				if p == "" {
					break
				}
			}
		}
	}
	var found []string
	for i, l := range leaves {
		r := records[i]
		if l.Side != SideRequest || !l.Required || r.Rule != "" || exposedToday[l.Operation+" "+l.Branch+" "+l.Path] {
			continue
		}
		if exposedToday[l.Operation+" "+l.Branch+" "+parentPath(l.Path)+"."] {
			found = append(found, l.Key.String())
		}
	}
	if len(found) > 0 {
		return fmt.Errorf("new Required leaves inside an object exposed today, which would break configurations unless the register approves them: %s", strings.Join(found, ", "))
	}
	return nil
}

func (p *Pinned) class(k Key) string {
	if p.classes == nil {
		p.classes = map[Key]string{}
		for _, l := range p.Attributes.Leaves {
			p.classes[l.Key] = l.Class
		}
	}
	return p.classes[k]
}

// copyClass is the class of an embedded copy that `attributes.json` keys by its members rather
// than as one leaf, as it keys an invoice's `default_tax_rates`: the class its members share, or
// none where they differ or it lists none. The copy is read in full, so one class must hold for
// all of it.
func (p *Pinned) copyClass(k Key) string {
	class := ""
	for _, l := range p.Attributes.Leaves {
		if l.Object != k.Object || l.Side != k.Side || l.Operation != k.Operation || l.Branch != k.Branch || !strings.HasPrefix(l.Path, k.Path+".") {
			continue
		}
		if class != "" && l.Class != class {
			return ""
		}
		class = l.Class
	}
	return class
}

// refusedOnly reports a leaf `attributes.json` marks as one Stripe has only ever refused.
// answers is what Stripe answered each probe of a rule the record keeps for the request leaf,
// which takes it in some requests and refuses it in others.
func (p *Pinned) answers(k Key) []RuleObservation {
	if p == nil {
		return nil
	}
	var out []RuleObservation
	for _, r := range p.Evidence.Rules {
		if r.answer() && r.Object == k.Object && r.Operation == k.Operation && r.Path == k.Path {
			out = append(out, r.Observations...)
		}
	}
	return out
}

func (p *Pinned) refusedOnly(k Key) bool {
	if p.refused == nil {
		p.refused = map[Key]bool{}
		for _, l := range p.Attributes.Leaves {
			if l.Reason == ReasonRefusedOnly {
				p.refused[l.Key] = true
			}
		}
	}
	return p.refused[k]
}

func (p *Pinned) leaf(k Key) *EvidenceLeaf {
	if p == nil {
		return nil
	}
	if p.leaves == nil {
		p.leaves = map[Key]*EvidenceLeaf{}
		for i := range p.Evidence.Leaves {
			p.leaves[p.Evidence.Leaves[i].Key] = &p.Evidence.Leaves[i]
		}
	}
	return p.leaves[k]
}

// facts records on the gate what the evidence shows about the table's verified fields: the
// request fields Stripe leaves null on a minimal create and unsets as the engine sends the
// unset, `""` on `/v1` and null on `/v2` (E2), and the response fields that no update of the
// object changed as a side effect and that Stripe does not fill from another field (E5). An
// update shows nothing unless the suite proved a leaf of the table's update, whose probes
// record the side effects. A field holding several leaves needs each of them to show it.
func (p *Pinned) facts(res Resource, object, create, update string, leaves []Leaf, g *gate) {
	unsetBy := unsetFor(res)
	updated := false
	var changed []string
	filled := map[string]bool{}
	for _, e := range p.Evidence.Leaves {
		if e.Object == object && e.Side == SideRequest && update != "" && e.Operation == update && e.Status == "proven" {
			updated = true
		}
	}
	for _, se := range p.Evidence.SideEffects {
		if se.Object == object {
			for _, c := range se.Changed {
				changed = append(changed, c.Path)
			}
		}
	}
	for _, f := range p.Evidence.FilledFrom {
		if f.Object == object {
			filled[f.Path] = true
		}
	}
	g.clears, g.stable = map[string]bool{}, map[string]bool{}
	all := func(m map[string]bool, at string, ok bool) {
		if prev, seen := m[at]; seen {
			ok = ok && prev
		}
		m[at] = ok
	}
	for _, l := range leaves {
		e := p.leaf(l.Key)
		if p.class(l.Key) != ClassVerified || e == nil {
			continue
		}
		switch {
		case l.Side == SideResponse:
			all(g.stable, l.At, updated && !filled[l.Path] && !near(changed, l.Path))
		case l.Operation == create:
			all(g.clears, l.At, e.Default.none())
		default:
			all(g.clears, l.At, e.Unset != nil && e.Unset.By == unsetBy && (e.Unset.Result == "null" || e.Unset.Result == "absent"))
		}
	}
}

// unsetFor is how the engine unsets a field of the table's API, in the record's terms: an empty
// string on `/v1`, null on `/v2`.
func unsetFor(res Resource) string {
	if strings.HasPrefix(res.CreatePath, "/v2/") || strings.HasPrefix(res.ReadPath, "/v2/") {
		return "null"
	}
	return "empty_string"
}

// mapUpdate is how the record shows an update of a map leaf: merged or replaced, and whether a key
// sent empty stays as an empty value. A merged map whose record does not show what a key sent
// empty becomes keeps it, as far as the provider knows, so no key removal is sent. It refuses a
// record whose empty value is sent otherwise than the engine sends an unset, which the engine
// could not send.
func mapUpdate(k Key, e *EvidenceLeaf, unsetBy string) (*fields.MapUpdate, error) {
	if e == nil || e.Map == nil || e.Map.Update == "" {
		return nil, nil
	}
	m := &fields.MapUpdate{Replaced: e.Map.Update == MapReplaced, EmptyKept: true}
	if ev := e.Map.EmptyValue; ev != nil {
		if ev.By != unsetBy {
			return nil, fmt.Errorf("%s: the record shows a key of the map sent empty as %s, but the engine sends an unset on this API as %s", k, ev.By, unsetBy)
		}
		m.EmptyKept = ev.Result != MapRemoved
	}
	return m, nil
}

// leafDefault is an optional create leaf's default from the evidence (D4): what Stripe set on
// a minimal create. A default the variants of the minimal create give differently is
// DefaultVaries, which a declaration words, and so is a rule whose text runs over several lines.
// So is one a variant that leaves the leaf out contradicts with a value Stripe generates or copies,
// such as a charge's `transfer_group` on a destination charge, which the leaf's own
// `default.variants` does not hold. The value of a leaf lists of hashes hold (lists) is one per
// element, nested as deep, such as a schedule phase's `["create_prorations"]` for its one phase:
// the default is the value every element holds, and varies where they differ.
func (p *Pinned) leafDefault(k Key, lists int) *fields.Default {
	e := p.leaf(k)
	if e == nil || e.Status != "proven" {
		return nil
	}
	d := e.Default
	if d == nil {
		return nil
	}
	for _, v := range d.Variants {
		if !d.same(&v.Default) {
			return &fields.Default{Kind: fields.DefaultVaries}
		}
	}
	if d.Kind != "rule" && p.contradicted(k, e) {
		return &fields.Default{Kind: fields.DefaultVaries}
	}
	switch d.Kind {
	case "value":
		v, ok := elementValue(d.Value, lists)
		switch {
		case !ok:
			return &fields.Default{Kind: fields.DefaultVaries}
		case string(v) == "null":
			return &fields.Default{Kind: fields.DefaultNone}
		}
		return &fields.Default{Kind: fields.DefaultValue, Value: literal(v)}
	case "null", "absent":
		return &fields.Default{Kind: fields.DefaultNone}
	case "rule":
		r := p.Evidence.Rules[*d.RuleRef]
		if strings.Contains(r.Rule, "\n") || r.Beside != "" && !strings.Contains(r.Rule, "`"+besideLeaf(r.Beside)+"`") {
			// A rule that quotes the values it derived, as JSON over several lines, cannot be a
			// docs line, and one that holds only beside another argument must name it, so a
			// declaration words either.
			return &fields.Default{Kind: fields.DefaultVaries}
		}
		return &fields.Default{Kind: fields.DefaultRule, Rule: r.Rule, Observed: p.observed(r)}
	}
	return nil
}

// besideLeaf is the request path of a Beside rule's leaf, without its union branch.
func besideLeaf(beside string) string {
	leaf, _, _ := strings.Cut(beside, "@")
	return leaf
}

// besideOf is the argument beside which alone the record's default of a leaf holds (a Beside
// rule's leaf), or "".
func (p *Pinned) besideOf(k Key) string {
	e := p.leaf(k)
	if e == nil || e.Default == nil {
		return ""
	}
	for _, d := range append([]EvidenceDefault{*e.Default}, variantDefaults(e.Default)...) {
		if d.Kind == "rule" && d.RuleRef != nil {
			if r := p.Evidence.Rules[*d.RuleRef]; r.Beside != "" {
				return besideLeaf(r.Beside)
			}
		}
	}
	return ""
}

func variantDefaults(d *EvidenceDefault) []EvidenceDefault {
	out := make([]EvidenceDefault, len(d.Variants))
	for i, v := range d.Variants {
		out[i] = v.Default
	}
	return out
}

// checkBesideReturns refuses a table that exposes a request leaf whose returned form holds only
// for a create without another leaf (its `returned.rule_ref`, a Beside rule), beside that leaf:
// a create that sent both would read back another value than the one configured, as a test
// clock created with a `customer` freezes at its `created`.
func checkBesideReturns(table string, leaves []Leaf, records []Record, ev *Pinned) error {
	exposedAt := map[Key]bool{}
	for i, l := range leaves {
		if records[i].State == StateExposed {
			exposedAt[l.Key] = true
		}
	}
	for i, l := range leaves {
		e := ev.leaf(l.Key)
		if records[i].State != StateExposed || l.Side != SideRequest || e == nil {
			continue
		}
		ref := e.returnedRule()
		if ref == nil {
			continue
		}
		r := ev.Evidence.Rules[*ref]
		path, branch, _ := strings.Cut(r.Beside, "@")
		beside := l.Key
		beside.Path, beside.Branch = path, branch
		if exposedAt[beside] {
			return fmt.Errorf("%s: %s reads back as sent only without `%s`, which the table exposes too (the record's rule: %s)", table, l.Key, r.Beside, r.Rule)
		}
	}
	return nil
}

// checkHashUnsets refuses a hash the record shows an update clearing whole (`hash_unsets`) where
// the engine would remove the block otherwise: the engine sends a top-level hash removed from the
// configuration as the unset of the table's API, `""` on `/v1` and null on `/v2`, so the record's
// way must be that one. A nested hash, or one the table holds Computed, which its removal keeps,
// keeps the engine's rule.
func checkHashUnsets(res Resource, object, update string, table []fields.Field, ev *Pinned) error {
	if ev == nil || update == "" {
		return nil
	}
	for _, u := range ev.Evidence.HashUnsets {
		if u.Object != object || u.Operation != update || strings.Contains(u.Hash, ".") {
			continue
		}
		f := fieldAt(table, u.Hash)
		if f == nil || f.Shape != fields.Object || !f.Update || f.ServerDefaulted {
			continue
		}
		if by := unsetFor(res); u.By != by {
			return fmt.Errorf("%s: the record clears `%s` whole by %s, but the engine removes the block on this API by %s", res.Package, u.Hash, u.By, by)
		}
	}
	return nil
}

// markTogether marks on the table each set of request leaves that the record shows Stripe taking
// only all at once (its `together`) and that the table exposes, on the hash that holds the set,
// with Stripe's refusals of the set sent whole, so that the engine refuses a configuration that
// sets some of its members and not all. The set's answer is no member's own and proves nothing of
// a member alone, so generation fails on a table that exposes some members of a set and not the
// others, or a member the gate does not let through. A set whose every member the record shows
// Stripe taking apart from the others (takenApart) is no such set: its answers prove only that
// Stripe takes the members at once too, so it marks nothing, and generation fails where Stripe
// refused such a set, which the engine has no way to refuse.
func markTogether(res Resource, object, create, update string, table []fields.Field, leaves []Leaf, records []Record, ev *Pinned) error {
	if ev == nil {
		return nil
	}
	at := map[Key]int{}
	for i, l := range leaves {
		at[l.Key] = i
	}
	type marked struct {
		hash    string
		members []string
		refused []refusedOn
	}
	var sets []*marked
	for _, t := range ev.Evidence.Together {
		if t.Object != object || t.Operation != create && t.Operation != update {
			continue
		}
		var paths []string
		exposedN := 0
		for _, m := range t.Members {
			path, branch, _ := strings.Cut(m, "@")
			i, ok := at[Key{Object: object, Side: SideRequest, Operation: t.Operation, Path: path, Branch: branch}]
			if !ok {
				return fmt.Errorf("%s: the record's set %v of %s names `%s`, which the walk does not have", res.Package, t.Members, t.Operation, m)
			}
			if r := records[i]; r.State == StateExposed {
				if r.Class != ClassVerified && (r.Class != ClassAcceptedOnly || r.Rule != "") {
					return fmt.Errorf("%s: %s is exposed, but the gate does not let it through (class %q), and Stripe takes it only with %v", res.Package, leaves[i].Key, r.Class, t.Members)
				}
				exposedN++
			}
			paths = append(paths, leaves[i].At)
		}
		if exposedN == 0 {
			continue
		}
		if exposedN < len(t.Members) {
			return fmt.Errorf("%s: %s exposes some members of %v and not the others, which Stripe takes only all at once", res.Package, t.Operation, t.Members)
		}
		if takenApart(t, ev.Evidence.SideEffects) {
			for _, a := range t.Answers {
				if a.Status != http.StatusOK {
					return fmt.Errorf("%s: %s takes each member of %v apart from the others and refused the set (probe %s), which the engine cannot hold yet", res.Package, t.Operation, t.Members, a.Condition.Probe)
				}
			}
			continue
		}
		hash := parentPath(paths[0])
		for _, p := range paths[1:] {
			for hash != "" && !strings.HasPrefix(p, hash+".") {
				hash = parentPath(hash)
			}
		}
		if hash == "" {
			return fmt.Errorf("%s: %s takes %v only all at once, a set of top-level arguments, which the engine cannot hold yet", res.Package, t.Operation, t.Members)
		}
		members := make([]string, len(paths))
		for i, p := range paths {
			members[i] = strings.TrimPrefix(p, hash+".")
		}
		slices.Sort(members)
		on := "update"
		if t.Operation == create {
			on = "create"
		}
		i := slices.IndexFunc(sets, func(s *marked) bool { return s.hash == hash && slices.Equal(s.members, members) })
		if i < 0 {
			sets = append(sets, &marked{hash: hash, members: members})
			i = len(sets) - 1
		}
		for _, a := range t.Answers {
			if a.Status != http.StatusOK {
				sets[i].refused = append(sets[i].refused, refusedOn{on: on, country: a.Condition.AccountCountry, message: a.Message})
			}
		}
	}
	for _, s := range sets {
		f := fieldAt(table, s.hash)
		if f == nil || f.Shape != fields.Object && f.Shape != fields.ObjectList {
			return fmt.Errorf("%s: `%s`, which holds a set Stripe takes only all at once, is not a hash of the table", res.Package, s.hash)
		}
		set := fields.Together{Members: s.members}
		if len(s.refused) > 0 {
			sentence, err := refusedSentence("Sent together, Stripe refused them", s.refused)
			if err != nil {
				return fmt.Errorf("%s: %s: %w", res.Package, s.hash, err)
			}
			set.Refused = sentence
		}
		f.Together = append(f.Together, set)
	}
	return nil
}

// takenApart reports whether, for every member of a set, a side effect of the set's operation
// shows Stripe taking a request that sent or carried that member and no other member of the set,
// as the v2 recipient's `applied` (`isolated/change_applied_to_false`) and transfers request
// (`isolated/transfers_requested_false`) were each taken apart from the pair.
func takenApart(t Together, effects []SideEffect) bool {
	for _, m := range t.Members {
		apart := slices.ContainsFunc(effects, func(s SideEffect) bool {
			if s.Object != t.Object || s.Operation != t.Operation {
				return false
			}
			_, sent := s.Sent[m]
			_, carried := s.Carried[m]
			if !sent && !carried {
				return false
			}
			for _, other := range t.Members {
				_, otherSent := s.Sent[other]
				_, otherCarried := s.Carried[other]
				if other != m && (otherSent || otherCarried) {
					return false
				}
			}
			return true
		})
		if !apart {
			return false
		}
	}
	return true
}

// checkKeptDiscounts refuses an accepted-only form of a list of discounts that a table keeps
// (Resource.AcceptedOnly) where the engine could not read it back as the record shows Stripe
// returning it: the top-level `discounts` as discount IDs at `discounts`, and a list element's,
// such as a quote line's, as each discount whole at `<list>.data.discounts.discount`, in the
// table's own object. The engine maps either back by what each form names (D3). A list the object
// never returns, such as a subscription's `add_invoice_items`, the provider keeps from
// configuration and reads nothing back of, so its form need only be one the record shows Stripe
// holding on another object (`held_by`), as the invoice item the subscription adds.
func checkKeptDiscounts(res Resource, leaves []Leaf, records []Record, ev *Pinned) error {
	if ev == nil {
		return nil
	}
	for i, l := range leaves {
		list, form, ok := strings.Cut(l.Path, ".discounts.")
		if ok {
			list += ".discounts"
		} else {
			list, form, ok = strings.Cut(l.Path, ".")
		}
		if !ok || l.Side != SideRequest || records[i].State != StateExposed || records[i].Class != ClassAcceptedOnly || discountRelations[form] == "" || !strings.HasSuffix(list, "discounts") {
			continue
		}
		if element := strings.TrimSuffix(list, ".discounts"); list != "discounts" && !returnedAt(leaves, element) {
			if !slices.ContainsFunc(ev.Evidence.Discounts, func(d Discount) bool {
				return d.Object == l.Object && d.Operation == l.Operation && d.Path == list && d.Form == form && d.HeldBy != ""
			}) {
				return fmt.Errorf("%s: kept accepted-only %s sits in `%s`, which the object never returns, and the record's discounts do not show Stripe holding it on another object", res.Package, l.Key, element)
			}
			continue
		}
		want, returned := "discounts", DiscountID
		if list != "discounts" {
			want, returned = strings.TrimSuffix(list, "discounts")+"data.discounts.discount", DiscountObject
		}
		// A form may be listed at more than one response path, as a quote's own coupon is listed
		// on each line too with the share it takes off that line; the engine reads it back where
		// the table's own list returns it.
		found := slices.ContainsFunc(ev.Evidence.Discounts, func(d Discount) bool {
			return d.Object == l.Object && d.Operation == l.Operation && d.Path == list && d.Form == form && d.HeldBy == "" && d.Returned == returned && d.ResponsePath == want
		})
		if !found {
			return fmt.Errorf("%s: kept accepted-only %s is read back as the record shows Stripe returning it only as %s at %s in the object itself, which the record's discounts do not show", res.Package, l.Key, returned, want)
		}
	}
	return nil
}

// returnedAt reports whether the object's response holds a leaf at path or below it.
func returnedAt(leaves []Leaf, path string) bool {
	return slices.ContainsFunc(leaves, func(l Leaf) bool {
		return l.Side == SideResponse && (l.Path == path || strings.HasPrefix(l.Path, path+"."))
	})
}

// contradicted reports a minimal create that leaves the leaf out and that Stripe answers at the
// leaf's response path with anything but the leaf's default.
func (p *Pinned) contradicted(k Key, e *EvidenceLeaf) bool {
	at := k.Path
	var returned struct {
		ResponsePath string `json:"response_path"`
	}
	if e.Returned != nil && json.Unmarshal(e.Returned, &returned) == nil && returned.ResponsePath != "" {
		at = returned.ResponsePath
	}
	for _, c := range p.creates[k.Object+" "+k.Operation] {
		held, ok := c.Response[at]
		if !ok || c.Status != http.StatusOK || sends(c.Params, strings.Split(k.Path, ".")) {
			continue
		}
		if !e.Default.same(&EvidenceDefault{Kind: held.Kind, Value: held.Value}) {
			return true
		}
	}
	return false
}

// checkCreateProbes returns a line for each default statement the suite's create probes do not
// bear out. A probe that leaves a leaf out and holds something else contradicts a none or a literal
// from the record, which holds only for the minimal create, so a declaration has to word it. A
// worded default names each literal the probes show, and words a none only where a probe
// contradicts it. A rule
// from the record is prose about another argument, so nothing here can weigh it.
func (tc *tableCoverage) checkCreateProbes(res Resource, worded map[string]string, ev *Pinned) []string {
	if ev == nil || res.CreatePath == "" {
		return nil
	}
	var out []string
	seen := map[string]bool{}
	for i, l := range tc.leaves {
		f := fieldAt(tc.table, l.At)
		if l.Side != SideRequest || l.Operation != tc.ops[0] || tc.records[i].State != StateExposed || f == nil || f.Default == nil || seen[l.At] {
			continue
		}
		held := ev.probed[l.Key]
		rule, isWorded := worded[l.At]
		var found []string
		switch {
		case isWorded:
			for _, v := range held {
				for _, s := range literalsOf(v) {
					if !strings.Contains(rule, fields.Code(s)) {
						found = append(found, fmt.Sprintf("probe %s holds %s, which the wording does not name", v.Probe, s))
					}
				}
			}
			if f.Default.Kind == fields.DefaultNone && !slices.ContainsFunc(held, func(v evidencecheck.ProbedValue) bool { return !noneLike(v) }) {
				found = append(found, "the record says none, and no probe contradicts it")
			}
		case isWorded:
		case f.Default.Kind == fields.DefaultNone:
			for _, v := range held {
				if !noneLike(v) {
					found = append(found, fmt.Sprintf("probe %s holds %s", v.Probe, heldWords(v)))
				}
			}
		case f.Default.Kind == fields.DefaultValue:
			for _, v := range held {
				matches := append(literalsOf(v), literal(v.Value))
				if e, ok := elementValue(v.Value, l.Lists); ok && v.Kind == "value" {
					matches = append(matches, literal(e))
				}
				if !slices.Contains(matches, f.Default.Value) {
					found = append(found, fmt.Sprintf("probe %s holds %s", v.Probe, heldWords(v)))
				}
			}
		}
		if len(found) > 0 {
			seen[l.At] = true
			out = append(out, fmt.Sprintf("%s %s (%s): %s", res.Package, l.At, f.Default.Statement(), strings.Join(found, "; ")))
		}
	}
	return out
}

// noneLike reports a value the docs call none: null, an empty string or collection, or a list or a
// map of nothing but those, which is how a probe holds a member several elements, or the entries
// of a map keyed by currency, leave unset.
func noneLike(v evidencecheck.ProbedValue) bool {
	if v.Kind != "value" {
		return false
	}
	var items []json.RawMessage
	if json.Unmarshal(v.Value, &items) == nil {
		return !slices.ContainsFunc(items, func(item json.RawMessage) bool {
			return !noneLike(evidencecheck.ProbedValue{Kind: "value", Value: item})
		})
	}
	var byKey map[string]json.RawMessage
	if json.Unmarshal(v.Value, &byKey) == nil {
		for _, item := range byKey {
			if !noneLike(evidencecheck.ProbedValue{Kind: "value", Value: item}) {
				return false
			}
		}
		return true
	}
	switch strings.TrimSpace(string(v.Value)) {
	case "null", `""`, "{}":
		return true
	}
	return false
}

// literalsOf is a probe's literal as the docs show it, each element of a list by itself, because
// a probe holds a member of a list's elements as a list. A value none is no literal, and a kind
// that names what Stripe made or copied has none.
func literalsOf(v evidencecheck.ProbedValue) []string {
	if v.Kind != "value" || noneLike(v) {
		return nil
	}
	var items []json.RawMessage
	if json.Unmarshal(v.Value, &items) != nil {
		return []string{literal(v.Value)}
	}
	out := make([]string, len(items))
	for i, item := range items {
		out[i] = literal(item)
	}
	return out
}

// heldWords says what a probe held, for an error.
func heldWords(v evidencecheck.ProbedValue) string {
	if v.Kind == "value" {
		return literal(v.Value)
	}
	return "a value of kind " + v.Kind
}

// sends reports whether params name path, through the hashes and lists on the way.
func sends(params any, path []string) bool {
	if len(path) == 0 {
		return true
	}
	switch v := params.(type) {
	case map[string]any:
		next, ok := v[path[0]]
		return ok && sends(next, path[1:])
	case []any:
		return slices.ContainsFunc(v, func(item any) bool { return sends(item, path) })
	}
	return false
}

// same reports whether two defaults state the same thing: null and absent both state none. Two
// rule defaults both carry a rule_ref, which Evidence.check requires of every rule default.
func (d *EvidenceDefault) same(o *EvidenceDefault) bool {
	none := func(k string) bool { return k == "null" || k == "absent" }
	switch {
	case none(d.Kind) || none(o.Kind):
		return none(d.Kind) && none(o.Kind)
	case d.Kind != o.Kind:
		return false
	case d.Kind == "rule":
		return *d.RuleRef == *o.RuleRef
	}
	return literal(d.Value) == literal(o.Value)
}

// literal is a recorded value as the docs show it: a string bare, anything else as compact JSON.
func literal(v json.RawMessage) string {
	var s string
	if json.Unmarshal(v, &s) == nil {
		return s
	}
	var b bytes.Buffer
	if json.Compact(&b, v) != nil {
		return string(v)
	}
	return b.String()
}

// observed is the literal a rule gave on the suite's own account with nothing else set: the
// observation of the plain create, such as "`usd` on a US account", or "" where that create
// holds no literal, as one that sends the value or that Stripe generates it for does not.
func (p *Pinned) observed(r EvidenceRule) string {
	for _, o := range r.Observations {
		if o.Condition.Variant != "" || o.Value.Kind != "value" {
			continue
		}
		s := fields.Code(literal(o.Value.Value))
		if c := o.Condition.AccountCountry; c != "" {
			s += " on " + articleFor(c) + " " + c + " account"
		}
		return s
	}
	return ""
}

// articleFor is the indefinite article before a country code read as letters: "an" before
// one whose first letter's name starts with a vowel sound (`an FR account`, `a US account`).
func articleFor(country string) string {
	if country != "" && strings.ContainsRune("AEFHILMNORSX", rune(country[0])) {
		return "an"
	}
	return "a"
}

// specDefault is the literal a description's "Defaults to" sentence names, such as `once` in
// "Defaults to `once`.", or "" when it names none or a rule.
func specDefault(desc string) string {
	m := specDefaultText.FindStringSubmatch(desc)
	if m == nil {
		return ""
	}
	if m[1] != "" {
		return m[1]
	}
	return m[2]
}

var specDefaultText = regexp.MustCompile("Defaults to (?:`([^`]+)`|(true|false|-?[0-9]+(?:\\.[0-9]+)?))(?:[ ,.]|$)")

// disagreement is the spec's default for an optional argument where the evidence says
// otherwise, or "". A rule, a default the variants give differently and an argument only the
// update takes are not compared.
func disagreement(desc string, d *fields.Default) string {
	spec := specDefault(desc)
	if spec == "" || d == nil {
		return ""
	}
	switch d.Kind {
	case fields.DefaultValue:
		if d.Value == spec {
			return ""
		}
	case fields.DefaultNone:
	default:
		return ""
	}
	return spec
}

// checkVerifiedKeys fails on a verified leaf of a table's object and operations that no
// table's walk reaches, so that a difference between the provider's path language and the
// fake's shows as a list of keys. The response's own `object` field, which the provider's
// walk leaves out, is not one, and nor is a member of an embedded copy the walk holds as one
// leaf (copyClass).
func checkVerifiedKeys(ev *Pinned, covered []*tableCoverage) error {
	walked := map[Key]bool{}
	var copies []Key
	objects, ops := map[string]bool{}, map[string]bool{}
	for _, tc := range covered {
		objects[tc.object] = true
		for _, op := range tc.ops {
			ops[op] = true
		}
		for i, r := range tc.records {
			walked[r.Key] = true
			if tc.leaves[i].Kind == KindEmbedded {
				copies = append(copies, r.Key)
			}
		}
	}
	inCopy := func(k Key) bool {
		return slices.ContainsFunc(copies, func(c Key) bool {
			return c.Object == k.Object && c.Side == k.Side && c.Operation == k.Operation && c.Branch == k.Branch && strings.HasPrefix(k.Path, c.Path+".")
		})
	}
	var missing []string
	for _, l := range ev.Attributes.Leaves {
		if l.Class != ClassVerified || walked[l.Key] || !objects[l.Object] || inCopy(l.Key) {
			continue
		}
		if l.Side == SideResponse && l.Path != "object" || l.Side == SideRequest && ops[l.Operation] {
			missing = append(missing, l.Key.String())
		}
	}
	if len(missing) > 0 {
		return fmt.Errorf("spec/attributes.json verifies leaves the leaf walk does not have: %s", strings.Join(missing, ", "))
	}
	return nil
}

// checkAcceptedOnly fails on a Resource.AcceptedOnly entry that keeps no leaf: one that names no
// leaf of the class in the pinned spec/attributes.json, or whose leaves another rule or the
// register leaves out anyway, so that an entry goes when it stops mattering.
func checkAcceptedOnly(resources []Resource, records []Record) error {
	met := map[string]bool{}
	for _, r := range records {
		if r.Class == ClassAcceptedOnly && r.Rule == "" {
			met[r.Table+" "+r.Path] = true
			met[r.Table+" "+r.Operation+" "+r.Path] = true
		}
	}
	for _, res := range resources {
		for _, p := range sortedKeys(res.AcceptedOnly) {
			if !met[res.Package+" "+p] {
				return fmt.Errorf("%s: accepted-only entry %q keeps no %s leaf: none is listed, or another rule leaves it out", res.Package, p, ClassAcceptedOnly)
			}
		}
	}
	return nil
}

// checkRequiredMembers fails on a hash a table exposes without a member its request requires, in
// the same union branch: no configuration could send the hash. The register leaves such a hash
// out.
func checkRequiredMembers(records []Record) error {
	hash := func(r Record, p string) string {
		return r.Table + " " + r.Operation + " " + r.Branch + " " + p
	}
	exposedHash := map[string]bool{}
	for _, r := range records {
		if r.Side == SideRequest && r.State == StateExposed {
			for p := parentPath(r.Path); p != ""; p = parentPath(p) {
				exposedHash[hash(r, p)] = true
			}
		}
	}
	var found []string
	for _, r := range records {
		if r.Side == SideRequest && r.Required && r.State != StateExposed && r.Rule != RuleRegister && exposedHash[hash(r, parentPath(r.Path))] {
			found = append(found, r.Table+" "+r.Key.String())
		}
	}
	if len(found) > 0 {
		return fmt.Errorf("a table exposes a hash without a member its request requires, which no configuration could send; expose the member or put the hash in the register: %s", strings.Join(found, ", "))
	}
	return nil
}

// checkStates fails on a leaf that is neither exposed, excluded nor awaiting proof: with a
// pinned spec/attributes.json, a leaf it does not list.
func checkStates(records []Record) error {
	var bad []string
	for _, r := range records {
		switch r.State {
		case StateExposed, StateExcluded, StateAwaiting:
		default:
			bad = append(bad, r.Table+" "+r.Key.String())
		}
	}
	if len(bad) > 0 {
		return fmt.Errorf("leaves with no state (not exposed, excluded or listed in spec/attributes.json): %s", strings.Join(bad, ", "))
	}
	return nil
}

func sortRecords(records []Record) {
	sort.Slice(records, func(i, j int) bool {
		a, b := records[i], records[j]
		if a.Table != b.Table {
			return a.Table < b.Table
		}
		if a.Side != b.Side {
			return a.Side < b.Side
		}
		if a.Operation != b.Operation {
			return a.Operation < b.Operation
		}
		if a.Path != b.Path {
			return a.Path < b.Path
		}
		return a.Branch < b.Branch
	})
}

// counts is the leaves of one side in each state.
type counts struct {
	Exposed  int `json:"exposed"`
	Excluded int `json:"excluded"`
	Awaiting int `json:"awaiting_proof"`
}

func (c *counts) add(s State) {
	switch s {
	case StateExposed:
		c.Exposed++
	case StateExcluded:
		c.Excluded++
	case StateAwaiting:
		c.Awaiting++
	}
}

func (c counts) total() int { return c.Exposed + c.Excluded + c.Awaiting }

type sideCounts struct {
	Request  counts `json:"request"`
	Response counts `json:"response"`
}

func (s *sideCounts) add(r Record) {
	if r.Side == SideRequest {
		s.Request.add(r.State)
	} else {
		s.Response.add(r.State)
	}
}

// coverageHeader names what the report was generated from.
type coverageHeader struct {
	APIVersion string       `json:"api_version"`
	SpecTag    string       `json:"spec_tag"`
	Evidence   *EvidencePin `json:"evidence"`
	Totals     sideCounts   `json:"totals"`
}

// RenderCoverageJSON writes coverage.json: the header, then one record per line, so that a
// change of evidence or spec shows as a line diff.
func RenderCoverageJSON(h coverageHeader, records []Record) ([]byte, error) {
	var buf, line bytes.Buffer
	head := json.NewEncoder(&line)
	head.SetEscapeHTML(false)
	head.SetIndent("  ", "  ")
	if err := head.Encode(h); err != nil {
		return nil, err
	}
	buf.WriteString("{\n  \"header\": ")
	buf.Write(bytes.TrimSuffix(line.Bytes(), []byte("\n")))
	buf.WriteString(",\n  \"leaves\": [\n")
	enc := json.NewEncoder(&line)
	enc.SetEscapeHTML(false)
	for i, r := range records {
		line.Reset()
		if err := enc.Encode(r); err != nil {
			return nil, err
		}
		buf.WriteString("    ")
		buf.Write(bytes.TrimSuffix(line.Bytes(), []byte("\n")))
		if i < len(records)-1 {
			buf.WriteByte(',')
		}
		buf.WriteByte('\n')
	}
	buf.WriteString("  ]\n}\n")
	return buf.Bytes(), nil
}

// unrecordedDefaults is one table's optional arguments whose docs line has no default
// statement yet: by path those the pinned evidence verifies, which records no default the docs
// can state, and the count of the others, which it does not verify yet.
type unrecordedDefaults struct {
	Verified   []string
	Unverified int
}

func (u unrecordedDefaults) total() int { return len(u.Verified) + u.Unverified }

// defaultsNotRecorded finds, per table, the optional request paths exposed with no default
// statement on any of their leaves. A path the create requires is no optional argument, though
// its update leaf is optional. A path is verified when any of its exposed request leaves is.
func defaultsNotRecorded(records []Record) map[string]unrecordedDefaults {
	stated, required, verified := map[string]bool{}, map[string]bool{}, map[string]bool{}
	type arg struct{ table, path string }
	var optional []arg
	seen := map[string]bool{}
	for _, r := range records {
		if r.Side != SideRequest || r.State != StateExposed {
			continue
		}
		k := r.Table + " " + r.Path
		if r.Required {
			required[k] = true
			continue
		}
		if !seen[k] {
			seen[k] = true
			optional = append(optional, arg{r.Table, r.Path})
		}
		if exposable(r.Class) {
			verified[k] = true
		}
		if r.Default != nil && r.Default.Statement != "" {
			stated[k] = true
		}
	}
	out := map[string]unrecordedDefaults{}
	for _, a := range optional {
		k := a.table + " " + a.path
		if stated[k] || required[k] {
			continue
		}
		u := out[a.table]
		if verified[k] {
			u.Verified = append(u.Verified, a.path)
		} else {
			u.Unverified++
		}
		out[a.table] = u
	}
	return out
}

// RenderCoverageMarkdown writes coverage.md: the totals, the totals per table, then every
// leaf exposed against a rule, excluded or awaiting proof.
func RenderCoverageMarkdown(h coverageHeader, records []Record) []byte {
	var b strings.Builder
	b.WriteString("# Attribute coverage\n\n")
	b.WriteString("Generated by `tools/schemagen` (`go generate ./internal/generated`); do not edit. ")
	fmt.Fprintf(&b, "Spec `%s`, API version `%s`. ", h.SpecTag, h.APIVersion)
	if h.Evidence == nil {
		b.WriteString("No evidence is pinned (`spec/EVIDENCE_PIN.json`), so no leaf is verified yet.\n\n")
	} else {
		fmt.Fprintf(&b, "Evidence: fake `%s`, suite commit `%s`, run `%d`.\n\n", h.Evidence.FakeVersion, h.Evidence.SuiteRef, h.Evidence.RunID)
	}
	b.WriteString("Every leaf of each table's create and update requests and of its object is exposed, excluded by a rule or the register, or awaiting proof: the pinned fake release has not verified it (`adrs/1-pending/2026-10-29-full-attribute-coverage/ADR.md`, D1). Request leaves are counted per operation, so a parameter both requests take counts twice. `coverage.json` has every leaf.\n\n")
	b.WriteString("|Side|Exposed|Excluded|Awaiting proof|Total|\n|-|-|-|-|-|\n")
	for _, s := range []struct {
		name string
		c    counts
	}{{"Request", h.Totals.Request}, {"Response", h.Totals.Response}} {
		fmt.Fprintf(&b, "|%s|%d|%d|%d|%d|\n", s.name, s.c.Exposed, s.c.Excluded, s.c.Awaiting, s.c.total())
	}

	b.WriteString("\n## Per table\n\n")
	b.WriteString("Defaults not yet recorded counts the optional arguments a table exposes whose docs line has no default statement yet (D4); \"Defaults not yet recorded\" below lists them.\n\n")
	b.WriteString("|Table|Object|Request exposed|Request excluded|Request awaiting|Response exposed|Response excluded|Response awaiting|Defaults not yet recorded|\n|-|-|-|-|-|-|-|-|-|\n")
	var tables []string
	per := map[string]*sideCounts{}
	object := map[string]string{}
	for _, r := range records {
		if per[r.Table] == nil {
			per[r.Table] = &sideCounts{}
			tables = append(tables, r.Table)
			object[r.Table] = r.Object
		}
		per[r.Table].add(r)
	}
	unrecorded := defaultsNotRecorded(records)
	for _, t := range tables {
		c := per[t]
		fmt.Fprintf(&b, "|`%s`|`%s`|%d|%d|%d|%d|%d|%d|%d|\n", t, object[t], c.Request.Exposed, c.Request.Excluded, c.Request.Awaiting, c.Response.Exposed, c.Response.Excluded, c.Response.Awaiting, unrecorded[t].total())
	}

	b.WriteString("\n## Defaults not yet recorded\n\nPer table, the optional arguments it exposes whose docs line has no default statement yet (D4). Verified, default not recorded: the pinned evidence verifies the argument but records no default the docs can state, such as one the minimal creates set; each is named. Unverified: the evidence does not verify the argument yet; they are counted. Until `v0.1.0` docgen requires a statement only of the provider's arguments and those only an update takes; its `everyArgumentHasADefault` then requires one of both kinds.\n\n")
	b.WriteString("|Table|Verified, default not recorded|Unverified|\n|-|-|-|\n")
	n := 0
	for _, t := range tables {
		u := unrecorded[t]
		if u.total() == 0 {
			continue
		}
		names := make([]string, len(u.Verified))
		for i, p := range u.Verified {
			names[i] = "`" + p + "`"
		}
		fmt.Fprintf(&b, "|`%s`|%s|%d|\n", t, strings.Join(names, ", "), u.Unverified)
		n++
	}
	if n == 0 {
		b.WriteString("\nNone.\n")
	}

	line := func(r Record) string {
		owner := r.Operation
		if r.Side == SideResponse {
			owner = "response"
		}
		p := r.Path
		if r.Branch != "" {
			p += "@" + r.Branch
		}
		return fmt.Sprintf("- `%s` %s `%s` (%s)", r.Table, owner, p, r.Shape)
	}

	b.WriteString("\n## The spec's \"Defaults to\" against the evidence\n\nWhere a description's \"Defaults to\" names another value than the pinned evidence records, for the conformance suite to check. The spec's text is never a source of the docs' default statements (D4).\n\n")
	n = 0
	for _, r := range records {
		if r.SpecDefault != "" {
			fmt.Fprintf(&b, "%s: the spec says `%s`; the evidence says %s\n", line(r), r.SpecDefault, r.Default.Statement)
			n++
		}
	}
	if n == 0 {
		b.WriteString("None.\n")
	}

	b.WriteString("\n## Exposed against a rule\n\nExposed today and excluded by a rule; removed before `v0.1.0` (D6, Step 6).\n\n")
	n = 0
	for _, r := range records {
		if r.State == StateExposed && r.Rule != "" {
			fmt.Fprintf(&b, "%s: %s\n", line(r), r.Rule)
			n++
		}
	}
	if n == 0 {
		b.WriteString("None.\n")
	}

	b.WriteString("\n## Accepted only and kept\n\nStripe takes these leaves and never returns them; `Resource.AcceptedOnly` keeps each in spite of rule X6, for the reason given.\n\n")
	n = 0
	for _, r := range records {
		if r.State == StateExposed && r.Class == ClassAcceptedOnly {
			fmt.Fprintf(&b, "%s: %s\n", line(r), r.Reason)
			n++
		}
	}
	if n == 0 {
		b.WriteString("None.\n")
	}

	b.WriteString("\n## Excluded\n")
	for _, rl := range []string{RuleControl, RuleTypeMarker, RulePagedList, RuleRecursion, RuleConfirm, RuleAcceptedOnly, RuleRegister} {
		var lines []string
		for _, r := range records {
			if r.State == StateExcluded && r.Rule == rl {
				l := line(r)
				if rl == RuleRegister {
					l += ": " + r.Reason
				}
				lines = append(lines, l)
			}
		}
		title := rl + ", " + ruleText[rl]
		if rl == RuleRegister {
			title = "The register (`Resource.Exclude`)"
		}
		fmt.Fprintf(&b, "\n### %s\n\n", title)
		if len(lines) == 0 {
			b.WriteString("None.\n")
		}
		for _, l := range lines {
			b.WriteString(l + "\n")
		}
	}

	b.WriteString("\n## Awaiting proof\n")
	table := ""
	for _, r := range records {
		if r.State != StateAwaiting {
			continue
		}
		if r.Table != table {
			table = r.Table
			fmt.Fprintf(&b, "\n### `%s`\n\n", table)
		}
		b.WriteString(line(r) + "\n")
	}
	return []byte(b.String())
}
