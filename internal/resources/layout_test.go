package resources

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// requiredTests are the acceptance tests of the P2 per-resource standard (ADR D6), as the
// suffixes of `TestAcc<Resource>_<suffix>`.
var requiredTests = []string{"create", "update", "replace", "import", "delete", "drift"}

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

	return append(problems, missingTests(dir, name, requiredTests)...)
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
		tests.WriteString("func TestAccBillingMeter_" + suffix + "() {}\n")
	}
	write("internal/resources/billing_meter/resource_test.go", tests.String())
	write("examples/resources/stripe_billing_meter/resource.tf", "")
	write("examples/resources/stripe_billing_meter/import.sh", "")
	if got := layoutProblems(root, "billing_meter"); len(got) != 0 {
		t.Fatalf("a complete resource has no problems: %v", got)
	}

	write("internal/resources/billing_meter/datasource.go", "package billing_meter\n")
	if got := layoutProblems(root, "billing_meter"); len(got) != 1 {
		t.Fatalf("a data source needs its example: %v", got)
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
