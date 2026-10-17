package engine

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	rschema "github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/types/basetypes"

	"github.com/computomatic/terraform-provider-stripe/internal/fields"
	"github.com/computomatic/terraform-provider-stripe/internal/stripeapi"
	"github.com/computomatic/terraform-provider-stripe/internal/wire"
)

// NewResource compiles the declaration against its table and returns the resource
// constructor the provider registers. It fails when the declaration does not fit the table.
func NewResource(decl Declaration, table []fields.Field) (func() resource.Resource, error) {
	if decl.Delete == NoDelete {
		return nil, fmt.Errorf("%s: the declaration has no delete strategy, so there is no resource; register a data source only", decl.TypeName)
	}
	m, err := compile(decl, table)
	if err != nil {
		return nil, err
	}
	s := m.resourceSchema(context.Background())
	objType, ok := s.Type().(basetypes.ObjectType)
	if !ok {
		return nil, fmt.Errorf("%s: schema type is %T, not an object", decl.TypeName, s.Type())
	}
	return func() resource.Resource {
		return &Resource{m: m, schema: s, objType: objType, pollInterval: expirePollInterval}
	}, nil
}

// Resource is the generic resource implementation. Every Stripe resource is one of these
// with a different model.
type Resource struct {
	m       *model
	schema  rschema.Schema
	objType basetypes.ObjectType
	api     api
	// pollInterval is how often expire checks the status.
	pollInterval time.Duration
}

var (
	_ resource.ResourceWithConfigure   = (*Resource)(nil)
	_ resource.ResourceWithImportState = (*Resource)(nil)
	_ resource.ResourceWithIdentity    = (*Resource)(nil)
)

func (r *Resource) Metadata(_ context.Context, _ resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = r.m.decl.TypeName
}

func (r *Resource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = r.schema
}

func (r *Resource) IdentitySchema(_ context.Context, _ resource.IdentitySchemaRequest, resp *resource.IdentitySchemaResponse) {
	resp.IdentitySchema = r.m.identitySchema()
}

func (r *Resource) Configure(ctx context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	if req.ProviderData == nil {
		return
	}
	client, ok := req.ProviderData.(Client)
	if !ok {
		resp.Diagnostics.AddError("Unexpected provider data", fmt.Sprintf("expected a Stripe client, got %T", req.ProviderData))
		return
	}
	r.api = api{m: r.m, client: client, hasAttribute: func(p path.Path) bool {
		_, diags := r.schema.AttributeAtPath(ctx, p)
		return !diags.HasError()
	}}
}

func (r *Resource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan basetypes.ObjectValue
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}
	ctx, cancel, diags := operationContext(ctx, plan, "create")
	defer cancel()
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	params, err := r.m.encodeCreate(plan)
	if err != nil {
		resp.Diagnostics.AddError("Cannot build the create request", err.Error())
		return
	}
	_, parent := r.m.ids(plan)
	body, err := r.api.call(ctx, "POST", r.m.expand(r.m.decl.Paths.Create, "", parent), r.api.expanded(params, ""))
	if err != nil {
		r.api.fail(&resp.Diagnostics, err)
		return
	}
	id, _ := body.GetString("id")

	// Whatever happens next, the object exists in Stripe. Record its ID first so a failure
	// below leaves a tainted resource that destroy can remove instead of an orphan.
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("id"), id)...)

	if r.m.decl.Paths.Update != "" {
		followUp, err := r.m.encodeFollowUp(plan)
		if err != nil {
			resp.Diagnostics.AddError("Cannot build the follow-up update", err.Error())
			return
		}
		if len(followUp) > 0 {
			updated, err := r.api.call(ctx, "POST", r.m.expand(r.m.decl.Paths.Update, id, parent), r.api.expanded(followUp, ""))
			if err != nil {
				r.api.fail(&resp.Diagnostics, err)
				return
			}
			// Stripe sends once-returned fields, such as a webhook secret, on the create
			// response only.
			body = mergeBodies(body, updated)
		}
	}

	state, diags := r.m.decodeState(body, plan, r.objType, false)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, state)...)
	resp.Diagnostics.Append(r.m.setIdentity(ctx, resp.Identity, id, parent)...)
}

func (r *Resource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state basetypes.ObjectValue
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	ctx, cancel := context.WithTimeout(ctx, ReadTimeout)
	defer cancel()

	id, parent := r.m.ids(state)
	body, err := r.api.call(ctx, "GET", r.m.expand(r.m.decl.Paths.Retrieve, id, parent), r.api.expanded(nil, ""))
	if stripeapi.IsNotFound(err) {
		resp.State.RemoveResource(ctx)
		return
	}
	if err != nil {
		r.api.fail(&resp.Diagnostics, err)
		return
	}
	next, diags := r.m.decodeState(body, state, r.objType, false)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, next)...)
	resp.Diagnostics.Append(r.m.setIdentity(ctx, resp.Identity, id, parent)...)
}

func (r *Resource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan, state basetypes.ObjectValue
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	ctx, cancel, diags := operationContext(ctx, plan, "update")
	defer cancel()
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	params, err := r.m.encodeUpdate(plan, state)
	if err != nil {
		resp.Diagnostics.AddError("Cannot build the update request", err.Error())
		return
	}
	id, parent := r.m.ids(state)
	// With nothing to send (only `timeouts` changed) a read refreshes the computed values.
	method, target, sent := "GET", r.m.expand(r.m.decl.Paths.Retrieve, id, parent), wire.Object(nil)
	if len(params) > 0 {
		method, target, sent = "POST", r.m.expand(r.m.decl.Paths.Update, id, parent), params
	}
	sent = r.api.expanded(sent, "")
	body, err := r.api.call(ctx, method, target, sent)
	if err != nil {
		r.api.fail(&resp.Diagnostics, err)
		return
	}
	next, diags := r.m.decodeState(body, plan, r.objType, false)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, next)...)
	resp.Diagnostics.Append(r.m.setIdentity(ctx, resp.Identity, id, parent)...)
}

func (r *Resource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state basetypes.ObjectValue
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	ctx, cancel, diags := operationContext(ctx, state, "delete")
	defer cancel()
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	id, parent := r.m.ids(state)
	var err error
	switch r.m.decl.Delete {
	case Delete:
		_, err = r.api.call(ctx, "DELETE", r.m.expand(r.m.decl.Paths.Delete, id, parent), nil)
	case DeleteOrArchive:
		_, err = r.api.call(ctx, "DELETE", r.m.expand(r.m.decl.Paths.Delete, id, parent), nil)
		if refused(err, r.m.decl.ArchiveOn) {
			refusal := err
			if err = r.archive(ctx, id, parent); err == nil {
				resp.Diagnostics.AddWarning(
					"Archived instead of deleted",
					fmt.Sprintf("Stripe refused to delete %s %s: %v\n\nThe object was archived instead. It is out of Terraform's state and still exists in the Stripe account.",
						r.m.decl.TypeName, id, refusal),
				)
			}
		}
	case Archive:
		err = r.archive(ctx, id, parent)
	case Expire:
		err = r.expire(ctx, id, parent)
	}
	if err != nil && !stripeapi.IsNotFound(err) {
		r.api.fail(&resp.Diagnostics, err)
	}
}

// mergeBodies overlays the fields of later onto earlier.
func mergeBodies(earlier, later wire.Object) wire.Object {
	out := append(wire.Object(nil), earlier...)
	for _, f := range later {
		replaced := false
		for i := range out {
			if out[i].Name == f.Name {
				out[i].Value, replaced = f.Value, true
				break
			}
		}
		if !replaced {
			out = append(out, f)
		}
	}
	return out
}

// refused reports whether err is the Stripe refusal the declaration names.
func refused(err error, want *Refusal) bool {
	var apiErr *stripeapi.APIError
	return errors.As(err, &apiErr) && apiErr.Status == want.Status && apiErr.Type == want.Type
}

// archive retrieves the object first, because archiving twice can be an error, and sends
// the declared update only when the object is still live.
func (r *Resource) archive(ctx context.Context, id, parent string) error {
	body, err := r.api.call(ctx, "GET", r.m.expand(r.m.decl.Paths.Retrieve, id, parent), nil)
	if err != nil {
		return err
	}
	a := r.m.decl.Archive
	if archived(body, a, time.Now()) {
		return nil
	}
	if a.Path != "" {
		_, err = r.api.call(ctx, "POST", r.m.expand(a.Path, id, parent), nil)
		return err
	}
	value := a.Value
	if a.Now {
		value = wire.Int(time.Now().Unix())
	}
	_, err = r.api.call(ctx, "POST", r.m.expand(r.m.decl.Paths.Update, id, parent), wire.Object{{Name: a.Field, Value: value}})
	return err
}

func archived(body wire.Object, a *ArchiveAction, now time.Time) bool {
	v, ok := body.Get(a.Field)
	if !ok {
		return false
	}
	if a.Now {
		at, isInt := v.(wire.Int)
		return isInt && int64(at) <= now.Unix()
	}
	have, errHave := wire.EncodeJSON(v)
	want, errWant := wire.EncodeJSON(a.Value)
	return errHave == nil && errWant == nil && string(have) == string(want)
}

// expire ends the object a few seconds ahead and waits for its status to say so.
func (r *Resource) expire(ctx context.Context, id, parent string) error {
	e := r.m.decl.Expire
	retrieve := r.m.expand(r.m.decl.Paths.Retrieve, id, parent)
	body, err := r.api.call(ctx, "GET", retrieve, nil)
	if err != nil {
		return err
	}
	if !expired(body, e) {
		at := wire.Int(time.Now().Unix() + e.Seconds)
		if _, err := r.api.call(ctx, "POST", r.m.expand(r.m.decl.Paths.Update, id, parent), wire.Object{{Name: e.Field, Value: at}}); err != nil {
			return err
		}
	}
	for {
		if body, err = r.api.call(ctx, "GET", retrieve, nil); err != nil {
			return err
		}
		if expired(body, e) {
			return nil
		}
		select {
		case <-ctx.Done():
			return fmt.Errorf("waiting for %s to reach %s=%s: %w", id, e.StatusField, e.StatusValue, ctx.Err())
		case <-time.After(r.pollInterval):
		}
	}
}

func expired(body wire.Object, e *ExpireAction) bool {
	status, _ := body.GetString(e.StatusField)
	return status == e.StatusValue
}

func (r *Resource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	id, parent := req.ID, ""
	if req.ID == "" && req.Identity != nil {
		resp.Diagnostics.Append(req.Identity.GetAttribute(ctx, path.Root("id"), &id)...)
		if r.m.decl.Parent != "" {
			resp.Diagnostics.Append(req.Identity.GetAttribute(ctx, path.Root(r.m.decl.Parent), &parent)...)
		}
	} else if r.m.decl.Parent != "" {
		var found bool
		parent, id, found = strings.Cut(req.ID, "/")
		if !found || parent == "" || id == "" {
			resp.Diagnostics.AddError("Invalid import ID", fmt.Sprintf("expected `<%s id>/<id>`, got %q", r.m.decl.Parent, req.ID))
			return
		}
	}
	if resp.Diagnostics.HasError() {
		return
	}
	if p := r.m.decl.IDPrefix; p != "" && !strings.HasPrefix(id, p) {
		resp.Diagnostics.AddWarning("Unexpected ID", fmt.Sprintf("Stripe IDs for %s start with `%s`; %q does not", r.m.decl.TypeName, p, id))
	}
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("id"), id)...)
	if r.m.decl.Parent != "" {
		resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root(r.m.decl.Parent), parent)...)
	}
	resp.Diagnostics.Append(r.m.setIdentity(ctx, resp.Identity, id, parent)...)
}
