package provider

import (
	"context"
	"fmt"

	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-framework/types/basetypes"
	"github.com/hashicorp/terraform-plugin-log/tflog"
	"github.com/stripe/stripe-go/v82"
	"github.com/stripe/stripe-go/v82/client"
)

var (
	_ resource.Resource                = &priceResource{}
	_ resource.ResourceWithConfigure   = &priceResource{}
	_ resource.ResourceWithImportState = &priceResource{}
)

func NewPriceResource() resource.Resource {
	return &priceResource{}
}

type priceResource struct {
	client *client.API
}

// priceResourceModel maps the resource schema data.
type priceResourceModel struct {
	ID                types.String      `tfsdk:"id"`
	Active            types.Bool        `tfsdk:"active"`
	Currency          types.String      `tfsdk:"currency"`
	Interval          types.String      `tfsdk:"interval"`
	IntervalCount     types.Int64       `tfsdk:"interval_count"`
	LookupKey         types.String      `tfsdk:"lookup_key"`
	Metadata          types.Map         `tfsdk:"metadata"`
	Nickname          types.String      `tfsdk:"nickname"`
	Product           types.String      `tfsdk:"product"`
	Recurring         types.Object      `tfsdk:"recurring"`
	TiersMode         types.String      `tfsdk:"tiers_mode"`
	TransformUsage    types.Object      `tfsdk:"transform_usage"`
	Type              types.String      `tfsdk:"type"`
	UnitAmount        types.Int64       `tfsdk:"unit_amount"`
	UnitAmountDecimal types.String      `tfsdk:"unit_amount_decimal"`
	Created           types.Int64       `tfsdk:"created"`
}

// recurringModel maps recurring price data.
type recurringModel struct {
	Interval     types.String `tfsdk:"interval"`
	IntervalCount types.Int64  `tfsdk:"interval_count"`
	UsageType    types.String `tfsdk:"usage_type"`
}

// transformUsageModel maps transform usage data.
type transformUsageModel struct {
	DivideBy types.Int64  `tfsdk:"divide_by"`
	Round    types.String `tfsdk:"round"`
}

func (r *priceResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_price"
}

func (r *priceResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: "Manages a Stripe Price.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				Description: "Unique identifier for the price.",
				Computed:    true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"active": schema.BoolAttribute{
				Description: "Whether the price can be used for new purchases.",
				Optional:    true,
				Computed:    true,
			},
			"currency": schema.StringAttribute{
				Description: "Three-letter ISO currency code.",
				Required:    true,
			},
			"interval": schema.StringAttribute{
				Description: "The recurring interval.",
				Optional:    true,
			},
			"interval_count": schema.Int64Attribute{
				Description: "The number of intervals between recurring billings.",
				Optional:    true,
			},
			"lookup_key": schema.StringAttribute{
				Description: "A lookup key used to retrieve prices.",
				Optional:    true,
			},
			"metadata": schema.MapAttribute{
				Description: "Set of key-value pairs that you can attach to a price.",
				ElementType: types.StringType,
				Optional:    true,
			},
			"nickname": schema.StringAttribute{
				Description: "A brief description of the price.",
				Optional:    true,
			},
			"product": schema.StringAttribute{
				Description: "The ID of the product this price is associated with.",
				Required:    true,
			},
			"recurring": schema.SingleNestedAttribute{
				Description: "The recurring components of a price.",
				Optional:    true,
				Attributes: map[string]schema.Attribute{
					"interval": schema.StringAttribute{
						Description: "The recurring interval.",
						Optional:    true,
					},
					"interval_count": schema.Int64Attribute{
						Description: "The number of intervals between recurring billings.",
						Optional:    true,
					},
					"usage_type": schema.StringAttribute{
						Description: "Configures how the quantity per period should be determined.",
						Optional:    true,
					},
				},
			},
			"tiers_mode": schema.StringAttribute{
				Description: "Defines if the tiering price should be `graduated` or `volume` based.",
				Optional:    true,
			},
			"transform_usage": schema.SingleNestedAttribute{
				Description: "Apply a transformation to the reported usage or set quantity before computing the amount billed.",
				Optional:    true,
				Attributes: map[string]schema.Attribute{
					"divide_by": schema.Int64Attribute{
						Description: "Divide usage by this number.",
						Optional:    true,
					},
					"round": schema.StringAttribute{
						Description: "After division, either round the result `up` or `down`.",
						Optional:    true,
					},
				},
			},
			"type": schema.StringAttribute{
				Description: "One of `one_time` or `recurring` depending on whether the price is for a one-time purchase or a recurring (subscription) purchase.",
				Computed:    true,
			},
			"unit_amount": schema.Int64Attribute{
				Description: "The unit amount in cents to be charged.",
				Optional:    true,
			},
			"unit_amount_decimal": schema.StringAttribute{
				Description: "The unit amount in decimal form.",
				Computed:    true,
			},
			"created": schema.Int64Attribute{
				Description: "Time at which the price was created.",
				Computed:    true,
			},
		},
	}
}

func (r *priceResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	if req.ProviderData == nil {
		return
	}

	client, ok := req.ProviderData.(*client.API)
	if !ok {
		resp.Diagnostics.AddError(
			"Unexpected Resource Configure Type",
			fmt.Sprintf("Expected *client.API, got: %T. Please report this issue to the provider developers.", req.ProviderData),
		)
		return
	}

	r.client = client
}

func (r *priceResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	// Retrieve values from plan
	var plan priceResourceModel
	diags := req.Plan.Get(ctx, &plan)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	tflog.Debug(ctx, "Creating Stripe price", map[string]any{"product": plan.Product.ValueString()})

	// Generate API request parameters from plan
	params := &stripe.PriceParams{
		Currency: stripe.String(plan.Currency.ValueString()),
		Product:  stripe.String(plan.Product.ValueString()),
	}

	if !plan.Active.IsNull() {
		params.Active = stripe.Bool(plan.Active.ValueBool())
	}

	if !plan.LookupKey.IsNull() {
		params.LookupKey = stripe.String(plan.LookupKey.ValueString())
	}

	if !plan.Metadata.IsNull() {
		for k, v := range plan.Metadata.Elements() {
			params.AddMetadata(k, v.(types.String).ValueString())
		}
	}

	if !plan.Nickname.IsNull() {
		params.Nickname = stripe.String(plan.Nickname.ValueString())
	}

	// Handle recurring price
	if !plan.Recurring.IsNull() {
		var recurring recurringModel
		diags := plan.Recurring.As(ctx, &recurring, basetypes.ObjectAsOptions{})
		resp.Diagnostics.Append(diags...)
		if resp.Diagnostics.HasError() {
			return
		}

		params.Recurring = &stripe.PriceRecurringParams{}

		if !recurring.Interval.IsNull() {
			params.Recurring.Interval = stripe.String(recurring.Interval.ValueString())
		}

		if !recurring.IntervalCount.IsNull() {
			params.Recurring.IntervalCount = stripe.Int64(recurring.IntervalCount.ValueInt64())
		}

		if !recurring.UsageType.IsNull() {
			params.Recurring.UsageType = stripe.String(recurring.UsageType.ValueString())
		}
	}

	if !plan.TiersMode.IsNull() {
		params.TiersMode = stripe.String(plan.TiersMode.ValueString())
	}

	// Handle transform usage
	if !plan.TransformUsage.IsNull() {
		var transformUsage transformUsageModel
		diags := plan.TransformUsage.As(ctx, &transformUsage, basetypes.ObjectAsOptions{})
		resp.Diagnostics.Append(diags...)
		if resp.Diagnostics.HasError() {
			return
		}

		params.TransformQuantity = &stripe.PriceTransformQuantityParams{}

		if !transformUsage.DivideBy.IsNull() {
			params.TransformQuantity.DivideBy = stripe.Int64(transformUsage.DivideBy.ValueInt64())
		}

		if !transformUsage.Round.IsNull() {
			params.TransformQuantity.Round = stripe.String(transformUsage.Round.ValueString())
		}
	}

	if !plan.UnitAmount.IsNull() {
		params.UnitAmount = stripe.Int64(plan.UnitAmount.ValueInt64())
	}

	// Create the price
	price, err := r.client.Prices.New(params)
	if err != nil {
		resp.Diagnostics.AddError(
			"Error creating Stripe price",
			fmt.Sprintf("Could not create Stripe price: %s", err.Error()),
		)
		return
	}

	// Map response body to model
	state := priceResourceModel{
		ID:                types.StringValue(price.ID),
		Active:            types.BoolValue(price.Active),
		Currency:          types.StringValue(string(price.Currency)),
		Created:           types.Int64Value(price.Created),
		Type:              types.StringValue(string(price.Type)),
		UnitAmountDecimal: types.StringValue(fmt.Sprintf("%.2f", price.UnitAmountDecimal)),
	}

	// Handle recurring attributes
	if price.Recurring != nil {
		state.Interval = types.StringValue(string(price.Recurring.Interval))
		state.IntervalCount = types.Int64Value(price.Recurring.IntervalCount)

		recurring := map[string]attr.Value{
			"interval":       types.StringValue(string(price.Recurring.Interval)),
			"interval_count": types.Int64Value(price.Recurring.IntervalCount),
			"usage_type":     types.StringValue(string(price.Recurring.UsageType)),
		}
		state.Recurring = types.ObjectValueMust(
			map[string]attr.Type{
				"interval":       types.StringType,
				"interval_count": types.Int64Type,
				"usage_type":     types.StringType,
			},
			recurring,
		)
	} else {
		state.Interval = types.StringNull()
		state.IntervalCount = types.Int64Null()
		state.Recurring = types.ObjectNull(
			map[string]attr.Type{
				"interval":       types.StringType,
				"interval_count": types.Int64Type,
				"usage_type":     types.StringType,
			},
		)
	}

	if price.LookupKey != "" {
		state.LookupKey = types.StringValue(price.LookupKey)
	} else {
		state.LookupKey = types.StringNull()
	}

	// Handle metadata
	metadata := make(map[string]attr.Value)
	for k, v := range price.Metadata {
		metadata[k] = types.StringValue(v)
	}
	state.Metadata = types.MapValueMust(types.StringType, metadata)

	if price.Nickname != "" {
		state.Nickname = types.StringValue(price.Nickname)
	} else {
		state.Nickname = types.StringNull()
	}

	state.Product = types.StringValue(price.Product.ID)

	if price.TiersMode != "" {
		state.TiersMode = types.StringValue(string(price.TiersMode))
	} else {
		state.TiersMode = types.StringNull()
	}

	// Handle transform usage
	if price.TransformQuantity != nil {
		transformUsage := map[string]attr.Value{
			"divide_by": types.Int64Value(price.TransformQuantity.DivideBy),
			"round":     types.StringValue(string(price.TransformQuantity.Round)),
		}
		state.TransformUsage = types.ObjectValueMust(
			map[string]attr.Type{
				"divide_by": types.Int64Type,
				"round":     types.StringType,
			},
			transformUsage,
		)
	} else {
		state.TransformUsage = types.ObjectNull(
			map[string]attr.Type{
				"divide_by": types.Int64Type,
				"round":     types.StringType,
			},
		)
	}

	if price.UnitAmount != 0 {
		state.UnitAmount = types.Int64Value(price.UnitAmount)
	} else {
		state.UnitAmount = types.Int64Null()
	}

	// Set state
	diags = resp.State.Set(ctx, &state)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	tflog.Debug(ctx, "Created Stripe price", map[string]any{"id": state.ID.ValueString()})
}

func (r *priceResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	// Get current state
	var state priceResourceModel
	diags := req.State.Get(ctx, &state)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	tflog.Debug(ctx, "Reading Stripe price", map[string]any{"id": state.ID.ValueString()})

	// Get refreshed price value from Stripe
	params := &stripe.PriceParams{}
	price, err := r.client.Prices.Get(state.ID.ValueString(), params)
	if err != nil {
		resp.Diagnostics.AddError(
			"Error reading Stripe price",
			fmt.Sprintf("Could not read Stripe price %s: %s", state.ID.ValueString(), err.Error()),
		)
		return
	}

	// Overwrite state with refreshed values
	state.Active = types.BoolValue(price.Active)
	state.Currency = types.StringValue(string(price.Currency))
	state.Created = types.Int64Value(price.Created)
	state.Type = types.StringValue(string(price.Type))
	state.UnitAmountDecimal = types.StringValue(fmt.Sprintf("%.2f", price.UnitAmountDecimal))

	// Handle recurring attributes
	if price.Recurring != nil {
		state.Interval = types.StringValue(string(price.Recurring.Interval))
		state.IntervalCount = types.Int64Value(price.Recurring.IntervalCount)

		recurring := map[string]attr.Value{
			"interval":       types.StringValue(string(price.Recurring.Interval)),
			"interval_count": types.Int64Value(price.Recurring.IntervalCount),
			"usage_type":     types.StringValue(string(price.Recurring.UsageType)),
		}
		state.Recurring = types.ObjectValueMust(
			map[string]attr.Type{
				"interval":       types.StringType,
				"interval_count": types.Int64Type,
				"usage_type":     types.StringType,
			},
			recurring,
		)
	} else {
		state.Interval = types.StringNull()
		state.IntervalCount = types.Int64Null()
		state.Recurring = types.ObjectNull(
			map[string]attr.Type{
				"interval":       types.StringType,
				"interval_count": types.Int64Type,
				"usage_type":     types.StringType,
			},
		)
	}

	if price.LookupKey != "" {
		state.LookupKey = types.StringValue(price.LookupKey)
	} else {
		state.LookupKey = types.StringNull()
	}

	// Handle metadata
	metadata := make(map[string]attr.Value)
	for k, v := range price.Metadata {
		metadata[k] = types.StringValue(v)
	}
	state.Metadata = types.MapValueMust(types.StringType, metadata)

	if price.Nickname != "" {
		state.Nickname = types.StringValue(price.Nickname)
	} else {
		state.Nickname = types.StringNull()
	}

	state.Product = types.StringValue(price.Product.ID)

	if price.TiersMode != "" {
		state.TiersMode = types.StringValue(string(price.TiersMode))
	} else {
		state.TiersMode = types.StringNull()
	}

	// Handle transform usage
	if price.TransformQuantity != nil {
		transformUsage := map[string]attr.Value{
			"divide_by": types.Int64Value(price.TransformQuantity.DivideBy),
			"round":     types.StringValue(string(price.TransformQuantity.Round)),
		}
		state.TransformUsage = types.ObjectValueMust(
			map[string]attr.Type{
				"divide_by": types.Int64Type,
				"round":     types.StringType,
			},
			transformUsage,
		)
	} else {
		state.TransformUsage = types.ObjectNull(
			map[string]attr.Type{
				"divide_by": types.Int64Type,
				"round":     types.StringType,
			},
		)
	}

	if price.UnitAmount != 0 {
		state.UnitAmount = types.Int64Value(price.UnitAmount)
	} else {
		state.UnitAmount = types.Int64Null()
	}

	// Set refreshed state
	diags = resp.State.Set(ctx, &state)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	tflog.Debug(ctx, "Read Stripe price", map[string]any{"id": state.ID.ValueString()})
}

func (r *priceResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	// Retrieve values from plan
	var plan priceResourceModel
	diags := req.Plan.Get(ctx, &plan)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	// Get current state
	var state priceResourceModel
	diags = req.State.Get(ctx, &state)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	tflog.Debug(ctx, "Updating Stripe price", map[string]any{"id": state.ID.ValueString()})

	// Generate API request parameters from plan
	params := &stripe.PriceParams{}

	if !plan.Active.IsNull() {
		params.Active = stripe.Bool(plan.Active.ValueBool())
	}

	if !plan.LookupKey.IsNull() {
		params.LookupKey = stripe.String(plan.LookupKey.ValueString())
	}

	if !plan.Metadata.IsNull() {
		for k, v := range plan.Metadata.Elements() {
			params.AddMetadata(k, v.(types.String).ValueString())
		}
	}

	if !plan.Nickname.IsNull() {
		params.Nickname = stripe.String(plan.Nickname.ValueString())
	}

	// Update the price
	price, err := r.client.Prices.Update(state.ID.ValueString(), params)
	if err != nil {
		resp.Diagnostics.AddError(
			"Error updating Stripe price",
			fmt.Sprintf("Could not update Stripe price %s: %s", state.ID.ValueString(), err.Error()),
		)
		return
	}

	// Create a new state model to avoid modifying the original state
	newState := priceResourceModel{
		ID:                state.ID,
		Active:            types.BoolValue(price.Active),
		Currency:          types.StringValue(string(price.Currency)),
		Created:           types.Int64Value(price.Created),
		Type:              types.StringValue(string(price.Type)),
		UnitAmountDecimal: types.StringValue(fmt.Sprintf("%.2f", price.UnitAmountDecimal)),
	}

	// Copy over values from plan for attributes that should be preserved
	if !plan.LookupKey.IsNull() {
		newState.LookupKey = plan.LookupKey
	} else if price.LookupKey != "" {
		newState.LookupKey = types.StringValue(price.LookupKey)
	} else {
		newState.LookupKey = types.StringNull()
	}

	// Handle metadata
	if !plan.Metadata.IsNull() {
		newState.Metadata = plan.Metadata
	} else {
		metadata := make(map[string]attr.Value)
		for k, v := range price.Metadata {
			metadata[k] = types.StringValue(v)
		}
		newState.Metadata = types.MapValueMust(types.StringType, metadata)
	}

	if !plan.Nickname.IsNull() {
		newState.Nickname = plan.Nickname
	} else if price.Nickname != "" {
		newState.Nickname = types.StringValue(price.Nickname)
	} else {
		newState.Nickname = types.StringNull()
	}

	newState.Product = types.StringValue(price.Product.ID)

	if price.TiersMode != "" {
		newState.TiersMode = types.StringValue(string(price.TiersMode))
	} else {
		newState.TiersMode = types.StringNull()
	}

	// Handle transform usage
	if price.TransformQuantity != nil {
		transformUsage := map[string]attr.Value{
			"divide_by": types.Int64Value(price.TransformQuantity.DivideBy),
			"round":     types.StringValue(string(price.TransformQuantity.Round)),
		}
		newState.TransformUsage = types.ObjectValueMust(
			map[string]attr.Type{
				"divide_by": types.Int64Type,
				"round":     types.StringType,
			},
			transformUsage,
		)
	} else {
		newState.TransformUsage = types.ObjectNull(
			map[string]attr.Type{
				"divide_by": types.Int64Type,
				"round":     types.StringType,
			},
		)
	}

	if price.UnitAmount != 0 {
		newState.UnitAmount = types.Int64Value(price.UnitAmount)
	} else {
		newState.UnitAmount = types.Int64Null()
	}

	// Set refreshed state
	diags = resp.State.Set(ctx, &newState)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	tflog.Debug(ctx, "Updated Stripe price", map[string]any{"id": newState.ID.ValueString()})
}

func (r *priceResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	// Get current state
	var state priceResourceModel
	diags := req.State.Get(ctx, &state)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	tflog.Warn(ctx, "Stripe prices will not be deleted, only deactivated", map[string]any{"id": state.ID.ValueString()})
	tflog.Debug(ctx, "Setting Stripe price to inactive", map[string]any{"id": state.ID.ValueString()})

	// Prices cannot be deleted in Stripe, only set to inactive
	params := &stripe.PriceParams{
		Active: stripe.Bool(false),
	}

	_, err := r.client.Prices.Update(state.ID.ValueString(), params)
	if err != nil {
		resp.Diagnostics.AddError(
			"Error deactivating price",
			"Could not set price to inactive, unexpected error: "+err.Error(),
		)
		return
	}

	tflog.Debug(ctx, "Set Stripe price to inactive", map[string]any{"id": state.ID.ValueString()})
}

func (r *priceResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	// Retrieve import ID and save to id attribute
	resource.ImportStatePassthroughID(ctx, path.Root("id"), req, resp)
}
