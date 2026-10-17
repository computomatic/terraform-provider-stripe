// Package tfacc connects the harness to terraform-plugin-testing: the provider factories
// and the typed client tests use to change or inspect objects behind the provider's back.
// It is separate from acctest because it imports the provider, which the engine's own
// tests (which use acctest) must not.
package tfacc

import (
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/providerserver"
	"github.com/hashicorp/terraform-plugin-go/tfprotov6"

	"github.com/computomatic/terraform-provider-stripe/internal/acctest"
	"github.com/computomatic/terraform-provider-stripe/internal/provider"
	"github.com/computomatic/terraform-provider-stripe/internal/stripeapi"
	"github.com/computomatic/terraform-provider-stripe/internal/wire"
)

// ProviderFactories serves the provider over protocol 6, as the release binary does.
var ProviderFactories = map[string]func() (tfprotov6.ProviderServer, error){
	"stripe": func() (tfprotov6.ProviderServer, error) {
		return providerserver.NewProtocol6WithError(provider.New("test")())()
	},
}

// Client returns a Stripe client for the test's account. Tests use it to change an
// object out of band (drift) and to check what is left after destroy.
func Client(c acctest.Config) *stripeapi.Client {
	return stripeapi.New(stripeapi.Config{APIKey: c.Key, BaseURL: c.BaseURL, MaxRetries: 3}, stripeapi.Options{Version: "test"})
}

// Post sends an update to an object behind the provider's back.
func Post(t *testing.T, c acctest.Config, path string, params wire.Object) {
	t.Helper()
	if _, err := Client(c).Do(t.Context(), stripeapi.Request{Method: "POST", Path: path, Params: params}); err != nil {
		t.Fatalf("POST %s: %v", path, err)
	}
}
