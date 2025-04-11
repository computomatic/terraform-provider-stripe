package provider

import (
	"context"
	"fmt"

	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/datasource/schema"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-log/tflog"
	"github.com/stripe/stripe-go/v82"
	"github.com/stripe/stripe-go/v82/client"
)

// Ensure the implementation satisfies the expected interfaces.
var (
	_ datasource.DataSource              = &productDataSource{}
	_ datasource.DataSourceWithConfigure = &productDataSource{}
)

// NewProductDataSource is a helper function to simplify the provider implementation.
func NewProductDataSource() datasource.DataSource {
	return &productDataSource{}
}

// productDataSource is the data source implementation.
type productDataSource struct {
	client *client.API
}

// productDataSourceModel maps the data source schema data.
type productDataSourceModel struct {
	ID          types.String `tfsdk:"id"`
	Name        types.String `tfsdk:"name"`
	Description types.String `tfsdk:"description"`
	Active      types.Bool   `tfsdk:"active"`
	Created     types.Int64  `tfsdk:"created"`
	Updated     types.Int64  `tfsdk:"updated"`
	Metadata    types.Map    `tfsdk:"metadata"`
	Images      types.List   `tfsdk:"images"`
}

// Metadata returns the data source type name.
func (d *productDataSource) Metadata(_ context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_product"
}

// Schema defines the schema for the data source.
func (d *productDataSource) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: "Use this data source to get information about a Stripe Product.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				Description: "Unique identifier for the product.",
				Required:    true,
			},
			"name": schema.StringAttribute{
				Description: "The product's name.",
				Computed:    true,
			},
			"description": schema.StringAttribute{
				Description: "The product's description.",
				Computed:    true,
			},
			"active": schema.BoolAttribute{
				Description: "Whether the product is currently available for purchase.",
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
				Computed:    true,
			},
			"images": schema.ListAttribute{
				Description: "A list of up to 8 URLs of images for this product.",
				ElementType: types.StringType,
				Computed:    true,
			},
		},
	}
}

// Configure adds the provider configured client to the data source.
func (d *productDataSource) Configure(_ context.Context, req datasource.ConfigureRequest, resp *datasource.ConfigureResponse) {
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

	d.client = client
}

// Read refreshes the Terraform state with the latest data.
func (d *productDataSource) Read(ctx context.Context, req datasource.ReadRequest, resp *datasource.ReadResponse) {
	var config productDataSourceModel

	// Read Terraform configuration data into the model
	resp.Diagnostics.Append(req.Config.Get(ctx, &config)...)
	if resp.Diagnostics.HasError() {
		return
	}

	tflog.Debug(ctx, "Reading Stripe product", map[string]any{"id": config.ID.ValueString()})

	// Retrieve the product from Stripe
	params := &stripe.ProductParams{}
	product, err := d.client.Products.Get(config.ID.ValueString(), params)
	if err != nil {
		resp.Diagnostics.AddError(
			"Error reading Stripe product",
			fmt.Sprintf("Could not read Stripe product %s: %s", config.ID.ValueString(), err.Error()),
		)
		return
	}

	// Map response body to model
	state := productResourceModel{
		ID:      types.StringValue(product.ID),
		Name:    types.StringValue(product.Name),
		Created: types.Int64Value(product.Created),
		Updated: types.Int64Value(product.Updated),
		Active:  types.BoolValue(product.Active),
	}

	if product.Description != "" {
		state.Description = types.StringValue((product.Description))
	} else {
		state.Description = types.StringNull()
	}

	// Handle metadata
	if product.Metadata == nil {
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
	} else {
		images := make([]attr.Value, len(product.Images))
		for i, img := range product.Images {
			images[i] = types.StringValue(img)
		}
		state.Images = types.ListValueMust(types.StringType, images)
	}

	// Set state
	diags := resp.State.Set(ctx, &state)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	tflog.Debug(ctx, "Read Stripe product", map[string]any{"id": state.ID.ValueString()})
}
