package evidencecheck

import (
	"encoding/json"
	"strings"
	"testing"
)

const probeRecord = `{
  "leaves": [
    {"object": "gadget", "side": "request", "operation": "PostGadgets", "path": "group"},
    {"object": "gadget", "side": "request", "operation": "PostGadgets", "path": "note"},
    {"object": "gadget", "side": "request", "operation": "PostGadgets", "path": "items.price", "returned": {"response_path": "items.data.price"}},
    {"object": "gadget", "side": "request", "operation": "PostGadgets", "path": "items.price_data.name", "returned": {"response_path": "items.data.price"}},
    {"object": "gadget", "side": "request", "operation": "PostGadgetsId", "path": "group"},
    {"object": "gadget", "side": "response", "operation": "", "path": "created"}
  ],
  "minimal_creates": [{"object": "gadget", "operation": "PostGadgets"}]
}`

const probeObservations = `{
  "object": "gadget",
  "probes": {
    "b": {"requests": [{"operation": "PostGadgets", "status": 200, "sent": ["note"], "response": {
      "group": {"kind": "created", "prefix": "group_"},
      "note": {"kind": "sent", "path": "note"},
      "created": {"kind": "timestamp"},
      "items.price": {"kind": "value", "value": "p"}
    }}]},
    "d": {"requests": [{"operation": "PostGadgets", "status": 200, "sent": ["items.price_data.name"], "response": {"items.price": {"kind": "id", "prefix": "price_"}}}]},
    "a": {"requests": [
      {"operation": "PostGadgets", "status": 200, "sent": [], "response": {"group": {"kind": "created", "prefix": "group_"}, "note": {"kind": "sent", "path": "name"}}},
      {"operation": "PostGadgets", "status": 400, "sent": [], "response": {"group": {"kind": "value", "value": "refused"}}},
      {"operation": "PostGadgetsId", "status": 200, "sent": [], "response": {"group": {"kind": "value", "value": "updated"}}}
    ]},
    "c": {"requests": [{"operation": "PostGadgets", "status": 200, "sent": ["group.a"], "response": {"group": {"kind": "value", "value": "sent in part"}}}]}
  }
}`

// The digest holds what a successful create left the leaf out for: not what it sent, what an update
// or a refusal held, or a leaf the record has no create of, and each value once, with its first
// probe by name. A value another argument brought counts; a list's member is read by its request
// path, and leaves that share a response path each count, but for the one a request sent.
func TestBuildCreateProbes(t *testing.T) {
	data, err := BuildCreateProbes("abc", []byte(probeRecord), [][]byte{[]byte(probeObservations)})
	if err != nil {
		t.Fatal(err)
	}
	var got CreateProbes
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatal(err)
	}
	var lines []string
	for _, l := range got.Leaves {
		for _, v := range l.Held {
			lines = append(lines, l.Operation+" "+l.Path+" "+v.Kind+" "+string(v.Value)+" "+v.Probe)
		}
	}
	want := `PostGadgets group created  a|PostGadgets items.price id  d|PostGadgets items.price value "p" b|PostGadgets items.price_data.name value "p" b|PostGadgets note sent  a`
	if joined := strings.Join(lines, "|"); got.SuiteRef != "abc" || joined != want {
		t.Errorf("digest %s %s, want %s", got.SuiteRef, joined, want)
	}
}

// An object the record creates with no observations fails, or the digest would pass for clean.
func TestBuildCreateProbesRefusesAMissingObject(t *testing.T) {
	if _, err := BuildCreateProbes("abc", []byte(probeRecord), nil); err == nil || !strings.Contains(err.Error(), "no observations of gadget") {
		t.Errorf("got %v", err)
	}
}
