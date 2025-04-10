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
	ID                  types.String            `tfsdk:"id"`
	Name                types.String            `tfsdk:"name"`
	Description         types.String            `tfsdk:"description"`
	Active              types.Bool              `tfsdk:"active"`
	Created             types.Int64             `tfsdk:"created"`
	Updated             types.Int64             `tfsdk:"updated"`
	Metadata            types.Map               `tfsdk:"metadata"`
	Images              types.List              `tfsdk:"images"`
	DefaultPrice        types.String            `tfsdk:"default_price"`
	PackageDimensions   *packageDimensionsModel `tfsdk:"package_dimensions"`
	Shippable           types.Bool              `tfsdk:"shippable"`
	StatementDescriptor types.String            `tfsdk:"statement_descriptor"`
	TaxCode             types.String            `tfsdk:"tax_code"`
	UnitLabel           types.String            `tfsdk:"unit_label"`
	URL                 types.String            `tfsdk:"url"`
}

// packageDimensionsModel maps package dimensions data.
type packageDimensionsModel struct {
	Height types.Float64 `tfsdk:"height"`
	Length types.Float64 `tfsdk:"length"`
	Weight types.Float64 `tfsdk:"weight"`
	Width  types.Float64 `tfsdk:"width"`
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
			},
			"created": schema.Int64Attribute{
				Description: "Time at which the product was created.",
				Computed:    true,
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
			"default_price": schema.StringAttribute{
				Description: "The ID of the default price for this product.",
				Optional:    true,
				Computed:    true,
			},
			"package_dimensions": schema.SingleNestedAttribute{
				Description: "The dimensions of this product for shipping purposes.",
				Optional:    true,
				Attributes: map[string]schema.Attribute{
					"height": schema.Float64Attribute{
						Description: "Height, in inches.",
						Required:    true,
					},
					"length": schema.Float64Attribute{
						Description: "Length, in inches.",
						Required:    true,
					},
					"weight": schema.Float64Attribute{
						Description: "Weight, in ounces.",
						Required:    true,
					},
					"width": schema.Float64Attribute{
						Description: "Width, in inches.",
						Required:    true,
					},
				},
			},
			"shippable": schema.BoolAttribute{
				Description: "Whether this product is a shipped good.",
				Optional:    true,
			},
			"statement_descriptor": schema.StringAttribute{
				Description: "Extra information about a product which will appear on a customer's credit card statement.",
				Optional:    true,
			},
			"tax_code": schema.StringAttribute{
				Description: "A tax code ID.",
				Optional:    true,
			},
			"unit_label": schema.StringAttribute{
				Description: "The label for the product's unit of measure.",
				Optional:    true,
			},
			"url": schema.StringAttribute{
				Description: "The URL of the product.",
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
	params := &stripe.ProductParams{
		Name: stripe.String(plan.Name.ValueString()),
	}

	if !plan.Description.IsNull() {
		params.Description = stripe.String(plan.Description.ValueString())
	}

	if !plan.Active.IsNull() {
		params.Active = stripe.Bool(plan.Active.ValueBool())
	}

	if !plan.Metadata.IsNull() {
		metadata := make(map[string]string)
		plan.Metadata.ElementsAs(ctx, &metadata, false)
		params.Metadata = metadata
	}

	if !plan.Images.IsNull() {
		var imageStrings []string
		plan.Images.ElementsAs(ctx, &imageStrings, false)

		// Convert []string to []*string
		images := make([]*string, len(imageStrings))
		for i, img := range imageStrings {
			images[i] = stripe.String(img)
		}
		params.Images = images
	}

	// Note: default_price is not a valid parameter for product creation
	// It's only available when retrieving a product
	// We'll handle it in the response mapping

	if plan.PackageDimensions != nil {
		params.PackageDimensions = &stripe.ProductPackageDimensionsParams{
			Height: stripe.Float64(plan.PackageDimensions.Height.ValueFloat64()),
			Length: stripe.Float64(plan.PackageDimensions.Length.ValueFloat64()),
			Weight: stripe.Float64(plan.PackageDimensions.Weight.ValueFloat64()),
			Width:  stripe.Float64(plan.PackageDimensions.Width.ValueFloat64()),
		}
	}

	if !plan.Shippable.IsNull() {
		params.Shippable = stripe.Bool(plan.Shippable.ValueBool())
	}

	if !plan.StatementDescriptor.IsNull() {
		params.StatementDescriptor = stripe.String(plan.StatementDescriptor.ValueString())
	}

	if !plan.TaxCode.IsNull() {
		params.TaxCode = stripe.String(plan.TaxCode.ValueString())
	}

	if !plan.UnitLabel.IsNull() {
		params.UnitLabel = stripe.String(plan.UnitLabel.ValueString())
	}

	if !plan.URL.IsNull() {
		params.URL = stripe.String(plan.URL.ValueString())
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
	plan.ID = types.StringValue(product.ID)
	plan.Created = types.Int64Value(product.Created)
	plan.Updated = types.Int64Value(product.Updated)
	plan.Active = types.BoolValue(product.Active)
	plan.Shippable = types.BoolValue(product.Shippable)

	// Handle metadata - preserve null state if it was null in the plan
	if plan.Metadata.IsNull() {
		plan.Metadata = types.MapNull(types.StringType)
	} else {
		metadata := make(map[string]attr.Value)
		for k, v := range product.Metadata {
			metadata[k] = types.StringValue(v)
		}
		plan.Metadata = types.MapValueMust(types.StringType, metadata)
	}

	// Handle images - preserve null state if it was null in the plan
	if plan.Images.IsNull() {
		plan.Images = types.ListNull(types.StringType)
	} else {
		images := make([]attr.Value, len(product.Images))
		for i, img := range product.Images {
			images[i] = types.StringValue(img)
		}
		plan.Images = types.ListValueMust(types.StringType, images)
	}

	// Handle default price
	if product.DefaultPrice != nil {
		plan.DefaultPrice = types.StringValue(product.DefaultPrice.ID)
	} else {
		plan.DefaultPrice = types.StringNull()
	}

	// Set state to fully populated data
	diags = resp.State.Set(ctx, plan)
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
	newState := productResourceModel{
		// Preserve the ID from the state
		ID: state.ID,

		// Set computed attributes
		Name:    types.StringValue(product.Name),
		Active:  types.BoolValue(product.Active),
		Created: types.Int64Value(product.Created),
		Updated: types.Int64Value(product.Updated),
	}

	// Set description from the API response
	if product.Description != "" {
		newState.Description = types.StringValue(product.Description)
	} else {
		newState.Description = types.StringNull()
	}

	// Set metadata from the API response
	if len(product.Metadata) > 0 {
		metadata := make(map[string]attr.Value)
		for k, v := range product.Metadata {
			metadata[k] = types.StringValue(v)
		}
		newState.Metadata = types.MapValueMust(types.StringType, metadata)
	} else {
		newState.Metadata = types.MapNull(types.StringType)
	}

	// Set images from the API response
	if len(product.Images) > 0 {
		images := make([]attr.Value, len(product.Images))
		for i, img := range product.Images {
			images[i] = types.StringValue(img)
		}
		newState.Images = types.ListValueMust(types.StringType, images)
	} else {
		newState.Images = types.ListNull(types.StringType)
	}

	// Set shippable from the API response
	newState.Shippable = types.BoolValue(product.Shippable)

	// Set statement descriptor from the API response
	if product.StatementDescriptor != "" {
		newState.StatementDescriptor = types.StringValue(product.StatementDescriptor)
	} else {
		newState.StatementDescriptor = types.StringNull()
	}

	// Set unit label from the API response
	if product.UnitLabel != "" {
		newState.UnitLabel = types.StringValue(product.UnitLabel)
	} else {
		newState.UnitLabel = types.StringNull()
	}

	// Set URL from the API response
	if product.URL != "" {
		newState.URL = types.StringValue(product.URL)
	} else {
		newState.URL = types.StringNull()
	}

	// Handle default price
	if product.DefaultPrice != nil {
		newState.DefaultPrice = types.StringValue(product.DefaultPrice.ID)
	} else {
		newState.DefaultPrice = types.StringNull()
	}

	// Handle package dimensions
	if product.PackageDimensions != nil {
		newState.PackageDimensions = &packageDimensionsModel{
			Height: types.Float64Value(product.PackageDimensions.Height),
			Length: types.Float64Value(product.PackageDimensions.Length),
			Weight: types.Float64Value(product.PackageDimensions.Weight),
			Width:  types.Float64Value(product.PackageDimensions.Width),
		}
	} else {
		newState.PackageDimensions = nil
	}

	// Handle tax code
	if product.TaxCode != nil {
		newState.TaxCode = types.StringValue(product.TaxCode.ID)
	} else {
		newState.TaxCode = types.StringNull()
	}

	// Set refreshed state
	diags = resp.State.Set(ctx, &newState)
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
	params := &stripe.ProductParams{
		Name: stripe.String(plan.Name.ValueString()),
	}

	if !plan.Description.IsNull() {
		params.Description = stripe.String(plan.Description.ValueString())
	}

	if !plan.Active.IsNull() {
		params.Active = stripe.Bool(plan.Active.ValueBool())
	}

	if !plan.Metadata.IsNull() {
		metadata := make(map[string]string)
		plan.Metadata.ElementsAs(ctx, &metadata, false)
		params.Metadata = metadata
	}

	if !plan.Images.IsNull() {
		var imageStrings []string
		plan.Images.ElementsAs(ctx, &imageStrings, false)

		// Convert []string to []*string
		images := make([]*string, len(imageStrings))
		for i, img := range imageStrings {
			images[i] = stripe.String(img)
		}
		params.Images = images
	}

	if plan.PackageDimensions != nil {
		params.PackageDimensions = &stripe.ProductPackageDimensionsParams{
			Height: stripe.Float64(plan.PackageDimensions.Height.ValueFloat64()),
			Length: stripe.Float64(plan.PackageDimensions.Length.ValueFloat64()),
			Weight: stripe.Float64(plan.PackageDimensions.Weight.ValueFloat64()),
			Width:  stripe.Float64(plan.PackageDimensions.Width.ValueFloat64()),
		}
	}

	if !plan.Shippable.IsNull() {
		params.Shippable = stripe.Bool(plan.Shippable.ValueBool())
	}

	if !plan.StatementDescriptor.IsNull() {
		params.StatementDescriptor = stripe.String(plan.StatementDescriptor.ValueString())
	}

	if !plan.TaxCode.IsNull() {
		params.TaxCode = stripe.String(plan.TaxCode.ValueString())
	}

	if !plan.UnitLabel.IsNull() {
		params.UnitLabel = stripe.String(plan.UnitLabel.ValueString())
	}

	if !plan.URL.IsNull() {
		params.URL = stripe.String(plan.URL.ValueString())
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
	plan.ID = types.StringValue(product.ID)
	plan.Created = types.Int64Value(product.Created)
	plan.Updated = types.Int64Value(product.Updated)
	plan.Active = types.BoolValue(product.Active)
	plan.Shippable = types.BoolValue(product.Shippable)

	// Handle metadata - preserve null state if it was null in the plan
	if plan.Metadata.IsNull() {
		plan.Metadata = types.MapNull(types.StringType)
	} else {
		metadata := make(map[string]attr.Value)
		for k, v := range product.Metadata {
			metadata[k] = types.StringValue(v)
		}
		plan.Metadata = types.MapValueMust(types.StringType, metadata)
	}

	// Handle images - preserve null state if it was null in the plan
	if plan.Images.IsNull() {
		plan.Images = types.ListNull(types.StringType)
	} else {
		images := make([]attr.Value, len(product.Images))
		for i, img := range product.Images {
			images[i] = types.StringValue(img)
		}
		plan.Images = types.ListValueMust(types.StringType, images)
	}

	// Handle default price
	if product.DefaultPrice != nil {
		plan.DefaultPrice = types.StringValue(product.DefaultPrice.ID)
	} else {
		plan.DefaultPrice = types.StringNull()
	}

	// Set state to fully populated data
	diags = resp.State.Set(ctx, plan)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	tflog.Info(ctx, "Updated Stripe product", map[string]any{"id": product.ID})
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
