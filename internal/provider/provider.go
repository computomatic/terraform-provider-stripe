package provider

import (
	"context"
	"os"

	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/provider"
	"github.com/hashicorp/terraform-plugin-framework/provider/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/stripe/stripe-go/v82/client"
)

var (
    _ provider.Provider = &stripeProvider{}
)

func New(version string) func() provider.Provider {
    return func() provider.Provider {
        return &stripeProvider{
            version: version,
        }
    }
}

type stripeProvider struct {
    version string
}

type stripeProviderModel struct {
	ApiKey types.String `tfsdk:"api_key"`
}

func (p *stripeProvider) Metadata(_ context.Context, _ provider.MetadataRequest, resp *provider.MetadataResponse) {
    resp.TypeName = "stripe"
    resp.Version = p.version
}

func (p *stripeProvider) Schema(_ context.Context, _ provider.SchemaRequest, resp *provider.SchemaResponse) {
	resp.Schema = schema.Schema{
			Attributes: map[string]schema.Attribute{
				"api_key": schema.StringAttribute{
					Optional: true,
					Sensitive: true,
				},
			},
	}
}

func (p *stripeProvider) Configure(ctx context.Context, req provider.ConfigureRequest, resp *provider.ConfigureResponse) {
	var config stripeProviderModel
	diags := req.Config.Get(ctx, &config)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
			return
	}

	if config.ApiKey.IsUnknown() {
			resp.Diagnostics.AddAttributeError(
					path.Root("api_key"),
					"Unknown Stripe API Key",
					"The provider cannot create the Stripe API client as there is an unknown configuration value for the Stripe API key. "+
							"Either target apply the source of the value first, set the value statically in the configuration, or use the STRIPE_API_KEY environment variable.",
			)
	}

	if resp.Diagnostics.HasError() {
			return
	}

	apiKey := os.Getenv("STRIPE_API_KEY")

	if !config.ApiKey.IsNull() {
			apiKey = config.ApiKey.ValueString()
	}

	if apiKey == "" {
			resp.Diagnostics.AddAttributeError(
					path.Root("api_key"),
					"Missing Stripe API Key",
					"The provider cannot create the Stripe API client as there is a missing or empty value for the Stripe API key. "+
							"Set the api_key value in the configuration or use the STRIPE_API_KEY environment variable. "+
							"If either is already set, ensure the value is not empty.",
			)
	}

	if resp.Diagnostics.HasError() {
			return
	}

	client := &client.API{}
	client.Init(apiKey, nil)

	resp.DataSourceData = client
	resp.ResourceData = client
}

func (p *stripeProvider) DataSources(_ context.Context) []func() datasource.DataSource {
  return []func() datasource.DataSource {
    NewBalanceDataSource,
  }
}

func (p *stripeProvider) Resources(_ context.Context) []func() resource.Resource {
    return nil
}
