package engine

import (
	"fmt"
	"slices"

	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-framework/types/basetypes"

	"github.com/computomatic/terraform-provider-stripe/internal/fields"
	"github.com/computomatic/terraform-provider-stripe/internal/wire"
)

// discountForms are the members of a `discounts` element, each one way to name a discount,
// in the order an unmatched discount is written back.
var discountForms = []string{"discount", "promotion_code", "coupon"}

// discountsNode returns a resource's top-level `discounts` when it takes Stripe's request
// shape, a list of hashes each naming a discount by one of discountForms, while the response
// returns discount IDs (ADR 2026-10-29-full-attribute-coverage, D3, "Objects in, IDs out"),
// or nil. The data source reads the IDs.
func discountsNode(nodes []*node) *node {
	for _, n := range nodes {
		if n.Name != "discounts" || n.Shape != fields.ObjectList || n.DataSource == nil || !n.Writable() {
			continue
		}
		for _, k := range n.Kids {
			if !slices.Contains(discountForms, k.Name) {
				return nil
			}
		}
		return n
	}
	return nil
}

// mapDiscounts reads the discounts Stripe returns, which the resource expands on every request,
// back onto the prior elements by key, never by position: a `coupon` matches a discount from
// that coupon with no promotion code, a `promotion_code` the discount with that promotion code,
// and a `discount` the discount with that ID. A prior element that no discount matches is
// dropped, and a discount that matches no element is added in the first form the element takes
// of `discount`, `promotion_code` and `coupon`, so both show as drift.
func (d *decoder) mapDiscounts(n *node, w wire.Value, prior attr.Value) attr.Value {
	var arr wire.Array
	if !isAbsent(w) {
		a, ok := w.(wire.Array)
		if !ok {
			return d.wrongShape(n, "expected an array, got "+wireLabel(w), prior)
		}
		arr = a
	}
	found := make([]wire.Object, len(arr))
	for i, item := range arr {
		o, ok := item.(wire.Object)
		if !ok {
			return d.wrongShape(n, fmt.Sprintf("expected expanded discounts, got an array holding %s", wireLabel(item)), prior)
		}
		found[i] = o
	}
	var priorElems []attr.Value
	if l, ok := prior.(basetypes.ListValue); ok && known(l) {
		priorElems = l.Elements()
	}
	claimed := make([]bool, len(found))
	elems := []attr.Value{}
	for _, p := range priorElems {
		for i, o := range found {
			if !claimed[i] && discountMatches(n, objectAttrs(p), o) {
				claimed[i] = true
				elems = append(elems, p)
				break
			}
		}
	}
	typ := objectType(n.Kids)
	for i, o := range found {
		if claimed[i] {
			continue
		}
		attrs := make(map[string]attr.Value, len(n.Kids))
		for _, k := range n.Kids {
			attrs[k.AttrName()] = nullOf(attrType(k))
		}
		for _, form := range discountForms {
			if k := kidNamed(n, form); k != nil {
				if v := discountKey(o, form); v != "" {
					attrs[k.AttrName()] = types.StringValue(v)
					break
				}
			}
		}
		v, diags := types.ObjectValue(typ.AttrTypes, attrs)
		d.diags.Append(diags...)
		elems = append(elems, v)
	}
	if len(elems) == 0 && !known(prior) {
		return nullOf(attrType(n))
	}
	return d.list(typ, elems)
}

// discountMatches reports whether a configured element names the discount o.
func discountMatches(n *node, elem map[string]attr.Value, o wire.Object) bool {
	for _, form := range discountForms {
		k := kidNamed(n, form)
		if k == nil {
			continue
		}
		s, ok := elem[k.AttrName()].(basetypes.StringValue)
		if !ok || s.IsNull() || s.IsUnknown() {
			continue
		}
		if form == "coupon" && discountKey(o, "promotion_code") != "" {
			return false
		}
		return discountKey(o, form) == s.ValueString()
	}
	return false
}

// discountKey is the ID by which a form names the discount o: its own ID, its promotion code's,
// or the coupon's it was redeemed from (`source.coupon`), each a string or an expanded object.
func discountKey(o wire.Object, form string) string {
	switch form {
	case "discount":
		id, _ := o.GetString("id")
		return id
	case "coupon":
		src, _ := o.Get("source")
		so, ok := src.(wire.Object)
		if !ok {
			return ""
		}
		return refID(so, "coupon")
	}
	return refID(o, form)
}

// refID is the ID at a field that holds an ID or an expanded object, or "".
func refID(o wire.Object, name string) string {
	switch v, _ := o.Get(name); t := v.(type) {
	case wire.String:
		return string(t)
	case wire.Object:
		id, _ := t.GetString("id")
		return id
	}
	return ""
}
