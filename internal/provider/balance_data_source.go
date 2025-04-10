package provider

import (
	"context"
	"fmt"

	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/datasource/schema"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-log/tflog"
	"github.com/stripe/stripe-go/v82/client"
)

// Ensure the implementation satisfies the expected interfaces.
var (
	_ datasource.DataSource              = &balanceDataSource{}
	_ datasource.DataSourceWithConfigure = &balanceDataSource{}
)

// NewBalanceDataSource is a helper function to simplify the provider implementation.
func NewBalanceDataSource() datasource.DataSource {
	return &balanceDataSource{}
}

// balanceDataSource is the data source implementation.
type balanceDataSource struct {
	client *client.API
}

// Metadata returns the data source type name.
func (d *balanceDataSource) Metadata(_ context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_balance"
}

// balanceDataSourceModel maps the data source schema data.
type balanceDataSourceModel struct {
	Available        []balanceAmountModel `tfsdk:"available"`
	ConnectReserved  []balanceAmountModel `tfsdk:"connect_reserved"`
	InstantAvailable []balanceAmountModel `tfsdk:"instant_available"`
	Issuing          *balanceIssuingModel `tfsdk:"issuing"`
	Pending          []balanceAmountModel `tfsdk:"pending"`
}

// balanceAmountModel represents a balance amount for a specific currency
type balanceAmountModel struct {
	Amount   types.Int64  `tfsdk:"amount"`
	Currency types.String `tfsdk:"currency"`
}

// balanceIssuingModel represents the issuing portion of a balance
type balanceIssuingModel struct {
	Available types.Int64 `tfsdk:"available"`
}

// Schema defines the schema for the data source.
func (d *balanceDataSource) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: "Use this data source to get information about your Stripe account balance.",
		Attributes: map[string]schema.Attribute{
			"available": schema.ListNestedAttribute{
				Description: "Available balance for each currency.",
				NestedObject: schema.NestedAttributeObject{
					Attributes: map[string]schema.Attribute{
						"amount": schema.Int64Attribute{
							Description: "Amount available in the smallest currency unit (e.g., cents for USD).",
							Computed:    true,
						},
						"currency": schema.StringAttribute{
							Description: "Three-letter ISO currency code (e.g., 'usd').",
							Computed:    true,
						},
					},
				},
				Computed: true,
			},
			"connect_reserved": schema.ListNestedAttribute{
				Description: "Balance reserved for Connect accounts.",
				NestedObject: schema.NestedAttributeObject{
					Attributes: map[string]schema.Attribute{
						"amount": schema.Int64Attribute{
							Description: "Amount reserved in the smallest currency unit.",
							Computed:    true,
						},
						"currency": schema.StringAttribute{
							Description: "Three-letter ISO currency code.",
							Computed:    true,
						},
					},
				},
				Computed: true,
			},
			"instant_available": schema.ListNestedAttribute{
				Description: "Balance available for instant payouts.",
				NestedObject: schema.NestedAttributeObject{
					Attributes: map[string]schema.Attribute{
						"amount": schema.Int64Attribute{
							Description: "Amount available in the smallest currency unit.",
							Computed:    true,
						},
						"currency": schema.StringAttribute{
							Description: "Three-letter ISO currency code.",
							Computed:    true,
						},
					},
				},
				Computed: true,
			},
			"issuing": schema.SingleNestedAttribute{
				Description: "Balance information for Stripe Issuing.",
				Attributes: map[string]schema.Attribute{
					"available": schema.Int64Attribute{
						Description: "Available balance for issuing operations.",
						Computed:    true,
					},
				},
				Computed: true,
			},
			"pending": schema.ListNestedAttribute{
				Description: "Pending balance for each currency.",
				NestedObject: schema.NestedAttributeObject{
					Attributes: map[string]schema.Attribute{
						"amount": schema.Int64Attribute{
							Description: "Amount pending in the smallest currency unit.",
							Computed:    true,
						},
						"currency": schema.StringAttribute{
							Description: "Three-letter ISO currency code.",
							Computed:    true,
						},
					},
				},
				Computed: true,
			},
		},
	}
}

// Configure adds the provider configured client to the data source.
func (d *balanceDataSource) Configure(_ context.Context, req datasource.ConfigureRequest, resp *datasource.ConfigureResponse) {
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
func (d *balanceDataSource) Read(ctx context.Context, req datasource.ReadRequest, resp *datasource.ReadResponse) {
	var state balanceDataSourceModel

	tflog.Debug(ctx, "Reading Stripe balance")

	balance, err := d.client.Balance.Get(nil)
	if err != nil {
		resp.Diagnostics.AddError(
			"Error reading Stripe balance",
			fmt.Sprintf("Could not read Stripe balance: %s", err.Error()),
		)
		return
	}

	// Map response body to model
	state.Available = make([]balanceAmountModel, len(balance.Available))
	for i, amount := range balance.Available {
		state.Available[i] = balanceAmountModel{
			Amount:   types.Int64Value(amount.Amount),
			Currency: types.StringValue(string(amount.Currency)),
		}
	}

	state.ConnectReserved = make([]balanceAmountModel, len(balance.ConnectReserved))
	for i, amount := range balance.ConnectReserved {
		state.ConnectReserved[i] = balanceAmountModel{
			Amount:   types.Int64Value(amount.Amount),
			Currency: types.StringValue(string(amount.Currency)),
		}
	}

	state.InstantAvailable = make([]balanceAmountModel, len(balance.InstantAvailable))
	for i, amount := range balance.InstantAvailable {
		state.InstantAvailable[i] = balanceAmountModel{
			Amount:   types.Int64Value(amount.Amount),
			Currency: types.StringValue(string(amount.Currency)),
		}
	}

	if balance.Issuing != nil {
		state.Issuing = &balanceIssuingModel{
			Available: types.Int64Value(balance.Issuing.Available[0].Amount),
		}
	}

	state.Pending = make([]balanceAmountModel, len(balance.Pending))
	for i, amount := range balance.Pending {
		state.Pending[i] = balanceAmountModel{
			Amount:   types.Int64Value(amount.Amount),
			Currency: types.StringValue(string(amount.Currency)),
		}
	}

	// Set state
	diags := resp.State.Set(ctx, &state)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	tflog.Debug(ctx, "Read Stripe balance")
}
