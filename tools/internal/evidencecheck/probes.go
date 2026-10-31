package evidencecheck

import (
	"encoding/json"
	"fmt"
	"slices"
	"strings"
)

// CreateProbes is spec/create_probes.json: what Stripe held, on a create of the conformance
// suite that left a request leaf out, in the observations of the suite commit the evidence record
// names. The record's defaults hold only the minimal create and its variants, so they say nothing
// of a hash sent in part or of a value another argument brings; these probes do. The observations
// run to tens of megabytes, so the provider vendors this digest of them.
type CreateProbes struct {
	SuiteRef string       `json:"suite_ref"`
	Leaves   []ProbedLeaf `json:"leaves"`
}

// ProbedLeaf is one request leaf of a create and each thing Stripe held with the leaf left out.
type ProbedLeaf struct {
	Object    string        `json:"object"`
	Operation string        `json:"operation"`
	Path      string        `json:"path"`
	Held      []ProbedValue `json:"held"`
}

// ProbedValue is what Stripe held: a literal of kind `value`, or a kind that names what it made or
// copied. Probe is the first probe, by name, that shows it.
type ProbedValue struct {
	Kind  string          `json:"kind"`
	Value json.RawMessage `json:"value,omitempty"`
	Probe string          `json:"probe"`
}

// recordOutline is the part of the evidence record the digest needs.
type recordOutline struct {
	Leaves []struct {
		Object    string          `json:"object"`
		Side      string          `json:"side"`
		Operation string          `json:"operation"`
		Path      string          `json:"path"`
		Returned  json.RawMessage `json:"returned"`
	} `json:"leaves"`
	MinimalCreates []struct {
		Object    string `json:"object"`
		Operation string `json:"operation"`
	} `json:"minimal_creates"`
}

// observations is the part of a suite observation file the digest needs.
type observations struct {
	Object string `json:"object"`
	Probes map[string]struct {
		Requests []struct {
			Operation string   `json:"operation"`
			Status    int      `json:"status"`
			Sent      []string `json:"sent"`
			Response  map[string]struct {
				Kind  string          `json:"kind"`
				Path  string          `json:"path"`
				Value json.RawMessage `json:"value"`
			} `json:"response"`
		} `json:"requests"`
	} `json:"probes"`
}

// BuildCreateProbes digests the suite's observation files against the evidence record: each create
// the record has a minimal create for, on the request leaves it holds. An object the record
// creates with no observations fails, so that a thin digest cannot pass for a clean one.
func BuildCreateProbes(suiteRef string, evidence []byte, files [][]byte) ([]byte, error) {
	var record recordOutline
	if err := json.Unmarshal(evidence, &record); err != nil {
		return nil, fmt.Errorf("decode the evidence record: %w", err)
	}
	creates := map[string]map[string]bool{}
	for _, c := range record.MinimalCreates {
		if creates[c.Object] == nil {
			creates[c.Object] = map[string]bool{}
		}
		creates[c.Object][c.Operation] = true
	}
	// A list's members are `line_items.data.price` in the record's response paths and
	// `line_items.price` in a probe's response. Several leaves can share a response path, as
	// `shipping_rate` and the members of `shipping_rate_data` do.
	requests := map[string]map[string][]string{}
	for _, l := range record.Leaves {
		if l.Side != "request" || !creates[l.Object][l.Operation] {
			continue
		}
		at := l.Path
		var returned struct {
			ResponsePath string `json:"response_path"`
		}
		if l.Returned != nil && json.Unmarshal(l.Returned, &returned) == nil && returned.ResponsePath != "" {
			at = strings.ReplaceAll(returned.ResponsePath, ".data.", ".")
		}
		key := l.Object + " " + l.Operation
		if requests[key] == nil {
			requests[key] = map[string][]string{}
		}
		requests[key][at] = append(requests[key][at], l.Path)
	}

	held := map[string]map[string]ProbedValue{}
	read := map[string]bool{}
	for _, data := range files {
		var o observations
		if err := json.Unmarshal(data, &o); err != nil {
			return nil, fmt.Errorf("decode an observation file: %w", err)
		}
		read[o.Object] = true
		names := make([]string, 0, len(o.Probes))
		for name := range o.Probes {
			names = append(names, name)
		}
		slices.Sort(names)
		for _, name := range names {
			for _, r := range o.Probes[name].Requests {
				if r.Status != 200 || !creates[o.Object][r.Operation] {
					continue
				}
				key := o.Object + " " + r.Operation
				for at, m := range r.Response {
					if m.Kind == "null" || m.Kind == "absent" {
						continue
					}
					for _, path := range requests[key][at] {
						own := (m.Kind == "sent" || m.Kind == "normalised") && m.Path == path
						if own || sent(r.Sent, path) {
							continue
						}
						leaf := key + " " + path
						if held[leaf] == nil {
							held[leaf] = map[string]ProbedValue{}
						}
						value := ProbedValue{Kind: m.Kind, Probe: name}
						if m.Kind == "value" {
							value.Value = m.Value
						}
						id := m.Kind + " " + string(value.Value)
						if _, seen := held[leaf][id]; !seen {
							held[leaf][id] = value
						}
					}
				}
			}
		}
	}
	for object := range creates {
		if !read[object] {
			return nil, fmt.Errorf("the suite has no observations of %s, which the record creates", object)
		}
	}

	out := CreateProbes{SuiteRef: suiteRef, Leaves: []ProbedLeaf{}}
	for leaf, values := range held {
		parts := strings.SplitN(leaf, " ", 3)
		p := ProbedLeaf{Object: parts[0], Operation: parts[1], Path: parts[2]}
		for _, v := range values {
			p.Held = append(p.Held, v)
		}
		slices.SortFunc(p.Held, func(a, b ProbedValue) int {
			return strings.Compare(a.Kind+" "+string(a.Value), b.Kind+" "+string(b.Value))
		})
		out.Leaves = append(out.Leaves, p)
	}
	slices.SortFunc(out.Leaves, func(a, b ProbedLeaf) int {
		return strings.Compare(a.Object+" "+a.Operation+" "+a.Path, b.Object+" "+b.Operation+" "+b.Path)
	})
	data, err := json.MarshalIndent(out, "", " ")
	if err != nil {
		return nil, err
	}
	return append(data, '\n'), nil
}

// sent reports whether a request sent the leaf at path, or any member below it.
func sent(paths []string, path string) bool {
	return slices.ContainsFunc(paths, func(p string) bool { return p == path || strings.HasPrefix(p, path+".") })
}
