// Package evidencecheck holds the checks on the headers of a fake release's `attributes.json`
// and of the conformance suite's evidence record that tools/schemagen applies before it
// generates from a pin and tools/evidencepin applies before it writes one, so that the tool
// never writes a pin that generation refuses. The files are named as the provider vendors
// them, `spec/attributes.json` and `spec/evidence.json`.
package evidencecheck

import (
	"fmt"
	"strings"
)

// Spec is what spec/PIN.json pins of the OpenAPI spec.
type Spec struct {
	APIVersion string `json:"api_version"`
	SHA256     string `json:"sha256"`
}

// Attributes is the header of a fake release's `attributes.json`.
type Attributes struct {
	FakeVersion string `json:"fake_version"`
	SuiteRef    string `json:"suite_ref"`
	RunID       int64  `json:"run_id"`
	APIVersion  string `json:"api_version"`
	SpecSHA256  string `json:"spec_sha256"`
}

// Evidence is the header of the suite's evidence record.
type Evidence struct {
	SuiteCommit string `json:"suite_commit"`
	APIVersion  string `json:"api_version"`
	SpecSHA256  string `json:"spec_sha256"`
	StripeRun   struct {
		ID     int64 `json:"id"`
		Passed bool  `json:"passed"`
	} `json:"stripe_run"`
}

// CheckAttributes refuses an `attributes.json` that names no suite run or is for another spec
// than the pinned one.
func CheckAttributes(a Attributes, spec Spec) error {
	if err := NamesRun("spec/attributes.json", a.SuiteRef, a.RunID); err != nil {
		return err
	}
	return sameSpec("spec/attributes.json", a.APIVersion, a.SpecSHA256, spec)
}

// CheckEvidence refuses an evidence record that names no suite run, is of another commit or run
// than the one `attributes.json` names, is for another spec than the pinned one, or is the
// record of a run that did not pass.
func CheckEvidence(e Evidence, a Attributes, spec Spec) error {
	if err := NamesRun("spec/evidence.json", e.SuiteCommit, e.StripeRun.ID); err != nil {
		return err
	}
	if e.SuiteCommit != a.SuiteRef || e.StripeRun.ID != a.RunID {
		return fmt.Errorf("spec/evidence.json is suite run %s/%d, spec/attributes.json names %s/%d", e.SuiteCommit, e.StripeRun.ID, a.SuiteRef, a.RunID)
	}
	if err := sameSpec("spec/evidence.json", e.APIVersion, e.SpecSHA256, spec); err != nil {
		return err
	}
	if !e.StripeRun.Passed {
		return fmt.Errorf("spec/evidence.json is the record of suite run %s/%d, which did not pass", e.SuiteCommit, e.StripeRun.ID)
	}
	return nil
}

// NamesRun refuses a file that names no suite commit or run.
func NamesRun(file, suiteRef string, runID int64) error {
	if strings.TrimSpace(suiteRef) == "" || runID == 0 {
		return fmt.Errorf("%s names no suite run (suite commit %q, run %d)", file, suiteRef, runID)
	}
	return nil
}

func sameSpec(file, apiVersion, specSHA string, spec Spec) error {
	if apiVersion != spec.APIVersion {
		return fmt.Errorf("%s is for API version %s, spec/PIN.json pins %s", file, apiVersion, spec.APIVersion)
	}
	if specSHA != spec.SHA256 {
		return fmt.Errorf("%s is for the spec with sha256 %s, spec/PIN.json pins %s", file, specSHA, spec.SHA256)
	}
	return nil
}
