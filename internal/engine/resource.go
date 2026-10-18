package engine

import (
	"context"
	"errors"
	"fmt"
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
func (r *Resource) ModifyPlan(ctx context.Context, req resource.ModifyPlanRequest, resp *resource.ModifyPlanResponse) {
	if !req.Plan.Raw.IsNull() {
		r.refuseDuplicateKeys(ctx, req, resp)
	}
	if req.State.Raw.IsNull() || req.Plan.Raw.IsNull() {
		return
	}
	if r.requiresReplaceForCurrencyOptions(ctx, req, resp) {
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
	if stripeapi.IsNotFound(err) || err == nil && isDeletedStub(body) {
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
	body, err := r.api.call(ctx, method, target, sent)
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
		var kept *keptError
		if errors.As(err, &kept) {
			resp.Diagnostics.AddWarning(
				"Left active instead of archived",
				fmt.Sprintf("Stripe refused to archive %s %s: %v\n\nThe object is protected (%s). It is out of Terraform's state and still active in the Stripe account.",
					r.m.decl.TypeName, id, kept.refusal, r.m.decl.KeepWhen.Field),
			)
			err = nil
		}
	case Expire:
		err = r.expire(ctx, id, parent)
	}
	if err != nil && !stripeapi.IsNotFound(err) {
		r.api.fail(&resp.Diagnostics, err)
	}
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
	if archived(body, a, time.Now()) {
		return nil
	}
	if a.Path != "" {
		_, err = r.api.call(ctx, "POST", r.m.expand(a.Path, id, parent), nil)
		return err
	}
	value := a.Value
	if a.Now {
		value = wire.String("now")
	}
	_, err = r.api.call(ctx, "POST", r.m.expand(r.m.decl.Paths.Update, id, parent), wire.Object{{Name: a.Field, Value: value}})
	return r.keepIfProtected(ctx, id, parent, err)
}

// keptError is an archive that Stripe refused for an object the declaration protects.
type keptError struct{ refusal error }

func (e *keptError) Error() string { return e.refusal.Error() }

// keepIfProtected turns a failed archive into a keptError when the object is the one the
// declaration says Stripe protects. It reads the object after the failure; a failed read
// leaves the archive error as it is.
func (r *Resource) keepIfProtected(ctx context.Context, id, parent string, archiveErr error) error {
	keep := r.m.decl.KeepWhen
	if archiveErr == nil || keep == nil {
		return archiveErr
	}
	body, err := r.api.call(ctx, "GET", r.m.expand(r.m.decl.Paths.Retrieve, id, parent), nil)
	if err != nil {
		return archiveErr
	}
	if v, ok := body.Get(keep.Field); ok && sameWire(v, keep.Value) {
		return &keptError{refusal: archiveErr}
	}
	return archiveErr
}

func sameWire(a, b wire.Value) bool {
	have, errHave := wire.EncodeJSON(a)
	want, errWant := wire.EncodeJSON(b)
	return errHave == nil && errWant == nil && string(have) == string(want)
}

func archived(body wire.Object, a *ArchiveAction, now time.Time) bool {
	if holds(body, a.Field, a.Value, a.Now, now) {
		return true
	}
	for _, e := range a.Ended {
		if holds(body, e.Field, e.Value, e.Past, now) {
			return true
		}
	}
	return false
}

// holds reports whether field has the value, or with past a Unix time that has passed.
func holds(body wire.Object, field string, value wire.Value, past bool, now time.Time) bool {
	v, ok := body.Get(field)
	if !ok {
		return false
	}
	if past {
		at, isInt := v.(wire.Int)
		return isInt && int64(at) <= now.Unix()
	}
	return sameWire(v, value)
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
		update := r.m.expand(r.m.decl.Paths.Update, id, parent)
		if status, _ := body.GetString(e.StatusField); e.NotStarted != "" && status == e.NotStarted {
			if _, err := r.api.call(ctx, "POST", update, e.Start); err != nil {
				return err
			}
		}
		at := wire.Int(time.Now().Unix() + e.Seconds)
		if _, err := r.api.call(ctx, "POST", update, wire.Object{{Name: e.Field, Value: at}}); err != nil {
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
