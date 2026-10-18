// Package identity_verification_session declares the stripe_identity_verification_session
// resource and data source.
package identity_verification_session

import (
	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/resource"

	"github.com/computomatic/terraform-provider-stripe/internal/engine"
	"github.com/computomatic/terraform-provider-stripe/internal/generated/identity_verification_session"
	"github.com/computomatic/terraform-provider-stripe/internal/wire"
)

// Declaration is what the spec cannot say about a verification session. Stripe never deletes
// one, so destroy cancels it (suite: "IdentityVerificationSession", `cancel`) and drops it
// from state. A canceled session cancels again without error ("already canceled is
// idempotent"), but destroy reads the status first anyway. Stripe cancels only a session that
// still requires input, so when the cancel fails and a retrieve shows the session verified,
// destroy warns, leaves it and drops it from state. Any other failure fails the destroy.
var Declaration = engine.Declaration{
	TypeName:    "stripe_identity_verification_session",
	IDPrefix:    "vs_",
	Description: "A Stripe Identity check of one person's document or ID number. `terraform destroy` cancels the session, because Stripe cannot delete one; the canceled session stays in the account, and a session that is already verified cannot be canceled and is left as it is, with a warning. The `client_secret` and `url` that start the check are sensitive and stay in the state. Redacting a session is not a resource: redact it with Stripe's API, after which Stripe no longer serves it. Verification flows and related persons are not available, so `type` is required. `type` and `metadata` change in place; changing `options` or `provided_details`, or any argument that only a create takes, creates a new session. Stripe does not return `provided_details`, so an imported session has none in state and the next plan replaces it if the configuration sets some.",
	Paths: engine.Paths{
		Create:   "/v1/identity/verification_sessions",
		Retrieve: "/v1/identity/verification_sessions/{id}",
		Update:   "/v1/identity/verification_sessions/{id}",
		List:     "/v1/identity/verification_sessions",
	},
	Delete: engine.Archive,
	Archive: &engine.ArchiveAction{
		Field: "status",
		Value: wire.String("canceled"),
		Path:  "/v1/identity/verification_sessions/{id}/cancel",
	},
	KeepWhen: &engine.FieldIs{Field: "status", Value: wire.String("verified")},
	// Stripe returns the client secret and the URL while the session requires input, and
	// nulls both once it is canceled.
	OnceReturned: []string{"client_secret", "url"},
	// The verified outputs are the person's identity data.
	Sensitive: []string{"verified_outputs"},
	// Stripe accepts `provided_details` and never returns it (suite: "create with optional
	// fields"). The suite shows the options echoed on create and accepted on update, but not
	// that an update changes them, so a change replaces the session.
	PreserveConfig: []string{"provided_details"},
	ReplaceOn:      []string{"options", "provided_details"},
	// Stripe returns `options` as an empty object when none is set (suite: "create minimal"),
	// and fills in the checks of a document option the configuration leaves out.
	ServerDefaulted: []string{"options", "options.document.allowed_types", "options.document.require_id_number", "options.document.require_live_capture", "options.document.require_matching_selfie"},
}

// NewResource returns the resource constructor the provider registers.
func NewResource() (func() resource.Resource, error) {
	return engine.NewResource(Declaration, identity_verification_session.Table)
}

// NewDataSource returns the data source constructor. The list endpoint filters by no unique field of a session, so the data source looks up by `id` only.
func NewDataSource() (func() datasource.DataSource, error) {
	return engine.NewDataSource(Declaration, identity_verification_session.Table)
}
