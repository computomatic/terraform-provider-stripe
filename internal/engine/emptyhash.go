package engine

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/types/basetypes"

	"github.com/hashicorp/terraform-plugin-framework/schema/validator"

	"github.com/computomatic/terraform-provider-stripe/internal/fields"
	"github.com/computomatic/terraform-provider-stripe/internal/wire"
)

// noEmptyObjectDetail says why a /v1 object with nothing set is refused (ADR
// 2026-10-29-full-attribute-coverage, D3; the product owner's decisions of 2026-10-29).
const noEmptyObjectDetail = "Stripe's /v1 API reads an empty value as unsetting a field, and the conformance suite has not yet proven what it takes for an object sent with nothing set, so the provider does not send one. Set one of its arguments, or leave it out of the configuration."

// noEmptyObjectOnV1 refuses, at plan, a /v1 object whose configuration would encode to
// nothing: `{}`, or every argument it holds null. Members names the arguments.
type noEmptyObjectOnV1 struct{ members []string }

var _ validator.Object = noEmptyObjectOnV1{}

// emptyObjectValidator returns the refusal for an object attribute of a /v1 request, built
// from the node's members that a request takes.
func emptyObjectValidator(kids []*node, all bool) noEmptyObjectOnV1 {
	var members []string
	for _, k := range kids {
		if all || k.Writable() {
			members = append(members, k.AttrName())
		}
	}
	return noEmptyObjectOnV1{members: members}
}

func (noEmptyObjectOnV1) Description(context.Context) string {
	return "Refuses an object with nothing set until the conformance suite proves what Stripe's /v1 API takes for one."
}

func (v noEmptyObjectOnV1) MarkdownDescription(ctx context.Context) string { return v.Description(ctx) }

func (v noEmptyObjectOnV1) ValidateObject(_ context.Context, req validator.ObjectRequest, resp *validator.ObjectResponse) {
	if req.ConfigValue.IsNull() || req.ConfigValue.IsUnknown() {
		return
	}
	attrs := req.ConfigValue.Attributes()
	for _, name := range v.members {
		if a := attrs[name]; a != nil && (a.IsUnknown() || !a.IsNull()) {
			return
		}
	}
	resp.Diagnostics.AddAttributeError(req.Path, "Empty object", "`"+req.Path.String()+"`: "+noEmptyObjectDetail)
}

// operation is what a request sends of an object: a create its create arguments, an update its
// update arguments, a lookup all of them.
type operation struct {
	name  string
	sends func(*node) bool
}

var (
	opCreate = operation{"a create", func(k *node) bool { return k.Create }}
	opUpdate = operation{"an update", func(k *node) bool { return k.Update }}
	opLookup = operation{"a lookup", func(*node) bool { return true }}
)

// emptyObjectDetail explains why op would send the object n at at empty: which of its arguments
// op sends, and which it never sends.
func emptyObjectDetail(at string, n *node, op operation) string {
	var sent, onCreate, onUpdate []string
	defaults := false
	for _, k := range n.Kids {
		switch {
		case op.sends(k):
			sent = append(sent, "`"+k.AttrName()+"`")
			defaults = defaults || k.Computed && k.Shape == fields.Object
		case k.Create && !k.Update:
			onCreate = append(onCreate, "`"+k.AttrName()+"`")
		case k.Update && !k.Create:
			onUpdate = append(onUpdate, "`"+k.AttrName()+"`")
		}
	}
	var b strings.Builder
	switch {
	case len(sent) == 0:
		fmt.Fprintf(&b, "`%s`: %s sends none of its arguments", at, op.name)
	case len(onCreate)+len(onUpdate) > 0:
		fmt.Fprintf(&b, "`%s`: %s sends only %s of it, %s", at, op.name, words(sent), unset(len(sent)))
	default:
		fmt.Fprintf(&b, "`%s`: %s sends %s of it, %s", at, op.name, words(sent), unset(len(sent)))
	}
	if op.name == opUpdate.name && defaults {
		b.WriteString(", or leaves out because Stripe filled it in and the update does not change it")
	}
	for _, other := range []struct {
		names []string
		on    string
	}{{onCreate, "a create"}, {onUpdate, "an update"}} {
		switch len(other.names) {
		case 0:
		case 1:
			fmt.Fprintf(&b, "; %s is sent only on %s", other.names[0], other.on)
		default:
			fmt.Fprintf(&b, "; %s are sent only on %s", words(other.names), other.on)
		}
	}
	return b.String() + ". " + noEmptyObjectDetail
}

func unset(n int) string {
	if n == 1 {
		return "which is not set"
	}
	return "none of which is set"
}

// words joins names as a sentence does: `a`, `b` and `c`.
func words(names []string) string {
	if len(names) == 1 {
		return names[0]
	}
	return strings.Join(names[:len(names)-1], ", ") + " and " + names[len(names)-1]
}

// emptyObjectAt returns the attribute path, and the node, of the first object of a /v1 body
// that would be sent empty, below kids, or "". Map values are not objects here.
func emptyObjectAt(kids []*node, obj wire.Object, prefix string) (string, *node) {
	for _, f := range obj {
		k := findNodeFor(kids, f)
		if k == nil {
			continue
		}
		at := prefix + k.AttrName()
		switch k.Shape {
		case fields.Object:
			if o, ok := f.Value.(wire.Object); ok {
				if len(o) == 0 {
					return at, k
				}
				if p, n := emptyObjectAt(k.Kids, o, at+"."); p != "" {
					return p, n
				}
			}
		case fields.ObjectList:
			arr, _ := f.Value.(wire.Array)
			for i, e := range arr {
				o, ok := e.(wire.Object)
				if !ok {
					continue
				}
				element := fmt.Sprintf("%s[%d]", at, i)
				if len(o) == 0 {
					return element, k
				}
				if p, n := emptyObjectAt(k.Kids, o, element+"."); p != "" {
					return p, n
				}
			}
		case fields.ObjectMap:
			entries, _ := f.Value.(wire.Object)
			for _, e := range entries {
				if o, ok := e.Value.(wire.Object); ok {
					if p, n := emptyObjectAt(k.Kids, o, fmt.Sprintf("%s[%q].", at, e.Name)); p != "" {
						return p, n
					}
				}
			}
		}
	}
	return "", nil
}

// refuseEmptyObjects fails a /v1 body of op that would send an object empty.
func (m *model) refuseEmptyObjects(kids []*node, body wire.Object, op operation) error {
	if m.v2 {
		return nil
	}
	if at, n := emptyObjectAt(kids, body, ""); at != "" {
		return errors.New(emptyObjectDetail(at, n, op))
	}
	return nil
}

// emptyOnCreate adds an error, at plan, for each object of a /v1 create whose configured
// arguments a create does not send, so that it would be sent empty: the validator counts an
// argument either request takes, and only a create's plan says which request it is. An
// unknown argument counts as set.
func (m *model) emptyOnCreate(kids []*node, attrs map[string]attr.Value, at path.Path, root bool, diags *diag.Diagnostics) {
	for _, k := range kids {
		v := attrs[k.AttrName()]
		if !k.Create || v == nil || v.IsNull() || v.IsUnknown() {
			continue
		}
		p := at.AtName(k.AttrName())
		if root {
			p = path.Root(k.AttrName())
		}
		check := func(p path.Path, o basetypes.ObjectValue) {
			if o.IsNull() || o.IsUnknown() {
				return
			}
			sent := false
			for _, kid := range k.Kids {
				if a := o.Attributes()[kid.AttrName()]; kid.Create && a != nil && !a.IsNull() {
					sent = true
				}
			}
			if !sent {
				diags.AddAttributeError(p, "Empty object", emptyObjectDetail(p.String(), k, opCreate))
				return
			}
			m.emptyOnCreate(k.Kids, o.Attributes(), p, false, diags)
		}
		switch c := v.(type) {
		case basetypes.ObjectValue:
			check(p, c)
		case basetypes.ListValue:
			for i, e := range c.Elements() {
				if o, ok := e.(basetypes.ObjectValue); ok {
					check(p.AtListIndex(i), o)
				}
			}
		case basetypes.MapValue:
			for _, key := range sortedKeys(c.Elements()) {
				if o, ok := c.Elements()[key].(basetypes.ObjectValue); ok && !o.IsNull() && !o.IsUnknown() {
					m.emptyOnCreate(k.Kids, o.Attributes(), p.AtMapKey(key), false, diags)
				}
			}
		}
	}
}
