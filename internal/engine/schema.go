package engine

import (
	"context"
	"strings"

	"github.com/hashicorp/terraform-plugin-framework-timeouts/resource/timeouts"
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

	"github.com/computomatic/terraform-provider-stripe/internal/fields"
)

func describe(n *node) string {
	d := n.Description
	if len(n.Enum) > 0 {
		quoted := make([]string, len(n.Enum))
		for i, e := range n.Enum {
			quoted[i] = "`" + e + "`"
		}
		d += " Possible values: " + strings.Join(quoted, ", ") + "."
	}
	return strings.TrimSpace(d)
}

// planMods builds a node's plan modifiers from the framework constructors of one type.
func planMods[M comparable](n *node, useState, replace func() M) []M {
	var mods []M
	if n.UseState {
		mods = append(mods, useState())
	}
	if n.Replace {
		mods = append(mods, replace())
	}
	return mods
}

func (m *model) resourceSchema(ctx context.Context) rschema.Schema {
	attrs := make(map[string]rschema.Attribute, len(m.nodes)+2)
	for _, n := range m.nodes {
		attrs[n.AttrName()] = resourceAttribute(n)
	}
	if m.decl.Parent != "" {
		attrs[m.decl.Parent] = rschema.StringAttribute{
			Required:            true,
			MarkdownDescription: "The ID of the parent object. Changing it replaces the resource.",
			PlanModifiers:       []planmodifier.String{stringplanmodifier.RequiresReplace()},
		}
	}
	attrs["timeouts"] = timeouts.Attributes(ctx, timeouts.Opts{
		Create: true,
		Update: m.decl.Paths.Update != "",
		Delete: true,
	})
	return rschema.Schema{MarkdownDescription: m.decl.Description, Attributes: attrs}
}

func resourceAttribute(n *node) rschema.Attribute {
	desc := describe(n)
	switch n.Shape {
	case fields.Bool:
		return rschema.BoolAttribute{Required: n.Required, Optional: n.Optional, Computed: n.Computed, Sensitive: n.Sensitive, MarkdownDescription: desc,
			PlanModifiers: planMods(n, boolplanmodifier.UseStateForUnknown, boolplanmodifier.RequiresReplace)}
	case fields.Int:
		return rschema.Int64Attribute{Required: n.Required, Optional: n.Optional, Computed: n.Computed, Sensitive: n.Sensitive, MarkdownDescription: desc,
			PlanModifiers: planMods(n, int64planmodifier.UseStateForUnknown, int64planmodifier.RequiresReplace)}
	case fields.Float:
		return rschema.Float64Attribute{Required: n.Required, Optional: n.Optional, Computed: n.Computed, Sensitive: n.Sensitive, MarkdownDescription: desc,
			PlanModifiers: planMods(n, float64planmodifier.UseStateForUnknown, float64planmodifier.RequiresReplace)}
	case fields.Object:
		return rschema.SingleNestedAttribute{Required: n.Required, Optional: n.Optional, Computed: n.Computed, Sensitive: n.Sensitive, MarkdownDescription: desc,
			Attributes:    resourceAttributes(n.Kids),
			PlanModifiers: planMods(n, objectplanmodifier.UseStateForUnknown, objectplanmodifier.RequiresReplace)}
	case fields.ObjectList:
		return rschema.ListNestedAttribute{Required: n.Required, Optional: n.Optional, Computed: n.Computed, Sensitive: n.Sensitive, MarkdownDescription: desc,
			NestedObject:  rschema.NestedAttributeObject{Attributes: resourceAttributes(n.Kids)},
			PlanModifiers: planMods(n, listplanmodifier.UseStateForUnknown, listplanmodifier.RequiresReplace)}
	case fields.ObjectMap:
		return rschema.MapNestedAttribute{Required: n.Required, Optional: n.Optional, Computed: n.Computed, Sensitive: n.Sensitive, MarkdownDescription: desc,
			NestedObject:  rschema.NestedAttributeObject{Attributes: resourceAttributes(n.Kids)},
			PlanModifiers: planMods(n, mapplanmodifier.UseStateForUnknown, mapplanmodifier.RequiresReplace)}
	case fields.StringMap:
		return rschema.MapAttribute{ElementType: scalarType(fields.String), Required: n.Required, Optional: n.Optional, Computed: n.Computed, Sensitive: n.Sensitive, MarkdownDescription: desc,
			PlanModifiers: planMods(n, mapplanmodifier.UseStateForUnknown, mapplanmodifier.RequiresReplace)}
	case fields.IntMap:
		return rschema.MapAttribute{ElementType: scalarType(fields.Int), Required: n.Required, Optional: n.Optional, Computed: n.Computed, Sensitive: n.Sensitive, MarkdownDescription: desc,
			PlanModifiers: planMods(n, mapplanmodifier.UseStateForUnknown, mapplanmodifier.RequiresReplace)}
	case fields.ScalarList:
		return rschema.ListAttribute{ElementType: scalarType(n.Elem), Required: n.Required, Optional: n.Optional, Computed: n.Computed, Sensitive: n.Sensitive, MarkdownDescription: desc,
			PlanModifiers: planMods(n, listplanmodifier.UseStateForUnknown, listplanmodifier.RequiresReplace)}
	case fields.ScalarSet:
		return rschema.SetAttribute{ElementType: scalarType(n.Elem), Required: n.Required, Optional: n.Optional, Computed: n.Computed, Sensitive: n.Sensitive, MarkdownDescription: desc,
			PlanModifiers: planMods(n, setplanmodifier.UseStateForUnknown, setplanmodifier.RequiresReplace)}
	default:
		return rschema.StringAttribute{Required: n.Required, Optional: n.Optional, Computed: n.Computed, Sensitive: n.Sensitive, MarkdownDescription: desc,
			PlanModifiers: planMods(n, stringplanmodifier.UseStateForUnknown, stringplanmodifier.RequiresReplace)}
	}
}

func resourceAttributes(kids []*node) map[string]rschema.Attribute {
	attrs := make(map[string]rschema.Attribute, len(kids))
	for _, k := range kids {
		attrs[k.AttrName()] = resourceAttribute(k)
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
	if !m.decl.Singleton {
		lookups["id"] = true
	}
	for _, l := range m.decl.Lookups {
		lookups[l.Field] = true
	}
	for _, n := range m.nodes {
		a := dataSourceAttribute(n)
		if lookups[n.Name] {
			a = dschema.StringAttribute{Optional: true, Computed: true, MarkdownDescription: describe(n)}
		}
		attrs[n.AttrName()] = a
	}
	if m.decl.Parent != "" {
		attrs[m.decl.Parent] = dschema.StringAttribute{Required: true, MarkdownDescription: "The ID of the parent object."}
	}
	return dschema.Schema{MarkdownDescription: m.decl.Description, Attributes: attrs}
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

func dataSourceAttributes(kids []*node) map[string]dschema.Attribute {
	attrs := make(map[string]dschema.Attribute, len(kids))
	for _, k := range kids {
		attrs[k.AttrName()] = dataSourceAttribute(k)
	}
	return attrs
}
