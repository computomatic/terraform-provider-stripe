package engine

import (
	"context"
	"fmt"

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

// emptyObjectAt returns the attribute path of the first object of a /v1 body that would be
// sent empty, below kids, or "". Map values are not objects here.
func emptyObjectAt(kids []*node, obj wire.Object, prefix string) string {
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
					return at
				}
				if p := emptyObjectAt(k.Kids, o, at+"."); p != "" {
					return p
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
					return element
				}
				if p := emptyObjectAt(k.Kids, o, element+"."); p != "" {
					return p
				}
			}
		case fields.ObjectMap:
			entries, _ := f.Value.(wire.Object)
			for _, e := range entries {
				if o, ok := e.Value.(wire.Object); ok {
					if p := emptyObjectAt(k.Kids, o, fmt.Sprintf("%s[%q].", at, e.Name)); p != "" {
						return p
					}
				}
			}
		}
	}
	return ""
}

// refuseEmptyObjects fails a /v1 body that would send an object empty.
func (m *model) refuseEmptyObjects(kids []*node, body wire.Object) error {
	if m.v2 {
		return nil
	}
	if at := emptyObjectAt(kids, body, ""); at != "" {
		return fmt.Errorf("`%s`: %s", at, noEmptyObjectDetail)
	}
	return nil
}
