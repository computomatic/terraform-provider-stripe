// Command schemagen reads the pinned Stripe OpenAPI spec and evidence and writes the
// checked-in field tables under internal/generated, the coverage report beside them
// (coverage.json and coverage.md), and the pinned API version constant.
//
// Run it through `go generate ./internal/generated`.
package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
)

// Pin is spec/PIN.json.
type Pin struct {
	APIVersion string `json:"api_version"`
	Tag        string `json:"tag"`
	Commit     string `json:"commit"`
	SHA256     string `json:"sha256"`
	Bytes      int    `json:"bytes"`
}

// File is one generated file: a path relative to the repository root and its content.
type File struct {
	Path    string
	Content []byte
}

func main() {
	root := flag.String("root", ".", "repository root")
	flag.Parse()
	files, err := Generate(*root, Resources)
	if err != nil {
		fmt.Fprintln(os.Stderr, "schemagen:", err)
		os.Exit(1)
	}
	for _, f := range files {
		path := filepath.Join(*root, f.Path)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			fmt.Fprintln(os.Stderr, "schemagen:", err)
			os.Exit(1)
		}
		if err := os.WriteFile(path, f.Content, 0o644); err != nil {
			fmt.Fprintln(os.Stderr, "schemagen:", err)
			os.Exit(1)
		}
	}
}

// Generate reads root/spec and returns every file the generator owns. It writes nothing,
// so a test can compare the result with the checked-in files.
func Generate(root string, resources []Resource) ([]File, error) {
	pinData, err := os.ReadFile(filepath.Join(root, "spec", "PIN.json"))
	if err != nil {
		return nil, err
	}
	var pin Pin
	if err := json.Unmarshal(pinData, &pin); err != nil {
		return nil, fmt.Errorf("decode spec/PIN.json: %w", err)
	}
	specData, err := os.ReadFile(filepath.Join(root, "spec", "openapi.json"))
	if err != nil {
		return nil, err
	}
	if err := checkPin(pin, specData); err != nil {
		return nil, err
	}
	spec, err := ParseSpec(specData)
	if err != nil {
		return nil, err
	}
	if spec.Info.Version != pin.APIVersion {
		return nil, fmt.Errorf("spec/openapi.json info.version is %q, spec/PIN.json api_version is %q", spec.Info.Version, pin.APIVersion)
	}

	version, err := RenderVersion(pin.APIVersion)
	if err != nil {
		return nil, err
	}
	ev, err := LoadEvidence(root, pin)
	if err != nil {
		return nil, err
	}
	files := []File{{Path: "internal/stripeapi/version.go", Content: version}}
	worded, err := declaredDefaults(root)
	if err != nil {
		return nil, err
	}
	var covered []*tableCoverage
	var records []Record
	readers := map[string][]string{}
	for _, res := range resources {
		tc, err := coverTable(spec, res, ev)
		if err != nil {
			return nil, err
		}
		tc.applyDeclaredDefaults(worded[res.Package])
		covered = append(covered, tc)
		readers[tc.object] = append(readers[tc.object], "stripe_"+res.Package)
	}
	for i, res := range resources {
		tc := covered[i]
		annotateCopies(tc, readers)
		src, err := RenderTable(res.Package, tc.table)
		if err != nil {
			return nil, err
		}
		files = append(files, File{Path: filepath.Join("internal", "generated", res.Package, "fields.go"), Content: src})
		records = append(records, tc.records...)
	}
	if err := checkPagedReaders(resources); err != nil {
		return nil, err
	}
	if ev != nil {
		if err := checkVerifiedKeys(ev, covered); err != nil {
			return nil, err
		}
	}
	sortRecords(records)
	if err := checkStates(records); err != nil {
		return nil, err
	}
	if ev != nil {
		if err := checkAcceptedOnly(resources, records); err != nil {
			return nil, err
		}
		if err := checkRequiredMembers(records); err != nil {
			return nil, err
		}
	}
	h := coverageHeader{APIVersion: pin.APIVersion, SpecTag: pin.Tag}
	if ev != nil {
		h.Evidence = &ev.Pin
	}
	for _, r := range records {
		h.Totals.add(r)
	}
	report, err := RenderCoverageJSON(h, records)
	if err != nil {
		return nil, err
	}
	files = append(files,
		File{Path: filepath.Join("internal", "generated", "coverage.json"), Content: report},
		File{Path: filepath.Join("internal", "generated", "coverage.md"), Content: RenderCoverageMarkdown(h, records)})
	return files, nil
}

func checkPin(pin Pin, specData []byte) error {
	sum := sha256.Sum256(specData)
	if got := hex.EncodeToString(sum[:]); got != pin.SHA256 {
		return fmt.Errorf("spec/openapi.json has sha256 %s, spec/PIN.json says %s", got, pin.SHA256)
	}
	if len(specData) != pin.Bytes {
		return fmt.Errorf("spec/openapi.json is %d bytes, spec/PIN.json says %d", len(specData), pin.Bytes)
	}
	if pin.APIVersion == "" {
		return fmt.Errorf("spec/PIN.json has no api_version")
	}
	return nil
}
