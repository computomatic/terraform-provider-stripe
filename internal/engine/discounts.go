package engine

import (
	"fmt"
	"maps"
	"slices"
	"strings"

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

// lineDiscountsNodes returns the `discounts` of the elements of each top-level list that take
// Stripe's request shape, hashes each naming a discount by one of discountForms, beside members
// only the response holds, such as a quote line's: Stripe returns each of a line's discounts
// whole beside the amount it takes off, where `<list>.data.discounts` is expanded (the record's
// `discounts`, `discount_object` at `line_items.data.discounts.discount`). The data source, whose
// table holds the response's shape, has none.
func lineDiscountsNodes(nodes []*node) []*node {
	var out []*node
	for _, list := range nodes {
		if list.Shape != fields.ObjectList || !list.Writable() || !list.Response {
			continue
		}
		n := kidNamed(list, "discounts")
		if n == nil || n.Shape != fields.ObjectList || !n.Writable() || !n.Response {
			continue
		}
		requestForm := false
		for _, k := range n.Kids {
			form := slices.Contains(discountForms, k.Name)
			if !form && k.Writable() {
				requestForm = false
				break
			}
			requestForm = requestForm || form && k.Writable() && !k.Response
		}
		if requestForm {
			out = append(out, n)
		}
	}
	return out
}

// heldDiscountIDs are the IDs of the discounts at an object's top-level `discounts`, each an
// ID or an expanded discount.
func heldDiscountIDs(body wire.Object) map[string]bool {
	out := map[string]bool{}
	w, _ := body.Get("discounts")
	arr, _ := w.(wire.Array)
	for _, item := range arr {
		switch t := item.(type) {
		case wire.String:
			out[string(t)] = true
		case wire.Object:
			if id, ok := t.GetString("id"); ok {
				out[id] = true
			}
		}
	}
	return out
}

// lineDiscountsExpand is the `expand[]` path that shows the discounts of a list's elements.
func lineDiscountsExpand(n *node) string {
	return strings.TrimSuffix(n.Path, "discounts") + "data.discounts"
}

// mapDiscounts reads the discounts Stripe returns, which the resource expands on every request,
// back onto the prior elements by key, never by position: a `coupon` matches a discount from
// that coupon with no promotion code, a `promotion_code` the discount with that promotion code,
// and a `discount` the discount with that ID. A prior element that no discount matches is
// dropped, and a discount that matches no element is added in the first form the element takes
// of `discount`, `promotion_code` and `coupon`, so both show as drift. held names the member of
// each returned element that holds the discount, beside the members only the response holds,
// which are read from it; "" where the element is the discount. A list element's discounts
// leave out those the object itself holds (ownDiscounts).
func (d *decoder) mapDiscounts(n *node, w wire.Value, prior attr.Value, held string) attr.Value {
	var arr wire.Array
	if !isAbsent(w) {
		a, ok := w.(wire.Array)
		if !ok {
			return d.wrongShape(n, "expected an array, got "+wireLabel(w), prior)
		}
		arr = a
	}
	var found, entries []wire.Object
	for _, item := range arr {
		o, ok := item.(wire.Object)
		var entry wire.Object
		if ok && held != "" {
			entry = o
			inner, _ := o.Get(held)
			o, ok = inner.(wire.Object)
		}
		if !ok {
			return d.wrongShape(n, fmt.Sprintf("expected expanded discounts, got an array holding %s", wireLabel(item)), prior)
		}
		// Stripe lists the object's own discount on each line too, which the line cannot name
		// (suite: TestQuoteAttributes, probe `isolated/line_discount_of_quote`), so it is not
		// one of the line's.
		if id, _ := o.GetString("id"); held != "" && d.ownDiscounts[id] {
			continue
		}
		found, entries = append(found, o), append(entries, entry)
	}
	var priorElems []attr.Value
	if l, ok := prior.(basetypes.ListValue); ok && known(l) {
		priorElems = l.Elements()
	}
	typ := objectType(n.Kids)
	// element is a discount's element: the prior one it matched, or a new one naming it, with
	// what the response alone holds read from its entry. A form the plan left unknown, which
	// Stripe fills on a line, is the form configured elsewhere in the element, so null.
	element := func(i int, p attr.Value) attr.Value {
		attrs := make(map[string]attr.Value, len(n.Kids))
		if p != nil {
			maps.Copy(attrs, objectAttrs(p))
		} else {
			for _, k := range n.Kids {
				attrs[k.AttrName()] = nullOf(attrType(k))
			}
			for _, form := range discountForms {
				if k := kidNamed(n, form); k != nil {
					if v := discountKey(found[i], form); v != "" {
						attrs[k.AttrName()] = types.StringValue(v)
						break
					}
				}
			}
		}
		for _, k := range n.Kids {
			switch {
			case entries[i] != nil && !k.Writable():
				w, _ := entries[i].Get(k.Name)
				attrs[k.AttrName()] = d.node(k, w, attrs[k.AttrName()])
			case attrs[k.AttrName()] == nil || attrs[k.AttrName()].IsUnknown():
				attrs[k.AttrName()] = nullOf(attrType(k))
			}
		}
		v, diags := types.ObjectValue(typ.AttrTypes, attrs)
		d.diags.Append(diags...)
		return v
	}
	claimed := make([]bool, len(found))
	elems := []attr.Value{}
	for _, p := range priorElems {
		for i, o := range found {
			if !claimed[i] && discountMatches(n, objectAttrs(p), o) {
				claimed[i] = true
				elems = append(elems, element(i, p))
				break
			}
		}
	}
	for i := range found {
		if !claimed[i] {
			elems = append(elems, element(i, nil))
		}
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
