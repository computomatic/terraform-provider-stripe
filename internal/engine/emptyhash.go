package engine

import (
	"context"
	"slices"

	"github.com/hashicorp/terraform-plugin-framework/schema/validator"

	"github.com/computomatic/terraform-provider-stripe/internal/fields"
)

// noEmptyHashDetail says why a /v1 hash with no members is refused (ADR
// 2026-10-29-full-attribute-coverage, D3; the owner's decision of 2026-10-29).
const noEmptyHashDetail = "Stripe's /v1 API reads an empty value as unsetting a field, and the conformance suite has not yet proven what it takes for a hash with no members, so the provider does not send one. Leave it out of the configuration."

// noMembers reports a request hash with no members the request sets, such as a payment method
// type under `payment_method_options`, which is configured as `{}`.
func (n *node) noMembers() bool {
	return n.Shape == fields.Object && n.Writable() && !slices.ContainsFunc(n.Kids, func(k *node) bool { return k.Writable() })
}

// noEmptyHashOnV1 refuses, at plan, a /v1 hash with no members that the configuration sets.
type noEmptyHashOnV1 struct{}

var _ validator.Object = noEmptyHashOnV1{}

func (noEmptyHashOnV1) Description(context.Context) string {
	return "Refuses the hash until the conformance suite proves what Stripe's /v1 API takes for it."
}

func (v noEmptyHashOnV1) MarkdownDescription(ctx context.Context) string { return v.Description(ctx) }

func (noEmptyHashOnV1) ValidateObject(_ context.Context, req validator.ObjectRequest, resp *validator.ObjectResponse) {
	if req.ConfigValue.IsNull() || req.ConfigValue.IsUnknown() {
		return
	}
	resp.Diagnostics.AddAttributeError(req.Path, "Unsupported argument", "`"+req.Path.String()+"`: "+noEmptyHashDetail)
}
