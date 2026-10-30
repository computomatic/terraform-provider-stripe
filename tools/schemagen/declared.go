package main

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/computomatic/terraform-provider-stripe/internal/fields"
)

// declaredDefaults reads, by package, the rules the declarations under internal/resources word
// in `Defaults`, so that the coverage report counts a worded argument as recorded. It parses
// the source because a declaration compiles against the tables this tool writes, so importing
// one would need the output of the run that makes it. A package holds one declaration's
// `Defaults`: the report keys them by package, so a second would hide the first's.
func declaredDefaults(root string) (map[string]map[string]string, error) {
	files, err := filepath.Glob(filepath.Join(root, "internal", "resources", "*", "*.go"))
	if err != nil {
		return nil, err
	}
	out := map[string]map[string]string{}
	fset := token.NewFileSet()
	for _, path := range files {
		if strings.HasSuffix(path, "_test.go") {
			continue
		}
		f, err := parser.ParseFile(fset, path, nil, 0)
		if err != nil {
			return nil, err
		}
		var failed error
		ast.Inspect(f, func(n ast.Node) bool {
			kv, ok := n.(*ast.KeyValueExpr)
			if !ok || failed != nil {
				return failed == nil
			}
			if key, isIdent := kv.Key.(*ast.Ident); !isIdent || key.Name != "Defaults" {
				return true
			}
			rules, err := stringMapLiteral(kv.Value)
			if err != nil {
				failed = fmt.Errorf("%s: `Defaults`: %w", fset.Position(kv.Pos()), err)
				return false
			}
			pkg := filepath.Base(filepath.Dir(path))
			if _, twice := out[pkg]; twice {
				failed = fmt.Errorf("%s: package %s has a second `Defaults`", fset.Position(kv.Pos()), pkg)
				return false
			}
			out[pkg] = rules
			return false
		})
		if failed != nil {
			return nil, failed
		}
	}
	return out, nil
}

// stringMapLiteral is a `map[string]string{...}` literal of string literals.
func stringMapLiteral(e ast.Expr) (map[string]string, error) {
	lit, ok := e.(*ast.CompositeLit)
	if !ok {
		return nil, fmt.Errorf("not a map literal")
	}
	out := make(map[string]string, len(lit.Elts))
	for _, elt := range lit.Elts {
		kv, ok := elt.(*ast.KeyValueExpr)
		if !ok {
			return nil, fmt.Errorf("not a key and value")
		}
		key, err := stringLiteral(kv.Key)
		if err != nil {
			return nil, err
		}
		if out[key], err = stringLiteral(kv.Value); err != nil {
			return nil, err
		}
	}
	return out, nil
}

func stringLiteral(e ast.Expr) (string, error) {
	lit, ok := e.(*ast.BasicLit)
	if !ok || lit.Kind != token.STRING {
		return "", fmt.Errorf("not a string literal")
	}
	return strconv.Unquote(lit.Value)
}

// applyDeclaredDefaults gives a request leaf its declaration's statement, the one the engine
// gives the docs (Declaration.Defaults), where the evidence alone has none or another rule.
func (tc *tableCoverage) applyDeclaredDefaults(rules map[string]string) {
	for i, l := range tc.leaves {
		r := &tc.records[i]
		rule, ok := rules[l.At]
		if !ok || r.Default == nil {
			continue
		}
		if f := fieldAt(tc.table, l.At); f != nil && f.Default != nil {
			d := fields.Default{Kind: fields.DefaultRule, Rule: rule, Observed: f.Default.Observed}
			r.Default.Statement, r.Default.Source = d.Statement(), "declaration"
		}
	}
}
