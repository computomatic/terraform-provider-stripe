package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"regexp"
	"slices"
	"sort"
	"strings"

	"github.com/computomatic/terraform-provider-stripe/internal/fields"
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
			if _, ok := register[p]; ok {
				register[p] = true
				r.Rule, r.Reason = RuleRegister, res.Exclude[p]
			}
		}
		if ev != nil {
			r.Class = ev.class(l.Key)
			if e := ev.leaf(l.Key); e != nil && e.Status == "proven" {
				r.Tests = e.Tests
			}
			r.Refused = ev.refusedOnly(l.Key)
		}
		if r.Class == ClassAcceptedOnly && r.Rule == "" {
			if reason, ok := res.AcceptedOnly[l.Path]; ok {
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
		if d := defaultOf(l, *r, create, leaves, records, ev); d != nil {
			r.Default = defaultRecord(d)
			if f := fieldAt(tc.table, l.At); f != nil {
				r.SpecDefault = disagreement(f.Description, d)
			}
		}
	}
	if err := annotate(tc.table, leaves, records, create, ev); err != nil {
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
// request leaf holds, for its docs line (D4), and Stripe's refusal of each argument it has only
// ever refused, which it fails on where the record holds none. The create's default comes before
// an update's `not sent`.
func annotate(table []fields.Field, leaves []Leaf, records []Record, create string, ev *Pinned) error {
	refused := map[string][]refusedOn{}
	var paths []string
	for i, l := range leaves {
		r := records[i]
		if l.Side != SideRequest || r.State != StateExposed {
			continue
		}
		f := fieldAt(table, l.At)
		if f == nil {
			continue
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
		if f.Required {
			continue
		}
		if d := defaultOf(l, r, create, leaves, records, ev); d != nil && (f.Default == nil || l.Operation == create) {
			f.Default = d
		}
	}
	for _, at := range paths {
		s, err := refusedSentence(refused[at])
		if err != nil {
			return fmt.Errorf("%s: %w", at, err)
		}
		fieldAt(table, at).Refused = s
	}
	return nil
}

// refusedOn is one recorded refusal of an argument: on which request, on an account of which
// country, and Stripe's message.
type refusedOn struct{ on, country, message string }

// refusedSentence words an argument's recorded refusals for its docs line, one sentence per
// message, in the order the record gives them.
func refusedSentence(all []refusedOn) (string, error) {
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
		sentences[i] = fmt.Sprintf("Stripe refused it on %s account on %s: %q.", strings.Join(accounts, " and "), strings.Join(c.on, " and "), m)
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
// update requires it, because the create never sends it, and so is one whose create leaf rule X6
// leaves out.
func defaultOf(l Leaf, r Record, create string, leaves []Leaf, records []Record, ev *Pinned) *fields.Default {
	if l.Side != SideRequest || r.State != StateExposed {
		return nil
	}
	if l.Operation != create {
		onCreate := false
		for i, c := range leaves {
			if c.Operation == create && c.At == l.At && records[i].Rule != RuleAcceptedOnly {
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
	return ev.leafDefault(l.Key)
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

// refusedOnly reports a leaf `attributes.json` marks as one Stripe has only ever refused.
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
	unsetBy := "empty_string"
	if strings.HasPrefix(res.CreatePath, "/v2/") || strings.HasPrefix(res.ReadPath, "/v2/") {
		unsetBy = "null"
	}
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
	if p.parentDefaults == nil {
		p.parentDefaults = map[Key]bool{}
	}
	for _, l := range leaves {
		if l.Operation == create && slices.Contains(res.ParentDefaults, l.At) {
			p.parentDefaults[l.Key] = true
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

// leafDefault is an optional create leaf's default from the evidence (D4): what Stripe set on
// a minimal create. A default the variants of the minimal create give differently, or that the
// table's ParentDefaults names, is DefaultVaries, which a declaration words.
func (p *Pinned) leafDefault(k Key) *fields.Default {
	e := p.leaf(k)
	if e == nil || e.Status != "proven" {
		return nil
	}
	d := e.Default
	if d == nil {
		return nil
	}
	if p.parentDefaults[k] {
		return &fields.Default{Kind: fields.DefaultVaries}
	}
	for _, v := range d.Variants {
		if !d.same(&v.Default) {
			return &fields.Default{Kind: fields.DefaultVaries}
		}
	}
	switch d.Kind {
	case "value":
		return &fields.Default{Kind: fields.DefaultValue, Value: literal(d.Value)}
	case "null", "absent":
		return &fields.Default{Kind: fields.DefaultNone}
	case "rule":
		r := p.Evidence.Rules[*d.RuleRef]
		return &fields.Default{Kind: fields.DefaultRule, Rule: r.Rule, Observed: p.observed(r)}
	}
	return nil
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
// walk leaves out, is not one.
func checkVerifiedKeys(ev *Pinned, covered []*tableCoverage) error {
	walked := map[Key]bool{}
	objects, ops := map[string]bool{}, map[string]bool{}
	for _, tc := range covered {
		objects[tc.object] = true
		for _, op := range tc.ops {
			ops[op] = true
		}
		for _, r := range tc.records {
			walked[r.Key] = true
		}
	}
	var missing []string
	for _, l := range ev.Attributes.Leaves {
		if l.Class != ClassVerified || walked[l.Key] || !objects[l.Object] {
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
