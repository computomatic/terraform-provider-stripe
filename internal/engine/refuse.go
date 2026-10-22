package engine

import (
	"context"
	"errors"
	"fmt"
	"net/http"

	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/hashicorp/terraform-plugin-go/tftypes"

	"github.com/computomatic/terraform-provider-stripe/internal/wire"
)

// refuses reports whether destroy of some object of the resource is refused (ADR
// 2026-10-22-destroy-semantics, D2).
func (m *model) refuses() bool { return m.decl.Delete == Refuse || m.decl.RefuseWhen != nil }

// refusal is the reason destroy and replacement of the object in state are refused, or ""
// when they are not. The state is the refreshed prior state.
func (m *model) refusal(state tftypes.Value) string {
	if m.decl.Delete == Refuse {
		return m.decl.RefuseReason
	}
	if w := m.decl.RefuseWhen; w != nil && !state.IsNull() {
		n, _ := m.top(w.Field)
		if holdsValue(valueAt(state, tftypes.NewAttributePath().WithAttributeName(n.AttrName())), w.Value) {
			return w.Reason
		}
	}
	return ""
}

// holdsValue reports whether a state value equals a wire.Bool or wire.String.
func holdsValue(v tftypes.Value, want wire.Value) bool {
	if !v.IsKnown() || v.IsNull() {
		return false
	}
	switch want := want.(type) {
	case wire.Bool:
		var b bool
		return v.As(&b) == nil && b == bool(want)
	case wire.String:
		var s string
		return v.As(&s) == nil && s == string(want)
	}
	return false
}

// removeFromState is the guidance every refusal ends with. %[1]s is the type name.
const removeFromState = "To stop managing it, replace its resource block with a removed block and apply (Terraform 1.7 or later, OpenTofu 1.10 or later):\n\n" +
	"removed {\n  from = %[1]s.<name>\n\n  lifecycle {\n    destroy = false\n  }\n}\n\n" +
	"or run `terraform state rm %[1]s.<name>` (`tofu state rm` in OpenTofu)."

func (m *model) destroyRefusal(reason string) string {
	return fmt.Sprintf("%s The provider does not destroy what Stripe cannot delete or archive, so the %s is still in the Stripe account and in the state.\n\n%s",
		reason, noun(m.decl.TypeName), fmt.Sprintf(removeFromState, m.decl.TypeName))
}

func (m *model) replaceRefusal(p path.Path, reason string) string {
	thing := noun(m.decl.TypeName)
	return fmt.Sprintf("Changing `%[1]s` needs a new %[2]s, because Stripe cannot change it on an existing one, and the existing %[2]s cannot be destroyed. %[3]s\n\n"+
		"Revert the change, add `lifecycle { ignore_changes = [%[1]s] }` to leave it as it is, or remove the existing %[2]s from the state with a removed block or `terraform state rm` and apply again to create a new one.",
		p.String(), thing, reason)
}

// destroyNote is the page's callout for a resource that refuses destroy, without its sigil,
// or "" for one that never does.
func (m *model) destroyNote() string {
	thing := noun(m.decl.TypeName)
	const remove = " remove it from the state as [Removing from State](#removing-from-state) shows; it stays in the Stripe account."
	switch {
	case m.decl.Delete == Refuse:
		return m.decl.RefuseReason + " `terraform destroy`, and a change that would replace the " + thing + ", fail at plan time. To stop managing " + WithArticle(thing) + "," + remove
	case m.decl.RefuseWhen != nil:
		return m.decl.RefuseWhen.Reason + " `terraform destroy`, and a change that would replace it, fail at plan time for such " + WithArticle(thing) + ". To stop managing one," + remove
	}
	return ""
}

// refusedByStripe reports whether err is Stripe refusing to delete or archive an object
// that a retrieve afterwards shows in the declaration's RefuseWhen state. A failed retrieve
// leaves err as Stripe's.
func (r *Resource) refusedByStripe(ctx context.Context, id, parent string, err error) bool {
	w := r.m.decl.RefuseWhen
	if w == nil || !refused(err, &Refusal{Status: http.StatusBadRequest, Type: "invalid_request_error"}) {
		return false
	}
	body, getErr := r.api.call(ctx, "GET", r.m.expand(r.m.decl.Paths.Retrieve, id, parent), nil)
	if getErr != nil {
		return false
	}
	v, ok := body.Get(w.Field)
	return ok && sameWire(v, w.Value)
}

// refuser fails a replacement of an object whose destroy is refused. It wraps the plan
// modifier that forces the replacement, so it sees each attribute at every depth, list
// elements included.
type refuser struct{ m *model }

// refuse fails the plan when the wrapped modifier asks for a replacement that is a real change.
// The framework runs a nested object's modifiers before its children's, so an object whose
// computed children are still unknown asks for one that Terraform drops once the children keep
// their state; only a known planned value that differs, or a configured value that differs,
// is a change.
func (r refuser) refuse(ctx context.Context, state tfsdk.State, p path.Path, plan, config, prior attr.Value, replace *bool, diags *diag.Diagnostics) {
	if !*replace {
		return
	}
	reason := r.m.refusal(state.Raw)
	if reason == "" {
		return
	}
	pv, errPlan := plan.ToTerraformValue(ctx)
	cv, errConfig := config.ToTerraformValue(ctx)
	sv, errState := prior.ToTerraformValue(ctx)
	if errPlan != nil || errConfig != nil || errState != nil {
		diags.AddAttributeError(p, "Planning a replacement", errors.Join(errPlan, errConfig, errState).Error())
		return
	}
	if !pv.IsFullyKnown() && !configChanges(cv, sv) {
		return
	}
	*replace = false
	diags.AddAttributeError(p, "Cannot replace "+r.m.decl.TypeName, r.m.replaceRefusal(p, reason))
}

// configChanges reports whether a configured value differs from the state's. A value the
// configuration leaves unset is no change, and one not known yet is.
func configChanges(config, state tftypes.Value) bool {
	switch {
	case !config.IsKnown():
		return true
	case config.IsNull():
		return false
	case !state.IsKnown() || state.IsNull():
		return true
	}
	switch config.Type().(type) {
	case tftypes.Object, tftypes.Map:
		var c, s map[string]tftypes.Value
		if config.As(&c) != nil || state.As(&s) != nil {
			return true
		}
		for k, v := range c {
			if prior, ok := s[k]; !ok && !v.IsNull() || ok && configChanges(v, prior) {
				return true
			}
		}
		return false
	case tftypes.List:
		var c, s []tftypes.Value
		if config.As(&c) != nil || state.As(&s) != nil || len(c) != len(s) {
			return true
		}
		for i := range c {
			if configChanges(c[i], s[i]) {
				return true
			}
		}
		return false
	}
	return !config.Equal(state)
}

type refuseBool struct {
	refuser
	planmodifier.Bool
}

func (r refuseBool) PlanModifyBool(ctx context.Context, req planmodifier.BoolRequest, resp *planmodifier.BoolResponse) {
	r.Bool.PlanModifyBool(ctx, req, resp)
	r.refuse(ctx, req.State, req.Path, req.PlanValue, req.ConfigValue, req.StateValue, &resp.RequiresReplace, &resp.Diagnostics)
}

type refuseInt64 struct {
	refuser
	planmodifier.Int64
}

func (r refuseInt64) PlanModifyInt64(ctx context.Context, req planmodifier.Int64Request, resp *planmodifier.Int64Response) {
	r.Int64.PlanModifyInt64(ctx, req, resp)
	r.refuse(ctx, req.State, req.Path, req.PlanValue, req.ConfigValue, req.StateValue, &resp.RequiresReplace, &resp.Diagnostics)
}

type refuseFloat64 struct {
	refuser
	planmodifier.Float64
}

func (r refuseFloat64) PlanModifyFloat64(ctx context.Context, req planmodifier.Float64Request, resp *planmodifier.Float64Response) {
	r.Float64.PlanModifyFloat64(ctx, req, resp)
	r.refuse(ctx, req.State, req.Path, req.PlanValue, req.ConfigValue, req.StateValue, &resp.RequiresReplace, &resp.Diagnostics)
}

type refuseString struct {
	refuser
	planmodifier.String
}

func (r refuseString) PlanModifyString(ctx context.Context, req planmodifier.StringRequest, resp *planmodifier.StringResponse) {
	r.String.PlanModifyString(ctx, req, resp)
	r.refuse(ctx, req.State, req.Path, req.PlanValue, req.ConfigValue, req.StateValue, &resp.RequiresReplace, &resp.Diagnostics)
}

type refuseObject struct {
	refuser
	planmodifier.Object
}

func (r refuseObject) PlanModifyObject(ctx context.Context, req planmodifier.ObjectRequest, resp *planmodifier.ObjectResponse) {
	r.Object.PlanModifyObject(ctx, req, resp)
	r.refuse(ctx, req.State, req.Path, req.PlanValue, req.ConfigValue, req.StateValue, &resp.RequiresReplace, &resp.Diagnostics)
}

type refuseList struct {
	refuser
	planmodifier.List
}

func (r refuseList) PlanModifyList(ctx context.Context, req planmodifier.ListRequest, resp *planmodifier.ListResponse) {
	r.List.PlanModifyList(ctx, req, resp)
	r.refuse(ctx, req.State, req.Path, req.PlanValue, req.ConfigValue, req.StateValue, &resp.RequiresReplace, &resp.Diagnostics)
}

type refuseMap struct {
	refuser
	planmodifier.Map
}

func (r refuseMap) PlanModifyMap(ctx context.Context, req planmodifier.MapRequest, resp *planmodifier.MapResponse) {
	r.Map.PlanModifyMap(ctx, req, resp)
	r.refuse(ctx, req.State, req.Path, req.PlanValue, req.ConfigValue, req.StateValue, &resp.RequiresReplace, &resp.Diagnostics)
}

type refuseSet struct {
	refuser
	planmodifier.Set
}

func (r refuseSet) PlanModifySet(ctx context.Context, req planmodifier.SetRequest, resp *planmodifier.SetResponse) {
	r.Set.PlanModifySet(ctx, req, resp)
	r.refuse(ctx, req.State, req.Path, req.PlanValue, req.ConfigValue, req.StateValue, &resp.RequiresReplace, &resp.Diagnostics)
}
