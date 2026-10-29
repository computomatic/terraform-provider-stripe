package engine

import (
	"context"
	"fmt"
	"regexp"
	"slices"
	"strings"

	"github.com/hashicorp/terraform-plugin-framework-timeouts/resource/timeouts"
	"github.com/hashicorp/terraform-plugin-framework/attr"
	dschema "github.com/hashicorp/terraform-plugin-framework/datasource/schema"
	ischema "github.com/hashicorp/terraform-plugin-framework/resource/identityschema"
	rschema "github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/boolplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/float64planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/int64planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/listplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/mapplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/objectplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/setplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types/basetypes"

	"github.com/computomatic/terraform-provider-stripe/internal/fields"
)

// describe is the description of a data source attribute.
func describe(n *node) string {
	return strings.TrimSpace(describeValues(n.Description, n.Enum) + pagedNote(n))
}

// pagedNote is the sentence that says a list Stripe returns a page at a time holds only its
// first page, and which type reads every element (ADR 2026-10-29-full-attribute-coverage, D3).
func pagedNote(n *node) string {
	if n.PagedBy == "" {
		return ""
	}
	return " Holds the first page of the list Stripe returns with the object; `has_more` is true when there are more. Read every element with the `" + n.PagedBy + "` data source."
}

// describeValues is a description followed by the values the attribute can hold.
func describeValues(d string, values []string) string {
	if len(values) > 0 {
		quoted := make([]string, len(values))
		for i, e := range values {
			quoted[i] = "`" + e + "`"
		}
		d += " Possible values: " + strings.Join(quoted, ", ") + "."
	}
	return strings.TrimSpace(d)
}

// describeResource is describe plus what a resource user must know: a change to a field that
// Stripe cannot update replaces the resource, or fails the plan where destroy is refused. A
// transition field lists only the values its transitions set, the ones a configuration takes,
// without the spec's own sentence that lists every value Stripe returns.
func (m *model) describeResource(n *node) string {
	desc, values := n.Description, n.Enum
	if set := m.transitionValues(n); len(set) > 0 {
		desc, values = specValues.ReplaceAllString(desc, ""), set
	}
	d := describeValues(desc, values) + pagedNote(n) + m.lowerCaseNote(n)
	if n.Replace {
		d += m.changingIt()
	}
	if n.FixedOnceSet != "" {
		d += m.changingFixed(n)
	}
	return strings.TrimSpace(d)
}

// lowerCaseNote is the sentence that says a LowerCase field must be written in lower case.
func (m *model) lowerCaseNote(n *node) string {
	if !m.lowerCase(n) {
		return ""
	}
	return " Stripe stores it in lower case, so it must be written in lower case."
}

func (m *model) lowerCase(n *node) bool {
	return n.Path == n.Name && slices.Contains(m.decl.LowerCase, n.Name)
}

// changingFixed is the sentence that ends the description of a FixedOnceSet field.
func (m *model) changingFixed(n *node) string {
	if m.decl.Delete == Refuse {
		return fmt.Sprintf(" Stripe changes it only from `%s`, so changing it from any other value fails the plan, because this resource cannot be replaced.", n.FixedOnceSet)
	}
	return fmt.Sprintf(" Stripe changes it only from `%s`, so changing it from any other value replaces the resource.", n.FixedOnceSet)
}

// specValues is the sentence in which Stripe's reference lists a field's values, such as a
// schedule's "Possible values are `not_started`, `active`, ... and `canceled`."
var specValues = regexp.MustCompile(` ?Possible values are [^.]*\.`)

// transitionValues lists the values the declaration's transitions set on a top-level node, in
// declaration order.
func (m *model) transitionValues(n *node) []string {
	var values []string
	for _, t := range m.decl.Transitions {
		if t.Field == n.Path && !slices.Contains(values, t.Value) {
			values = append(values, t.Value)
		}
	}
	return values
}

// changingIt is the sentence that ends the description of a field whose change would replace
// the resource.
func (m *model) changingIt() string {
	if m.decl.Delete == Refuse {
		return " Changing it fails the plan, because this resource cannot be replaced."
	}
	return " Changing it replaces the resource."
}

// planMods builds a node's plan modifiers from the framework constructors of one type. refuse
// wraps the modifiers that force a replacement, so that a resource whose destroy is refused
// fails the plan instead.
func planMods[M comparable](n *node, refuse func(M) M, useState, replace func() M, replaceIfSet ...func() M) []M {
	var mods []M
	if n.UseState {
		mods = append(mods, useState())
	}
	if n.Replace {
		mods = append(mods, refuse(replace()))
	}
	if n.ReplaceIfSet && len(replaceIfSet) > 0 {
		mods = append(mods, refuse(replaceIfSet[0]()))
	}
	return mods
}

// The refuse wrappers of planMods, one per framework modifier type. A resource that never
// refuses destroy keeps the framework's modifiers as they are.

func (m *model) refuseBool(mod planmodifier.Bool) planmodifier.Bool {
	if !m.refuses() {
		return mod
	}
	return refuseBool{refuser{m}, mod}
}

func (m *model) refuseInt64(mod planmodifier.Int64) planmodifier.Int64 {
	if !m.refuses() {
		return mod
	}
	return refuseInt64{refuser{m}, mod}
}

func (m *model) refuseFloat64(mod planmodifier.Float64) planmodifier.Float64 {
	if !m.refuses() {
		return mod
	}
	return refuseFloat64{refuser{m}, mod}
}

func (m *model) refuseString(mod planmodifier.String) planmodifier.String {
	if !m.refuses() {
		return mod
	}
	return refuseString{refuser{m}, mod}
}

func (m *model) refuseObject(mod planmodifier.Object) planmodifier.Object {
	if !m.refuses() {
		return mod
	}
	return refuseObject{refuser{m}, mod}
}

func (m *model) refuseList(mod planmodifier.List) planmodifier.List {
	if !m.refuses() {
		return mod
	}
	return refuseList{refuser{m}, mod}
}

func (m *model) refuseMap(mod planmodifier.Map) planmodifier.Map {
	if !m.refuses() {
		return mod
	}
	return refuseMap{refuser{m}, mod}
}

func (m *model) refuseSet(mod planmodifier.Set) planmodifier.Set {
	if !m.refuses() {
		return mod
	}
	return refuseSet{refuser{m}, mod}
}

// changedByConfig reports whether the configuration sets a value that differs from the
// state's. An unknown plan value, or a value the configuration leaves unset, is not one.
func changedByConfig(config, state attr.Value) bool {
	known := func(v attr.Value) bool { return !v.IsNull() && !v.IsUnknown() }
	return known(config) && known(state) && !config.Equal(state)
}

const replaceIfSetDescription = "Changing the configured value replaces the resource."

func (m *model) resourceSchema(ctx context.Context) rschema.Schema {
	attrs := make(map[string]rschema.Attribute, len(m.nodes)+2)
	for _, n := range m.nodes {
		attrs[n.AttrName()] = m.resourceAttribute(n)
	}
	if m.decl.Parent != "" {
		attrs[m.decl.Parent] = rschema.StringAttribute{
			Required:            true,
			MarkdownDescription: "The ID of the parent object." + m.changingIt(),
			PlanModifiers:       []planmodifier.String{m.refuseString(stringplanmodifier.RequiresReplace())},
		}
	}
	if m.decl.Attach != "" {
		attrs[m.decl.Attach] = rschema.StringAttribute{
			Required:            true,
			MarkdownDescription: "The ID of the existing object to attach to the parent." + m.changingIt() + " The resource's `id` is the ID Stripe returns, which differs from this when a test ID is attached.",
			PlanModifiers:       []planmodifier.String{m.refuseString(stringplanmodifier.RequiresReplace())},
		}
	}
	if m.decl.Upload != nil {
		m.uploadAttributes(attrs)
	}
	attrs["timeouts"] = timeouts.Attributes(ctx, timeouts.Opts{
		Create: true,
		Update: m.decl.Paths.Update != "" || len(m.decl.Transitions) > 0 || m.decl.Advance != nil,
		Delete: true,
	})
	return rschema.Schema{MarkdownDescription: m.decl.Description, Attributes: attrs}
}

func (m *model) resourceAttribute(n *node) rschema.Attribute {
	desc := m.describeResource(n)
	switch n.Shape {
	case fields.Bool:
		return rschema.BoolAttribute{Required: n.Required, Optional: n.Optional, Computed: n.Computed, Sensitive: n.Sensitive, MarkdownDescription: desc,
			PlanModifiers: planMods(n, m.refuseBool, boolplanmodifier.UseStateForUnknown, boolplanmodifier.RequiresReplace, func() planmodifier.Bool {
				return boolplanmodifier.RequiresReplaceIf(func(_ context.Context, req planmodifier.BoolRequest, resp *boolplanmodifier.RequiresReplaceIfFuncResponse) {
					resp.RequiresReplace = changedByConfig(req.ConfigValue, req.StateValue)
				}, replaceIfSetDescription, replaceIfSetDescription)
			})}
	case fields.Int:
		return rschema.Int64Attribute{Required: n.Required, Optional: n.Optional, Computed: n.Computed, Sensitive: n.Sensitive, MarkdownDescription: desc,
			PlanModifiers: planMods(n, m.refuseInt64, int64planmodifier.UseStateForUnknown, int64planmodifier.RequiresReplace, func() planmodifier.Int64 {
				return int64planmodifier.RequiresReplaceIf(func(_ context.Context, req planmodifier.Int64Request, resp *int64planmodifier.RequiresReplaceIfFuncResponse) {
					resp.RequiresReplace = changedByConfig(req.ConfigValue, req.StateValue)
				}, replaceIfSetDescription, replaceIfSetDescription)
			})}
	case fields.Float:
		return rschema.Float64Attribute{Required: n.Required, Optional: n.Optional, Computed: n.Computed, Sensitive: n.Sensitive, MarkdownDescription: desc,
			PlanModifiers: planMods(n, m.refuseFloat64, float64planmodifier.UseStateForUnknown, float64planmodifier.RequiresReplace, func() planmodifier.Float64 {
				return float64planmodifier.RequiresReplaceIf(func(_ context.Context, req planmodifier.Float64Request, resp *float64planmodifier.RequiresReplaceIfFuncResponse) {
					resp.RequiresReplace = changedByConfig(req.ConfigValue, req.StateValue)
				}, replaceIfSetDescription, replaceIfSetDescription)
			})}
	case fields.Object:
		a := rschema.SingleNestedAttribute{Required: n.Required, Optional: n.Optional, Computed: n.Computed, Sensitive: n.Sensitive, MarkdownDescription: desc,
			Attributes:    m.resourceAttributes(n.Kids),
			PlanModifiers: planMods(n, m.refuseObject, objectplanmodifier.UseStateForUnknown, objectplanmodifier.RequiresReplace)}
		if n.TokenForm != "" {
			a.Validators = append(a.Validators, oneForm{token: n.TokenForm, hash: n.AttrName()})
		}
		if !m.v2 && n.Writable() && !n.computedOnly() {
			a.Validators = append(a.Validators, emptyObjectValidator(n.Kids, false))
		}
		return a
	case fields.ObjectList:
		return rschema.ListNestedAttribute{Required: n.Required, Optional: n.Optional, Computed: n.Computed, Sensitive: n.Sensitive, MarkdownDescription: desc,
			NestedObject:  rschema.NestedAttributeObject{Attributes: m.resourceAttributes(n.Kids), Validators: m.elementValidators(n)},
			PlanModifiers: planMods(n, m.refuseList, listplanmodifier.UseStateForUnknown, listplanmodifier.RequiresReplace)}
	case fields.ObjectMap:
		return rschema.MapNestedAttribute{Required: n.Required, Optional: n.Optional, Computed: n.Computed, Sensitive: n.Sensitive, MarkdownDescription: desc,
			NestedObject:  rschema.NestedAttributeObject{Attributes: m.resourceAttributes(n.Kids)},
			PlanModifiers: planMods(n, m.refuseMap, mapplanmodifier.UseStateForUnknown, mapplanmodifier.RequiresReplace)}
	case fields.StringMap:
		return rschema.MapAttribute{ElementType: scalarType(fields.String), Required: n.Required, Optional: n.Optional, Computed: n.Computed, Sensitive: n.Sensitive, MarkdownDescription: desc,
			PlanModifiers: planMods(n, m.refuseMap, mapplanmodifier.UseStateForUnknown, mapplanmodifier.RequiresReplace)}
	case fields.IntMap:
		return rschema.MapAttribute{ElementType: scalarType(fields.Int), Required: n.Required, Optional: n.Optional, Computed: n.Computed, Sensitive: n.Sensitive, MarkdownDescription: desc,
			PlanModifiers: planMods(n, m.refuseMap, mapplanmodifier.UseStateForUnknown, mapplanmodifier.RequiresReplace)}
	case fields.ScalarList:
		return rschema.ListAttribute{ElementType: scalarType(n.Elem), Required: n.Required, Optional: n.Optional, Computed: n.Computed, Sensitive: n.Sensitive, MarkdownDescription: desc,
			PlanModifiers: planMods(n, m.refuseList, listplanmodifier.UseStateForUnknown, listplanmodifier.RequiresReplace)}
	case fields.ScalarSet:
		return rschema.SetAttribute{ElementType: scalarType(n.Elem), Required: n.Required, Optional: n.Optional, Computed: n.Computed, Sensitive: n.Sensitive, MarkdownDescription: desc,
			PlanModifiers: planMods(n, m.refuseSet, setplanmodifier.UseStateForUnknown, setplanmodifier.RequiresReplace)}
	default:
		useState := stringplanmodifier.UseStateForUnknown
		if n.DataForm != "" {
			useState = func() planmodifier.String { return keepToken{hash: n.DataForm} }
		}
		mods := planMods(n, m.refuseString, useState, stringplanmodifier.RequiresReplace, func() planmodifier.String {
			return stringplanmodifier.RequiresReplaceIf(func(_ context.Context, req planmodifier.StringRequest, resp *stringplanmodifier.RequiresReplaceIfFuncResponse) {
				resp.RequiresReplace = changedByConfig(req.ConfigValue, req.StateValue)
			}, replaceIfSetDescription, replaceIfSetDescription)
		})
		if from := n.FixedOnceSet; from != "" {
			mods = append(mods, m.refuseString(stringplanmodifier.RequiresReplaceIf(func(_ context.Context, req planmodifier.StringRequest, resp *stringplanmodifier.RequiresReplaceIfFuncResponse) {
				resp.RequiresReplace = changedFromFixed(req.ConfigValue, req.StateValue, from)
			}, replaceFixedDescription, replaceFixedDescription)))
		}
		a := rschema.StringAttribute{Required: n.Required, Optional: n.Optional, Computed: n.Computed, Sensitive: n.Sensitive, MarkdownDescription: desc,
			PlanModifiers: mods}
		if n.OneRequired {
			a.Validators = []validator.String{requireOneForm{token: n.AttrName(), hash: n.DataForm}}
		}
		return a
	}
}

// changedFromFixed reports whether the configuration changes a FixedOnceSet field that holds
// a value other than from, the one Stripe lets it change from.
func changedFromFixed(config, state basetypes.StringValue, from string) bool {
	return changedByConfig(config, state) && state.ValueString() != from
}

const replaceFixedDescription = "Changing a value Stripe has fixed replaces the resource."

// elementValidators refuses, on /v1, a list element whose configuration would encode to
// nothing.
func (m *model) elementValidators(n *node) []validator.Object {
	if m.v2 || !n.Writable() || n.computedOnly() {
		return nil
	}
	return []validator.Object{emptyObjectValidator(n.Kids, false)}
}

func (m *model) resourceAttributes(kids []*node) map[string]rschema.Attribute {
	attrs := make(map[string]rschema.Attribute, len(kids))
	for _, k := range kids {
		attrs[k.AttrName()] = m.resourceAttribute(k)
	}
	return attrs
}

// identitySchema declares the resource identity: the ID, plus the parent ID for an object
// addressed under a parent.
func (m *model) identitySchema() ischema.Schema {
	attrs := map[string]ischema.Attribute{"id": ischema.StringAttribute{RequiredForImport: true}}
	if m.decl.Parent != "" {
		attrs[m.decl.Parent] = ischema.StringAttribute{RequiredForImport: true}
	}
	return ischema.Schema{Attributes: attrs}
}

func (m *model) dataSourceSchema() dschema.Schema {
	attrs := make(map[string]dschema.Attribute, len(m.nodes)+1)
	lookups := map[string]bool{}
	if !m.decl.Singleton && len(m.decl.FindBy) == 0 {
		lookups["id"] = true
	}
	for _, l := range m.decl.Lookups {
		lookups[l.Field] = true
	}
	for _, n := range m.nodes {
		a := dataSourceAttribute(n)
		if lookups[n.Name] {
			desc := strings.TrimSpace(describe(n) + m.lowerCaseNote(n))
			if n.Name == "id" {
				// The create-time description tells a resource user how to choose an ID.
				desc = "The ID of the " + Noun(m.decl.TypeName) + " to read."
			}
			a = dschema.StringAttribute{Optional: true, Computed: true, MarkdownDescription: desc}
		}
		if m.findParam(n.Name) {
			a = findAttribute(n, !m.v2)
		}
		if p, ok := m.listParam(n.Name); ok {
			a = dschema.StringAttribute{Optional: true, Computed: true, MarkdownDescription: p.Description}
		}
		attrs[n.AttrName()] = a
	}
	for _, p := range m.decl.FindBy {
		if _, ok := m.top(p.Name); !ok {
			attrs[p.Name] = dschema.StringAttribute{Required: true, MarkdownDescription: p.Description}
		}
	}
	if m.decl.Parent != "" {
		attrs[m.decl.Parent] = dschema.StringAttribute{Required: true, MarkdownDescription: "The ID of the parent object."}
	}
	desc := m.decl.DataSourceDescription
	if desc == "" {
		desc = m.decl.Description
	}
	return dschema.Schema{MarkdownDescription: desc, Attributes: attrs}
}

// Noun is a type name in words, such as `product feature` for `stripe_product_feature`. A
// `/v2` type name ends in `_v2` (architecture ADR D7), which reads first in words:
// `v2 account person` for `stripe_account_person_v2`. tools/docgen writes the pages with it.
func Noun(typeName string) string {
	name := strings.TrimPrefix(typeName, "stripe_")
	if base, ok := strings.CutSuffix(name, "_v2"); ok {
		name = "v2_" + base
	}
	return strings.ReplaceAll(name, "_", " ")
}

// WithArticle is a noun with `a` or `an` before it, by its first letter. tools/docgen writes
// the pages with the same rule.
func WithArticle(n string) string {
	if strings.ContainsAny(n[:1], "aeiou") {
		return "an " + n
	}
	return "a " + n
}

func dataSourceAttribute(n *node) dschema.Attribute {
	desc := describe(n)
	switch n.Shape {
	case fields.Bool:
		return dschema.BoolAttribute{Computed: true, Sensitive: n.Sensitive, MarkdownDescription: desc}
	case fields.Int:
		return dschema.Int64Attribute{Computed: true, Sensitive: n.Sensitive, MarkdownDescription: desc}
	case fields.Float:
		return dschema.Float64Attribute{Computed: true, Sensitive: n.Sensitive, MarkdownDescription: desc}
	case fields.Object:
		return dschema.SingleNestedAttribute{Computed: true, Sensitive: n.Sensitive, MarkdownDescription: desc, Attributes: dataSourceAttributes(n.Kids)}
	case fields.ObjectList:
		return dschema.ListNestedAttribute{Computed: true, Sensitive: n.Sensitive, MarkdownDescription: desc, NestedObject: dschema.NestedAttributeObject{Attributes: dataSourceAttributes(n.Kids)}}
	case fields.ObjectMap:
		return dschema.MapNestedAttribute{Computed: true, Sensitive: n.Sensitive, MarkdownDescription: desc, NestedObject: dschema.NestedAttributeObject{Attributes: dataSourceAttributes(n.Kids)}}
	case fields.StringMap:
		return dschema.MapAttribute{ElementType: scalarType(fields.String), Computed: true, Sensitive: n.Sensitive, MarkdownDescription: desc}
	case fields.IntMap:
		return dschema.MapAttribute{ElementType: scalarType(fields.Int), Computed: true, Sensitive: n.Sensitive, MarkdownDescription: desc}
	case fields.ScalarList:
		return dschema.ListAttribute{ElementType: scalarType(n.Elem), Computed: true, Sensitive: n.Sensitive, MarkdownDescription: desc}
	case fields.ScalarSet:
		return dschema.SetAttribute{ElementType: scalarType(n.Elem), Computed: true, Sensitive: n.Sensitive, MarkdownDescription: desc}
	default:
		return dschema.StringAttribute{Computed: true, Sensitive: n.Sensitive, MarkdownDescription: desc}
	}
}

// findAttribute is the Required attribute of a find parameter that names a field. The fields
// of an object are Optional, because Stripe, not the schema, says which it needs; on /v1 an
// object with none set is refused, as it would be sent empty.
func findAttribute(n *node, v1 bool) dschema.Attribute {
	desc := describe(n)
	switch n.Shape {
	case fields.Int:
		return dschema.Int64Attribute{Required: true, MarkdownDescription: desc}
	case fields.Object:
		kids := make(map[string]dschema.Attribute, len(n.Kids))
		for _, k := range n.Kids {
			if k.Shape == fields.Int {
				kids[k.AttrName()] = dschema.Int64Attribute{Optional: true, MarkdownDescription: describe(k)}
			} else {
				kids[k.AttrName()] = dschema.StringAttribute{Optional: true, MarkdownDescription: describe(k)}
			}
		}
		a := dschema.SingleNestedAttribute{Required: true, MarkdownDescription: desc, Attributes: kids}
		if v1 {
			a.Validators = []validator.Object{emptyObjectValidator(n.Kids, true)}
		}
		return a
	default:
		return dschema.StringAttribute{Required: true, MarkdownDescription: desc}
	}
}

func dataSourceAttributes(kids []*node) map[string]dschema.Attribute {
	attrs := make(map[string]dschema.Attribute, len(kids))
	for _, k := range kids {
		attrs[k.AttrName()] = dataSourceAttribute(k)
	}
	return attrs
}
