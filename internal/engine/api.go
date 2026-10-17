package engine

import (
	"context"
	"fmt"
	"net/url"
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

	expirePollInterval = time.Second
)

// api sends the model's requests.
type api struct {
	m      *model
	client Client
	// hasAttribute reports whether a path exists in the schema, to attach errors to it.
	hasAttribute func(path.Path) bool
}

func (a *api) call(ctx context.Context, method, target string, params wire.Object) (wire.Object, error) {
	req := stripeapi.Request{
		Method:    method,
		Path:      target,
		Params:    params,
		Sensitive: a.m.sensitiveNames,
	}
	if a.m.decl.V2 {
		req.Encoding = stripeapi.JSONEncoding
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

func (a *api) fail(diags *diag.Diagnostics, err error) {
	stripeapi.AddToDiagnostics(diags, err, a.hasAttribute)
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
