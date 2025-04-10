package provider

import (
	"context"
	"fmt"

	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/datasource/schema"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/stripe/stripe-go/v82/client"
)

// Ensure the implementation satisfies the expected interfaces.
var (
	_ datasource.DataSource              = &balanceDataSource{}
	_ datasource.DataSourceWithConfigure = &balanceDataSource{}
)

// NewCoffeesDataSource is a helper function to simplify the provider implementation.
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
	Livemode         types.Bool           `tfsdk:"livemode"`
	Object           types.String         `tfsdk:"object"`
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
	Pending   types.Int64 `tfsdk:"pending"`
}

// Schema defines the schema for the data source.
func (d *balanceDataSource) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Attributes: map[string]schema.Attribute{
			"available": schema.ListNestedAttribute{
				NestedObject: schema.NestedAttributeObject{
					Attributes: map[string]schema.Attribute{
						"amount": schema.Int64Attribute{
							Computed: true,
						},
						"currency": schema.StringAttribute{
							Computed: true,
						},
					},
				},
				Computed: true,
			},
			"connect_reserved": schema.ListNestedAttribute{
				NestedObject: schema.NestedAttributeObject{
					Attributes: map[string]schema.Attribute{
						"amount": schema.Int64Attribute{
							Computed: true,
						},
						"currency": schema.StringAttribute{
							Computed: true,
						},
					},
				},
				Computed: true,
			},
			"instant_available": schema.ListNestedAttribute{
				NestedObject: schema.NestedAttributeObject{
					Attributes: map[string]schema.Attribute{
						"amount": schema.Int64Attribute{
							Computed: true,
						},
						"currency": schema.StringAttribute{
							Computed: true,
						},
					},
				},
				Computed: true,
			},
			"issuing": schema.SingleNestedAttribute{
				Attributes: map[string]schema.Attribute{
					"available": schema.Int64Attribute{
						Computed: true,
					},
					"pending": schema.Int64Attribute{
						Computed: true,
					},
				},
				Computed: true,
			},
			"livemode": schema.BoolAttribute{
				Computed: true,
			},
			"object": schema.StringAttribute{
				Computed: true,
			},
			"pending": schema.ListNestedAttribute{
				NestedObject: schema.NestedAttributeObject{
					Attributes: map[string]schema.Attribute{
						"amount": schema.Int64Attribute{
							Computed: true,
						},
						"currency": schema.StringAttribute{
							Computed: true,
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
	// Add a nil check when handling ProviderData because Terraform
	// sets that data after it calls the ConfigureProvider RPC.
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

	balance, err := d.client.Balance.Get(nil)
	if err != nil {
		resp.Diagnostics.AddError(
			"Unable to Read Stripe balance",
			err.Error(),
		)
		return
	}

	// Map response body to model
	for _, amount := range balance.Available {
		state.Available = append(state.Available, balanceAmountModel{
			Amount:   types.Int64Value(amount.Amount),
			Currency: types.StringValue(string(amount.Currency)),
		})
	}

	for _, amount := range balance.ConnectReserved {
		state.ConnectReserved = append(state.ConnectReserved, balanceAmountModel{
			Amount:   types.Int64Value(amount.Amount),
			Currency: types.StringValue(string(amount.Currency)),
		})
	}

	for _, amount := range balance.InstantAvailable {
		state.InstantAvailable = append(state.InstantAvailable, balanceAmountModel{
			Amount:   types.Int64Value(amount.Amount),
			Currency: types.StringValue(string(amount.Currency)),
		})
	}

	if balance.Issuing != nil {
		state.Issuing = &balanceIssuingModel{
			Available: types.Int64Value(balance.Issuing.Available[0].Amount),
		}
	}

	for _, amount := range balance.Pending {
		state.Pending = append(state.Pending, balanceAmountModel{
			Amount:   types.Int64Value(amount.Amount),
			Currency: types.StringValue(string(amount.Currency)),
		})
	}

	state.Livemode = types.BoolValue(balance.Livemode)
	state.Object = types.StringValue(balance.Object)
	state.Pending = append(state.Pending, balanceAmountModel{
		Amount:   types.Int64Value(balance.Pending[0].Amount),
		Currency: types.StringValue(string(balance.Pending[0].Currency)),
	})

	// Set state
	diags := resp.State.Set(ctx, &state)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}
}
