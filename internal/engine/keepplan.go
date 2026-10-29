package engine

import (
	"context"
	"fmt"
	"math/big"
	"sort"
	"strconv"
	"strings"

	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/hashicorp/terraform-plugin-go/tftypes"
	"github.com/hashicorp/terraform-plugin-log/tflog"
)

// planDiff is a value the plan knew that Stripe answered otherwise.
type planDiff struct {
	at             path.Path
	tfAt           *tftypes.AttributePath
	planned, given tftypes.Value
}

// keepPlan puts every value the plan knows back into the new state after a create or an
// update, and reports each that Stripe answered otherwise (ADR
// 2026-10-29-full-attribute-coverage, E4): a warning for a configured attribute, which the
// next plan shows as a difference, and a DEBUG log line for any other. Unknown planned values
// keep what Stripe returned. In strict mode every difference is an error, so that an
// acceptance test fails where Stripe or the fake answers otherwise than the conformance suite
// recorded.
func (r *Resource) keepPlan(ctx context.Context, state *tfsdk.State, plan, config tftypes.Value) diag.Diagnostics {
	var diags diag.Diagnostics
	var diffs []planDiff
	state.Raw = keepPlanned(plan, state.Raw, path.Empty(), tftypes.NewAttributePath(), &diffs)
	for _, d := range diffs {
		name := d.at.String()
		planned, given := display(d.planned), display(d.given)
		if r.m.sensitiveAt(d.at) {
			planned, given = "(sensitive value)", "(sensitive value)"
		}
		configured := !valueAt(config, d.tfAt).IsNull()
		switch {
		case configured:
			summary := "Stripe stored `" + name + "` differently"
			detail := fmt.Sprintf("Stripe stored `%s` as `%s`, not as configured (`%s`). The next plan will show the difference; configure Stripe's form to avoid it.", name, given, planned)
			if r.api.strict {
				diags.AddAttributeError(d.at, summary, detail)
			} else {
				diags.AddAttributeWarning(d.at, summary, detail)
			}
		case r.api.strict:
			diags.AddAttributeError(d.at, "Stripe returned `"+name+"` differently",
				fmt.Sprintf("Stripe returned `%s` as `%s`, not as planned (`%s`).", name, given, planned))
		default:
			tflog.Debug(ctx, fmt.Sprintf("Stripe returned %s as %s, not as planned (%s); the state keeps the plan until the next refresh", name, given, planned))
		}
	}
	return diags
}

// keepPlanned returns given with every value planned knows in its place, and adds to diffs
// each place where the two differ. It descends into objects, and into lists and maps whose
// elements line up, so that a difference names the attribute it is in; a set, or a list or map
// whose elements do not line up, is kept whole when the plan knows all of it and taken from
// given otherwise.
func keepPlanned(planned, given tftypes.Value, at path.Path, tfAt *tftypes.AttributePath, diffs *[]planDiff) tftypes.Value {
	if !planned.IsKnown() {
		return given
	}
	fully := planned.IsFullyKnown()
	if fully && planned.Equal(given) {
		return given
	}
	keep := func() tftypes.Value {
		*diffs = append(*diffs, planDiff{at: at, tfAt: tfAt, planned: planned, given: given})
		return planned
	}
	if planned.IsNull() || fully && (given.IsNull() || !given.IsKnown()) {
		return keep()
	}
	typ := planned.Type()
	switch {
	case typ.Is(tftypes.Object{}):
		var p, g map[string]tftypes.Value
		if planned.As(&p) != nil {
			return given
		}
		if given.IsKnown() && !given.IsNull() && given.As(&g) != nil {
			return given
		}
		out := make(map[string]tftypes.Value, len(p))
		for name, pv := range p {
			gv, ok := g[name]
			if !ok {
				gv = tftypes.NewValue(pv.Type(), nil)
			}
			out[name] = keepPlanned(pv, gv, at.AtName(name), tfAt.WithAttributeName(name), diffs)
		}
		return tftypes.NewValue(typ, out)
	case typ.Is(tftypes.List{}):
		var p, g []tftypes.Value
		if planned.As(&p) == nil && given.IsKnown() && !given.IsNull() && given.As(&g) == nil && len(p) == len(g) {
			out := make([]tftypes.Value, len(p))
			for i := range p {
				out[i] = keepPlanned(p[i], g[i], at.AtListIndex(i), tfAt.WithElementKeyInt(i), diffs)
			}
			return tftypes.NewValue(typ, out)
		}
	case typ.Is(tftypes.Map{}):
		var p, g map[string]tftypes.Value
		if planned.As(&p) == nil && given.IsKnown() && !given.IsNull() && given.As(&g) == nil && sameKeys(p, g) {
			out := make(map[string]tftypes.Value, len(p))
			for k := range p {
				out[k] = keepPlanned(p[k], g[k], at.AtMapKey(k), tfAt.WithElementKeyString(k), diffs)
			}
			return tftypes.NewValue(typ, out)
		}
	}
	if fully {
		return keep()
	}
	return given
}

func sameKeys(a, b map[string]tftypes.Value) bool {
	if len(a) != len(b) {
		return false
	}
	for k := range a {
		if _, ok := b[k]; !ok {
			return false
		}
	}
	return true
}

// display renders a value for a message: a string as it is, anything else as in HCL.
func display(v tftypes.Value) string {
	if s, ok := displayString(v); ok {
		return s
	}
	return displayHCL(v)
}

func displayString(v tftypes.Value) (string, bool) {
	var s string
	if v.IsKnown() && !v.IsNull() && v.Type().Is(tftypes.String) && v.As(&s) == nil {
		return s, true
	}
	return "", false
}

func displayHCL(v tftypes.Value) string {
	if !v.IsKnown() {
		return "(known after apply)"
	}
	if v.IsNull() {
		return "null"
	}
	typ := v.Type()
	switch {
	case typ.Is(tftypes.String):
		s, _ := displayString(v)
		return strconv.Quote(s)
	case typ.Is(tftypes.Number):
		f := new(big.Float)
		if v.As(&f) == nil {
			return f.Text('f', -1)
		}
	case typ.Is(tftypes.Bool):
		var b bool
		if v.As(&b) == nil {
			return strconv.FormatBool(b)
		}
	case typ.Is(tftypes.List{}), typ.Is(tftypes.Set{}), typ.Is(tftypes.Tuple{}):
		var elems []tftypes.Value
		if v.As(&elems) == nil {
			parts := make([]string, len(elems))
			for i, e := range elems {
				parts[i] = displayHCL(e)
			}
			return "[" + strings.Join(parts, ", ") + "]"
		}
	case typ.Is(tftypes.Map{}), typ.Is(tftypes.Object{}):
		var attrs map[string]tftypes.Value
		if v.As(&attrs) == nil {
			keys := make([]string, 0, len(attrs))
			for k := range attrs {
				keys = append(keys, k)
			}
			sort.Strings(keys)
			parts := make([]string, len(keys))
			for i, k := range keys {
				parts[i] = k + " = " + displayHCL(attrs[k])
			}
			return "{" + strings.Join(parts, ", ") + "}"
		}
	}
	return v.String()
}

// sensitiveAt reports whether the attribute at p, or one it is inside, is Sensitive.
func (m *model) sensitiveAt(p path.Path) bool {
	nodes := m.nodes
	for _, step := range p.Steps() {
		name, ok := step.(path.PathStepAttributeName)
		if !ok {
			continue
		}
		var found *node
		for _, n := range nodes {
			if n.AttrName() == string(name) {
				found = n
				break
			}
		}
		if found == nil {
			return false
		}
		if found.Sensitive {
			return true
		}
		nodes = found.Kids
	}
	return false
}

// strictly makes the warnings of a decode errors in strict mode.
func (a *api) strictly(diags diag.Diagnostics) diag.Diagnostics {
	if !a.strict {
		return diags
	}
	out := make(diag.Diagnostics, 0, len(diags))
	for _, d := range diags {
		if d.Severity() == diag.SeverityWarning {
			d = diag.NewErrorDiagnostic(d.Summary(), d.Detail())
		}
		out = append(out, d)
	}
	return out
}
