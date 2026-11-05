package resources

import (
	"encoding/json"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"
)

// requiredTests are the acceptance tests of the P2 per-resource standard (ADR D6), as the
// suffixes of `TestAcc<Resource>_<suffix>`.
var requiredTests = []string{"create", "update", "replace", "import", "delete", "drift"}

// An update sends a hash as the record's `families[].hashes` answers for the table's object; the
// summary `nested_hash_update` proves nothing of a hash. A hash listed as `siblings_kept` that the
// update takes sends only its changed members (SendChangedMembers) wherever its parent is sent in
// part, unless it has a member Stripe requires, which the engine sends whole, or RefuseChange
// refuses its change. SendChangedMembers holds no other hash: none the record lists as
// `siblings_cleared` or `refused`, or does not list. SendWholeMembers holds no hash listed as
// kept, which would send Stripe's own values back for nothing, unless the hash goes whole for a
// member it requires. A hash listed as `refused`, which Stripe will not take in part, goes whole:
// it has a member Stripe requires, whose absence is what Stripe refuses, or SendWholeMembers names
// it or a hash it sits in. The engine's checkSentMembers makes each entry a hash and every hash
// below these objects an entry.
func TestSendChangedMembersFollowTheEvidence(t *testing.T) {
	var evidence struct {
		Families []struct {
			Hashes []struct {
				Object string `json:"object"`
				Hash   string `json:"hash"`
				Update string `json:"update"`
			} `json:"hashes"`
		} `json:"families"`
		MinimalCreates []struct {
			Object    string `json:"object"`
			Operation string `json:"operation"`
		} `json:"minimal_creates"`
	}
	raw, err := os.ReadFile(filepath.Join("..", "..", "spec", "evidence.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(raw, &evidence); err != nil {
		t.Fatal(err)
	}
	var coverage struct {
		Leaves []struct {
			Table, Object, Side, Operation, Path, State string
			Required                                    bool
		} `json:"leaves"`
	}
	if raw, err = os.ReadFile(filepath.Join("..", "generated", "coverage.json")); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(raw, &coverage); err != nil {
		t.Fatal(err)
	}
	creates := map[string]bool{}
	for _, c := range evidence.MinimalCreates {
		creates[c.Object+" "+c.Operation] = true
	}
	answers := map[string]map[string]string{}
	for _, f := range evidence.Families {
		for _, h := range f.Hashes {
			if answers[h.Object] == nil {
				answers[h.Object] = map[string]string{}
			}
			answers[h.Object][h.Hash] = h.Update
		}
	}
	// updated holds, per table, each path below which the update takes an exposed leaf, and
	// required each hash with a member Stripe requires.
	objects, updated, required := map[string]string{}, map[string]map[string]bool{}, map[string]map[string]bool{}
	for _, l := range coverage.Leaves {
		objects[l.Table] = l.Object
		if l.Side != "request" {
			continue
		}
		parent := l.Path[:max(strings.LastIndex(l.Path, "."), 0)]
		if l.Required && parent != "" {
			if required[l.Table] == nil {
				required[l.Table] = map[string]bool{}
			}
			required[l.Table][parent] = true
		}
		if l.State != "exposed" || creates[l.Object+" "+l.Operation] {
			continue
		}
		if updated[l.Table] == nil {
			updated[l.Table] = map[string]bool{}
		}
		for p := parent; p != ""; p = p[:max(strings.LastIndex(p, "."), 0)] {
			updated[l.Table][p] = true
		}
	}
	list := func(field string) *regexp.Regexp {
		return regexp.MustCompile(field + `:\s*\[\]string\{([^}]*)\}`)
	}
	changed, whole, refused, name := list("SendChangedMembers"), list("SendWholeMembers"), list("RefuseChange"), regexp.MustCompile(`"([^"]+)"`)
	entries := func(re *regexp.Regexp, src []byte) []string {
		var out []string
		if m := re.FindSubmatch(src); m != nil {
			for _, e := range name.FindAllSubmatch(m[1], -1) {
				out = append(out, string(e[1]))
			}
		}
		return out
	}
	declarations, err := filepath.Glob(filepath.Join("*", "resource.go"))
	if err != nil {
		t.Fatal(err)
	}
	checked := 0
	for _, file := range declarations {
		src, err := os.ReadFile(file)
		if err != nil {
			t.Fatal(err)
		}
		table := filepath.Dir(file)
		sent, kept, fixed := entries(changed, src), entries(whole, src), entries(refused, src)
		record := answers[objects[table]]
		goesWhole := func(hash string) bool {
			return required[table][hash] || slices.Contains(kept, hash)
		}
		// within reports a hash that sits in, or is, one the list names.
		within := func(names []string, hash string) bool {
			return slices.ContainsFunc(names, func(n string) bool { return hash == n || strings.HasPrefix(hash, n+".") })
		}
		for _, hash := range sent {
			if update := record[hash]; update != "siblings_kept" {
				t.Errorf("%s sends only the changed members of %s, which the record gives as %q", table, hash, update)
			}
		}
		for _, hash := range kept {
			if record[hash] == "siblings_kept" && !required[table][hash] {
				t.Errorf("%s sends %s whole, which the record lists as keeping its siblings: send its changed members", table, hash)
			}
		}
		for hash, update := range record {
			if !updated[table][hash] {
				continue
			}
			checked++
			i := strings.LastIndex(hash, ".")
			inPart := i < 0 || slices.Contains(sent, hash[:i])
			switch {
			case update == "siblings_kept" && inPart && !slices.Contains(sent, hash) && !goesWhole(hash) && !within(fixed, hash):
				t.Errorf("%s sends %s whole, which the record lists as keeping its siblings: name it in SendChangedMembers", table, hash)
			case update == "refused" && !required[table][hash] && !within(kept, hash):
				t.Errorf("%s can send %s in part, which Stripe refuses: name it in SendWholeMembers", table, hash)
			}
		}
	}
	if checked == 0 {
		t.Error("no table's update takes a hash the record lists; the record or the pattern no longer matches")
	}
}

// A resource directory without a declaration, its docs examples or the six acceptance tests
// fails the build (ADR D1, drift control). The pages under docs/ are generated from the
// schema and the examples; scripts/check-docs.sh checks they are current.
func TestEveryResourceMeetsTheLayout(t *testing.T) {
	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		t.Run(e.Name(), func(t *testing.T) {
			for _, problem := range layoutProblems("../..", e.Name()) {
				t.Error(problem)
			}
		})
	}
}

// layoutProblems checks internal/resources/<name> under the repository root.
func layoutProblems(root, name string) []string {
	dir := filepath.Join(root, "internal", "resources", name)
	typeName := "stripe_" + name
	if _, err := os.Stat(filepath.Join(dir, "resource.go")); err != nil {
		return dataSourceOnlyProblems(root, dir, name, typeName)
	}
	var problems []string
	if _, err := os.Stat(filepath.Join(root, "examples", "resources", typeName, "resource.tf")); err != nil {
		problems = append(problems, fmt.Sprintf("%s: no docs example (examples/resources/%s/resource.tf)", name, typeName))
	}
	if _, err := os.Stat(filepath.Join(root, "examples", "resources", typeName, "import.sh")); err != nil {
		problems = append(problems, fmt.Sprintf("%s: no import example (examples/resources/%s/import.sh)", name, typeName))
	}
	if _, err := os.Stat(filepath.Join(dir, "datasource.go")); err == nil {
		if _, err := os.Stat(filepath.Join(root, "examples", "data-sources", typeName, "data-source.tf")); err != nil {
			problems = append(problems, fmt.Sprintf("%s: no data source example (examples/data-sources/%s/data-source.tf)", name, typeName))
		}
	}

	problems = append(problems, importScriptProblems(root, dir, name, typeName)...)
	problems = append(problems, missingTests(dir, name, requiredTests)...)
	return append(problems, missingImportBlock(dir, name)...)
}

var importCommand = regexp.MustCompile(`^terraform import (\S+)\.(\S+) (\S+)$`)

// importScriptProblems checks that import.sh, which the docs show as the import example, is
// `terraform import <type>.<name> <id>` with the resource's own type name, and that its ID
// has the shape the declaration implies: a slash for a child resource with a Parent, none
// otherwise (docs ADR D4, D5). A missing file is reported by the caller.
func importScriptProblems(root, dir, name, typeName string) []string {
	data, err := os.ReadFile(filepath.Join(root, "examples", "resources", typeName, "import.sh"))
	if err != nil {
		return nil
	}
	var command string
	for _, l := range strings.Split(strings.TrimSpace(string(data)), "\n") {
		if !strings.HasPrefix(l, "#") {
			command = l
		}
	}
	m := importCommand.FindStringSubmatch(command)
	if m == nil {
		return []string{fmt.Sprintf("%s: import.sh must end in `terraform import %s.<name> <id>`, got %q", name, typeName, command)}
	}
	var problems []string
	if m[1] != typeName {
		problems = append(problems, fmt.Sprintf("%s: import.sh imports %s, want %s", name, m[1], typeName))
	}
	if child, hasSlash := hasParent(dir), strings.Contains(m[3], "/"); child != hasSlash {
		if child {
			problems = append(problems, fmt.Sprintf("%s: the declaration has a Parent, so the import ID %q must be <parent_id>/<id>", name, m[3]))
		} else {
			problems = append(problems, fmt.Sprintf("%s: the declaration has no Parent, so the import ID %q must have no slash", name, m[3]))
		}
	}
	return problems
}

// hasParent reports whether the directory's resource.go sets `Parent` in a composite literal.
func hasParent(dir string) bool {
	parsed, err := parser.ParseFile(token.NewFileSet(), filepath.Join(dir, "resource.go"), nil, 0)
	if err != nil {
		return false
	}
	found := false
	ast.Inspect(parsed, func(n ast.Node) bool {
		if kv, ok := n.(*ast.KeyValueExpr); ok {
			if key, ok := kv.Key.(*ast.Ident); ok && key.Name == "Parent" {
				found = true
			}
		}
		return !found
	})
	return found
}

// missingImportBlock reports a resource whose `import` test never uses
// `resource.ImportBlockWithID`, so the `import` block the docs show is not proven (docs ADR D5).
// A resource without an import test is already reported by missingTests.
func missingImportBlock(dir, name string) []string {
	want := "TestAcc" + pascal(name) + "_import"
	files, _ := filepath.Glob(filepath.Join(dir, "*_test.go"))
	for _, file := range files {
		parsed, err := parser.ParseFile(token.NewFileSet(), file, nil, 0)
		if err != nil {
			continue
		}
		for _, decl := range parsed.Decls {
			fn, ok := decl.(*ast.FuncDecl)
			if !ok || fn.Recv != nil || fn.Name.Name != want {
				continue
			}
			found := false
			ast.Inspect(fn.Body, func(n ast.Node) bool {
				if sel, ok := n.(*ast.SelectorExpr); ok && sel.Sel.Name == "ImportBlockWithID" {
					found = true
				}
				return !found
			})
			if !found {
				return []string{fmt.Sprintf("%s: %s has no ImportStateKind: resource.ImportBlockWithID step", name, want)}
			}
		}
	}
	return nil
}

// dataSourceOnlyProblems checks a directory with a data source declaration and no
// resource, such as the balance: a docs example and one `read` acceptance test.
func dataSourceOnlyProblems(root, dir, name, typeName string) []string {
	var problems []string
	if _, err := os.Stat(filepath.Join(dir, "datasource.go")); err != nil {
		problems = append(problems, name+": no declaration (resource.go or datasource.go)")
	}
	if _, err := os.Stat(filepath.Join(root, "examples", "data-sources", typeName, "data-source.tf")); err != nil {
		problems = append(problems, fmt.Sprintf("%s: no data source example (examples/data-sources/%s/data-source.tf)", name, typeName))
	}
	return append(problems, missingTests(dir, name, []string{"read"})...)
}

// missingTests lists the `TestAcc<Name>_<suffix>` functions the directory's tests lack.
func missingTests(dir, name string, suffixes []string) []string {
	var problems []string
	have := map[string]bool{}
	files, _ := filepath.Glob(filepath.Join(dir, "*_test.go"))
	for _, file := range files {
		parsed, err := parser.ParseFile(token.NewFileSet(), file, nil, 0)
		if err != nil {
			problems = append(problems, fmt.Sprintf("%s: %v", name, err))
			continue
		}
		for _, decl := range parsed.Decls {
			if fn, ok := decl.(*ast.FuncDecl); ok && fn.Recv == nil {
				have[fn.Name.Name] = true
			}
		}
	}
	for _, suffix := range suffixes {
		if want := "TestAcc" + pascal(name) + "_" + suffix; !have[want] {
			problems = append(problems, fmt.Sprintf("%s: no acceptance test %s", name, want))
		}
	}
	return problems
}

func pascal(snake string) string {
	var b strings.Builder
	for _, part := range strings.Split(snake, "_") {
		if part != "" {
			b.WriteString(strings.ToUpper(part[:1]) + part[1:])
		}
	}
	return b.String()
}

func TestLayoutCheckFindsGaps(t *testing.T) {
	root := t.TempDir()
	write := func(rel, content string) {
		path := filepath.Join(root, rel)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("internal/resources/billing_meter/resource.go", "package billing_meter\n")
	if got := layoutProblems(root, "billing_meter"); len(got) != 8 {
		t.Fatalf("an empty resource has 8 problems (two examples and six tests), got %d: %v", len(got), got)
	}

	var tests strings.Builder
	tests.WriteString("package billing_meter\n")
	for _, suffix := range requiredTests {
		body := "{}"
		if suffix == "import" {
			body = "{ _ = resource.ImportBlockWithID }"
		}
		tests.WriteString("func TestAccBillingMeter_" + suffix + "() " + body + "\n")
	}
	write("internal/resources/billing_meter/resource_test.go", tests.String())
	write("examples/resources/stripe_billing_meter/resource.tf", "")
	write("examples/resources/stripe_billing_meter/import.sh", "terraform import stripe_billing_meter.example mtr_123\n")
	if got := layoutProblems(root, "billing_meter"); len(got) != 0 {
		t.Fatalf("a complete resource has no problems: %v", got)
	}

	write("examples/resources/stripe_billing_meter/import.sh", "terraform import stripe_coupon.example mtr_123\n")
	if got := layoutProblems(root, "billing_meter"); len(got) != 1 {
		t.Fatalf("an import.sh of another type is one problem: %v", got)
	}
	write("examples/resources/stripe_billing_meter/import.sh", "terraform import stripe_billing_meter.example cus_1/mtr_123\n")
	if got := layoutProblems(root, "billing_meter"); len(got) != 1 {
		t.Fatalf("a slash in the ID of a resource without a Parent is one problem: %v", got)
	}
	write("internal/resources/billing_meter/resource.go", "package billing_meter\nvar _ = Declaration{Parent: \"customer\"}\n")
	if got := layoutProblems(root, "billing_meter"); len(got) != 0 {
		t.Fatalf("a slash in the ID of a child resource is right: %v", got)
	}
	write("examples/resources/stripe_billing_meter/import.sh", "terraform import stripe_billing_meter.example mtr_123\n")
	if got := layoutProblems(root, "billing_meter"); len(got) != 1 {
		t.Fatalf("no slash in the ID of a child resource is one problem: %v", got)
	}
	write("internal/resources/billing_meter/resource.go", "package billing_meter\n")
	write("examples/resources/stripe_billing_meter/import.sh", "terraform import stripe_billing_meter.example mtr_123\n")

	write("internal/resources/billing_meter/datasource.go", "package billing_meter\n")
	if got := layoutProblems(root, "billing_meter"); len(got) != 1 {
		t.Fatalf("a data source needs its example: %v", got)
	}

	write("internal/resources/billing_meter/datasource.go", "package billing_meter\n")
	write("examples/data-sources/stripe_billing_meter/data-source.tf", "")
	tests.Reset()
	tests.WriteString("package billing_meter\n")
	for _, suffix := range requiredTests {
		tests.WriteString("func TestAccBillingMeter_" + suffix + "() {}\n")
	}
	write("internal/resources/billing_meter/resource_test.go", tests.String())
	if got := layoutProblems(root, "billing_meter"); len(got) != 1 {
		t.Fatalf("an import test without ImportBlockWithID is one problem: %v", got)
	}
}

func TestLayoutCheckFindsDataSourceGaps(t *testing.T) {
	root := t.TempDir()
	write := func(rel, content string) {
		path := filepath.Join(root, rel)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("internal/resources/balance/datasource.go", "package balance\n")
	if got := layoutProblems(root, "balance"); len(got) != 2 {
		t.Fatalf("a data source without example or read test has 2 problems, got %d: %v", len(got), got)
	}
	write("internal/resources/balance/datasource_test.go", "package balance\nfunc TestAccBalance_read() {}\n")
	write("examples/data-sources/stripe_balance/data-source.tf", "")
	if got := layoutProblems(root, "balance"); len(got) != 0 {
		t.Fatalf("a complete data source has no problems: %v", got)
	}
}
