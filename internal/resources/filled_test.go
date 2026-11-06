package resources

import (
	"encoding/json"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

// A declaration's FilledFrom leaves the fields Stripe fills from another unknown in a plan that
// changes it. Where it relies on a relation the record's `filled_from` lists, the record must show
// the relation holding wherever the declaration applies it, which is every plan that changes the
// source: an entry may not rely on a relation the record lists exceptions to (`except`), which a
// plan cannot tell apart.
func TestFilledFromHoldsInTheRecord(t *testing.T) {
	var evidence struct {
		FilledFrom []struct {
			Object string            `json:"object"`
			Path   string            `json:"path"`
			From   string            `json:"from"`
			Except []json.RawMessage `json:"except"`
		} `json:"filled_from"`
	}
	raw, err := os.ReadFile(filepath.Join("..", "..", "spec", "evidence.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(raw, &evidence); err != nil {
		t.Fatal(err)
	}
	var coverage struct {
		Leaves []struct{ Table, Object string } `json:"leaves"`
	}
	if raw, err = os.ReadFile(filepath.Join("..", "generated", "coverage.json")); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(raw, &coverage); err != nil {
		t.Fatal(err)
	}
	objects := map[string]string{}
	for _, l := range coverage.Leaves {
		objects[l.Table] = l.Object
	}
	declarations, err := filepath.Glob(filepath.Join("*", "resource.go"))
	if err != nil {
		t.Fatal(err)
	}
	relied := 0
	for _, file := range declarations {
		table := filepath.Dir(file)
		for source, filled := range filledFrom(t, file) {
			if filled == nil {
				for _, r := range evidence.FilledFrom {
					if r.Object == objects[table] && r.From == source {
						t.Errorf("%s: the FilledFrom entry of `%s` is not a list literal this test can read, and the record relates `%s` to it", table, source, r.Path)
					}
				}
			}
			for _, f := range filled {
				for _, r := range evidence.FilledFrom {
					if r.Object != objects[table] || r.From != source || r.Path != f && !strings.HasPrefix(r.Path, f+".") {
						continue
					}
					relied++
					if len(r.Except) > 0 {
						t.Errorf("%s fills `%s` from `%s`, which the record shows not holding in %d responses (`filled_from[].except`)", table, r.Path, source, len(r.Except))
					}
				}
			}
		}
	}
	if relied == 0 {
		t.Error("no declaration relies on a relation the record lists; the record or the pattern no longer matches")
	}
}

// filledFrom reads the FilledFrom map literal of a declaration's source, keyed by the source
// field.
func filledFrom(t *testing.T, file string) map[string][]string {
	t.Helper()
	f, err := parser.ParseFile(token.NewFileSet(), file, nil, 0)
	if err != nil {
		t.Fatal(err)
	}
	out := map[string][]string{}
	unquote := func(e ast.Expr) string {
		lit, ok := e.(*ast.BasicLit)
		if !ok || lit.Kind != token.STRING {
			t.Fatalf("%s: FilledFrom holds %T, not a string literal", file, e)
		}
		s, err := strconv.Unquote(lit.Value)
		if err != nil {
			t.Fatal(err)
		}
		return s
	}
	ast.Inspect(f, func(n ast.Node) bool {
		kv, ok := n.(*ast.KeyValueExpr)
		if !ok {
			return true
		}
		if key, isIdent := kv.Key.(*ast.Ident); !isIdent || key.Name != "FilledFrom" {
			return true
		}
		lit, ok := kv.Value.(*ast.CompositeLit)
		if !ok {
			t.Fatalf("%s: FilledFrom is not a map literal", file)
		}
		for _, elt := range lit.Elts {
			entry := elt.(*ast.KeyValueExpr)
			// An entry whose fields a variable lists, as a dispute's evidence hashes, keeps its
			// source with no fields: the test then fails if the record relates anything to it.
			var filled []string
			if v, isList := entry.Value.(*ast.CompositeLit); isList {
				for _, e := range v.Elts {
					filled = append(filled, unquote(e))
				}
			}
			out[unquote(entry.Key)] = filled
		}
		return false
	})
	return out
}
