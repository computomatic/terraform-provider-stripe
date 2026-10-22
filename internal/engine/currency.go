package engine

import (
	"context"
	"fmt"
	"regexp"
	"slices"
	"strings"

	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-framework/types/basetypes"

	"github.com/computomatic/terraform-provider-stripe/internal/wire"
)

// withoutOwnCurrency drops the object's own currency from a response's currency options.
// Stripe adds it to the expanded map although the object never takes it as an option.
func withoutOwnCurrency(options wire.Value, body wire.Object) wire.Value {
	o, ok := options.(wire.Object)
	if !ok {
		return options
	}
	own, _ := body.GetString("currency")
	kept := wire.Object{}
	for _, f := range o {
		if !strings.EqualFold(f.Name, own) {
			kept = append(kept, f)
		}
	}
	return kept
}

// addedKeys returns the entries of the planned options map that the state does not have, or
// null when there are none, so the update sends nothing for the field. Stripe refuses a
// request that repeats an existing currency, and an empty map would unset the options.
func addedKeys(plan, state attr.Value) attr.Value {
	p, isMap := plan.(basetypes.MapValue)
	if !isMap || p.IsNull() || p.IsUnknown() {
		return plan
	}
	have := map[string]attr.Value{}
	if s, ok := state.(basetypes.MapValue); ok && !s.IsNull() && !s.IsUnknown() {
		have = s.Elements()
	}
	added := map[string]attr.Value{}
	for k, v := range p.Elements() {
		if _, exists := have[k]; !exists {
			added[k] = v
		}
	}
	if len(added) == 0 {
		return types.MapNull(p.ElementType(context.Background()))
	}
	out, diags := types.MapValue(p.ElementType(context.Background()), added)
	if diags.HasError() {
		return plan
	}
	return out
}

// covers reports whether the state holds what the configuration asks for. A value the
// configuration leaves null asks for nothing, because Stripe fills it in.
//
// A value that is unknown at plan time, such as a reference to another resource, may turn
// out different from the state, and the replace decision cannot wait for apply: an update
// that found it changed would send nothing for an existing currency and leave a replacement
// for the next plan. So an unknown value counts as a possible change. The cost is a
// replacement when it turns out equal.
func covers(config, state attr.Value) bool {
	if config == nil || config.IsNull() {
		return true
	}
	if config.IsUnknown() {
		return false
	}
	if state == nil || state.IsNull() || state.IsUnknown() {
		return false
	}
	switch c := config.(type) {
	case basetypes.ObjectValue:
		s, ok := state.(basetypes.ObjectValue)
		if !ok {
			return false
		}
		for name, cv := range c.Attributes() {
			if !covers(cv, s.Attributes()[name]) {
				return false
			}
		}
		return true
	case basetypes.ListValue:
		s, ok := state.(basetypes.ListValue)
		if !ok || len(c.Elements()) != len(s.Elements()) {
			return false
		}
		for i, cv := range c.Elements() {
			if !covers(cv, s.Elements()[i]) {
				return false
			}
		}
		return true
	}
	return config.Equal(state)
}

// currencyOptionsChanged reports whether the configuration changes or removes an option
// the state already has. Stripe refuses both on update.
func currencyOptionsChanged(config, state basetypes.MapValue) bool {
	if state.IsNull() || state.IsUnknown() || len(state.Elements()) == 0 || config.IsUnknown() {
		return false
	}
	want := config.Elements()
	for key, have := range state.Elements() {
		w, kept := want[key]
		if !kept || !covers(w, have) {
			return true
		}
	}
	return false
}

// currencyOptionsAttr returns the attribute name of the declaration's currency options, or
// an empty string when it declares none.
func (m *model) currencyOptionsAttr() string {
	if m.decl.CurrencyOptions == "" {
		return ""
	}
	n, _ := m.top(m.decl.CurrencyOptions)
	return n.AttrName()
}

// currencyCode is how Stripe spells a currency key: a lowercase ISO code.
var currencyCode = regexp.MustCompile(`^[a-z]{3}$`)

// ValidateConfig rejects a currency option key that is not a lowercase ISO code, and one
// for the object's own currency, which Stripe adds by itself and refuses to be given.
func (r *Resource) ValidateConfig(ctx context.Context, req resource.ValidateConfigRequest, resp *resource.ValidateConfigResponse) {
	r.validateExactlyOneOf(ctx, req, resp)
	r.validateTransitions(ctx, req, resp)
	name := r.m.currencyOptionsAttr()
	if name == "" {
		return
	}
	var currency types.String
	var options types.Map
	resp.Diagnostics.Append(req.Config.GetAttribute(ctx, path.Root("currency"), &currency)...)
	resp.Diagnostics.Append(req.Config.GetAttribute(ctx, path.Root(name), &options)...)
	if resp.Diagnostics.HasError() || currency.IsNull() || currency.IsUnknown() || options.IsNull() || options.IsUnknown() {
		return
	}
	for key := range options.Elements() {
		if !currencyCode.MatchString(key) {
			resp.Diagnostics.AddAttributeError(path.Root(name).AtMapKey(key), "Invalid currency option key",
				fmt.Sprintf("`%s` is not a currency code. Stripe keys currency options by three lowercase letters, such as `eur`.", key))
			continue
		}
		if strings.EqualFold(key, currency.ValueString()) {
			resp.Diagnostics.AddAttributeError(path.Root(name).AtMapKey(key), "Own currency in currency options",
				fmt.Sprintf("`%s` is the currency of this object. Remove the `%s` entry: Stripe adds the object's own currency by itself and refuses a request that names it.", key, key))
		}
	}
}

// requiresReplaceForCurrencyOptions flags a change to, or removal of, an existing currency
// option. Stripe refuses it on update. Adding a currency is an update.
func (r *Resource) requiresReplaceForCurrencyOptions(ctx context.Context, req resource.ModifyPlanRequest, resp *resource.ModifyPlanResponse) bool {
	name := r.m.currencyOptionsAttr()
	if name == "" {
		return false
	}
	var config, state basetypes.MapValue
	diags := req.Config.GetAttribute(ctx, path.Root(name), &config)
	diags.Append(req.State.GetAttribute(ctx, path.Root(name), &state)...)
	resp.Diagnostics.Append(diags...)
	if diags.HasError() || !currencyOptionsChanged(config, state) {
		return false
	}
	resp.RequiresReplace = append(resp.RequiresReplace, path.Root(name))
	return true
}

// validateExactlyOneOf refuses a configuration that sets none or several of the fields of a
// declared group. A group with an unknown value is left for apply.
func (r *Resource) validateExactlyOneOf(ctx context.Context, req resource.ValidateConfigRequest, resp *resource.ValidateConfigResponse) {
	for _, group := range r.m.decl.ExactlyOneOf {
		var names []string
		set, unknown := 0, 0
		for _, field := range group {
			n, _ := r.m.top(field)
			names = append(names, "`"+n.AttrName()+"`")
			var v attr.Value
			resp.Diagnostics.Append(req.Config.GetAttribute(ctx, path.Root(n.AttrName()), &v)...)
			switch {
			case resp.Diagnostics.HasError():
				return
			case v.IsUnknown():
				unknown++
			case !v.IsNull():
				set++
			}
		}
		if set > 1 || set == 0 && unknown == 0 {
			resp.Diagnostics.AddError("Invalid configuration", fmt.Sprintf("exactly one of %s must be set, got %d", strings.Join(names, ", "), set))
		}
	}
}

// validateTransitions refuses a configured value of a transition field that no transition
// sets, such as a quote's `accepted`, which Stripe reaches by other routes.
func (r *Resource) validateTransitions(ctx context.Context, req resource.ValidateConfigRequest, resp *resource.ValidateConfigResponse) {
	byField := map[string][]string{}
	for _, t := range r.m.decl.Transitions {
		byField[t.Field] = append(byField[t.Field], t.Value)
	}
	for field, values := range byField {
		n, _ := r.m.top(field)
		var v types.String
		resp.Diagnostics.Append(req.Config.GetAttribute(ctx, path.Root(n.AttrName()), &v)...)
		if resp.Diagnostics.HasError() || v.IsNull() || v.IsUnknown() || slices.Contains(values, v.ValueString()) {
			continue
		}
		resp.Diagnostics.AddAttributeError(path.Root(n.AttrName()), "Unsupported value",
			fmt.Sprintf("`%s` can be set to %s; got %q.", n.AttrName(), "`"+strings.Join(values, "` or `")+"`", v.ValueString()))
	}
}
