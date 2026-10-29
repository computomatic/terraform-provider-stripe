package main

import (
	"bytes"
	"encoding/json"
	"fmt"
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
	RuleRegister   = "register"
)

var ruleText = map[string]string{
	RuleControl:    "request controls: top-level `expand` and `include`",
	RuleTypeMarker: "type markers: a response hash with no keys",
	RulePagedList:  "paged lists returned only when expanded: a list envelope the object does not require and no request writes",
	RuleRecursion:  "the recursion cut: a schema met again on its own path",
	RuleConfirm:    "`confirm` where the declaration's `status` transition confirms",
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
	// Reason is the register entry's reason.
	Reason string `json:"reason,omitempty"`
	// Class is the leaf's class in the pinned spec/attributes.json.
	Class string `json:"class,omitempty"`
	// Tests are the suite's leaf tests that prove it, from the pinned spec/evidence.json.
	Tests   []string       `json:"tests,omitempty"`
	Default *DefaultRecord `json:"default,omitempty"`
}

// DefaultRecord is an optional argument's default statement (D4) and where it comes from.
type DefaultRecord struct {
	Statement string `json:"statement"`
	Source    string `json:"source"`
}

// tableCoverage is what one table contributes to the report.
type tableCoverage struct {
	object  string
	ops     []string
	records []Record
	table   []fields.Field
}

// coverTable walks one table, applies the rules and the register, builds the table through
// the gate and gives every leaf its state.
func coverTable(spec *Spec, res Resource, ev *Pinned) (*tableCoverage, error) {
	object, leaves, err := walkTable(spec, res)
	if err != nil {
		return nil, err
	}
	tc := &tableCoverage{object: object}
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

	records := make([]Record, len(leaves))
	g := &gate{}
	for i, l := range leaves {
		r := Record{Table: res.Package, Key: l.Key, Shape: l.Shape(), Target: l.Target, Required: l.Required && l.Side == SideRequest}
		r.Rule = rule(res, l, written)
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
		}
		if r.Class == ClassVerified && r.Rule == "" {
			at := tablePath(res, create, l.Key)
			switch l.Operation {
			case "":
				g.response = append(g.response, at)
			case create:
				g.create = append(g.create, at)
			default:
				g.update = append(g.update, at)
			}
		}
		records[i] = r
	}
	for p, met := range register {
		if !met {
			return nil, fmt.Errorf("%s: register entry %q names no leaf", res.Package, p)
		}
	}

	baseline, err := Build(spec, res)
	if err != nil {
		return nil, err
	}
	tc.table = baseline
	if len(g.create)+len(g.update)+len(g.response) > 0 {
		if tc.table, err = buildGated(spec, res, g); err != nil {
			return nil, err
		}
	}
	before, after := index(baseline), index(tc.table)
	if err := coversTable(res, create, update, leaves, after); err != nil {
		return nil, err
	}

	for i, l := range leaves {
		r := &records[i]
		switch {
		case exposed(res, create, l, after):
			r.State = StateExposed
			if r.Rule == RuleRegister {
				return nil, fmt.Errorf("%s: register entry for %s removes a leaf exposed today", res.Package, l.Key)
			}
		case r.Rule != "":
			r.State = StateExcluded
		case r.Class == ClassVerified:
			return nil, fmt.Errorf("%s: %s is verified, but the generator cannot hold it in the table yet", res.Package, l.Key)
		case ev == nil || r.Class != "":
			r.State = StateAwaiting
		}
		if ev != nil && r.State != StateExcluded && !r.Required {
			r.Default = ev.defaultStatement(l.Key, create, leaves)
		}
	}
	if err := newRequired(res, create, leaves, records, before); err != nil {
		return nil, err
	}
	tc.records = records
	return tc, nil
}

// rule returns the exclusion rule that leaves a leaf out, or "".
func rule(res Resource, l Leaf, written map[string]bool) string {
	switch {
	case l.Side == SideRequest && (l.Path == "expand" || l.Path == "include"):
		return RuleControl
	case l.Side == SideResponse && l.Kind == KindEmptyHash:
		return RuleTypeMarker
	case l.Side == SideResponse && l.Kind == KindPagedList && !l.Required && !written[l.Path]:
		return RulePagedList
	case l.Kind == KindRecursion:
		return RuleRecursion
	case l.Side == SideRequest && res.StatusConfirms && l.Path == "confirm":
		return RuleConfirm
	}
	return ""
}

// tablePath is where the table holds a leaf: its path, with a create-only hash
// (Resource.CreateHash) spread into the top level.
func tablePath(res Resource, create string, k Key) string {
	if res.CreateHash != "" && k.Operation == create {
		if rest, ok := strings.CutPrefix(k.Path, res.CreateHash+"."); ok {
			return rest
		}
	}
	return k.Path
}

// held is one field of a table, by dotted path.
type held struct {
	create, update, response bool
	nested                   bool
}

func index(table []fields.Field) map[string]held {
	out := map[string]held{}
	var walk func([]fields.Field, string)
	walk = func(fs []fields.Field, prefix string) {
		for _, f := range fs {
			p := join(prefix, f.Name)
			out[p] = held{create: f.Create, update: f.Update, response: f.Response, nested: len(f.Fields) > 0}
			walk(f.Fields, p)
		}
	}
	walk(table, "")
	return out
}

// whole reports a leaf that stands for everything below it: a copy, a list, a recursion or
// a reference.
func whole(k Kind) bool {
	return k == KindEmbedded || k == KindPagedList || k == KindRecursion || k == KindReference
}

// exposed reports whether the table holds a leaf on its side.
func exposed(res Resource, create string, l Leaf, table map[string]held) bool {
	h, ok := table[tablePath(res, create, l.Key)]
	if !ok || h.nested && !whole(l.Kind) {
		return false
	}
	switch l.Operation {
	case "":
		return h.response
	case create:
		return h.create
	}
	return h.update
}

// coversTable checks that every field the table holds is a leaf of the walk, or below a leaf
// that stands for its whole subtree, so that the report cannot miss an attribute.
func coversTable(res Resource, create, update string, leaves []Leaf, table map[string]held) error {
	at := map[string]Kind{}
	for _, l := range leaves {
		at[string(l.Side)+" "+l.Operation+" "+tablePath(res, create, l.Key)] = l.Kind
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
		for _, side := range []struct {
			on bool
			op string
		}{{h.create, create}, {h.update, update}, {h.response, ""}} {
			if side.on && !found(side.op, p) {
				return fmt.Errorf("%s: the table holds %s (operation %q), which the leaf walk does not reach", res.Package, p, side.op)
			}
		}
	}
	return nil
}

// newRequired fails on a Required request leaf, not exposed today, inside a hash that is
// exposed today (or at the top level of a request whose resource exists): exposing it would
// break the configurations that set the hash (D6). The register approves one by excluding it.
func newRequired(res Resource, create string, leaves []Leaf, records []Record, today map[string]held) error {
	exposedToday := map[string]bool{}
	for _, l := range leaves {
		if l.Side == SideRequest && exposed(res, create, l, today) {
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
		return fmt.Errorf("%s: new Required leaves inside an object exposed today, which would break configurations unless the register approves them: %s", res.Package, strings.Join(found, ", "))
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

func (p *Pinned) leaf(k Key) *EvidenceLeaf {
	if p.leaves == nil {
		p.leaves = map[Key]*EvidenceLeaf{}
		for i := range p.Evidence.Leaves {
			p.leaves[p.Evidence.Leaves[i].Key] = &p.Evidence.Leaves[i]
		}
	}
	return p.leaves[k]
}

// defaultStatement is an optional request leaf's default statement from the evidence (D4):
// what Stripe set on a minimal create, or `not sent` for an argument only the update takes.
func (p *Pinned) defaultStatement(k Key, create string, leaves []Leaf) *DefaultRecord {
	if k.Side != SideRequest {
		return nil
	}
	e := p.leaf(k)
	if e == nil || e.Status != "proven" {
		return nil
	}
	if k.Operation != create {
		onCreate := slices.ContainsFunc(leaves, func(l Leaf) bool { return l.Operation == create && l.Path == k.Path })
		if create != "" && onCreate {
			return nil
		}
		return &DefaultRecord{Statement: "Default: not sent.", Source: "provider"}
	}
	d := e.Default
	if d == nil {
		return nil
	}
	switch d.Kind {
	case "value":
		var s string
		v := string(d.Value)
		if json.Unmarshal(d.Value, &s) == nil {
			v = s
		}
		return &DefaultRecord{Statement: "Default (Stripe): `" + v + "`.", Source: "evidence"}
	case "null", "absent":
		return &DefaultRecord{Statement: "Default: none.", Source: "evidence"}
	case "rule":
		return &DefaultRecord{Statement: "Default (Stripe): " + strings.TrimSuffix(p.Evidence.Rules[*d.RuleRef].Rule, ".") + ".", Source: "evidence"}
	}
	return nil
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

	b.WriteString("\n## Per table\n\n|Table|Object|Request exposed|Request excluded|Request awaiting|Response exposed|Response excluded|Response awaiting|\n|-|-|-|-|-|-|-|-|\n")
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
	for _, t := range tables {
		c := per[t]
		fmt.Fprintf(&b, "|`%s`|`%s`|%d|%d|%d|%d|%d|%d|\n", t, object[t], c.Request.Exposed, c.Request.Excluded, c.Request.Awaiting, c.Response.Exposed, c.Response.Excluded, c.Response.Awaiting)
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

	b.WriteString("\n## Exposed against a rule\n\nExposed today and excluded by a rule; removed before `v0.1.0` (D6, Step 6).\n\n")
	n := 0
	for _, r := range records {
		if r.State == StateExposed && r.Rule != "" {
			fmt.Fprintf(&b, "%s: %s\n", line(r), r.Rule)
			n++
		}
	}
	if n == 0 {
		b.WriteString("None.\n")
	}

	b.WriteString("\n## Excluded\n")
	for _, rl := range []string{RuleControl, RuleTypeMarker, RulePagedList, RuleRecursion, RuleConfirm, RuleRegister} {
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
