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

	"github.com/computomatic/terraform-provider-stripe/internal/resources/account"
	"github.com/computomatic/terraform-provider-stripe/internal/resources/account_capability"
	"github.com/computomatic/terraform-provider-stripe/internal/resources/account_external_account"
	"github.com/computomatic/terraform-provider-stripe/internal/resources/apple_pay_domain"
	"github.com/computomatic/terraform-provider-stripe/internal/resources/balance"
	"github.com/computomatic/terraform-provider-stripe/internal/resources/balance_settings"
	"github.com/computomatic/terraform-provider-stripe/internal/resources/balance_transaction"
	"github.com/computomatic/terraform-provider-stripe/internal/resources/billing_alert"
	"github.com/computomatic/terraform-provider-stripe/internal/resources/billing_credit_grant"
	"github.com/computomatic/terraform-provider-stripe/internal/resources/billing_feedback_option"
	"github.com/computomatic/terraform-provider-stripe/internal/resources/billing_meter"
	"github.com/computomatic/terraform-provider-stripe/internal/resources/billing_portal_configuration"
	"github.com/computomatic/terraform-provider-stripe/internal/resources/charge"
	"github.com/computomatic/terraform-provider-stripe/internal/resources/checkout_session"
	"github.com/computomatic/terraform-provider-stripe/internal/resources/climate_order"
	"github.com/computomatic/terraform-provider-stripe/internal/resources/climate_product"
	"github.com/computomatic/terraform-provider-stripe/internal/resources/climate_supplier"
	"github.com/computomatic/terraform-provider-stripe/internal/resources/core_event_destination"
	"github.com/computomatic/terraform-provider-stripe/internal/resources/country_spec"
	"github.com/computomatic/terraform-provider-stripe/internal/resources/coupon"
	"github.com/computomatic/terraform-provider-stripe/internal/resources/credit_note"
	"github.com/computomatic/terraform-provider-stripe/internal/resources/credit_note_line"
	"github.com/computomatic/terraform-provider-stripe/internal/resources/customer"
	"github.com/computomatic/terraform-provider-stripe/internal/resources/customer_balance_transaction"
	"github.com/computomatic/terraform-provider-stripe/internal/resources/customer_cash_balance_transaction"
	"github.com/computomatic/terraform-provider-stripe/internal/resources/customer_payment_method"
	"github.com/computomatic/terraform-provider-stripe/internal/resources/dispute"
	"github.com/computomatic/terraform-provider-stripe/internal/resources/entitlements_feature"
	"github.com/computomatic/terraform-provider-stripe/internal/resources/event"
	"github.com/computomatic/terraform-provider-stripe/internal/resources/file"
	"github.com/computomatic/terraform-provider-stripe/internal/resources/file_link"
	"github.com/computomatic/terraform-provider-stripe/internal/resources/financial_connections_account"
	"github.com/computomatic/terraform-provider-stripe/internal/resources/financial_connections_session"
	"github.com/computomatic/terraform-provider-stripe/internal/resources/identity_verification_session"
	"github.com/computomatic/terraform-provider-stripe/internal/resources/invoice"
	"github.com/computomatic/terraform-provider-stripe/internal/resources/invoice_line"
	"github.com/computomatic/terraform-provider-stripe/internal/resources/invoice_payment"
	"github.com/computomatic/terraform-provider-stripe/internal/resources/invoiceitem"
	"github.com/computomatic/terraform-provider-stripe/internal/resources/issuing_card"
	"github.com/computomatic/terraform-provider-stripe/internal/resources/issuing_cardholder"
	"github.com/computomatic/terraform-provider-stripe/internal/resources/mandate"
	"github.com/computomatic/terraform-provider-stripe/internal/resources/payment_intent"
	"github.com/computomatic/terraform-provider-stripe/internal/resources/payment_link"
	"github.com/computomatic/terraform-provider-stripe/internal/resources/payment_method_configuration"
	"github.com/computomatic/terraform-provider-stripe/internal/resources/payment_method_domain"
	"github.com/computomatic/terraform-provider-stripe/internal/resources/payout"
	"github.com/computomatic/terraform-provider-stripe/internal/resources/plan"
	"github.com/computomatic/terraform-provider-stripe/internal/resources/price"
	"github.com/computomatic/terraform-provider-stripe/internal/resources/product"
	"github.com/computomatic/terraform-provider-stripe/internal/resources/product_catalog_trial_offer"
	"github.com/computomatic/terraform-provider-stripe/internal/resources/product_feature"
	"github.com/computomatic/terraform-provider-stripe/internal/resources/promotion_code"
	"github.com/computomatic/terraform-provider-stripe/internal/resources/quote"
	"github.com/computomatic/terraform-provider-stripe/internal/resources/quote_computed_upfront_line_item"
	"github.com/computomatic/terraform-provider-stripe/internal/resources/quote_line_item"
	"github.com/computomatic/terraform-provider-stripe/internal/resources/radar_early_fraud_warning"
	"github.com/computomatic/terraform-provider-stripe/internal/resources/radar_value_list"
	"github.com/computomatic/terraform-provider-stripe/internal/resources/radar_value_list_item"
	"github.com/computomatic/terraform-provider-stripe/internal/resources/refund"
	"github.com/computomatic/terraform-provider-stripe/internal/resources/reporting_report_type"
	"github.com/computomatic/terraform-provider-stripe/internal/resources/review"
	"github.com/computomatic/terraform-provider-stripe/internal/resources/setup_intent"
	"github.com/computomatic/terraform-provider-stripe/internal/resources/shipping_rate"
	"github.com/computomatic/terraform-provider-stripe/internal/resources/subscription"
	"github.com/computomatic/terraform-provider-stripe/internal/resources/subscription_item"
	"github.com/computomatic/terraform-provider-stripe/internal/resources/subscription_schedule"
	"github.com/computomatic/terraform-provider-stripe/internal/resources/tax_code"
	"github.com/computomatic/terraform-provider-stripe/internal/resources/tax_id"
	"github.com/computomatic/terraform-provider-stripe/internal/resources/tax_rate"
	"github.com/computomatic/terraform-provider-stripe/internal/resources/tax_registration"
	"github.com/computomatic/terraform-provider-stripe/internal/resources/tax_settings"
	"github.com/computomatic/terraform-provider-stripe/internal/resources/terminal_configuration"
	"github.com/computomatic/terraform-provider-stripe/internal/resources/terminal_location"
	"github.com/computomatic/terraform-provider-stripe/internal/resources/terminal_reader"
	"github.com/computomatic/terraform-provider-stripe/internal/resources/test_helpers_test_clock"
	"github.com/computomatic/terraform-provider-stripe/internal/resources/topup"
	"github.com/computomatic/terraform-provider-stripe/internal/resources/treasury_credit_reversal"
	"github.com/computomatic/terraform-provider-stripe/internal/resources/treasury_debit_reversal"
	"github.com/computomatic/terraform-provider-stripe/internal/resources/treasury_financial_account"
	"github.com/computomatic/terraform-provider-stripe/internal/resources/treasury_inbound_transfer"
	"github.com/computomatic/terraform-provider-stripe/internal/resources/treasury_outbound_payment"
	"github.com/computomatic/terraform-provider-stripe/internal/resources/treasury_outbound_transfer"
	"github.com/computomatic/terraform-provider-stripe/internal/resources/treasury_received_credit"
	"github.com/computomatic/terraform-provider-stripe/internal/resources/treasury_received_debit"
	"github.com/computomatic/terraform-provider-stripe/internal/resources/treasury_transaction"
	"github.com/computomatic/terraform-provider-stripe/internal/resources/treasury_transaction_entry"
	"github.com/computomatic/terraform-provider-stripe/internal/resources/webhook_endpoint"
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
		Description: "Every request is sent with Stripe API version `" + stripeapi.Version + "`.",
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
	return []func() resource.Resource{
		must(account.NewResource()),
		must(account_external_account.NewResource()),
		must(apple_pay_domain.NewResource()),
		must(billing_alert.NewResource()),
		must(billing_credit_grant.NewResource()),
		must(billing_feedback_option.NewResource()),
		must(billing_meter.NewResource()),
		must(billing_portal_configuration.NewResource()),
		must(core_event_destination.NewResource()),
		must(coupon.NewResource()),
		must(customer.NewResource()),
		must(customer_balance_transaction.NewResource()),
		must(customer_payment_method.NewResource()),
		must(entitlements_feature.NewResource()),
		must(file_link.NewResource()),
		must(invoiceitem.NewResource()),
		must(payment_link.NewResource()),
		must(payment_method_configuration.NewResource()),
		must(payment_method_domain.NewResource()),
		must(plan.NewResource()),
		must(price.NewResource()),
		must(product.NewResource()),
		must(product_catalog_trial_offer.NewResource()),
		must(product_feature.NewResource()),
		must(promotion_code.NewResource()),
		must(quote.NewResource()),
		must(radar_value_list.NewResource()),
		must(radar_value_list_item.NewResource()),
		must(climate_order.NewResource()),
		must(identity_verification_session.NewResource()),
		must(setup_intent.NewResource()),
		must(shipping_rate.NewResource()),
		must(subscription.NewResource()),
		must(subscription_item.NewResource()),
		must(subscription_schedule.NewResource()),
		must(terminal_reader.NewResource()),
		must(tax_id.NewResource()),
		must(tax_rate.NewResource()),
		must(tax_registration.NewResource()),
		must(terminal_configuration.NewResource()),
		must(terminal_location.NewResource()),
		must(webhook_endpoint.NewResource()),
		must(test_helpers_test_clock.NewResource()),
		must(issuing_card.NewResource()),
		must(issuing_cardholder.NewResource()),
		must(treasury_financial_account.NewResource()),
	}
}

func (p *stripeProvider) DataSources(_ context.Context) []func() datasource.DataSource {
	return []func() datasource.DataSource{
		must(account.NewDataSource()),
		must(account_capability.NewDataSource()),
		must(account_external_account.NewDataSource()),
		must(apple_pay_domain.NewDataSource()),
		must(balance.NewDataSource()),
		must(balance_settings.NewDataSource()),
		must(balance_transaction.NewDataSource()),
		must(billing_alert.NewDataSource()),
		must(billing_credit_grant.NewDataSource()),
		must(billing_feedback_option.NewDataSource()),
		must(billing_meter.NewDataSource()),
		must(billing_portal_configuration.NewDataSource()),
		must(core_event_destination.NewDataSource()),
		must(country_spec.NewDataSource()),
		must(coupon.NewDataSource()),
		must(credit_note.NewDataSource()),
		must(customer.NewDataSource()),
		must(customer_balance_transaction.NewDataSource()),
		must(customer_cash_balance_transaction.NewDataSource()),
		must(entitlements_feature.NewDataSource()),
		must(event.NewDataSource()),
		must(file.NewDataSource()),
		must(file_link.NewDataSource()),
		must(invoice.NewDataSource()),
		must(invoiceitem.NewDataSource()),
		must(payment_method_configuration.NewDataSource()),
		must(payment_method_domain.NewDataSource()),
		must(plan.NewDataSource()),
		must(price.NewDataSource()),
		must(product.NewDataSource()),
		must(product_catalog_trial_offer.NewDataSource()),
		must(product_feature.NewDataSource()),
		must(promotion_code.NewDataSource()),
		must(quote.NewDataSource()),
		must(radar_value_list.NewDataSource()),
		must(radar_value_list_item.NewDataSource()),
		must(reporting_report_type.NewDataSource()),
		must(shipping_rate.NewDataSource()),
		must(subscription.NewDataSource()),
		must(subscription_item.NewDataSource()),
		must(subscription_schedule.NewDataSource()),
		must(tax_code.NewDataSource()),
		must(tax_id.NewDataSource()),
		must(tax_rate.NewDataSource()),
		must(tax_registration.NewDataSource()),
		must(tax_settings.NewDataSource()),
		must(terminal_configuration.NewDataSource()),
		must(terminal_location.NewDataSource()),
		must(charge.NewDataSource()),
		must(checkout_session.NewDataSource()),
		must(customer_payment_method.NewDataSource()),
		must(dispute.NewDataSource()),
		must(mandate.NewDataSource()),
		must(payment_intent.NewDataSource()),
		must(payment_link.NewDataSource()),
		must(refund.NewDataSource()),
		must(setup_intent.NewDataSource()),
		must(climate_order.NewDataSource()),
		must(climate_product.NewDataSource()),
		must(climate_supplier.NewDataSource()),
		must(identity_verification_session.NewDataSource()),
		must(terminal_reader.NewDataSource()),
		must(webhook_endpoint.NewDataSource()),
		must(payout.NewDataSource()),
		must(issuing_card.NewDataSource()),
		must(issuing_cardholder.NewDataSource()),
		must(treasury_financial_account.NewDataSource()),
		must(treasury_transaction.NewDataSource()),
		must(treasury_transaction_entry.NewDataSource()),
		must(treasury_received_credit.NewDataSource()),
		must(treasury_received_debit.NewDataSource()),
		must(treasury_credit_reversal.NewDataSource()),
		must(treasury_debit_reversal.NewDataSource()),
		must(treasury_inbound_transfer.NewDataSource()),
		must(treasury_outbound_transfer.NewDataSource()),
		must(treasury_outbound_payment.NewDataSource()),
		must(financial_connections_account.NewDataSource()),
		must(financial_connections_session.NewDataSource()),
		must(topup.NewDataSource()),
		must(radar_early_fraud_warning.NewDataSource()),
		must(review.NewDataSource()),
		must(invoice_payment.NewDataSource()),
		must(invoice_line.NewDataSource()),
		must(credit_note_line.NewDataSource()),
		must(quote_line_item.NewDataSource()),
		must(quote_computed_upfront_line_item.NewDataSource()),
		must(test_helpers_test_clock.NewDataSource()),
	}
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
