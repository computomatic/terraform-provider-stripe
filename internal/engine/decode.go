package engine

import (
	"context"
	"fmt"
	"slices"
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
	// matchBy maps a keyed list's path to the field that identifies its elements.
	matchBy map[string]string
	// matchData maps a keyed list's path to the field that stands in for the key.
	matchData map[string]string
	// keep maps a keyed list's path to the elements the resource manages. A list with no
	// entry keeps every element Stripe returns.
	keep map[string]*managedSet
	// fixed lists the keyed lists whose elements must all be matched by key.
	fixed []string
	diags diag.Diagnostics
}

// decodeState maps the response onto the top-level attribute object. prior is the plan
// (after create and update), the state (read) or the config (data source). Attributes the
// table does not describe, such as `timeouts` and the parent ID, are carried from prior.
//
// keep limits each keyed list to the elements the resource manages (see managedKeys); nil
// keeps all of them.
func (m *model) decodeState(body wire.Object, prior basetypes.ObjectValue, objType basetypes.ObjectType, dataSource bool, keep map[string]*managedSet) (basetypes.ObjectValue, diag.Diagnostics) {
	d := decoder{dataSource: dataSource, matchBy: m.decl.MatchBy, matchData: m.decl.MatchData, keep: keep, fixed: m.decl.FixedKeys}
	priorAttrs := prior.Attributes()
	byAttr := make(map[string]*node, len(m.nodes))
	for _, n := range m.nodes {
		byAttr[n.AttrName()] = n
	}
	attrs := make(map[string]attr.Value, len(objType.AttrTypes))
	for name, t := range objType.AttrTypes {
		// A find parameter keeps the configured value the object was read by.
		if n, ok := byAttr[name]; ok && !(dataSource && m.findParam(n.Name)) {
			w, _ := body.Get(n.Name)
			if n.Name == m.decl.CurrencyOptions {
				w = withoutOwnCurrency(w, body)
			}
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
// never carries, a once-returned field the response omits, a kept field the response omits
// or returns null and a preserved field all keep the prior value. A data source has no prior value, so a
// field the response never carries is null; Stripe may still send a key of that name in
// another shape, such as an account's `capabilities` (statuses in the response, `requested`
// flags in the request).
func (d *decoder) node(n *node, w wire.Value, prior attr.Value) attr.Value {
	if d.dataSource && !n.Response && !n.Once {
		return nullOf(attrType(n))
	}
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
		case n.KeepOmitted && isAbsent(w) && known(prior) && fullyKnown(prior):
			return prior
		}
	}
	if n.NullAs != "" && isAbsent(w) {
		return types.StringValue(n.NullAs)
	}
	// Stripe may leave out a collection the user configured empty (`/v2` create sends none),
	// or return null for it. Absent and null read the same, and an empty collection in the
	// plan is what the user asked for, so it stays.
	if !d.dataSource && isAbsent(w) && emptyCollection(prior) {
		return prior
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
	if v == nil || v.IsNull() || v.IsUnknown() {
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
		arr, problem := listItems(w)
		if problem != "" {
			return d.fail(n, problem)
		}
		var priorElems []attr.Value
		if l, isList := prior.(basetypes.ListValue); isList && known(l) {
			priorElems = l.Elements()
		}
		items := make([]wire.Object, len(arr))
		for i, item := range arr {
			o, ok := item.(wire.Object)
			if !ok {
				return d.fail(n, fmt.Sprintf("expected an array of objects, got %T", item))
			}
			items[i] = o
		}
		pairs, problem := d.pair(n, items, priorElems)
		if problem != "" {
			return d.fail(n, problem)
		}
		elems := make([]attr.Value, len(pairs))
		for i, p := range pairs {
			elems[i] = d.object(n.Kids, p.item, objectAttrs(p.prior))
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
	case fields.IntMap:
		o, ok := w.(wire.Object)
		if !ok {
			return d.fail(n, fmt.Sprintf("expected an object, got %T", w))
		}
		elems := make(map[string]attr.Value, len(o))
		for _, f := range o {
			i, ok := d.scalar(n, fields.Int, f.Value).(basetypes.Int64Value)
			if !ok || i.IsNull() {
				return nullOf(attrType(n))
			}
			elems[f.Name] = i
		}
		return d.mapOf(types.Int64Type, elems)
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

// listItems returns the elements of an array, or of a list envelope, `{object: "list",
// data: [...]}`, which is how Stripe embeds a subscription's `items` or a quote's
// `line_items`. problem says why the value cannot be read as a list; an envelope with
// `has_more` is one, because the engine reads no further pages and would drop elements.
func listItems(w wire.Value) (arr wire.Array, problem string) {
	if o, isObj := w.(wire.Object); isObj {
		if kind, _ := o.GetString("object"); kind == "list" {
			if more, _ := o.GetBool("has_more"); more {
				return nil, "the embedded list has more elements than the response carries (`has_more` is true); the provider reads the first page only"
			}
			if data, has := o.Get("data"); has {
				w = data
			}
		}
	}
	arr, ok := w.(wire.Array)
	if !ok {
		return nil, fmt.Sprintf("expected an array, got %T", w)
	}
	return arr, ""
}

// pair is a response element and the prior element it continues.
type pair struct {
	item  wire.Object
	prior attr.Value
}

// managedSet is the elements of a keyed list that the resource manages, by key and by item
// ID: an element stays managed whatever its key becomes, so a price changed from outside
// shows as drift on the same element.
type managedSet struct {
	keys, ids map[string]bool
	// hint names, for each element of the prior list, the ID of the element it continues,
	// or "" for a new one. An update knows this from the request it built.
	hint []string
}

// pair matches the elements of a list to the prior ones. A plain list matches by position.
// A keyed list matches each prior element to the response element the update said it
// continues, else the one with its key, else the one with its ID; so a reordered, inserted or
// removed element never takes another's values, and the list follows the prior order. A
// prior element with no key yet (an unknown price, from `price_data`) takes the next element
// nothing else claimed. Elements the resource does not manage are dropped.
func (d *decoder) pair(n *node, items []wire.Object, prior []attr.Value) (pairs []pair, problem string) {
	key, keyed := d.matchBy[n.Path]
	if !keyed {
		out := make([]pair, len(items))
		for i, item := range items {
			out[i].item = item
			if i < len(prior) {
				out[i].prior = prior[i]
			}
		}
		return out, ""
	}
	managed := d.keep[n.Path]
	if managed != nil {
		var kept []wire.Object
		for _, item := range items {
			k, hasKey := wireKey(item, key)
			id, hasID := wireKey(item, "id")
			if hasKey && managed.keys[k] || hasID && managed.ids[id] {
				kept = append(kept, item)
			}
		}
		items = kept
	}
	used := make([]bool, len(items))
	claim := func(match func(wire.Object) bool) int {
		for j, item := range items {
			if !used[j] && match(item) {
				used[j] = true
				return j
			}
		}
		return -1
	}
	matched := make([]int, len(prior))
	for i := range prior {
		matched[i] = -1
		if managed != nil && i < len(managed.hint) && managed.hint[i] != "" {
			matched[i] = claim(func(o wire.Object) bool { id, _ := wireKey(o, "id"); return id == managed.hint[i] })
		}
	}
	for i, p := range prior {
		if matched[i] >= 0 {
			continue
		}
		if pk, ok := attrKey(p, key); ok {
			matched[i] = claim(func(o wire.Object) bool { k, has := wireKey(o, key); return has && k == pk })
		}
	}
	for i, p := range prior {
		if matched[i] >= 0 {
			continue
		}
		if pid, ok := attrKey(p, "id"); ok {
			matched[i] = claim(func(o wire.Object) bool { id, has := wireKey(o, "id"); return has && id == pid })
		}
	}
	// A prior element with no key yet (an unknown price, from `price_data`) is matched to the
	// element Stripe built from its data. All of them are matched, exactly first and then
	// loosely with the best fit, before any takes an element nothing else claimed.
	keyless := func(i int) (attr.Value, bool) {
		if matched[i] >= 0 {
			return nil, false
		}
		if _, ok := attrKey(prior[i], key); ok {
			return nil, false
		}
		field := d.matchData[n.Path]
		o, isObj := prior[i].(basetypes.ObjectValue)
		if field == "" || !isObj || o.IsNull() || o.IsUnknown() {
			return nil, false
		}
		data := o.Attributes()[field]
		return data, data != nil && !data.IsNull() && !data.IsUnknown()
	}
	for i := range prior {
		if data, ok := keyless(i); ok {
			matched[i] = claim(func(item wire.Object) bool {
				price, _ := item.Get(key)
				m := compareData(data, price)
				return m.ok && m.exact
			})
		}
	}
	for i := range prior {
		if data, ok := keyless(i); ok {
			best, score := -1, -1
			for j, item := range items {
				price, _ := item.Get(key)
				if m := compareData(data, price); !used[j] && m.ok && m.score > score {
					best, score = j, m.score
				}
			}
			if best >= 0 {
				used[best] = true
				matched[i] = best
			}
		}
	}
	for i, p := range prior {
		if _, ok := attrKey(p, key); !ok && matched[i] < 0 {
			// A guess would hand one element another's id and computed values.
			if slices.Contains(d.fixed, n.Path) {
				return nil, fmt.Sprintf("element %d of the prior list has no known %s to match it by, and elements of this list are matched by %s only", i, key, key)
			}
			matched[i] = claim(func(wire.Object) bool { return true })
		}
	}
	var out []pair
	for i, p := range prior {
		if matched[i] >= 0 {
			out = append(out, pair{items[matched[i]], p})
		}
	}
	for j, item := range items {
		if !used[j] {
			out = append(out, pair{item: item})
		}
	}
	return out, ""
}

// wireKey reads an element's key: a string, or the `id` of an expanded object.
func wireKey(o wire.Object, key string) (string, bool) {
	v, ok := o.Get(key)
	if !ok {
		return "", false
	}
	switch t := v.(type) {
	case wire.String:
		return string(t), true
	case wire.Object:
		return t.GetString("id")
	}
	return "", false
}

// attrKey reads a prior element's key when it is a known string.
func attrKey(p attr.Value, key string) (string, bool) {
	o, ok := p.(basetypes.ObjectValue)
	if !ok || o.IsNull() || o.IsUnknown() {
		return "", false
	}
	s, ok := o.Attributes()[key].(basetypes.StringValue)
	if !ok || s.IsNull() || s.IsUnknown() {
		return "", false
	}
	return s.ValueString(), true
}

// managedKeys returns, for each keyed list, the keys and item IDs of the elements the objects
// hold. The resource manages those elements and no others: an element added to the list from
// outside, such as a subscription item that stripe_subscription_item created, never enters
// the state and so is never changed or deleted by an update. A list that none of the objects
// holds has no entry, which keeps every element (an import adopts what Stripe returns).
func (m *model) managedKeys(objs ...basetypes.ObjectValue) map[string]*managedSet {
	var out map[string]*managedSet
	for list, key := range m.decl.MatchBy {
		n, ok := m.top(list)
		if !ok || slices.Contains(m.decl.FixedKeys, list) {
			continue
		}
		for _, o := range objs {
			l, isList := o.Attributes()[n.AttrName()].(basetypes.ListValue)
			if !isList || !known(l) {
				continue
			}
			if out == nil {
				out = map[string]*managedSet{}
			}
			if out[list] == nil {
				out[list] = &managedSet{keys: map[string]bool{}, ids: map[string]bool{}}
			}
			for _, e := range l.Elements() {
				if k, ok := attrKey(e, key); ok {
					out[list].keys[k] = true
				}
				if id, ok := attrKey(e, "id"); ok {
					out[list].ids[id] = true
				}
			}
		}
	}
	return out
}

// dataMatch is how well an element's data describes the object Stripe built from it.
type dataMatch struct {
	// ok is false when a value the data sets differs from the object's.
	ok bool
	// exact is true when, besides, every field the data leaves unset is unset on the object
	// too, so no more specific price could be mistaken for it.
	exact bool
	// score counts the values compared.
	score int
}

// dataMatches reports whether an element's data (a `price_data` object) describes the object
// Stripe built from it (the price): every value the data sets equals the same-named value of
// the object. A value the object does not carry cannot be compared and is skipped.
func dataMatches(data attr.Value, built wire.Value) bool { return compareData(data, built).ok }

func compareData(data attr.Value, built wire.Value) dataMatch {
	d, isObj := data.(basetypes.ObjectValue)
	if !isObj {
		return dataMatch{ok: scalarMatches(data, built), exact: true, score: 1}
	}
	o, ok := built.(wire.Object)
	if !ok {
		return dataMatch{}
	}
	out := dataMatch{ok: true, exact: true}
	for name, a := range d.Attributes() {
		v, has := o.Get(name)
		if a.IsNull() || a.IsUnknown() {
			// Stripe reports an unset tax behaviour as `unspecified`.
			if has && !isUnset(v) {
				out.exact = false
			}
			continue
		}
		if !has {
			continue
		}
		m := compareData(a, v)
		out.ok = out.ok && m.ok
		out.exact = out.exact && m.exact
		out.score += m.score
	}
	return out
}

// isUnset reports a value Stripe uses for "not set".
func isUnset(v wire.Value) bool {
	switch t := v.(type) {
	case wire.Null:
		return true
	case wire.String:
		return t == "unspecified"
	}
	return false
}

func scalarMatches(data attr.Value, built wire.Value) bool {
	switch d := data.(type) {
	case basetypes.StringValue:
		switch t := built.(type) {
		case wire.String:
			return string(t) == d.ValueString() || sameNumber(string(t), d.ValueString())
		case wire.Decimal:
			return string(t) == d.ValueString() || sameNumber(string(t), d.ValueString())
		case wire.Int:
			return sameNumber(strconv.FormatInt(int64(t), 10), d.ValueString())
		case wire.Object:
			id, _ := t.GetString("id")
			return id == d.ValueString()
		}
	case basetypes.Int64Value:
		switch t := built.(type) {
		case wire.Int:
			return int64(t) == d.ValueInt64()
		case wire.Decimal:
			return sameNumber(string(t), strconv.FormatInt(d.ValueInt64(), 10))
		}
	case basetypes.BoolValue:
		b, ok := built.(wire.Bool)
		return ok && bool(b) == d.ValueBool()
	case basetypes.Float64Value:
		switch t := built.(type) {
		case wire.Int:
			return float64(t) == d.ValueFloat64()
		case wire.Decimal:
			return sameNumber(string(t), strconv.FormatFloat(d.ValueFloat64(), 'f', -1, 64))
		}
	}
	return false
}

// sameNumber reports whether two strings are the same number, such as `1500` and `1500.00`.
func sameNumber(a, b string) bool {
	x, errA := strconv.ParseFloat(a, 64)
	y, errB := strconv.ParseFloat(b, 64)
	return errA == nil && errB == nil && x == y
}
