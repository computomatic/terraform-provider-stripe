// Command evidencepin vendors a fake release's `attributes.json` as spec/attributes.json and
// the conformance suite's evidence record it names as spec/evidence.json, writes their pin,
// spec/EVIDENCE_PIN.json, and moves .fake-version to the release. With -verify it fetches
// both again and fails unless the vendored files, the pin and .fake-version still match them.
// It fetches with `gh`, so it needs read access to both repositories.
//
//	evidencepin [-root .] [-fake vX.Y.Z]
//	evidencepin [-root .] -verify
package main

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"

	"github.com/computomatic/terraform-provider-stripe/tools/internal/evidencecheck"
)

const (
	fakeRepo  = "computomatic/fake-stripe-api"
	suiteRepo = "computomatic/stripe-api-conformance"
)

// Pin is spec/EVIDENCE_PIN.json, as tools/schemagen reads it.
type Pin struct {
	FakeVersion string     `json:"fake_version"`
	SuiteRef    string     `json:"suite_ref"`
	RunID       int64      `json:"run_id"`
	Attributes  PinnedFile `json:"attributes"`
	Evidence    PinnedFile `json:"evidence"`
}

// PinnedFile is the size and SHA-256 of one vendored file.
type PinnedFile struct {
	Bytes  int    `json:"bytes"`
	SHA256 string `json:"sha256"`
}

// Fetched is a fake release's `attributes.json`, the evidence record it names, and their pin.
type Fetched struct {
	Pin        Pin
	Attributes []byte
	Evidence   []byte
}

// gh runs the GitHub CLI and returns its standard output.
type gh func(args ...string) ([]byte, error)

func runGH(args ...string) ([]byte, error) {
	var stderr bytes.Buffer
	cmd := exec.Command("gh", args...)
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("gh %s: %w: %s", strings.Join(args, " "), err, strings.TrimSpace(stderr.String()))
	}
	return out, nil
}

func main() {
	root := flag.String("root", ".", "repository root")
	fake := flag.String("fake", "", "fake release to pin (default: the one in .fake-version)")
	verify := flag.Bool("verify", false, "fetch again and check the vendored files, the pin and .fake-version")
	flag.Parse()
	if flag.NArg() != 0 || *verify && *fake != "" {
		fmt.Fprintln(os.Stderr, "usage: evidencepin [-root .] [-fake vX.Y.Z] | evidencepin [-root .] -verify")
		os.Exit(64)
	}
	var err error
	if *verify {
		err = Verify(*root, runGH)
	} else {
		err = Write(*root, *fake, runGH)
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "evidencepin:", err)
		os.Exit(1)
	}
}

// Fetch downloads `attributes.json` from the fake release and the evidence record of the suite
// commit and run it names, and refuses what generation would: an `attributes.json` of another
// release or naming no run, a record of another commit or run, either file for another spec
// than spec/PIN.json under root pins, and a record of a run that did not pass (evidencecheck).
func Fetch(root, fake string, run gh) (*Fetched, error) {
	specData, err := os.ReadFile(filepath.Join(root, "spec", "PIN.json"))
	if err != nil {
		return nil, err
	}
	var spec evidencecheck.Spec
	if err := json.Unmarshal(specData, &spec); err != nil {
		return nil, fmt.Errorf("decode spec/PIN.json: %w", err)
	}
	attributes, err := run("release", "download", fake, "-R", fakeRepo, "-p", "attributes.json", "-O", "-")
	if err != nil {
		return nil, err
	}
	var a evidencecheck.Attributes
	if err := json.Unmarshal(attributes, &a); err != nil {
		return nil, fmt.Errorf("decode %s attributes.json: %w", fake, err)
	}
	if a.FakeVersion != fake {
		return nil, fmt.Errorf("the attributes.json of release %s names fake %q", fake, a.FakeVersion)
	}
	if err := evidencecheck.CheckAttributes(a, spec); err != nil {
		return nil, fmt.Errorf("release %s: %w", fake, err)
	}

	path := fmt.Sprintf("evidence/%s/%d.json", a.SuiteRef, a.RunID)
	evidence, err := run("api", "-H", "Accept: application/vnd.github.raw", fmt.Sprintf("repos/%s/contents/%s?ref=results", suiteRepo, path))
	if err != nil {
		return nil, err
	}
	var e evidencecheck.Evidence
	if err := json.Unmarshal(evidence, &e); err != nil {
		return nil, fmt.Errorf("decode %s: %w", path, err)
	}
	if err := evidencecheck.CheckEvidence(e, a, spec); err != nil {
		return nil, fmt.Errorf("%s of the suite's results branch: %w", path, err)
	}

	return &Fetched{
		Pin: Pin{
			FakeVersion: fake,
			SuiteRef:    a.SuiteRef,
			RunID:       a.RunID,
			Attributes:  pinned(attributes),
			Evidence:    pinned(evidence),
		},
		Attributes: attributes,
		Evidence:   evidence,
	}, nil
}

// Write fetches the release's files and vendors them with their pin under root. An empty fake
// re-pins the release .fake-version names.
func Write(root, fake string, run gh) error {
	if fake == "" {
		var err error
		if fake, err = fakeVersion(root); err != nil {
			return err
		}
	}
	f, err := Fetch(root, fake, run)
	if err != nil {
		return err
	}
	pin, err := f.Pin.encode()
	if err != nil {
		return err
	}
	for name, data := range map[string][]byte{
		"spec/attributes.json":   f.Attributes,
		"spec/evidence.json":     f.Evidence,
		"spec/EVIDENCE_PIN.json": pin,
		".fake-version":          []byte(fake + "\n"),
	} {
		if err := os.WriteFile(filepath.Join(root, name), data, 0o644); err != nil {
			return err
		}
	}
	return nil
}

// Verify fetches the files of the release .fake-version names and fails unless the pin and
// both vendored files are exactly what Write would write.
func Verify(root string, run gh) error {
	fake, err := fakeVersion(root)
	if err != nil {
		return err
	}
	f, err := Fetch(root, fake, run)
	if err != nil {
		return err
	}
	pin, err := f.Pin.encode()
	if err != nil {
		return err
	}
	var stale []string
	for name, want := range map[string][]byte{
		"spec/attributes.json":   f.Attributes,
		"spec/evidence.json":     f.Evidence,
		"spec/EVIDENCE_PIN.json": pin,
	} {
		got, err := os.ReadFile(filepath.Join(root, name))
		if err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}
		if !bytes.Equal(got, want) {
			stale = append(stale, name)
		}
	}
	if len(stale) > 0 {
		slices.Sort(stale)
		return fmt.Errorf("%s differ from fake %s and the evidence it names: run `go run ./tools/evidencepin`", strings.Join(stale, ", "), fake)
	}
	return nil
}

func fakeVersion(root string) (string, error) {
	data, err := os.ReadFile(filepath.Join(root, ".fake-version"))
	if err != nil {
		return "", err
	}
	v := strings.TrimSpace(string(data))
	if v == "" {
		return "", errors.New(".fake-version is empty")
	}
	return v, nil
}

func pinned(data []byte) PinnedFile {
	sum := sha256.Sum256(data)
	return PinnedFile{Bytes: len(data), SHA256: hex.EncodeToString(sum[:])}
}

func (p Pin) encode() ([]byte, error) {
	data, err := json.MarshalIndent(p, "", "  ")
	if err != nil {
		return nil, err
	}
	return append(data, '\n'), nil
}
