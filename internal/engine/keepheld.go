package engine

import (
	"context"

	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

// keepHeld is the framework's UseStateForUnknown, but for a member of a hash the state does not
// hold, whose state value is null only because its parent is: an update that adds the hash
// leaves such a member unknown for Stripe to fill, as a create does, where UseStateForUnknown
// would plan it null. A payment link's `name_collection.business.optional`, which the record
// shows no update changing unless it names it (E5), reads `false` once an update adds
// `name_collection.business` without it.
type keepHeld struct{}

var (
	_ planmodifier.Bool    = keepHeld{}
	_ planmodifier.Int64   = keepHeld{}
	_ planmodifier.Float64 = keepHeld{}
	_ planmodifier.String  = keepHeld{}
	_ planmodifier.Object  = keepHeld{}
	_ planmodifier.List    = keepHeld{}
	_ planmodifier.Map     = keepHeld{}
	_ planmodifier.Set     = keepHeld{}
)

func (keepHeld) Description(context.Context) string {
	return "Once set, the value of this attribute in state will not change."
}

func (k keepHeld) MarkdownDescription(ctx context.Context) string { return k.Description(ctx) }

// keeps reports whether the plan takes the state's value: on an update, where the plan leaves
// the value unknown, the configuration does not, and the state holds the value's parent.
func (keepHeld) keeps(ctx context.Context, state tfsdk.State, at path.Path, plan, config attr.Value) bool {
	if state.Raw.IsNull() || !plan.IsUnknown() || config.IsUnknown() {
		return false
	}
	parent := at.ParentPath()
	if len(parent.Steps()) == 0 {
		return true
	}
	var held types.Object
	if diags := state.GetAttribute(ctx, parent, &held); diags.HasError() {
		return true
	}
	return !held.IsNull()
}

func (k keepHeld) PlanModifyBool(ctx context.Context, req planmodifier.BoolRequest, resp *planmodifier.BoolResponse) {
	if k.keeps(ctx, req.State, req.Path, req.PlanValue, req.ConfigValue) {
		resp.PlanValue = req.StateValue
	}
}

func (k keepHeld) PlanModifyInt64(ctx context.Context, req planmodifier.Int64Request, resp *planmodifier.Int64Response) {
	if k.keeps(ctx, req.State, req.Path, req.PlanValue, req.ConfigValue) {
		resp.PlanValue = req.StateValue
	}
}

func (k keepHeld) PlanModifyFloat64(ctx context.Context, req planmodifier.Float64Request, resp *planmodifier.Float64Response) {
	if k.keeps(ctx, req.State, req.Path, req.PlanValue, req.ConfigValue) {
		resp.PlanValue = req.StateValue
	}
}

func (k keepHeld) PlanModifyString(ctx context.Context, req planmodifier.StringRequest, resp *planmodifier.StringResponse) {
	if k.keeps(ctx, req.State, req.Path, req.PlanValue, req.ConfigValue) {
		resp.PlanValue = req.StateValue
	}
}

func (k keepHeld) PlanModifyObject(ctx context.Context, req planmodifier.ObjectRequest, resp *planmodifier.ObjectResponse) {
	if k.keeps(ctx, req.State, req.Path, req.PlanValue, req.ConfigValue) {
		resp.PlanValue = req.StateValue
	}
}

func (k keepHeld) PlanModifyList(ctx context.Context, req planmodifier.ListRequest, resp *planmodifier.ListResponse) {
	if k.keeps(ctx, req.State, req.Path, req.PlanValue, req.ConfigValue) {
		resp.PlanValue = req.StateValue
	}
}

func (k keepHeld) PlanModifyMap(ctx context.Context, req planmodifier.MapRequest, resp *planmodifier.MapResponse) {
	if k.keeps(ctx, req.State, req.Path, req.PlanValue, req.ConfigValue) {
		resp.PlanValue = req.StateValue
	}
}

func (k keepHeld) PlanModifySet(ctx context.Context, req planmodifier.SetRequest, resp *planmodifier.SetResponse) {
	if k.keeps(ctx, req.State, req.Path, req.PlanValue, req.ConfigValue) {
		resp.PlanValue = req.StateValue
	}
}
