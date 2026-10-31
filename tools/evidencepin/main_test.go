package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

const (
	testRef   = "abc123"
	testRun   = 42
	testFake  = "v9.9.9"
	testSpec  = `{"api_version":"2000-01-01.fixture","sha256":"f00d"}`
	testAttrs = `{"fake_version":"v9.9.9","suite_ref":"abc123","run_id":42,"api_version":"2000-01-01.fixture","spec_sha256":"f00d","leaves":[]}`
	testEvid  = `{"format_version":1,"suite_commit":"abc123","api_version":"2000-01-01.fixture","spec_sha256":"f00d","stripe_run":{"id":42,"passed":true},"leaves":[]}`
)

// fakeGH answers the two `gh` calls Fetch makes with the given files.
func fakeGH(attributes, evidence string) gh {
	return func(args ...string) ([]byte, error) {
		switch {
		case slices.Equal(args, []string{"release", "download", testFake, "-R", fakeRepo, "-p", "attributes.json", "-O", "-"}):
			return []byte(attributes), nil
		case slices.Equal(args, []string{"api", "-H", "Accept: application/vnd.github.raw", fmt.Sprintf("repos/%s/contents/evidence/%s/%d.json?ref=results", suiteRepo, testRef, testRun)}):
			return []byte(evidence), nil
		case slices.Equal(args, []string{"api", fmt.Sprintf("repos/%s/contents/observations?ref=%s", suiteRepo, testRef)}):
			return []byte(`[{"name":"gadget.json"},{"name":"README.md"}]`), nil
		case slices.Equal(args, []string{"api", "-H", "Accept: application/vnd.github.raw", fmt.Sprintf("repos/%s/contents/observations/gadget.json?ref=%s", suiteRepo, testRef)}):
			return []byte(`{"object":"gadget","probes":{}}`), nil
		}
		return nil, fmt.Errorf("unexpected gh %s", strings.Join(args, " "))
	}
}

func testRoot(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	if err := os.Mkdir(filepath.Join(root, "spec"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, ".fake-version"), []byte("v0.0.1\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "spec", "PIN.json"), []byte(testSpec), 0o644); err != nil {
		t.Fatal(err)
	}
	return root
}

func TestWriteThenVerify(t *testing.T) {
	root := testRoot(t)
	run := fakeGH(testAttrs, testEvid)
	if err := Write(root, testFake, run); err != nil {
		t.Fatal(err)
	}
	read := func(name string) []byte {
		t.Helper()
		data, err := os.ReadFile(filepath.Join(root, name))
		if err != nil {
			t.Fatal(err)
		}
		return data
	}
	if got := string(read(".fake-version")); got != testFake+"\n" {
		t.Errorf(".fake-version is %q", got)
	}
	if got := string(read("spec/attributes.json")); got != testAttrs {
		t.Errorf("spec/attributes.json is %q", got)
	}
	var pin Pin
	if err := json.Unmarshal(read("spec/EVIDENCE_PIN.json"), &pin); err != nil {
		t.Fatal(err)
	}
	want := Pin{FakeVersion: testFake, SuiteRef: testRef, RunID: testRun, Attributes: pinned([]byte(testAttrs)), Evidence: pinned([]byte(testEvid)), CreateProbes: pinned(read("spec/create_probes.json"))}
	if pin != want {
		t.Errorf("pin is %+v, want %+v", pin, want)
	}
	if err := Verify(root, run); err != nil {
		t.Fatalf("Verify after Write: %v", err)
	}

	if err := os.WriteFile(filepath.Join(root, "spec", "evidence.json"), []byte("{}"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := Verify(root, run); err == nil || !strings.Contains(err.Error(), "spec/evidence.json differ") {
		t.Errorf("Verify of an edited evidence file: %v", err)
	}
}

func TestFetchRefuses(t *testing.T) {
	for name, tc := range map[string]struct{ attributes, evidence, want string }{
		"another release":                    {strings.Replace(testAttrs, testFake, "v9.9.8", 1), testEvid, `names fake "v9.9.8"`},
		"no run":                             {strings.Replace(testAttrs, `"run_id":42`, `"run_id":0`, 1), testEvid, "spec/attributes.json names no suite run"},
		"another run":                        {testAttrs, strings.Replace(testEvid, `"id":42`, `"id":43`, 1), "spec/evidence.json is suite run abc123/43"},
		"a failed run":                       {testAttrs, strings.Replace(testEvid, `"passed":true`, `"passed":false`, 1), "which did not pass"},
		"attributes for another API version": {strings.Replace(testAttrs, `"2000-01-01.fixture"`, `"2001-01-01.other"`, 1), testEvid, "spec/attributes.json is for API version 2001-01-01.other"},
		"evidence for another spec":          {testAttrs, strings.Replace(testEvid, `"f00d"`, `"0000"`, 1), "spec/evidence.json is for the spec with sha256 0000"},
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := Fetch(testRoot(t), testFake, fakeGH(tc.attributes, tc.evidence)); err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Errorf("got %v, want an error containing %q", err, tc.want)
			}
		})
	}
}
