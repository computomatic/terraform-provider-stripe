// Package provider defines the Stripe provider: its schema and configuration.
package provider

import (
	"context"
	"fmt"
	"net/url"
	"os"
	"strings"

	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/provider"
	"github.com/hashicorp/terraform-plugin-framework/provider/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/computomatic/terraform-provider-stripe/internal/resources/product"
	"github.com/computomatic/terraform-provider-stripe/internal/stripeapi"
)

var _ provider.Provider = &stripeProvider{}

// New returns a factory for the provider at the given release version.
func New(version string) func() provider.Provider {
	return func() provider.Provider {
		return &stripeProvider{version: version}
	}
}

type stripeProvider struct {
	version string
}

type stripeProviderModel struct {
	APIKey        types.String `tfsdk:"api_key"`
	BaseURL       types.String `tfsdk:"base_url"`
	FilesBaseURL  types.String `tfsdk:"files_base_url"`
	StripeAccount types.String `tfsdk:"stripe_account"`
	MaxRetries    types.Int64  `tfsdk:"max_retries"`
}

func (p *stripeProvider) Metadata(_ context.Context, _ provider.MetadataRequest, resp *provider.MetadataResponse) {
	resp.TypeName = "stripe"
	resp.Version = p.version
}

func (p *stripeProvider) Schema(_ context.Context, _ provider.SchemaRequest, resp *provider.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: "Manages Stripe objects. Every request is sent with Stripe API version " + stripeapi.Version + ".",
		Attributes: map[string]schema.Attribute{
			"api_key": schema.StringAttribute{
				Optional:    true,
				Sensitive:   true,
				Description: "A Stripe secret or restricted key (`sk_` or `rk_`). Defaults to the `STRIPE_API_KEY` environment variable.",
			},
			"base_url": schema.StringAttribute{
				Optional:    true,
				Description: "Base URL of the Stripe API, without a path. Defaults to the `STRIPE_API_BASE` environment variable, then `" + stripeapi.DefaultBaseURL + "`.",
			},
			"files_base_url": schema.StringAttribute{
				Optional:    true,
				Description: "Base URL of the Stripe Files API, without a path. Defaults to the `STRIPE_FILES_BASE_URL` environment variable, then `base_url` when that is set, then `" + stripeapi.DefaultFilesBaseURL + "`.",
			},
			"stripe_account": schema.StringAttribute{
				Optional:    true,
				Description: "A connected account ID, sent as the `Stripe-Account` header. Defaults to the `STRIPE_ACCOUNT` environment variable.",
			},
			"max_retries": schema.Int64Attribute{
				Optional:    true,
				Description: fmt.Sprintf("Retries per request on transport errors, 429 and 5xx responses. Defaults to %d.", stripeapi.DefaultMaxRetries),
			},
		},
	}
}

func (p *stripeProvider) Configure(ctx context.Context, req provider.ConfigureRequest, resp *provider.ConfigureResponse) {
	var config stripeProviderModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &config)...)
	if resp.Diagnostics.HasError() {
		return
	}

	for name, value := range map[string]types.String{
		"api_key":        config.APIKey,
		"base_url":       config.BaseURL,
		"files_base_url": config.FilesBaseURL,
		"stripe_account": config.StripeAccount,
	} {
		if value.IsUnknown() {
			resp.Diagnostics.AddAttributeError(path.Root(name), "Unknown value",
				fmt.Sprintf("The provider cannot be configured while %s is unknown. Set it statically or apply its source first.", name))
		}
	}
	if config.MaxRetries.IsUnknown() {
		resp.Diagnostics.AddAttributeError(path.Root("max_retries"), "Unknown value",
			"The provider cannot be configured while max_retries is unknown. Set it statically or apply its source first.")
	}
	if resp.Diagnostics.HasError() {
		return
	}

	cfg, diags := resolveConfig(config, os.Getenv)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	client := stripeapi.New(cfg, stripeapi.Options{Version: p.version})
	resp.DataSourceData = client
	resp.ResourceData = client
}

func (p *stripeProvider) Resources(_ context.Context) []func() resource.Resource {
	return []func() resource.Resource{must(product.NewResource())}
}

func (p *stripeProvider) DataSources(_ context.Context) []func() datasource.DataSource {
	return []func() datasource.DataSource{must(product.NewDataSource())}
}

// must unwraps a constructor built from a declaration. A declaration that does not fit its
// field table is a programming error, and the provider's tests construct every one.
func must[T any](constructor T, err error) T {
	if err != nil {
		panic(err)
	}
	return constructor
}

// resolveConfig applies environment fallbacks and defaults, and validates the result.
// It expects no unknown values.
func resolveConfig(config stripeProviderModel, getenv func(string) string) (stripeapi.Config, diag.Diagnostics) {
	var diags diag.Diagnostics

	apiKey := stringOrEnv(config.APIKey, getenv, "STRIPE_API_KEY")
	switch {
	case apiKey == "":
		diags.AddAttributeError(path.Root("api_key"), "Missing Stripe API key",
			"Set api_key in the provider configuration or the STRIPE_API_KEY environment variable.")
	case !strings.HasPrefix(apiKey, "sk_") && !strings.HasPrefix(apiKey, "rk_"):
		diags.AddAttributeError(path.Root("api_key"), "Invalid Stripe API key",
			"The key must be a secret or restricted key, starting with sk_ or rk_.")
	}

	baseURL := stringOrEnv(config.BaseURL, getenv, "STRIPE_API_BASE")
	baseURLSet := baseURL != ""
	if baseURLSet {
		validateBaseURL(&diags, "base_url", baseURL)
	} else {
		baseURL = stripeapi.DefaultBaseURL
	}

	filesBaseURL := stringOrEnv(config.FilesBaseURL, getenv, "STRIPE_FILES_BASE_URL")
	switch {
	case filesBaseURL != "":
		validateBaseURL(&diags, "files_base_url", filesBaseURL)
	case baseURLSet:
		filesBaseURL = baseURL
	default:
		filesBaseURL = stripeapi.DefaultFilesBaseURL
	}

	maxRetries := int64(stripeapi.DefaultMaxRetries)
	if !config.MaxRetries.IsNull() {
		maxRetries = config.MaxRetries.ValueInt64()
		if maxRetries < 0 {
			diags.AddAttributeError(path.Root("max_retries"), "Invalid max_retries", "max_retries must not be negative.")
		}
	}

	return stripeapi.Config{
		APIKey:        apiKey,
		BaseURL:       strings.TrimSuffix(baseURL, "/"),
		FilesBaseURL:  strings.TrimSuffix(filesBaseURL, "/"),
		StripeAccount: stringOrEnv(config.StripeAccount, getenv, "STRIPE_ACCOUNT"),
		MaxRetries:    maxRetries,
	}, diags
}

func stringOrEnv(value types.String, getenv func(string) string, name string) string {
	if !value.IsNull() {
		return value.ValueString()
	}
	return getenv(name)
}

func validateBaseURL(diags *diag.Diagnostics, attribute, raw string) {
	u, err := url.Parse(raw)
	switch {
	case err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "":
		diags.AddAttributeError(path.Root(attribute), "Invalid URL",
			fmt.Sprintf("%s must be an http or https URL with a host, got %q.", attribute, raw))
	case strings.Trim(u.Path, "/") != "" || u.RawQuery != "" || u.Fragment != "":
		diags.AddAttributeError(path.Root(attribute), "Invalid URL",
			fmt.Sprintf("%s must not have a path, query or fragment, got %q.", attribute, raw))
	}
}
