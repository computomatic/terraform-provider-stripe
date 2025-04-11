package provider

import (
	"context"
	"errors"
	"fmt"

	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/float64planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/int64planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/types"
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
	ID       types.String `tfsdk:"id"`
	Created  types.Int64  `tfsdk:"created"`
	Active   types.Bool   `tfsdk:"active"`
	Currency types.String `tfsdk:"currency"`
	// LookupKey         types.String      `tfsdk:"lookup_key"`
	Metadata types.Map `tfsdk:"metadata"`
	// Nickname          types.String      `tfsdk:"nickname"`
	Product types.String `tfsdk:"product"`
	// Recurring         types.Object      `tfsdk:"recurring"`
	// TiersMode         types.String      `tfsdk:"tiers_mode"`
	// TransformUsage    types.Object      `tfsdk:"transform_usage"`
	// Type              types.String      `tfsdk:"type"`
	UnitAmount        types.Int64   `tfsdk:"unit_amount"`
	UnitAmountDecimal types.Float64 `tfsdk:"unit_amount_decimal"`
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
			"metadata": schema.MapAttribute{
				Description: "Set of key-value pairs that you can attach to a price.",
				ElementType: types.StringType,
				Optional:    true,
			},
			"product": schema.StringAttribute{
				Description: "The ID of the product this price is associated with.",
				Required:    true,
			},
			"unit_amount": schema.Int64Attribute{
				Description: "The unit amount in cents to be charged.",
				Optional:    true,
				Computed:    true,
				PlanModifiers: []planmodifier.Int64{
					int64planmodifier.RequiresReplace(),
				},
			},
			"unit_amount_decimal": schema.Float64Attribute{
				Description: "The unit amount in decimal form.",
				Optional:    true,
				Computed:    true,
				PlanModifiers: []planmodifier.Float64{
					float64planmodifier.RequiresReplace(),
				},
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

func mapStripePriceToResourceState(price *stripe.Price, state *priceResourceModel) {
	// Map response body to model
	state.ID = types.StringValue(price.ID)
	state.Active = types.BoolValue(price.Active)
	state.Product = types.StringValue(price.Product.ID)
	state.Currency = types.StringValue(string(price.Currency))
	state.Created = types.Int64Value(price.Created)
	state.UnitAmount = types.Int64Value(price.UnitAmount)
	state.UnitAmountDecimal = types.Float64Value(price.UnitAmountDecimal)

	// Handle metadata
	if price.Metadata == nil {
		state.Metadata = types.MapNull(types.StringType)
	} else if len(price.Metadata) == 0 && state.Metadata.IsNull() {
		// Preserve null state
		state.Metadata = types.MapNull(types.StringType)
	} else {
		metadata := make(map[string]attr.Value)
		for k, v := range price.Metadata {
			metadata[k] = types.StringValue(v)
		}
		state.Metadata = types.MapValueMust(types.StringType, metadata)
	}
}

func mapPriceResourceStateToStripeParams(plan *priceResourceModel, params *stripe.PriceParams) error {
	// Generate API request parameters from plan
	if !plan.Currency.IsNull() && !plan.Currency.IsUnknown() {
		params.Currency = stripe.String(plan.Currency.ValueString())
	}

	if !plan.Product.IsNull() && !plan.Product.IsUnknown() {
		params.Product = stripe.String(plan.Product.ValueString())
	}

	if !plan.Active.IsNull() && !plan.Active.IsUnknown() {
		params.Active = stripe.Bool(plan.Active.ValueBool())
	}

	if !plan.Metadata.IsNull() && !plan.Metadata.IsUnknown() {
		for k, v := range plan.Metadata.Elements() {
			params.AddMetadata(k, v.(types.String).ValueString())
		}
	}

	unitAmountSet := !plan.UnitAmount.IsNull() && !plan.UnitAmount.IsUnknown()
	unitAmountDecimalSet := !plan.UnitAmountDecimal.IsNull() && !plan.UnitAmountDecimal.IsUnknown()

	if unitAmountSet && unitAmountDecimalSet {
		return fmt.Errorf("exactly one of `unit_amount` and `unit_amount_decimal` must be provided (received both: (%v, %v))", plan.UnitAmount.ValueInt64(), plan.UnitAmountDecimal.ValueFloat64())
	}
	if unitAmountSet {
		params.UnitAmount = stripe.Int64(plan.UnitAmount.ValueInt64())
	} else if unitAmountDecimalSet {
		params.UnitAmountDecimal = stripe.Float64(plan.UnitAmountDecimal.ValueFloat64())
	} else {
		return errors.New("exactly one of `unit_amount` and `unit_amount_decimal` must be provided (received neither)")
	}

	return nil
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
	params := &stripe.PriceParams{}
	err := mapPriceResourceStateToStripeParams(&plan, params)
	if err != nil {
		resp.Diagnostics.AddError(
			"Error creating Stripe price",
			fmt.Sprintf("Could not create Stripe price: %s", err.Error()),
		)
		return
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
	state := plan
	mapStripePriceToResourceState(price, &state)

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

	mapStripePriceToResourceState(price, &state)

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
	mapPriceResourceStateToStripeParams(&plan, params)

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
	mapStripePriceToResourceState(price, &state)

	// Set refreshed state
	diags = resp.State.Set(ctx, &state)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	tflog.Debug(ctx, "Updated Stripe price", map[string]any{"id": state.ID.ValueString()})
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
