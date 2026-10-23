package engine

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	rschema "github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/types/basetypes"
	"github.com/hashicorp/terraform-plugin-go/tftypes"

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
	_ resource.ResourceWithConfigure      = (*Resource)(nil)
	_ resource.ResourceWithImportState    = (*Resource)(nil)
	_ resource.ResourceWithIdentity       = (*Resource)(nil)
	_ resource.ResourceWithModifyPlan     = (*Resource)(nil)
	_ resource.ResourceWithValidateConfig = (*Resource)(nil)
)

func (r *Resource) Metadata(_ context.Context, _ resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = r.m.decl.TypeName
}

// ImportNote is the declaration's note for the documentation page's Import section.
func (r *Resource) ImportNote() (string, NoteLevel) {
	return r.m.decl.ImportNote, r.m.decl.ImportNoteLevel
}

// DestroyNote is the documentation page's callout for a resource that refuses destroy,
// without its sigil, or "" for one that never does.
func (r *Resource) DestroyNote() string { return r.m.destroyNote() }

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
	r.api = api{m: r.m, client: client, typeAt: func(p path.Path) (attr.Type, bool) {
		a, diags := r.schema.AttributeAtPath(ctx, p)
		if diags.HasError() {
			return nil, false
		}
		return a.GetType(), true
	}}
}

// ModifyPlan keeps a plan that changes nothing empty. Terraform proposes null for a nested
// attribute that is Optional and Computed and left out of the configuration, so the
// framework sees a change and marks every Computed attribute unknown. When all the values
// the plan does know equal the state, and every unknown one is a value the configuration
// leaves unset, no configured value changed, and the plan is the state. An unknown that the
// configuration sets, such as a reference to an object being replaced, is a pending change
// and leaves the plan alone.
//
// A destroy plan of an object whose destroy is refused fails here, so that `terraform destroy`
// and a resource dropped from the configuration stop at plan time.
func (r *Resource) ModifyPlan(ctx context.Context, req resource.ModifyPlanRequest, resp *resource.ModifyPlanResponse) {
	if !req.State.Raw.IsNull() && req.Plan.Raw.IsNull() {
		if reason := r.m.refusal(req.State.Raw); reason != "" {
			resp.Diagnostics.AddError("Cannot destroy "+r.m.decl.TypeName, r.m.destroyRefusal(reason))
		}
		return
	}
	if !req.Plan.Raw.IsNull() {
		r.refuseDuplicateKeys(ctx, req, resp)
	}
	if req.State.Raw.IsNull() || req.Plan.Raw.IsNull() {
		return
	}
	if r.refuseLeavingFinal(req, resp) {
		return
	}
	if r.requiresReplaceForCurrencyOptions(ctx, req, resp) || r.requiresReplaceForFixedKeys(ctx, req, resp) {
		return
	}
	if r.refuseGoingBack(ctx, req, resp) {
		return
	}
	// The refusal comes after every replace check of the engine: a plan any of them has made a
	// replace creates the object anew, with the new value.
	if r.refuseChanges(ctx, req, resp) {
		return
	}
	if r.forgetFilled(req, resp) {
		return
	}
	configured := false
	filled, err := tftypes.Transform(req.Plan.Raw, func(p *tftypes.AttributePath, v tftypes.Value) (tftypes.Value, error) {
		if v.IsKnown() {
			return v, nil
		}
		// A walk error means a parent is null in the configuration, so the value is unset.
		if set, _, err := tftypes.WalkAttributePath(req.Config.Raw, p); err == nil {
			if cv, ok := set.(tftypes.Value); ok && !cv.IsNull() {
				configured = true
				return v, nil
			}
		}
		prior, _, err := tftypes.WalkAttributePath(req.State.Raw, p)
		if err != nil {
			return v, nil
		}
		if pv, ok := prior.(tftypes.Value); ok {
			return pv, nil
		}
		return v, nil
	})
	if err == nil && !configured && filled.Equal(req.State.Raw) {
		resp.Plan.Raw = req.State.Raw
	}
}

// forgetFilled marks unknown in the plan the fields Stripe fills in from a FilledFrom source
// whose planned value differs from the state's, where the configuration leaves them unset, and
// reports whether it did. The state's values belong to the old value of the source.
func (r *Resource) forgetFilled(req resource.ModifyPlanRequest, resp *resource.ModifyPlanResponse) bool {
	var forget []*tftypes.AttributePath
	for source, filled := range r.m.decl.FilledFrom {
		_, p, _ := r.m.at(source)
		if valueAt(resp.Plan.Raw, p).Equal(valueAt(req.State.Raw, p)) {
			continue
		}
		for _, f := range filled {
			_, fp, _ := r.m.at(f)
			if v := valueAt(req.Config.Raw, fp); !v.IsKnown() || v.IsNull() {
				forget = append(forget, fp)
			}
		}
	}
	if len(forget) == 0 {
		return false
	}
	plan, err := tftypes.Transform(resp.Plan.Raw, func(p *tftypes.AttributePath, v tftypes.Value) (tftypes.Value, error) {
		if slices.ContainsFunc(forget, p.Equal) {
			return tftypes.NewValue(v.Type(), tftypes.UnknownValue), nil
		}
		return v, nil
	})
	if err != nil {
		resp.Diagnostics.AddError("Planning the filled-in fields", err.Error())
		return true
	}
	resp.Plan.Raw = plan
	return true
}

// valueAt is the value at p, or a null one when p does not reach a value, such as below a
// null object.
func valueAt(root tftypes.Value, p *tftypes.AttributePath) tftypes.Value {
	if v, _, err := tftypes.WalkAttributePath(root, p); err == nil {
		if tv, ok := v.(tftypes.Value); ok {
			return tv
		}
	}
	return tftypes.NewValue(tftypes.DynamicPseudoType, nil)
}

// refuseLeavingFinal fails the plan that sets a transition field, out of a value the transition
// declares Final, to the transition's value, such as an archived alert's `status` to `active`:
// Stripe would refuse the action at apply.
func (r *Resource) refuseLeavingFinal(req resource.ModifyPlanRequest, resp *resource.ModifyPlanResponse) bool {
	for _, t := range r.m.decl.Transitions {
		name := r.attrName(t.Field)
		p := tftypes.NewAttributePath().WithAttributeName(name)
		planned, okPlan := knownString(valueAt(resp.Plan.Raw, p))
		prior, okState := knownString(valueAt(req.State.Raw, p))
		if !okPlan || !okState || planned != t.Value || !slices.Contains(t.Final, prior) {
			continue
		}
		resp.Diagnostics.AddAttributeError(path.Root(name), "Cannot change `"+name+"` from `"+prior+"`",
			fmt.Sprintf("Stripe cannot move this %s out of `%s`. Remove `%s` from the configuration.", noun(r.m.decl.TypeName), prior, name))
	}
	return resp.Diagnostics.HasError()
}

// knownString is a known, non-null string value.
func knownString(v tftypes.Value) (string, bool) {
	var s string
	if !v.IsKnown() || v.IsNull() || !v.Type().Is(tftypes.String) || v.As(&s) != nil {
		return "", false
	}
	return s, true
}

// transitionValue is a known, non-null value of a transition field: a string, or a bool as
// `true` or `false`.
func transitionValue(v attr.Value) (string, bool) {
	switch t := v.(type) {
	case basetypes.StringValue:
		return t.ValueString(), known(t)
	case basetypes.BoolValue:
		return strconv.FormatBool(t.ValueBool()), known(t)
	}
	return "", false
}

// wireScalar is a response's string field, or its bool field as `true` or `false`.
func wireScalar(body wire.Object, name string) string {
	if b, ok := body.GetBool(name); ok {
		return strconv.FormatBool(b)
	}
	s, _ := body.GetString(name)
	return s
}

// refuseGoingBack fails the plan that moves the declared advance field to an earlier value,
// which Stripe refuses. A plan that also replaces the object is not refused: its create
// starts at the new value. A value not known at plan time is left for apply.
func (r *Resource) refuseGoingBack(ctx context.Context, req resource.ModifyPlanRequest, resp *resource.ModifyPlanResponse) bool {
	a := r.m.decl.Advance
	if a == nil {
		return false
	}
	var plan, state, config basetypes.ObjectValue
	diags := req.Plan.Get(ctx, &plan)
	diags.Append(req.State.Get(ctx, &state)...)
	diags.Append(req.Config.Get(ctx, &config)...)
	if resp.Diagnostics.Append(diags...); diags.HasError() {
		return true
	}
	if r.replaces(plan, state, config) {
		return false
	}
	name := r.attrName(a.Field)
	to, okTo := plan.Attributes()[name].(basetypes.Int64Value)
	from, okFrom := state.Attributes()[name].(basetypes.Int64Value)
	if !okTo || !okFrom || to.IsUnknown() || to.IsNull() || from.IsNull() || to.ValueInt64() >= from.ValueInt64() {
		return false
	}
	resp.Diagnostics.AddAttributeError(path.Root(name), "Cannot move `"+name+"` backwards",
		fmt.Sprintf("Stripe only advances `%s`, and this %s is at %d. Set a value of at least that, or replace the object to start again at an earlier time.", name, r.m.decl.TypeName, from.ValueInt64()))
	return true
}

// replaces reports whether the plan changes a field that forces replacement, the parent or
// the attached object.
func (r *Resource) replaces(plan, state, config basetypes.ObjectValue) bool {
	for _, n := range r.m.nodes {
		if n.Replace && differs(plan.Attributes()[n.AttrName()], state.Attributes()[n.AttrName()], config.Attributes()[n.AttrName()]) {
			return true
		}
	}
	for _, name := range []string{r.m.decl.Parent, r.m.decl.Attach} {
		if name != "" && differs(plan.Attributes()[name], state.Attributes()[name], config.Attributes()[name]) {
			return true
		}
	}
	return false
}

func (r *Resource) attrName(field string) string {
	n, _ := r.m.top(field)
	return n.AttrName()
}

// refuseChanges fails the plan when it changes a field the declaration lists in RefuseChange.
// A configured value not fully known at plan time is left for apply. Computed members the
// configuration leaves unset are unknown on every change of the field, so a planned value
// that is unknown only there is compared by what the configuration sets. A plan that also
// changes a field that forces replacement is a replace, whose create sends the new value, so
// it is not refused. The framework gives ModifyPlan no RequiresReplace for the attributes' own plan
// modifiers, so the declaration decides it: the top-level create-only and replace-on fields,
// the parent and the attached object. The engine's own checks (currency options, fixed keys)
// have run before, and a plan they replace never gets here. compile refuses a declaration
// with a replace source below the top level, which is not checked. (`-replace` is not seen: its first plan still carries the prior state.)
func (r *Resource) refuseChanges(ctx context.Context, req resource.ModifyPlanRequest, resp *resource.ModifyPlanResponse) bool {
	if len(r.m.decl.RefuseChange) == 0 {
		return false
	}
	var plan, state, config basetypes.ObjectValue
	diags := req.Plan.Get(ctx, &plan)
	diags.Append(req.State.Get(ctx, &state)...)
	diags.Append(req.Config.Get(ctx, &config)...)
	if resp.Diagnostics.Append(diags...); diags.HasError() {
		return true
	}
	if r.replaces(plan, state, config) {
		return false
	}
	for _, name := range r.m.decl.RefuseChange {
		n, _ := r.m.top(name)
		pv, sv := plan.Attributes()[n.AttrName()], state.Attributes()[n.AttrName()]
		if pv == nil || pv.Equal(sv) {
			continue
		}
		if v, err := pv.ToTerraformValue(ctx); err != nil || !v.IsFullyKnown() {
			cv, err := config.Attributes()[n.AttrName()].ToTerraformValue(ctx)
			if err != nil || !cv.IsFullyKnown() || unchangedBesidesComputed(n, pv, sv, req.Config.Raw) {
				continue
			}
		}
		recreate := "recreate the object deliberately with `terraform taint` (`tofu taint` in OpenTofu)."
		if r.m.refusal(req.State.Raw) != "" {
			recreate = "remove the object from the state with a removed block or `terraform state rm` and apply again to create a new one."
		}
		resp.Diagnostics.AddAttributeError(path.Root(n.AttrName()), "Changing `"+n.AttrName()+"` is not supported yet",
			"Changing the "+n.AttrName()+" of an existing "+r.m.decl.TypeName+" is not supported yet. Revert the configuration, add `lifecycle { ignore_changes = ["+n.AttrName()+"] }` to leave it as it is, or "+recreate)
	}
	return resp.Diagnostics.HasError()
}

// differs reports whether a planned value is a change from the state's. An unknown planned
// value counts only when the configuration sets it, such as a reference to an object being
// replaced: an unset computed value that is unknown carries no change.
func differs(plan, state, config attr.Value) bool {
	switch {
	case plan == nil:
		return false
	case plan.IsUnknown():
		return config != nil && !config.IsNull()
	}
	return !plan.Equal(state)
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
	createPath := r.m.expand(r.m.decl.Paths.Create, "", parent)
	if attach := r.m.decl.Attach; attach != "" {
		createPath = strings.ReplaceAll(createPath, "{"+attach+"}", url.PathEscape(stringAttr(plan, attach)))
		params = append(params, wire.Field{Name: r.m.decl.Parent, Value: wire.String(parent)})
	}
	body, err := r.api.call(ctx, "POST", createPath, r.api.expanded(params, ""))
	if err != nil {
		r.api.fail(&resp.Diagnostics, err)
		return
	}
	id, _ := body.GetString("id")

	// Whatever happens next, the object exists in Stripe. Record its ID first so a failure
	// below leaves a tainted resource that destroy can remove instead of an orphan.
	// Destroy and refresh address the object through its parent, so the parent and the
	// attached object's attribute go into the early state too.
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("id"), id)...)
	if r.m.decl.Parent != "" {
		resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root(r.m.decl.Parent), parent)...)
	}
	if r.m.decl.Attach != "" {
		resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root(r.m.decl.Attach), stringAttr(plan, r.m.decl.Attach))...)
	}

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
	body, err = r.applyTransitions(ctx, body, plan, id, parent)
	if err != nil {
		r.api.fail(&resp.Diagnostics, err)
		return
	}

	state, diags := r.m.decodeState(body, plan, r.objType, false, nil)
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
	// A create that failed before Stripe answered leaves a tainted resource with no ID. A
	// request for it would address the collection, not an object.
	if id == "" {
		resp.State.RemoveResource(ctx)
		return
	}
	body, err := r.api.call(ctx, "GET", r.m.expand(r.m.decl.Paths.Retrieve, id, parent), r.api.expanded(nil, ""))
	if r.m.gone(err, &resp.Diagnostics) || err == nil && isDeletedStub(body) {
		resp.State.RemoveResource(ctx)
		return
	}
	if err != nil {
		r.api.fail(&resp.Diagnostics, err)
		return
	}
	next, diags := r.m.decodeState(body, state, r.objType, false, r.m.managedKeys(state))
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

	params, err := r.m.encodeUpdate(plan, state, req.Config.Raw)
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
	keep := r.m.managedKeys(state, plan)
	hints, addsKeyless := r.m.keyedHints(plan, state, req.Config.Raw)
	for list, ids := range hints {
		if keep[list] != nil {
			keep[list].hint = ids
		}
	}
	// An item with no key of its own has no key to recognise it by in the response, so the
	// items that exist before the request tell the new ones from the old.
	var before map[string]map[string]bool
	if addsKeyless && len(params) > 0 {
		pre, err := r.api.call(ctx, "GET", r.m.expand(r.m.decl.Paths.Retrieve, id, parent), r.api.expanded(nil, ""))
		if err != nil {
			r.api.fail(&resp.Diagnostics, err)
			return
		}
		before = itemIDs(pre, r.m.decl.MatchBy)
	}
	if err := r.advance(ctx, plan, state, id, parent); err != nil {
		r.api.fail(&resp.Diagnostics, err)
		return
	}
	body, err := r.api.call(ctx, method, target, sent)
	if err != nil {
		r.api.fail(&resp.Diagnostics, err)
		return
	}
	body, err = r.applyTransitions(ctx, body, plan, id, parent)
	if err != nil {
		r.api.fail(&resp.Diagnostics, err)
		return
	}
	for list, ids := range itemIDs(body, r.m.decl.MatchBy) {
		for id := range ids {
			if before != nil && !before[list][id] && keep[list] != nil {
				keep[list].ids[id] = true
			}
		}
	}
	next, diags := r.m.decodeState(body, plan, r.objType, false, keep)
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
	// Nothing was created, so there is nothing to remove. See Read.
	if id == "" {
		return
	}
	// The backstop of the plan-time refusal: `-replace`, taint and `replace_triggered_by`
	// plan a replacement the provider cannot see, and a plan without refresh can hold a
	// stale state.
	if reason := r.m.refusal(req.State.Raw); reason != "" {
		resp.Diagnostics.AddError("Cannot destroy "+r.m.decl.TypeName, r.m.destroyRefusal(reason))
		return
	}
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
	}
	if err != nil && r.refusedByStripe(ctx, id, parent, err) {
		r.api.fail(&resp.Diagnostics, err)
		resp.Diagnostics.AddError("Cannot destroy "+r.m.decl.TypeName, r.m.destroyRefusal(r.m.decl.RefuseWhen.Reason))
		return
	}
	if err != nil && !r.m.gone(err, &resp.Diagnostics) {
		r.api.fail(&resp.Diagnostics, err)
	}
}

// applyTransitions calls the action endpoint of each transition whose planned value is known
// and differs from the object's, and returns the body with each action's response laid over it. A value the object already
// holds sends nothing.
func (r *Resource) applyTransitions(ctx context.Context, body wire.Object, plan basetypes.ObjectValue, id, parent string) (wire.Object, error) {
	for _, t := range r.m.decl.Transitions {
		n, _ := r.m.top(t.Field)
		if want, ok := transitionValue(plan.Attributes()[n.AttrName()]); !ok || want != t.Value || wireScalar(body, t.Field) == t.Value {
			continue
		}
		var params wire.Object
		for _, name := range t.Send {
			if v, ok := body.Get(name); ok {
				params = append(params, wire.Field{Name: name, Value: v})
			}
		}
		if !r.m.v2 {
			params = r.api.expanded(params, "")
		}
		next, err := r.api.call(ctx, "POST", r.m.expand(t.Path, id, parent), params)
		if err != nil {
			return nil, err
		}
		next = withoutIncluded(next, r.m.decl.Include)
		// A once-returned field is in the create response only, so the action's does not replace it.
		body = mergeBodies(body, next)
	}
	return body, nil
}

// withoutIncluded drops from a `/v2` action's response the top-level fields the declaration
// includes, which the action, sent no `include`, returns as null.
func withoutIncluded(body wire.Object, include []string) wire.Object {
	if len(include) == 0 {
		return body
	}
	tops := map[string]bool{}
	for _, v := range include {
		top, _, _ := strings.Cut(v, ".")
		tops[top] = true
	}
	var out wire.Object
	for _, f := range body {
		if !tops[f.Name] {
			out = append(out, f)
		}
	}
	return out
}

// isDeletedStub reports the `{id, object, deleted: true}` Stripe returns for an object it
// has deleted but still answers for, such as a detached product feature.
func isDeletedStub(body wire.Object) bool {
	deleted, _ := body.GetBool("deleted")
	return deleted
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
	if v, ok := body.Get(a.Field); ok && sameWire(v, a.Value) {
		return nil
	}
	if a.Path != "" {
		_, err = r.api.call(ctx, "POST", r.m.expand(a.Path, id, parent), nil)
		return err
	}
	_, err = r.api.call(ctx, "POST", r.m.expand(r.m.decl.Paths.Update, id, parent), wire.Object{{Name: a.Field, Value: a.Value}})
	return err
}

func sameWire(a, b wire.Value) bool {
	have, errHave := wire.EncodeJSON(a)
	want, errWant := wire.EncodeJSON(b)
	return errHave == nil && errWant == nil && string(have) == string(want)
}

// advance moves the declared time field to the planned value when it differs from the
// state's. It is safe to repeat after a failed or timed-out update: it first waits for any
// advance still running, skips the request when the object is already at or past the target,
// and counts the object as done only when it is ready and has reached the target.
func (r *Resource) advance(ctx context.Context, plan, state basetypes.ObjectValue, id, parent string) error {
	a := r.m.decl.Advance
	if a == nil {
		return nil
	}
	name := r.attrName(a.Field)
	to, ok := plan.Attributes()[name].(basetypes.Int64Value)
	if !ok || to.IsNull() || to.IsUnknown() || to.Equal(state.Attributes()[name]) {
		return nil
	}
	target := to.ValueInt64()
	retrieve := r.m.expand(r.m.decl.Paths.Retrieve, id, parent)
	body, err := r.awaitReady(ctx, id, retrieve, func(wire.Object) bool { return true })
	if err != nil {
		return err
	}
	if intField(body, a.Field) >= target {
		return nil
	}
	if _, err := r.api.call(ctx, "POST", r.m.expand(a.Path, id, parent), wire.Object{{Name: a.Field, Value: wire.Int(target)}}); err != nil {
		return err
	}
	_, err = r.awaitReady(ctx, id, retrieve, func(b wire.Object) bool {
		return intField(b, a.Field) >= target
	})
	return err
}

// intField is the integer a response holds under name, or 0.
func intField(o wire.Object, name string) int64 {
	v, _ := o.Get(name)
	n, _ := v.(wire.Int)
	return int64(n)
}

// awaitReady reads the object until its status is the declared ready value and done accepts
// it, and returns that body.
func (r *Resource) awaitReady(ctx context.Context, id, retrieve string, done func(wire.Object) bool) (wire.Object, error) {
	a := r.m.decl.Advance
	for {
		body, err := r.api.call(ctx, "GET", retrieve, nil)
		if err != nil {
			return nil, err
		}
		if status, _ := body.GetString(a.StatusField); status == a.ReadyValue && done(body) {
			return body, nil
		}
		select {
		case <-ctx.Done():
			return nil, fmt.Errorf("waiting for %s to reach %s=%s: %w", id, a.StatusField, a.ReadyValue, ctx.Err())
		case <-time.After(r.pollInterval):
		}
	}
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
	if id == "" {
		resp.Diagnostics.AddError("Invalid import ID", "the ID is empty")
		return
	}
	if p := r.m.decl.IDPrefix; p != "" && !strings.HasPrefix(id, p) {
		resp.Diagnostics.AddWarning("Unexpected ID", fmt.Sprintf("Stripe IDs for %s start with `%s`; %q does not", r.m.decl.TypeName, p, id))
	}
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("id"), id)...)
	if r.m.decl.Parent != "" {
		resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root(r.m.decl.Parent), parent)...)
	}
	if r.m.decl.Attach != "" {
		resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root(r.m.decl.Attach), id)...)
	}
	resp.Diagnostics.Append(r.m.setIdentity(ctx, resp.Identity, id, parent)...)
}

// refuseDuplicateKeys fails the plan of a keyed list that names one key twice: Stripe does
// not allow the same price twice on a subscription, and the elements could not be told
// apart.
func (r *Resource) refuseDuplicateKeys(ctx context.Context, req resource.ModifyPlanRequest, resp *resource.ModifyPlanResponse) {
	var plan basetypes.ObjectValue
	if diags := req.Plan.Get(ctx, &plan); diags.HasError() {
		return
	}
	for list, key := range r.m.decl.MatchBy {
		n, ok := r.m.top(list)
		if !ok {
			continue
		}
		seen := map[string]bool{}
		for i, e := range listElements(plan.Attributes()[n.AttrName()]) {
			k, has := attrKey(e, key)
			if !has {
				continue
			}
			if seen[k] {
				resp.Diagnostics.AddAttributeError(path.Root(n.AttrName()).AtListIndex(i),
					"Duplicate "+key+" in "+n.AttrName(),
					fmt.Sprintf("`%s` lists %s `%s` more than once. Stripe does not allow the same %s twice; give one element the combined quantity.", n.AttrName(), key, k, key))
			}
			seen[k] = true
		}
		data := r.m.decl.MatchData[list]
		if data == "" {
			continue
		}
		var built []attr.Value
		for i, e := range listElements(plan.Attributes()[n.AttrName()]) {
			if _, has := attrKey(e, key); has {
				continue
			}
			o, isObj := e.(basetypes.ObjectValue)
			if !isObj || o.IsNull() || o.IsUnknown() {
				continue
			}
			d := o.Attributes()[data]
			if d == nil || d.IsNull() || d.IsUnknown() || !fullyKnown(d) {
				continue
			}
			for _, b := range built {
				if sameData(b, d) {
					resp.Diagnostics.AddAttributeError(path.Root(n.AttrName()).AtListIndex(i).AtName(data),
						"Duplicate "+data+" in "+n.AttrName(),
						fmt.Sprintf("`%s` lists the same `%s` twice. The two items could not be told apart when Stripe returns them; give them different data or one the combined quantity.", n.AttrName(), data))
				}
			}
			built = append(built, d)
		}
	}
}

// fullyKnown reports whether a value holds no unknown part.
func fullyKnown(v attr.Value) bool {
	raw, err := v.ToTerraformValue(context.Background())
	return err == nil && raw.IsFullyKnown()
}

// itemIDs returns the IDs of the elements of each keyed list in a response.
func itemIDs(body wire.Object, lists map[string]string) map[string]map[string]bool {
	out := map[string]map[string]bool{}
	for list := range lists {
		w, _ := body.Get(list)
		arr, problem := listItems(w)
		if problem != "" {
			continue
		}
		out[list] = map[string]bool{}
		for _, item := range arr {
			if o, ok := item.(wire.Object); ok {
				if id, ok := o.GetString("id"); ok {
					out[list][id] = true
				}
			}
		}
	}
	return out
}

// sameData reports whether two `price_data` values describe one price. A fixed amount can be
// given as `unit_amount` or as `unit_amount_decimal`; the two name the same price when they
// are the same number.
func sameData(a, b attr.Value) bool {
	x, okX := a.(basetypes.ObjectValue)
	y, okY := b.(basetypes.ObjectValue)
	if !okX || !okY {
		return a.Equal(b)
	}
	amount := func(o basetypes.ObjectValue) (float64, bool) {
		attrs := o.Attributes()
		if i, ok := attrs["unit_amount"].(basetypes.Int64Value); ok && known(i) {
			return float64(i.ValueInt64()), true
		}
		if s, ok := attrs["unit_amount_decimal"].(basetypes.StringValue); ok && known(s) {
			f, err := strconv.ParseFloat(s.ValueString(), 64)
			return f, err == nil
		}
		return 0, false
	}
	fx, hasX := amount(x)
	fy, hasY := amount(y)
	if hasX != hasY || fx != fy {
		return false
	}
	names := map[string]bool{}
	for name := range x.Attributes() {
		names[name] = true
	}
	for name := range y.Attributes() {
		names[name] = true
	}
	for name := range names {
		if name == "unit_amount" || name == "unit_amount_decimal" {
			continue
		}
		av, bv := x.Attributes()[name], y.Attributes()[name]
		if av == nil || bv == nil {
			if av != nil && !av.IsNull() || bv != nil && !bv.IsNull() {
				return false
			}
			continue
		}
		if !sameData(av, bv) {
			return false
		}
	}
	return true
}
