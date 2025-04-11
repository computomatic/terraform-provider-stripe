package provider

import (
	"context"
	"fmt"

	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/boolplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/int64planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-log/tflog"
	"github.com/stripe/stripe-go/v82"
	"github.com/stripe/stripe-go/v82/client"
)

var (
	_ resource.Resource                = &productResource{}
	_ resource.ResourceWithConfigure   = &productResource{}
	_ resource.ResourceWithImportState = &productResource{}
)

func NewProductResource() resource.Resource {
	return &productResource{}
}

type productResource struct {
	client *client.API
}

// productResourceModel maps the resource schema data.
type productResourceModel struct {
	ID          types.String `tfsdk:"id"`
	Name        types.String `tfsdk:"name"`
	Description types.String `tfsdk:"description"`
	Active      types.Bool   `tfsdk:"active"`
	Created     types.Int64  `tfsdk:"created"`
	Updated     types.Int64  `tfsdk:"updated"`
	Metadata    types.Map    `tfsdk:"metadata"`
	Images      types.List   `tfsdk:"images"`
}

func (r *productResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_product"
}

func (r *productResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: "Manages a Stripe Product.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				Description: "Unique identifier for the product.",
				Computed:    true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"name": schema.StringAttribute{
				Description: "The product's name.",
				Required:    true,
			},
			"description": schema.StringAttribute{
				Description: "The product's description.",
				Optional:    true,
			},
			"active": schema.BoolAttribute{
				Description: "Whether the product is currently available for purchase.",
				Optional:    true,
				Computed:    true,
				PlanModifiers: []planmodifier.Bool{
					boolplanmodifier.UseStateForUnknown(),
				},
			},
			"created": schema.Int64Attribute{
				Description: "Time at which the product was created.",
				Computed:    true,
				PlanModifiers: []planmodifier.Int64{
					int64planmodifier.UseStateForUnknown(),
				},
			},
			"updated": schema.Int64Attribute{
				Description: "Time at which the product was last updated.",
				Computed:    true,
			},
			"metadata": schema.MapAttribute{
				Description: "Set of key-value pairs that you can attach to a product.",
				ElementType: types.StringType,
				Optional:    true,
			},
			"images": schema.ListAttribute{
				Description: "A list of up to 8 URLs of images for this product.",
				ElementType: types.StringType,
				Optional:    true,
			},
		},
	}
}

func (r *productResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	if req.ProviderData == nil {
		return
	}

	client, ok := req.ProviderData.(*client.API)

	if !ok {
		resp.Diagnostics.AddError(
			"Unexpected Data Source Configure Type",
			fmt.Sprintf("Expected *client.API, got: %T. Please report this issue to the provider developers.", req.ProviderData),
		)

		return
	}

	r.client = client
}

func mapProductResourceStateToStripeParams(ctx context.Context, plan *productResourceModel, params *stripe.ProductParams) error {
	if !plan.Name.IsNull() && !plan.Name.IsUnknown() {
		params.Name = stripe.String(plan.Name.ValueString())
	}

	if !plan.Description.IsNull() && !plan.Description.IsUnknown() {
		params.Description = stripe.String(plan.Description.ValueString())
	}

	if !plan.Active.IsNull() && !plan.Active.IsUnknown() {
		params.Active = stripe.Bool(plan.Active.ValueBool())
	}

	if !plan.Metadata.IsNull() && !plan.Metadata.IsUnknown() {
		metadata := make(map[string]string)
		plan.Metadata.ElementsAs(ctx, &metadata, false)
		params.Metadata = metadata
	}

	if !plan.Images.IsNull() && !plan.Images.IsUnknown() {
		var imageStrings []string
		plan.Images.ElementsAs(ctx, &imageStrings, false)

		// Convert []string to []*string
		images := make([]*string, len(imageStrings))
		for i, img := range imageStrings {
			images[i] = stripe.String(img)
		}
		params.Images = images
	}

	return nil
}

func mapStripeProductToResourceState(product *stripe.Product, state *productResourceModel) error {
	state.ID = types.StringValue(product.ID)
	state.Name = types.StringValue(product.Name)
	state.Created = types.Int64Value(product.Created)
	state.Updated = types.Int64Value(product.Updated)
	state.Active = types.BoolValue(product.Active)

	if product.Description != "" {
		state.Description = types.StringValue((product.Description))
	} else {
		state.Description = types.StringNull()
	}

	// Handle metadata
	if product.Metadata == nil {
		state.Metadata = types.MapNull(types.StringType)
	} else if len(product.Metadata) == 0 && state.Metadata.IsNull() {
		// Preserve null state if result is empty
		state.Metadata = types.MapNull(types.StringType)
	} else {
		metadata := make(map[string]attr.Value)
		for k, v := range product.Metadata {
			metadata[k] = types.StringValue(v)
		}
		state.Metadata = types.MapValueMust(types.StringType, metadata)
	}

	// Handle images
	if product.Images == nil {
		state.Images = types.ListNull(types.StringType)
	} else if len(product.Images) == 0 && state.Images.IsNull() {
		// Preserve null state if result is empty
		state.Images = types.ListNull(types.StringType)
	} else {
		images := make([]attr.Value, len(product.Images))
		for i, img := range product.Images {
			images[i] = types.StringValue(img)
		}
		state.Images = types.ListValueMust(types.StringType, images)
	}

	return nil
}

func (r *productResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	// Retrieve values from plan
	var plan productResourceModel
	diags := req.Plan.Get(ctx, &plan)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	tflog.Debug(ctx, "Creating Stripe product", map[string]any{"name": plan.Name.ValueString()})

	// Generate API request parameters from plan
	params := &stripe.ProductParams{}
	err := mapProductResourceStateToStripeParams(ctx, &plan, params)
	if err != nil {
		resp.Diagnostics.AddError(
			"Error creating Stripe product",
			"Could not create product, unexpected error: "+err.Error(),
		)
		return
	}

	// Create new product
	product, err := r.client.Products.New(params)
	if err != nil {
		resp.Diagnostics.AddError(
			"Error creating Stripe product",
			"Could not create product, unexpected error: "+err.Error(),
		)
		return
	}

	// Map response to schema and populate Computed attribute values
	state := plan
	err = mapStripeProductToResourceState(product, &state)
	if err != nil {
		resp.Diagnostics.AddError(
			"Error creating Stripe product",
			"Could not create product, unexpected error: "+err.Error(),
		)
		return
	}

	// Set state to fully populated data
	diags = resp.State.Set(ctx, state)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	tflog.Info(ctx, "Created Stripe product", map[string]any{"id": product.ID})
}

func (r *productResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	// Get current state
	var state productResourceModel
	diags := req.State.Get(ctx, &state)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	tflog.Debug(ctx, "Reading Stripe product", map[string]any{"id": state.ID.ValueString()})

	// Get refreshed product value from Stripe
	params := &stripe.ProductParams{}
	product, err := r.client.Products.Get(state.ID.ValueString(), params)
	if err != nil {
		resp.Diagnostics.AddError(
			"Error Reading Stripe Product",
			"Could not read Stripe product ID "+state.ID.ValueString()+": "+err.Error(),
		)
		return
	}

	// Create a new state model to avoid modifying the original state
	err = mapStripeProductToResourceState(product, &state)
	if err != nil {
		resp.Diagnostics.AddError("Error mapping Stripe Product to state", fmt.Sprintf(`%v`, err))
	}

	// Set refreshed state
	diags = resp.State.Set(ctx, &state)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	tflog.Debug(ctx, "Read Stripe product", map[string]any{"id": state.ID.ValueString()})
}

func (r *productResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	// Retrieve values from plan
	var plan productResourceModel
	diags := req.Plan.Get(ctx, &plan)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	// Get current state
	var state productResourceModel
	diags = req.State.Get(ctx, &state)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	tflog.Debug(ctx, "Updating Stripe product", map[string]any{"id": state.ID.ValueString()})

	// Generate API request parameters from plan
	params := &stripe.ProductParams{}
	err := mapProductResourceStateToStripeParams(ctx, &plan, params)
	if err != nil {
		resp.Diagnostics.AddError(
			"Error Updating Stripe Product",
			"Could not update product, unexpected error: "+err.Error(),
		)
		return
	}

	// Update existing product
	product, err := r.client.Products.Update(state.ID.ValueString(), params)
	if err != nil {
		resp.Diagnostics.AddError(
			"Error Updating Stripe Product",
			"Could not update product, unexpected error: "+err.Error(),
		)
		return
	}

	// Map response to schema and populate Computed attribute values
	mapStripeProductToResourceState(product, &state)

	// Set state to fully populated data
	diags = resp.State.Set(ctx, &state)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	tflog.Info(ctx, "Updated Stripe product", map[string]any{"id": state.ID.ValueString()})
}

func (r *productResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	// Retrieve values from state
	var state productResourceModel
	diags := req.State.Get(ctx, &state)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	tflog.Debug(ctx, "Checking if Stripe product has prices", map[string]any{"id": state.ID.ValueString()})

	// Check if product has any prices
	params := &stripe.PriceListParams{
		Product: stripe.String(state.ID.ValueString()),
	}
	params.Limit = stripe.Int64(1)

	i := r.client.Prices.List(params)
	hasPrices := i.Next()

	if hasPrices {
		tflog.Warn(ctx, "Product has prices, deactivating instead of deleting", map[string]any{"id": state.ID.ValueString()})

		// Deactivate the product instead of deleting
		updateParams := &stripe.ProductParams{
			Active: stripe.Bool(false),
		}

		_, err := r.client.Products.Update(state.ID.ValueString(), updateParams)
		if err != nil {
			resp.Diagnostics.AddError(
				"Error Deactivating Stripe Product",
				"Could not deactivate product, unexpected error: "+err.Error(),
			)
			return
		}

		tflog.Info(ctx, "Deactivated Stripe product", map[string]any{"id": state.ID.ValueString()})
	} else {
		tflog.Debug(ctx, "Deleting Stripe product", map[string]any{"id": state.ID.ValueString()})

		// Delete existing product
		_, err := r.client.Products.Del(state.ID.ValueString(), nil)
		if err != nil {
			resp.Diagnostics.AddError(
				"Error Deleting Stripe Product",
				"Could not delete product, unexpected error: "+err.Error(),
			)
			return
		}

		tflog.Info(ctx, "Deleted Stripe product", map[string]any{"id": state.ID.ValueString()})
	}
}

func (r *productResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	resource.ImportStatePassthroughID(ctx, path.Root("id"), req, resp)
}
