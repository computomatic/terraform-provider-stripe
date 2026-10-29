package engine

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-go/tfprotov6"
	"github.com/hashicorp/terraform-plugin-go/tftypes"

	"github.com/computomatic/terraform-provider-stripe/internal/fields"
	"github.com/computomatic/terraform-provider-stripe/internal/stripeapi"
	"github.com/computomatic/terraform-provider-stripe/internal/wire"
)

func uploadTable() []fields.Field {
	return []fields.Field{
		{Name: "id", Shape: fields.String, Response: true},
		{Name: "purpose", Shape: fields.String, Required: true, Create: true, Response: true},
		{Name: "filename", Shape: fields.String, Response: true},
		{Name: "size", Shape: fields.Int, Response: true},
	}
}

func uploadDeclaration() Declaration {
	return Declaration{
		TypeName:     "stripe_upload",
		Paths:        Paths{Create: "/v1/uploads", Retrieve: "/v1/uploads/{id}"},
		Upload:       &Upload{Part: "file"},
		Delete:       Refuse,
		RefuseReason: "Stripe cannot delete an upload.",
	}
}

// writeSource writes content to a new file in a test directory and returns its path.
func writeSource(t *testing.T, name, content string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(p, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	return p
}

func sha(content string) string {
	h := sha256.Sum256([]byte(content))
	return hex.EncodeToString(h[:])
}

// uploadStub answers an upload with the object and every other request with it too, and
// records the requests.
type uploadStub struct{ log []stripeapi.Request }

func (s *uploadStub) Do(_ context.Context, req stripeapi.Request) (*stripeapi.Response, error) {
	s.log = append(s.log, req)
	return &stripeapi.Response{Status: 200, Body: wire.Object{
		{Name: "id", Value: wire.String("upl_1")},
		{Name: "purpose", Value: wire.String("evidence")},
		{Name: "filename", Value: wire.String("logo.png")},
		{Name: "size", Value: wire.Int(95)},
	}}, nil
}

// uploadState is an uploaded object in state, whose source and hash are given.
func (p *planner) uploadState(source, hash tftypes.Value) map[string]tftypes.Value {
	return map[string]tftypes.Value{
		"id": tfString("upl_1"), "purpose": tfString("evidence"), "filename": tfString("logo.png"), "size": tftypes.NewValue(tftypes.Number, 95),
		sourceAttr: source, sourceHashAttr: hash,
		"timeouts": tftypes.NewValue(p.typ.(tftypes.Object).AttributeTypes["timeouts"], nil),
	}
}

// uploadChange plans the state with the configuration's source set to source.
func (p *planner) uploadChange(state map[string]tftypes.Value, source tftypes.Value) *tfprotov6.PlanResourceChangeResponse {
	p.t.Helper()
	proposed := clone(state, map[string]tftypes.Value{sourceAttr: source})
	config := clone(proposed, map[string]tftypes.Value{
		"id": tftypes.NewValue(tftypes.String, nil), "filename": tftypes.NewValue(tftypes.String, nil),
		"size": tftypes.NewValue(tftypes.Number, nil), sourceHashAttr: tftypes.NewValue(tftypes.String, nil),
	})
	return p.planResponse(p.value(state), p.value(proposed), p.value(config))
}

func plannedHash(t *testing.T, p *planner, resp *tfprotov6.PlanResourceChangeResponse) tftypes.Value {
	t.Helper()
	for _, d := range errorsOf(resp) {
		t.Fatalf("plan: %s %s", d.Summary, d.Detail)
	}
	planned, err := resp.PlannedState.Unmarshal(p.typ)
	if err != nil {
		t.Fatal(err)
	}
	return valueAt(planned, tftypes.NewAttributePath().WithAttributeName(sourceHashAttr))
}

// A create plans the SHA-256 of the file `source` names, which the plan reads.
func TestUploadPlansTheContentHash(t *testing.T) {
	p := newPlanner(t, uploadDeclaration(), uploadTable())
	source := writeSource(t, "logo.png", "png bytes")
	config := p.value(map[string]tftypes.Value{"purpose": tfString("evidence"), sourceAttr: tfString(source)})
	resp := p.planResponse(tftypes.NewValue(p.typ, nil), config, config)
	if got := plannedHash(t, p, resp); !got.Equal(tfString(sha("png bytes"))) {
		t.Errorf("planned hash %s", got)
	}
}

// New content in the file is a new upload: a replacement, which a resource that refuses destroy
// fails at plan time with how to remove the object from the state. One that deletes replaces.
func TestUploadNewContentIsAReplacement(t *testing.T) {
	source := writeSource(t, "logo.png", "new bytes")
	p := newPlanner(t, uploadDeclaration(), uploadTable())
	resp := p.uploadChange(p.uploadState(tfString(source), tfString(sha("old bytes"))), tfString(source))
	errs := errorsOf(resp)
	if len(errs) != 1 || errs[0].Summary != "Cannot replace stripe_upload" ||
		!strings.Contains(errs[0].Detail, "The content of `source` has changed") || !strings.Contains(errs[0].Detail, "Stripe cannot delete an upload.") ||
		!strings.Contains(errs[0].Detail, "removed block") {
		t.Fatalf("diagnostics: %+v", resp.Diagnostics)
	}

	deleting := uploadDeclaration()
	deleting.Delete, deleting.Paths.Delete, deleting.RefuseReason = Delete, "/v1/uploads/{id}", ""
	p = newPlanner(t, deleting, uploadTable())
	resp = p.uploadChange(p.uploadState(tfString(source), tfString(sha("old bytes"))), tfString(source))
	if got := plannedHash(t, p, resp); !got.Equal(tfString(sha("new bytes"))) {
		t.Errorf("planned hash %s", got)
	}
	if !slices.ContainsFunc(resp.RequiresReplace, func(ap *tftypes.AttributePath) bool {
		return ap.Equal(tftypes.NewAttributePath().WithAttributeName(sourceHashAttr))
	}) {
		t.Errorf("new content must replace: %v", resp.RequiresReplace)
	}
}

// A new path to the same content, and the first plan after an import, which has no hash, plan
// the file's hash with no replacement.
func TestUploadSameContentOrNoHashIsNoReplacement(t *testing.T) {
	p := newPlanner(t, uploadDeclaration(), uploadTable())
	moved := writeSource(t, "moved.png", "png bytes")
	for name, state := range map[string]map[string]tftypes.Value{
		"moved":    p.uploadState(tfString("/old/logo.png"), tfString(sha("png bytes"))),
		"imported": p.uploadState(tftypes.NewValue(tftypes.String, nil), tftypes.NewValue(tftypes.String, nil)),
	} {
		resp := p.uploadChange(state, tfString(moved))
		if got := plannedHash(t, p, resp); !got.Equal(tfString(sha("png bytes"))) {
			t.Errorf("%s: planned hash %s", name, got)
		}
		if len(resp.RequiresReplace) != 0 {
			t.Errorf("%s: replaces %v", name, resp.RequiresReplace)
		}
	}
}

// A source not known at plan time, or a file that does not exist yet because the same apply
// writes it, leaves the hash unknown for the apply to read: on a create, and on an existing object
// whose `source` changes, whose plan stays a change rather than the state. A file that exists but
// cannot be read still fails the plan.
func TestUploadSourceUnknownOrMissing(t *testing.T) {
	p := newPlanner(t, uploadDeclaration(), uploadTable())
	state := p.uploadState(tfString("/old/logo.png"), tfString(sha("png bytes")))
	missing := tfString(filepath.Join(t.TempDir(), "absent.png"))
	for name, source := range map[string]tftypes.Value{"unknown": tfUnknown(), "missing at a new path": missing} {
		resp := p.uploadChange(state, source)
		if got := plannedHash(t, p, resp); got.IsKnown() {
			t.Errorf("%s: an existing object's hash must be left for apply, got %s", name, got)
		}
		if len(resp.RequiresReplace) != 0 {
			t.Errorf("%s: replaces %v", name, resp.RequiresReplace)
		}
	}
	config := p.value(map[string]tftypes.Value{"purpose": tfString("evidence"), sourceAttr: missing})
	if got := plannedHash(t, p, p.planResponse(tftypes.NewValue(p.typ, nil), config, config)); got.IsKnown() {
		t.Errorf("a create's hash must be left for apply, got %s", got)
	}
	resp := p.uploadChange(state, tfString(t.TempDir()))
	if errs := errorsOf(resp); len(errs) != 1 || errs[0].Summary != "Cannot read the file to upload" {
		t.Errorf("a directory must fail the plan: %+v", resp.Diagnostics)
	}
}

// A file gone from the path the object was uploaded from, as on a machine that never had it,
// keeps the state's hash: the plan is the state, with a warning that the content was not compared.
// An update that runs anyway, for another change, keeps the hash without reading the file.
func TestUploadMissingFileAtTheUploadedPathKeepsTheHash(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "absent.png")
	p := newPlanner(t, uploadDeclaration(), uploadTable())
	state := p.uploadState(tfString(missing), tfString(sha("png bytes")))
	resp := p.uploadChange(state, tfString(missing))
	if errs := errorsOf(resp); len(errs) != 0 {
		t.Fatalf("errors: %+v", errs)
	}
	planned, err := resp.PlannedState.Unmarshal(p.typ)
	if err != nil {
		t.Fatal(err)
	}
	if !planned.Equal(p.value(state)) || len(resp.RequiresReplace) != 0 {
		t.Errorf("the plan must be the state: %s, replaces %v", planned, resp.RequiresReplace)
	}
	var warned bool
	for _, d := range resp.Diagnostics {
		warned = warned || d.Severity == tfprotov6.DiagnosticSeverityWarning && d.Summary == "The file to upload was not found" &&
			strings.Contains(d.Detail, "did not compare the content")
	}
	if !warned {
		t.Errorf("no warning: %+v", resp.Diagnostics)
	}

	stub := &uploadStub{}
	r := configured(t, uploadDeclaration(), uploadTable(), stub)
	prior := r.fill(map[string]attr.Value{"id": types.StringValue("upl_1"), "purpose": types.StringValue("evidence"),
		sourceAttr: types.StringValue(missing), sourceHashAttr: types.StringValue(sha("png bytes"))})
	next := r.update(t, r.fill(uploadPlan(r, missing, types.StringValue(sha("png bytes")))), prior)
	if len(stub.log) != 1 || stub.log[0].Method != "GET" {
		t.Errorf("requests: %+v", stub.log)
	}
	if got := str(t, next, sourceHashAttr); got != sha("png bytes") {
		t.Errorf("hash %q", got)
	}
}

func uploadPlan(r *Resource, source string, hash attr.Value) map[string]attr.Value {
	return map[string]attr.Value{"purpose": types.StringValue("evidence"), sourceAttr: types.StringValue(source), sourceHashAttr: hash}
}

// Create uploads the file to the Files API host as the form's file part, named as the file is,
// beside the other create fields, and records its hash.
func TestUploadCreateSendsTheFile(t *testing.T) {
	stub := &uploadStub{}
	r := configured(t, uploadDeclaration(), uploadTable(), stub)
	source := writeSource(t, "logo.png", "png bytes")
	state, _ := r.create(t, r.fill(uploadPlan(r, source, types.StringUnknown())))
	if len(stub.log) != 1 {
		t.Fatalf("requests: %+v", stub.log)
	}
	req := stub.log[0]
	if req.Method != "POST" || req.Path != "/v1/uploads" || req.Host != stripeapi.FilesHost || req.File == nil ||
		req.File.Part != "file" || req.File.Name != "logo.png" || string(req.File.Content) != "png bytes" {
		t.Errorf("request: %+v", req)
	}
	if got, _ := req.Params.GetString("purpose"); got != "evidence" || len(req.Params) != 1 {
		t.Errorf("params: %+v", req.Params)
	}
	if got := str(t, state, sourceHashAttr); got != sha("png bytes") {
		t.Errorf("hash %q", got)
	}
	if got := str(t, state, sourceAttr); got != source {
		t.Errorf("source %q", got)
	}
}

// A file that changed between plan and apply is not uploaded.
func TestUploadCreateRefusesAFileChangedAfterThePlan(t *testing.T) {
	stub := &uploadStub{}
	r := configured(t, uploadDeclaration(), uploadTable(), stub)
	source := writeSource(t, "logo.png", "changed bytes")
	ctx := context.Background()
	resp := resource.CreateResponse{State: tfsdk.State{Schema: r.schema, Raw: tftypes.NewValue(r.schema.Type().TerraformType(ctx), nil)}}
	r.Create(ctx, resource.CreateRequest{Plan: tfsdk.Plan{Schema: r.schema, Raw: raw(t, r.fill(uploadPlan(r, source, types.StringValue(sha("png bytes")))))}}, &resp)
	if errs := resp.Diagnostics.Errors(); len(errs) != 1 || errs[0].Summary() != "The file to upload changed after the plan" {
		t.Errorf("diagnostics: %v", resp.Diagnostics)
	}
	if len(stub.log) != 0 {
		t.Errorf("nothing may be sent: %+v", stub.log)
	}
}

// An update, such as a new path to the same content or the first apply after an import, sends
// nothing but a read and records the source and its hash.
func TestUploadUpdateRecordsTheHashWithoutUploading(t *testing.T) {
	stub := &uploadStub{}
	r := configured(t, uploadDeclaration(), uploadTable(), stub)
	source := writeSource(t, "moved.png", "png bytes")
	state := r.fill(map[string]attr.Value{"id": types.StringValue("upl_1"), "purpose": types.StringValue("evidence")})
	next := r.update(t, r.fill(uploadPlan(r, source, types.StringValue(sha("png bytes")))), state)
	if len(stub.log) != 1 || stub.log[0].Method != "GET" || stub.log[0].File != nil {
		t.Errorf("requests: %+v", stub.log)
	}
	if str(t, next, sourceHashAttr) != sha("png bytes") || str(t, next, sourceAttr) != source {
		t.Errorf("state: %v", next)
	}
}

// New content that the plan could not see, because `source` was not known or the file did not
// exist then, fails the update, which cannot upload it: with the refusal on a resource that
// refuses destroy, and with the advice to plan again, which replaces the object, on one that
// does not. Nothing is sent.
func TestUploadUpdateRefusesNewContentThePlanCouldNotSee(t *testing.T) {
	deleting := uploadDeclaration()
	deleting.Delete, deleting.Paths.Delete, deleting.RefuseReason = Delete, "/v1/uploads/{id}", ""
	for summary, decl := range map[string]Declaration{
		"Cannot replace stripe_upload":       uploadDeclaration(),
		"The file to upload has new content": deleting,
	} {
		stub := &uploadStub{}
		r := configured(t, decl, uploadTable(), stub)
		source := writeSource(t, "logo.png", "new bytes")
		state := r.fill(map[string]attr.Value{"id": types.StringValue("upl_1"), "purpose": types.StringValue("evidence"),
			sourceAttr: types.StringValue(source), sourceHashAttr: types.StringValue(sha("old bytes"))})
		resp := resource.UpdateResponse{State: tfsdk.State{Schema: r.schema, Raw: raw(t, state)}}
		r.Update(context.Background(), resource.UpdateRequest{
			Plan:  tfsdk.Plan{Schema: r.schema, Raw: raw(t, r.fill(uploadPlan(r, source, types.StringUnknown())))},
			State: tfsdk.State{Schema: r.schema, Raw: raw(t, state)},
		}, &resp)
		if errs := resp.Diagnostics.Errors(); len(errs) != 1 || errs[0].Summary() != summary {
			t.Errorf("%s: diagnostics: %v", summary, resp.Diagnostics)
		}
		if len(stub.log) != 0 {
			t.Errorf("%s: nothing may be sent: %+v", summary, stub.log)
		}
	}
}

// An upload needs a part name and a /v1 create path, and its attributes cannot clash with the
// table's.
func TestUploadDeclarationErrors(t *testing.T) {
	noPart := uploadDeclaration()
	noPart.Upload = &Upload{}
	clash := uploadTable()
	clash = append(clash, fields.Field{Name: "source", Shape: fields.String, Response: true})
	for name, c := range map[string]struct {
		decl  Declaration
		table []fields.Field
	}{
		"no part": {noPart, uploadTable()},
		"clash":   {uploadDeclaration(), clash},
	} {
		if _, err := NewResource(c.decl, c.table); err == nil {
			t.Errorf("%s: must fail", name)
		}
	}
}
