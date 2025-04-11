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
	_ datasource.DataSource              = &priceDataSource{}
	_ datasource.DataSourceWithConfigure = &priceDataSource{}
)

// NewPriceDataSource is a helper function to simplify the provider implementation.
func NewPriceDataSource() datasource.DataSource {
	return &priceDataSource{}
}

// priceDataSource is the data source implementation.
type priceDataSource struct {
	client *client.API
}

// priceDataSourceModel maps the data source schema data.
type priceDataSourceModel struct {
	ID                types.String  `tfsdk:"id"`
	Active            types.Bool    `tfsdk:"active"`
	Currency          types.String  `tfsdk:"currency"`
	Metadata          types.Map     `tfsdk:"metadata"`
	Product           types.String  `tfsdk:"product"`
	UnitAmount        types.Int64   `tfsdk:"unit_amount"`
	UnitAmountDecimal types.Float64 `tfsdk:"unit_amount_decimal"`
}

// Metadata returns the data source type name.
func (d *priceDataSource) Metadata(_ context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_price"
}

// Schema defines the schema for the data source.
func (d *priceDataSource) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: "Use this data source to get information about a Stripe price.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				Description: "Unique identifier for the price.",
				Required:    true,
			},
			"active": schema.BoolAttribute{
				Description: "Whether the price can be used for new purchases.",
				Computed:    true,
			},
			"currency": schema.StringAttribute{
				Description: "Three-letter ISO currency code.",
				Computed:    true,
			},
			"metadata": schema.MapAttribute{
				Description: "Set of key-value pairs that you can attach to a price.",
				ElementType: types.StringType,
				Computed:    true,
			},
			"product": schema.StringAttribute{
				Description: "The ID of the product this price is associated with.",
				Computed:    true,
			},
			"unit_amount": schema.Int64Attribute{
				Description: "The unit amount in cents to be charged.",
				Computed:    true,
			},
			"unit_amount_decimal": schema.Float64Attribute{
				Description: "The unit amount in decimal form.",
				Computed:    true,
			},
		},
	}
}

// Configure adds the provider configured client to the data source.
func (d *priceDataSource) Configure(_ context.Context, req datasource.ConfigureRequest, resp *datasource.ConfigureResponse) {
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
func (d *priceDataSource) Read(ctx context.Context, req datasource.ReadRequest, resp *datasource.ReadResponse) {
	var config priceDataSourceModel

	// Read Terraform configuration data into the model
	resp.Diagnostics.Append(req.Config.Get(ctx, &config)...)
	if resp.Diagnostics.HasError() {
		return
	}

	tflog.Debug(ctx, "Reading Stripe price", map[string]any{"id": config.ID.ValueString()})

	// Retrieve the price from Stripe
	params := &stripe.PriceParams{}
	price, err := d.client.Prices.Get(config.ID.ValueString(), params)
	if err != nil {
		resp.Diagnostics.AddError(
			"Error reading Stripe price",
			fmt.Sprintf("Could not read Stripe price %s: %s", config.ID.ValueString(), err.Error()),
		)
		return
	}

	// Map response body to model
	state := priceDataSourceModel{
		ID:                types.StringValue(price.ID),
		Active:            types.BoolValue(price.Active),
		Product:           types.StringValue(price.Product.ID),
		Currency:          types.StringValue(string(price.Currency)),
		UnitAmount:        types.Int64Value(price.UnitAmount),
		UnitAmountDecimal: types.Float64Value(price.UnitAmountDecimal),
	}

	// Handle metadata
	if price.Metadata == nil {
		state.Metadata = types.MapNull(types.StringType)
	} else {
		metadata := make(map[string]attr.Value)
		for k, v := range price.Metadata {
			metadata[k] = types.StringValue(v)
		}
		state.Metadata = types.MapValueMust(types.StringType, metadata)
	}

	// Set state
	diags := resp.State.Set(ctx, &state)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	tflog.Debug(ctx, "Read Stripe price", map[string]any{"id": state.ID.ValueString()})
}
