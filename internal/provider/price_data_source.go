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
	ID                types.String `tfsdk:"id"`
	Active            types.Bool   `tfsdk:"active"`
	Currency          types.String `tfsdk:"currency"`
	Description       types.String `tfsdk:"description"`
	Interval          types.String `tfsdk:"interval"`
	IntervalCount     types.Int64  `tfsdk:"interval_count"`
	LookupKey         types.String `tfsdk:"lookup_key"`
	Metadata          types.Map    `tfsdk:"metadata"`
	Nickname          types.String `tfsdk:"nickname"`
	Product           types.String `tfsdk:"product"`
	Recurring         types.Object `tfsdk:"recurring"`
	TiersMode         types.String `tfsdk:"tiers_mode"`
	TransformUsage    types.Object `tfsdk:"transform_usage"`
	Type              types.String `tfsdk:"type"`
	UnitAmount        types.Int64  `tfsdk:"unit_amount"`
	UnitAmountDecimal types.String `tfsdk:"unit_amount_decimal"`
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
			"description": schema.StringAttribute{
				Description: "The price's description.",
				Computed:    true,
			},
			"interval": schema.StringAttribute{
				Description: "The recurring interval.",
				Computed:    true,
			},
			"interval_count": schema.Int64Attribute{
				Description: "The number of intervals between recurring billings.",
				Computed:    true,
			},
			"lookup_key": schema.StringAttribute{
				Description: "A lookup key used to retrieve prices.",
				Computed:    true,
			},
			"metadata": schema.MapAttribute{
				Description: "Set of key-value pairs that you can attach to a price.",
				ElementType: types.StringType,
				Computed:    true,
			},
			"nickname": schema.StringAttribute{
				Description: "A brief description of the price.",
				Computed:    true,
			},
			"product": schema.StringAttribute{
				Description: "The ID of the product this price is associated with.",
				Computed:    true,
			},
			"recurring": schema.SingleNestedAttribute{
				Description: "The recurring components of a price.",
				Attributes: map[string]schema.Attribute{
					"interval": schema.StringAttribute{
						Description: "The recurring interval.",
						Computed:    true,
					},
					"interval_count": schema.Int64Attribute{
						Description: "The number of intervals between recurring billings.",
						Computed:    true,
					},
					"usage_type": schema.StringAttribute{
						Description: "Configures how the quantity per period should be determined.",
						Computed:    true,
					},
				},
				Computed: true,
			},
			"tiers_mode": schema.StringAttribute{
				Description: "Defines if the tiering price should be `graduated` or `volume` based.",
				Computed:    true,
			},
			"transform_usage": schema.SingleNestedAttribute{
				Description: "Apply a transformation to the reported usage or set quantity before computing the amount billed.",
				Attributes: map[string]schema.Attribute{
					"divide_by": schema.Int64Attribute{
						Description: "Divide usage by this number.",
						Computed:    true,
					},
					"round": schema.StringAttribute{
						Description: "After division, either round the result `up` or `down`.",
						Computed:    true,
					},
				},
				Computed: true,
			},
			"type": schema.StringAttribute{
				Description: "One of `one_time` or `recurring` depending on whether the price is for a one-time purchase or a recurring (subscription) purchase.",
				Computed:    true,
			},
			"unit_amount": schema.Int64Attribute{
				Description: "The unit amount in cents to be charged.",
				Computed:    true,
			},
			"unit_amount_decimal": schema.StringAttribute{
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
	var state priceDataSourceModel

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
	state.ID = types.StringValue(price.ID)
	state.Active = types.BoolValue(price.Active)
	state.Currency = types.StringValue(string(price.Currency))
	state.Description = types.StringNull()

	// Handle recurring attributes safely
	if price.Recurring != nil {
		state.Interval = types.StringValue(string(price.Recurring.Interval))
		state.IntervalCount = types.Int64Value(price.Recurring.IntervalCount)
	} else {
		state.Interval = types.StringNull()
		state.IntervalCount = types.Int64Null()
	}

	state.LookupKey = types.StringValue(price.LookupKey)

	// Handle metadata
	metadata := make(map[string]attr.Value)
	for k, v := range price.Metadata {
		metadata[k] = types.StringValue(v)
	}
	state.Metadata = types.MapValueMust(types.StringType, metadata)

	state.Nickname = types.StringValue(price.Nickname)
	state.Product = types.StringValue(price.Product.ID)

	// Handle recurring
	if price.Recurring != nil {
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
		recurring := map[string]attr.Value{
			"interval":       types.StringValue(""),
			"interval_count": types.Int64Value(0),
			"usage_type":     types.StringValue(""),
		}
		state.Recurring = types.ObjectValueMust(
			map[string]attr.Type{
				"interval":       types.StringType,
				"interval_count": types.Int64Type,
				"usage_type":     types.StringType,
			},
			recurring,
		)
	}

	state.TiersMode = types.StringValue(string(price.TiersMode))

	// TransformUsage is not available in the API response
	state.TransformUsage = types.ObjectNull(
		map[string]attr.Type{
			"divide_by": types.Int64Type,
			"round":     types.StringType,
		},
	)

	state.Type = types.StringValue(string(price.Type))
	state.UnitAmount = types.Int64Value(price.UnitAmount)
	state.UnitAmountDecimal = types.StringValue(fmt.Sprintf("%.2f", price.UnitAmountDecimal))

	// Set state
	diags := resp.State.Set(ctx, &state)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	tflog.Debug(ctx, "Read Stripe price", map[string]any{"id": state.ID.ValueString()})
}
