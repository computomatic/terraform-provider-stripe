package engine

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"

	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/path"
	rschema "github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-framework/types/basetypes"
	"github.com/hashicorp/terraform-plugin-go/tftypes"

	"github.com/computomatic/terraform-provider-stripe/internal/stripeapi"
	"github.com/computomatic/terraform-provider-stripe/internal/wire"
)

// The attributes an Upload adds: the local file's path and the SHA-256 of its content.
const (
	sourceAttr     = "source"
	sourceHashAttr = "source_sha256"
)

func (m *model) uploadAttributes(attrs map[string]rschema.Attribute) {
	attrs[sourceAttr] = rschema.StringAttribute{
		Required: true,
		MarkdownDescription: "The path of the local file to upload. Terraform cannot send a file's content to Stripe, so the provider reads the file when it plans and when it applies. " +
			"A file that does not exist yet when the plan runs, such as one another resource writes in the same apply, is read at apply time. " +
			"When the file is gone from the path it was uploaded from, as on a machine that never had it, the plan keeps the recorded `" + sourceHashAttr + "` and warns that it did not compare the content, so a changed file at that path goes unnoticed until it is there to read. " +
			"Changing the path alone changes nothing in Stripe; changing the file's content is a change of `" + sourceHashAttr + "`.",
	}
	attrs[sourceHashAttr] = rschema.StringAttribute{
		Computed: true,
		MarkdownDescription: "The SHA-256 of the content of `" + sourceAttr + "`, in hex, which the plan reads from the file, or the apply when the file does not exist yet at plan time. A file gone from an unchanged `" + sourceAttr + "` keeps the recorded value. Stripe re-encodes some files, so its `size` is no measure of the content." +
			m.changingIt(),
		PlanModifiers: []planmodifier.String{hashSource{m}},
	}
}

// hashSource plans `source_sha256` from the file that the configured `source` names. A source
// not known yet, or a file that does not exist yet because the same apply writes it, leaves it
// unknown, for apply to read. A file missing from the `source` the state already holds, with a
// hash, keeps the state's hash with a warning: the file was uploaded, and is gone from this
// machine, so the plan cannot compare the content and is no change for it. A known hash that differs from the state's is a new upload: a
// replacement, which a resource that refuses destroy fails here. A state with no hash, as after an
// import, takes the file's without one.
type hashSource struct{ m *model }

func (h hashSource) Description(context.Context) string {
	return "Plans the SHA-256 of the file to upload."
}

func (h hashSource) MarkdownDescription(ctx context.Context) string { return h.Description(ctx) }

func (h hashSource) PlanModifyString(ctx context.Context, req planmodifier.StringRequest, resp *planmodifier.StringResponse) {
	var source types.String
	resp.Diagnostics.Append(req.Plan.GetAttribute(ctx, path.Root(sourceAttr), &source)...)
	if resp.Diagnostics.HasError() {
		return
	}
	if source.IsUnknown() || source.IsNull() {
		resp.PlanValue = types.StringUnknown()
		return
	}
	_, sum, err := readSource(source.ValueString())
	if errors.Is(err, fs.ErrNotExist) {
		if h.uploadedFrom(ctx, req, source) {
			resp.PlanValue = req.StateValue
			resp.Diagnostics.AddAttributeWarning(path.Root(sourceAttr), "The file to upload was not found",
				fmt.Sprintf("`%s` does not exist, so the plan kept the SHA-256 recorded when the %s was uploaded and did not compare the content. A changed file at that path is not noticed until the file is there to read.",
					source.ValueString(), noun(h.m.decl.TypeName)))
			return
		}
		resp.PlanValue = types.StringUnknown()
		return
	}
	if err != nil {
		resp.Diagnostics.AddAttributeError(path.Root(sourceAttr), "Cannot read the file to upload", err.Error())
		return
	}
	resp.PlanValue = types.StringValue(sum)
	if req.StateValue.IsNull() || req.StateValue.IsUnknown() || req.StateValue.ValueString() == sum {
		return
	}
	if reason := h.m.refusal(req.State.Raw); reason != "" {
		resp.Diagnostics.AddAttributeError(path.Root(sourceAttr), "Cannot replace "+h.m.decl.TypeName, h.m.contentRefusal(reason))
		return
	}
	resp.RequiresReplace = true
}

// uploadedFrom reports whether the state holds a hash and the same `source` as the plan: the
// object was uploaded from that path.
func (h hashSource) uploadedFrom(ctx context.Context, req planmodifier.StringRequest, source types.String) bool {
	if req.State.Raw.IsNull() || req.StateValue.IsNull() || req.StateValue.IsUnknown() {
		return false
	}
	var prior types.String
	if diags := req.State.GetAttribute(ctx, path.Root(sourceAttr), &prior); diags.HasError() {
		return false
	}
	return prior.Equal(source)
}

// contentRefusal says why a resource that refuses destroy cannot upload a file's new content.
func (m *model) contentRefusal(reason string) string {
	thing := noun(m.decl.TypeName)
	return fmt.Sprintf("The content of `%[1]s` has changed since the %[2]s was uploaded. Uploading it needs a new %[2]s, because Stripe cannot change the content of an existing one, and the existing %[2]s cannot be destroyed. %[3]s\n\n"+
		"Restore the file's content, or remove the existing %[2]s from the state with a removed block or `terraform state rm` and apply again to upload a new one.",
		sourceAttr, thing, reason)
}

// readSource reads the file to upload and returns its content and its SHA-256 in hex.
func readSource(name string) (content []byte, sum string, err error) {
	content, err = os.ReadFile(name)
	if err != nil {
		return nil, "", err
	}
	h := sha256.Sum256(content)
	return content, hex.EncodeToString(h[:]), nil
}

// hashUnknown reports whether a plan of an Upload resource leaves `source_sha256` unknown, so the
// apply reads the file. Such a plan is a change even when nothing configured changed.
func (m *model) hashUnknown(plan tftypes.Value) bool {
	return m.decl.Upload != nil && !valueAt(plan, tftypes.NewAttributePath().WithAttributeName(sourceHashAttr)).IsKnown()
}

// uploadSource reads the file the plan's `source` names and checks it against the planned
// hash, which differs when the file changed between plan and apply. A missing file whose hash the
// plan kept from the state, because it is gone from the path it was uploaded from, is not read:
// the update keeps that hash. prior is the hash in state,
// or "" when there is none. A hash that differs from it is new content that the plan could not
// see, because `source` was not known or the file did not exist then: an update cannot upload it,
// so it fails, with refusal, the reason destroy is refused, when there is one, and otherwise with
// the advice to plan again, which then replaces the object.
func uploadSource(m *model, plan basetypes.ObjectValue, prior, refusal string, diags *diag.Diagnostics) (content []byte, sum string) {
	source := stringAttr(plan, sourceAttr)
	content, sum, err := readSource(source)
	if planned := stringAttr(plan, sourceHashAttr); errors.Is(err, fs.ErrNotExist) && prior != "" && planned == prior {
		return nil, prior
	}
	if err != nil {
		diags.AddAttributeError(path.Root(sourceAttr), "Cannot read the file to upload", err.Error())
		return nil, ""
	}
	if planned, ok := plan.Attributes()[sourceHashAttr].(types.String); ok && !planned.IsUnknown() && planned.ValueString() != sum {
		diags.AddAttributeError(path.Root(sourceAttr), "The file to upload changed after the plan",
			fmt.Sprintf("The content of `%s` is not what the plan read. Plan and apply again.", source))
		return nil, ""
	}
	switch {
	case prior == "" || prior == sum:
	case refusal != "":
		diags.AddAttributeError(path.Root(sourceAttr), "Cannot replace "+m.decl.TypeName, m.contentRefusal(refusal))
		return nil, ""
	default:
		thing := noun(m.decl.TypeName)
		diags.AddAttributeError(path.Root(sourceAttr), "The file to upload has new content",
			fmt.Sprintf("The content of `%s` is not what the %s was uploaded with, and the plan could not read the file to tell. An update cannot upload new content. Plan and apply again: the plan then replaces the %s.", source, thing, thing))
		return nil, ""
	}
	return content, sum
}

// upload sends the create of an Upload resource: params as the form's text parts and the
// file's content as its file part, named as the file is, to the Files API host.
func (a *api) upload(ctx context.Context, target string, params wire.Object, source string, content []byte) (wire.Object, error) {
	resp, err := a.client.Do(ctx, stripeapi.Request{
		Method:    "POST",
		Path:      target,
		Params:    params,
		Host:      stripeapi.FilesHost,
		File:      &stripeapi.File{Part: a.m.decl.Upload.Part, Name: filepath.Base(source), Content: content},
		Sensitive: a.m.sensitiveNames,
	})
	if err != nil {
		return nil, err
	}
	obj, ok := resp.Body.(wire.Object)
	if !ok {
		return nil, fmt.Errorf("POST %s: Stripe returned %T, not an object", target, resp.Body)
	}
	return obj, nil
}
