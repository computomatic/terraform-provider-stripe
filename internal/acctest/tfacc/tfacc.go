// Package tfacc connects the harness to terraform-plugin-testing: the provider factories
// and the typed client tests use to change or inspect objects behind the provider's back.
// It is separate from acctest because it imports the provider, which the engine's own
// tests (which use acctest) must not.
package tfacc

import (
	"errors"
	"strings"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/providerserver"
	"github.com/hashicorp/terraform-plugin-go/tfprotov6"

	"github.com/computomatic/terraform-provider-stripe/internal/acctest"
	"github.com/computomatic/terraform-provider-stripe/internal/provider"
	"github.com/computomatic/terraform-provider-stripe/internal/stripeapi"
	"github.com/computomatic/terraform-provider-stripe/internal/wire"
)

// ProviderFactories serves the provider over protocol 6, as the release binary does, in strict
// mode, so that a test fails where Stripe or the fake answers otherwise than planned or than the
// conformance suite recorded.
var ProviderFactories = map[string]func() (tfprotov6.ProviderServer, error){
	"stripe": func() (tfprotov6.ProviderServer, error) {
		return providerserver.NewProtocol6WithError(provider.NewStrict("test")())()
	},
}

// Client returns a Stripe client for the test's account, or for its connected account when c
// names one. Tests use it to change an object out of band (drift) and to check what is left
// after destroy.
func Client(c acctest.Config) *stripeapi.Client {
	return stripeapi.New(stripeapi.Config{APIKey: c.APIKey(), BaseURL: c.BaseURL, FilesBaseURL: c.FilesBaseURL(), StripeAccount: c.StripeAccount, MaxRetries: 3}, stripeapi.Options{Version: "test"})
}

// Post sends an update to an object behind the provider's back.
func Post(t *testing.T, c acctest.Config, path string, params wire.Object) {
	t.Helper()
	if _, err := Client(c).Do(t.Context(), stripeapi.Request{Method: "POST", Path: path, Params: params}); err != nil {
		t.Fatalf("POST %s: %v", path, err)
	}
}

// SkipWhenFakeLacks returns an ErrorCheck that skips the test when the fake answers
// `fake_not_implemented` or `fake_unproven` for something that mentions needle, such as a
// parameter name. Any other error, a fake crash included, fails the test. The provider never works around a fake
// gap: the test runs again once the fake serves it. Real Stripe has no such gaps, so in
// smoke mode every error passes through.
func SkipWhenFakeLacks(t *testing.T, needle string) func(error) error {
	return func(err error) error {
		if err == nil || acctest.Smoke() {
			return err
		}
		msg := err.Error()
		if (strings.Contains(msg, "fake_not_implemented") || strings.Contains(msg, "fake_unproven")) && strings.Contains(msg, needle) {
			t.Skipf("the fake does not serve %s yet: %v", needle, err)
		}
		return err
	}
}

// SkipUnlessFakeAnswers skips the test when the fake replies `fake_not_implemented` or
// `fake_unproven` to a GET of path, for a test whose step expects Stripe's own answer to it
// (an ErrorCheck cannot skip a step that has an ExpectError). Any other outcome, an error from
// Stripe included, lets the test run.
func SkipUnlessFakeAnswers(t *testing.T, c acctest.Config, path string) {
	t.Helper()
	SkipUnlessFakeServes(t, c, "GET", path, nil)
}

// SkipUnlessFakeServes is SkipUnlessFakeAnswers for any method and parameters. A test of a
// resource whose destroy is refused probes this way before it creates anything, because a test
// that skips after the create leaves the object in state, and the closing destroy fails on it.
func SkipUnlessFakeServes(t *testing.T, c acctest.Config, method, path string, params wire.Object) {
	t.Helper()
	if acctest.Smoke() {
		return
	}
	_, err := Client(c).Do(t.Context(), stripeapi.Request{Method: method, Path: path, Params: params})
	var apiErr *stripeapi.APIError
	if errors.As(err, &apiErr) && (apiErr.Code == "fake_not_implemented" || apiErr.Code == "fake_unproven") {
		t.Skipf("the fake does not serve %s %s yet: %v", method, path, err)
	}
}
