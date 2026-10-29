package engine

import (
	"context"
	"fmt"
	"net/url"
	"slices"
	"strings"
	"time"

	"github.com/hashicorp/terraform-plugin-framework-timeouts/resource/timeouts"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/hashicorp/terraform-plugin-framework/types/basetypes"

	"github.com/computomatic/terraform-provider-stripe/internal/stripeapi"
	"github.com/computomatic/terraform-provider-stripe/internal/wire"
)

const (
	// DefaultOperationTimeout bounds create, update and delete, retries included, unless
	// the resource's `timeouts` block says otherwise.
	DefaultOperationTimeout = 20 * time.Minute
	// ReadTimeout bounds a read, retries included. Reads have no `timeouts` entry.
	ReadTimeout = 5 * time.Minute
	// listLimit is the page size of a data source lookup.
	listLimit = 100
	// listLimitV2 is the page size of a `/v2` lookup: the largest the conformance suite
	// sends, because `/v2`'s maximum is unobserved.
	listLimitV2 = 20

	expirePollInterval = time.Second
)

// api sends the model's requests.
type api struct {
	m      *model
	client Client
	// typeAt gives the type of the attribute at a path, to attach errors to it.
	typeAt stripeapi.AttributeType
	// strict is the provider's strict mode (ProviderData.Strict).
	strict bool
}

func (a *api) call(ctx context.Context, method, target string, params wire.Object) (wire.Object, error) {
	req := stripeapi.Request{
		Method:    method,
		Path:      target,
		Params:    params,
		Sensitive: a.m.sensitiveNames,
	}
	resp, err := a.client.Do(ctx, req)
	if err != nil {
		return nil, err
	}
	if resp.Body == nil {
		return wire.Object{}, nil
	}
	obj, ok := resp.Body.(wire.Object)
	if !ok {
		return nil, fmt.Errorf("%s %s: Stripe returned %T, not an object", method, target, resp.Body)
	}
	return obj, nil
}

// expanded returns params with the declaration's `expand[]` entries added, and `discounts`
// where the resource maps them, or its `include` values for a `/v2` object. prefix is `data.` for a `/v1` list response; `include` has no
// prefix. A declaration that expands and includes nothing gets params back.
func (a *api) expanded(params wire.Object, prefix string) wire.Object {
	return a.withExpand(params, prefix, a.m.decl.Expand)
}

// retrieved is expanded for a retrieve, which also asks for the fields Stripe expands only
// there.
func (a *api) retrieved(params wire.Object) wire.Object {
	return a.withExpand(params, "", slices.Concat(a.m.decl.Expand, a.m.decl.ExpandOnRetrieve))
}

func (a *api) withExpand(params wire.Object, prefix string, expand []string) wire.Object {
	if a.m.discounts != nil {
		expand = append(slices.Clone(expand), "discounts")
	}
	if len(a.m.decl.Include) > 0 {
		values := make(wire.Array, len(a.m.decl.Include))
		for i, v := range a.m.decl.Include {
			values[i] = wire.String(v)
		}
		return append(append(wire.Object(nil), params...), wire.Field{Name: "include", Value: values})
	}
	if len(expand) == 0 {
		return params
	}
	paths := make(wire.Array, len(expand))
	for i, e := range expand {
		paths[i] = wire.String(prefix + e)
	}
	return append(append(wire.Object(nil), params...), wire.Field{Name: "expand", Value: paths})
}

func (a *api) fail(diags *diag.Diagnostics, err error) {
	stripeapi.AddToDiagnostics(diags, err, a.typeAt)
}

// expand fills a path template. A parent placeholder is only expanded when the
// declaration names a parent.
func (m *model) expand(template, id, parent string) string {
	out := strings.ReplaceAll(template, "{id}", url.PathEscape(id))
	if m.decl.Parent != "" {
		out = strings.ReplaceAll(out, "{"+m.decl.Parent+"}", url.PathEscape(parent))
	}
	return out
}

func stringAttr(obj basetypes.ObjectValue, name string) string {
	s, ok := obj.Attributes()[name].(basetypes.StringValue)
	if !ok || s.IsNull() || s.IsUnknown() {
		return ""
	}
	return s.ValueString()
}

// ids returns the object's own ID and its parent's ID from a plan, state or config.
func (m *model) ids(obj basetypes.ObjectValue) (id, parent string) {
	id = stringAttr(obj, "id")
	if m.decl.Parent != "" {
		parent = stringAttr(obj, m.decl.Parent)
	}
	return id, parent
}

// operationContext applies the resource's timeout for the operation to ctx.
func operationContext(ctx context.Context, obj basetypes.ObjectValue, operation string) (context.Context, context.CancelFunc, diag.Diagnostics) {
	limit := DefaultOperationTimeout
	var diags diag.Diagnostics
	if t, ok := obj.Attributes()["timeouts"].(timeouts.Value); ok {
		switch operation {
		case "create":
			limit, diags = t.Create(ctx, DefaultOperationTimeout)
		case "update":
			limit, diags = t.Update(ctx, DefaultOperationTimeout)
		case "delete":
			limit, diags = t.Delete(ctx, DefaultOperationTimeout)
		}
	}
	ctx, cancel := context.WithTimeout(ctx, limit)
	return ctx, cancel, diags
}

// setIdentity records the resource identity when Terraform supplies an identity slot.
func (m *model) setIdentity(ctx context.Context, identity *tfsdk.ResourceIdentity, id, parent string) diag.Diagnostics {
	if identity == nil {
		return nil
	}
	diags := identity.SetAttribute(ctx, path.Root("id"), id)
	if m.decl.Parent != "" {
		diags.Append(identity.SetAttribute(ctx, path.Root(m.decl.Parent), parent)...)
	}
	return diags
}
