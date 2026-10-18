package acctest

import (
	"go/ast"
	"go/parser"
	"go/token"
	"path/filepath"
	"testing"
)

// wave1 are the packages the smoke run executes.
var wave1 = []string{"product", "price", "coupon", "customer", "webhook_endpoint", "billing_meter", "balance"}

// fakeOnly are the calls that have no meaning on real Stripe.
var fakeOnly = map[string]bool{"RequestLog": true, "Seed": true}

// unguarded returns the position of each fake-only call that no earlier RequireFake call
// or read of Config.Smoke in the same top-level declaration guards.
func unguarded(fset *token.FileSet, file *ast.File) []token.Position {
	var out []token.Position
	for _, decl := range file.Decls {
		fn, ok := decl.(*ast.FuncDecl)
		if !ok || fn.Body == nil {
			continue
		}
		guard := token.NoPos
		var calls []*ast.CallExpr
		ast.Inspect(fn.Body, func(n ast.Node) bool {
			switch n := n.(type) {
			case *ast.SelectorExpr:
				if n.Sel.Name == "Smoke" && (guard == token.NoPos || n.Pos() < guard) {
					guard = n.Pos()
				}
			case *ast.CallExpr:
				sel, ok := n.Fun.(*ast.SelectorExpr)
				if !ok {
					return true
				}
				if pkg, ok := sel.X.(*ast.Ident); ok && pkg.Name == "acctest" {
					if sel.Sel.Name == "RequireFake" && (guard == token.NoPos || n.Pos() < guard) {
						guard = n.Pos()
					}
					if fakeOnly[sel.Sel.Name] {
						calls = append(calls, n)
					}
				}
			}
			return true
		})
		for _, call := range calls {
			if guard == token.NoPos || call.Pos() < guard {
				out = append(out, fset.Position(call.Pos()))
			}
		}
	}
	return out
}

func TestUnguardedFakeCallsAreFound(t *testing.T) {
	const src = `package p

func TestBad(t *testing.T) {
	acct := acctest.NewAccount(t)
	_ = acctest.RequestLog(t, acct)
}

func TestLate(t *testing.T) {
	acct := acctest.NewAccount(t)
	_ = acctest.Seed(t, acct)
	acctest.RequireFake(t)
}

func TestGood(t *testing.T) {
	acctest.RequireFake(t)
	acct := acctest.NewAccount(t)
	_ = acctest.RequestLog(t, acct)
}

func TestBranch(t *testing.T) {
	acct := acctest.NewAccount(t)
	func() {
		if acct.Smoke {
			return
		}
		_ = acctest.RequestLog(t, acct)
	}()
}
`
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, "p_test.go", src, 0)
	if err != nil {
		t.Fatal(err)
	}
	got := unguarded(fset, file)
	if len(got) != 2 || got[0].Line != 5 || got[1].Line != 10 {
		t.Fatalf("unguarded calls: %v", got)
	}
}

// A new wave 1 test that reaches for a fake-only aid without RequireFake would break the
// smoke run, so it fails here first.
func TestWave1FakeOnlyCallsAreGuarded(t *testing.T) {
	for _, pkg := range wave1 {
		files, err := filepath.Glob(filepath.Join("..", "resources", pkg, "*_test.go"))
		if err != nil || len(files) == 0 {
			t.Fatalf("no test files for %s: %v", pkg, err)
		}
		for _, name := range files {
			fset := token.NewFileSet()
			file, err := parser.ParseFile(fset, name, nil, 0)
			if err != nil {
				t.Fatal(err)
			}
			for _, pos := range unguarded(fset, file) {
				t.Errorf("%s: a fake-only call without acctest.RequireFake (or a Config.Smoke check) before it", pos)
			}
		}
	}
}
