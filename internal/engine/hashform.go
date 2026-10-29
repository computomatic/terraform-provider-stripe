package engine

import (
	"context"
	"fmt"

	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

// oneForm refuses a configuration that sets both forms of a token-or-hash parameter (ADR
// 2026-10-29-full-attribute-coverage, D3): the token or ID, and `<name>_data`, the hash, which
// Stripe takes under one name.
type oneForm struct{ token, hash string }

var _ validator.Object = oneForm{}

func (v oneForm) Description(context.Context) string {
	return fmt.Sprintf("Set at most one of `%s` and `%s`.", v.token, v.hash)
}

func (v oneForm) MarkdownDescription(ctx context.Context) string { return v.Description(ctx) }

func (v oneForm) ValidateObject(ctx context.Context, req validator.ObjectRequest, resp *validator.ObjectResponse) {
	if req.ConfigValue.IsNull() || req.ConfigValue.IsUnknown() {
		return
	}
	var token types.String
	if diags := req.Config.GetAttribute(ctx, req.Path.ParentPath().AtName(v.token), &token); diags.HasError() {
		resp.Diagnostics.Append(diags...)
		return
	}
	if !token.IsNull() && !token.IsUnknown() {
		resp.Diagnostics.AddAttributeError(req.Path, "Two forms of one argument",
			fmt.Sprintf("Set `%s` or `%s`, not both: Stripe takes the token or the hash under one name.", v.token, v.hash))
	}
}

// requireOneForm refuses a configuration that sets neither form of a parameter the create
// request requires.
type requireOneForm struct{ token, hash string }

var _ validator.String = requireOneForm{}

func (v requireOneForm) Description(context.Context) string {
	return fmt.Sprintf("Set `%s` or `%s`: Stripe requires one.", v.token, v.hash)
}

func (v requireOneForm) MarkdownDescription(ctx context.Context) string { return v.Description(ctx) }

func (v requireOneForm) ValidateString(ctx context.Context, req validator.StringRequest, resp *validator.StringResponse) {
	if !req.ConfigValue.IsNull() {
		return
	}
	var hash types.Object
	if diags := req.Config.GetAttribute(ctx, req.Path.ParentPath().AtName(v.hash), &hash); diags.HasError() {
		resp.Diagnostics.Append(diags...)
		return
	}
	if hash.IsNull() {
		resp.Diagnostics.AddAttributeError(req.Path, "Missing argument", v.Description(ctx))
	}
}

// keepToken plans the token form of a token-or-hash parameter, which is Computed beside its
// hash form: an unset token keeps its state value while the hash form is unchanged, and is
// unknown when the hash form changes, since Stripe returns the token of what the hash makes.
type keepToken struct{ hash string }

var _ planmodifier.String = keepToken{}

func (k keepToken) Description(context.Context) string {
	return fmt.Sprintf("Keeps the value in state unless `%s` changes.", k.hash)
}

func (k keepToken) MarkdownDescription(ctx context.Context) string { return k.Description(ctx) }

func (k keepToken) PlanModifyString(ctx context.Context, req planmodifier.StringRequest, resp *planmodifier.StringResponse) {
	if !req.ConfigValue.IsNull() || req.StateValue.IsNull() || !req.PlanValue.IsUnknown() {
		return
	}
	at := req.Path.ParentPath().AtName(k.hash)
	var planned, prior types.Object
	if diags := req.Plan.GetAttribute(ctx, at, &planned); diags.HasError() {
		resp.Diagnostics.Append(diags...)
		return
	}
	if diags := req.State.GetAttribute(ctx, at, &prior); diags.HasError() {
		resp.Diagnostics.Append(diags...)
		return
	}
	if planned.Equal(prior) {
		resp.PlanValue = req.StateValue
	}
}
