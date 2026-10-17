package engine

import (
	"context"
	"fmt"
	"strconv"

	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-framework/types/basetypes"
	"github.com/hashicorp/terraform-plugin-go/tftypes"

	"github.com/computomatic/terraform-provider-stripe/internal/fields"
	"github.com/computomatic/terraform-provider-stripe/internal/wire"
)

// decoder turns a Stripe object into the attribute values of a resource or data source.
type decoder struct {
	// dataSource drops the carry-forward rules: every attribute comes from the response.
	dataSource bool
	diags      diag.Diagnostics
}

// decodeState maps the response onto the top-level attribute object. prior is the plan
// (after create and update), the state (read) or the config (data source). Attributes the
// table does not describe, such as `timeouts` and the parent ID, are carried from prior.
func (m *model) decodeState(body wire.Object, prior basetypes.ObjectValue, objType basetypes.ObjectType, dataSource bool) (basetypes.ObjectValue, diag.Diagnostics) {
	d := decoder{dataSource: dataSource}
	priorAttrs := prior.Attributes()
	byAttr := make(map[string]*node, len(m.nodes))
	for _, n := range m.nodes {
		byAttr[n.AttrName()] = n
	}
	attrs := make(map[string]attr.Value, len(objType.AttrTypes))
	for name, t := range objType.AttrTypes {
		if n, ok := byAttr[name]; ok {
			w, _ := body.Get(n.Name)
			attrs[name] = d.node(n, w, priorAttrs[name])
			continue
		}
		if p, ok := priorAttrs[name]; ok && !p.IsUnknown() {
			attrs[name] = p
		} else {
			attrs[name] = nullOf(t)
		}
	}
	if d.diags.HasError() {
		return basetypes.ObjectValue{}, d.diags
	}
	obj, diags := types.ObjectValue(objType.AttrTypes, attrs)
	d.diags.Append(diags...)
	return obj, d.diags
}

func nullOf(t attr.Type) attr.Value {
	v, err := t.ValueFromTerraform(context.Background(), tftypes.NewValue(t.TerraformType(context.Background()), nil))
	if err != nil {
		return nil
	}
	return v
}

func known(v attr.Value) bool { return v != nil && !v.IsNull() && !v.IsUnknown() }

// node decodes one field, applying the carry-forward rules of ADR D2: a field the response
// never carries, a once-returned field the response omits and a preserved field all keep
// the prior value.
func (d *decoder) node(n *node, w wire.Value, prior attr.Value) attr.Value {
	if !d.dataSource {
		switch {
		case n.Preserve && known(prior):
			return prior
		case !n.Response && !n.Once:
			if known(prior) {
				return prior
			}
			return nullOf(attrType(n))
		case n.Once && isAbsent(w):
			if known(prior) {
				return prior
			}
			return nullOf(attrType(n))
		}
	}
	v := d.value(n, w, prior)
	// An Optional field the user left out reads back as an empty collection from Stripe;
	// that is null in the configuration, not a difference.
	if !d.dataSource && !n.Computed && !known(prior) && v != nil && emptyCollection(v) {
		return nullOf(attrType(n))
	}
	return v
}

func isAbsent(w wire.Value) bool {
	if w == nil {
		return true
	}
	_, null := w.(wire.Null)
	return null
}

func emptyCollection(v attr.Value) bool {
	if v.IsNull() || v.IsUnknown() {
		return false
	}
	switch c := v.(type) {
	case basetypes.ListValue:
		return len(c.Elements()) == 0
	case basetypes.SetValue:
		return len(c.Elements()) == 0
	case basetypes.MapValue:
		return len(c.Elements()) == 0
	}
	return false
}

func (d *decoder) fail(n *node, problem string) attr.Value {
	d.diags.AddError("Unexpected Stripe response", fmt.Sprintf("field `%s`: %s", n.Path, problem))
	return nullOf(attrType(n))
}

func (d *decoder) value(n *node, w wire.Value, prior attr.Value) attr.Value {
	if isAbsent(w) {
		return nullOf(attrType(n))
	}
	switch n.Shape {
	case fields.Object:
		o, ok := w.(wire.Object)
		if !ok {
			return d.fail(n, fmt.Sprintf("expected an object, got %T", w))
		}
		return d.object(n.Kids, o, objectAttrs(prior))
	case fields.ObjectList:
		arr, ok := w.(wire.Array)
		if !ok {
			return d.fail(n, fmt.Sprintf("expected an array, got %T", w))
		}
		var priorElems []attr.Value
		if l, isList := prior.(basetypes.ListValue); isList && known(l) {
			priorElems = l.Elements()
		}
		elems := make([]attr.Value, len(arr))
		for i, item := range arr {
			o, ok := item.(wire.Object)
			if !ok {
				return d.fail(n, fmt.Sprintf("expected an array of objects, got %T", item))
			}
			var p attr.Value
			if i < len(priorElems) {
				p = priorElems[i]
			}
			elems[i] = d.object(n.Kids, o, objectAttrs(p))
		}
		return d.list(objectType(n.Kids), elems)
	case fields.ObjectMap:
		o, ok := w.(wire.Object)
		if !ok {
			return d.fail(n, fmt.Sprintf("expected an object, got %T", w))
		}
		var priorElems map[string]attr.Value
		if mv, isMap := prior.(basetypes.MapValue); isMap && known(mv) {
			priorElems = mv.Elements()
		}
		elems := make(map[string]attr.Value, len(o))
		for _, f := range o {
			inner, ok := f.Value.(wire.Object)
			if !ok {
				return d.fail(n, fmt.Sprintf("expected an object for key %q, got %T", f.Name, f.Value))
			}
			elems[f.Name] = d.object(n.Kids, inner, objectAttrs(priorElems[f.Name]))
		}
		return d.mapOf(objectType(n.Kids), elems)
	case fields.StringMap:
		o, ok := w.(wire.Object)
		if !ok {
			return d.fail(n, fmt.Sprintf("expected an object, got %T", w))
		}
		elems := make(map[string]attr.Value, len(o))
		for _, f := range o {
			s, ok := d.scalar(n, fields.String, f.Value).(basetypes.StringValue)
			if !ok || s.IsNull() {
				return nullOf(attrType(n))
			}
			elems[f.Name] = s
		}
		return d.mapOf(types.StringType, elems)
	case fields.ScalarList, fields.ScalarSet:
		arr, ok := w.(wire.Array)
		if !ok {
			return d.fail(n, fmt.Sprintf("expected an array, got %T", w))
		}
		elems := make([]attr.Value, len(arr))
		for i, item := range arr {
			elems[i] = d.scalar(n, n.Elem, item)
		}
		if n.Shape == fields.ScalarSet {
			v, diags := types.SetValue(scalarType(n.Elem), elems)
			d.diags.Append(diags...)
			return v
		}
		return d.list(scalarType(n.Elem), elems)
	default:
		return d.scalar(n, n.Shape, w)
	}
}

// objectAttrs returns the members of a prior object value, or nil.
func objectAttrs(v attr.Value) map[string]attr.Value {
	if o, ok := v.(basetypes.ObjectValue); ok && known(o) {
		return o.Attributes()
	}
	return nil
}

func (d *decoder) object(kids []*node, body wire.Object, prior map[string]attr.Value) attr.Value {
	attrs := make(map[string]attr.Value, len(kids))
	for _, k := range kids {
		w, _ := body.Get(k.Name)
		attrs[k.AttrName()] = d.node(k, w, prior[k.AttrName()])
	}
	v, diags := types.ObjectValue(objectType(kids).AttrTypes, attrs)
	d.diags.Append(diags...)
	if diags.HasError() {
		return nullOf(objectType(kids))
	}
	return v
}

func (d *decoder) list(elem attr.Type, elems []attr.Value) attr.Value {
	v, diags := types.ListValue(elem, elems)
	d.diags.Append(diags...)
	return v
}

func (d *decoder) mapOf(elem attr.Type, elems map[string]attr.Value) attr.Value {
	v, diags := types.MapValue(elem, elems)
	d.diags.Append(diags...)
	return v
}

// scalar converts one wire value. A string field also accepts a number (a field that
// takes either a word or a number, such as a tier's `up_to`) and an expanded object, whose
// `id` is the reference.
func (d *decoder) scalar(n *node, shape fields.Shape, w wire.Value) attr.Value {
	if isAbsent(w) {
		return nullOf(scalarType(shape))
	}
	switch shape {
	case fields.Bool:
		if b, ok := w.(wire.Bool); ok {
			return types.BoolValue(bool(b))
		}
	case fields.Int:
		if i, ok := w.(wire.Int); ok {
			return types.Int64Value(int64(i))
		}
	case fields.Float:
		switch t := w.(type) {
		case wire.Int:
			return types.Float64Value(float64(t))
		case wire.Decimal:
			if f, err := strconv.ParseFloat(string(t), 64); err == nil {
				return types.Float64Value(f)
			}
		}
	default:
		switch t := w.(type) {
		case wire.String:
			return types.StringValue(string(t))
		case wire.Int:
			return types.StringValue(strconv.FormatInt(int64(t), 10))
		case wire.Decimal:
			return types.StringValue(string(t))
		case wire.Object:
			if id, ok := t.GetString("id"); ok {
				return types.StringValue(id)
			}
		}
	}
	return d.fail(n, fmt.Sprintf("unexpected %T for a %s value", w, shapeLabel(shape)))
}

func shapeLabel(s fields.Shape) string {
	switch s {
	case fields.Bool:
		return "boolean"
	case fields.Int:
		return "integer"
	case fields.Float:
		return "number"
	default:
		return "string"
	}
}
