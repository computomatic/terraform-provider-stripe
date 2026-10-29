package main

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
)

const repoRoot = "../.."

// The checked-in generated files are exactly what the generator writes from the pinned
// spec. This is the drift check of ADR D1: a changed spec, pin or generator without a
// regenerate fails here.
func TestGeneratedFilesAreCurrent(t *testing.T) {
	files, err := Generate(repoRoot, Resources)
	if err != nil {
		t.Fatal(err)
	}
	if len(files) != len(Resources)+3 {
		t.Fatalf("%d files for %d resources", len(files), len(Resources))
	}
	for _, f := range files {
		onDisk, err := os.ReadFile(filepath.Join(repoRoot, f.Path))
		if err != nil {
			t.Errorf("%s is missing: run `go generate ./internal/generated`", f.Path)
			continue
		}
		if !bytes.Equal(onDisk, f.Content) {
			t.Errorf("%s is out of date: run `go generate ./internal/generated`", f.Path)
		}
	}
}

// Running the generator twice yields the same bytes, so `go generate` leaves no diff.
func TestGenerateIsDeterministic(t *testing.T) {
	first, err := Generate(repoRoot, Resources)
	if err != nil {
		t.Fatal(err)
	}
	second, err := Generate(repoRoot, Resources)
	if err != nil {
		t.Fatal(err)
	}
	for i := range first {
		if first[i].Path != second[i].Path || !bytes.Equal(first[i].Content, second[i].Content) {
			t.Errorf("%s differs between runs", first[i].Path)
		}
	}
}

func TestGenerateChecksThePin(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "spec"), 0o755); err != nil {
		t.Fatal(err)
	}
	pin, err := os.ReadFile(filepath.Join(repoRoot, "spec", "PIN.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "spec", "PIN.json"), pin, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "spec", "openapi.json"), []byte(`{"openapi":"3.0.0"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := Generate(dir, nil); err == nil {
		t.Error("a spec that does not match its pin must be rejected")
	}
}
