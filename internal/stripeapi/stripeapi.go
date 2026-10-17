// Package stripeapi is the provider's client for the Stripe API.
package stripeapi

// Version is the Stripe API version the provider pins. It is sent on every request.
const Version = "2026-09-30.endive"

const (
	// DefaultBaseURL is the Stripe API host.
	DefaultBaseURL = "https://api.stripe.com"
	// DefaultFilesBaseURL is the Stripe Files API host.
	DefaultFilesBaseURL = "https://files.stripe.com"
	// DefaultMaxRetries is the number of retries per request when the user sets none.
	DefaultMaxRetries = 3
)

// Config is the resolved provider configuration that resources and data sources receive.
type Config struct {
	APIKey        string
	BaseURL       string
	FilesBaseURL  string
	StripeAccount string
	MaxRetries    int64
}
