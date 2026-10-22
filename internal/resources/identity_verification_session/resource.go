// Package identity_verification_session declares the stripe_identity_verification_session
// resource and data source.
package identity_verification_session

import (
	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/resource"

	"github.com/computomatic/terraform-provider-stripe/internal/engine"
	"github.com/computomatic/terraform-provider-stripe/internal/generated/identity_verification_session"
)

// Declaration is what the spec cannot say about a verification session. Stripe never deletes
// one, and canceling one is final, so destroy is refused. Setting `status` to `canceled`
// cancels it (suite: "IdentityVerificationSession", `cancel`); Stripe cancels only a session
// that still requires input.
var Declaration = engine.Declaration{
	TypeName:              "stripe_identity_verification_session",
	IDPrefix:              "vs_",
	Description:           "A Stripe Identity check of one person's document or ID number. Set `status` to `canceled` to cancel a session that still requires input. The `client_secret` and `url` that start the check are sensitive and stay in the state. Redacting a session is not a resource: redact it with Stripe's API, after which Stripe no longer serves it. Verification flows and related persons are not available, so `type` is required. `type` and `metadata` change in place; changing `options` or `provided_details`, or any argument that only a create takes, fails the plan.",
	DataSourceDescription: "A Stripe Identity check of one person's document or ID number.",
	ImportNote:            "Stripe does not return `provided_details`, so an imported session has none in state and the next plan replaces it if the configuration sets some.",
	ImportNoteLevel:       engine.NoteWarning,
	Paths: engine.Paths{
		Create:   "/v1/identity/verification_sessions",
		Retrieve: "/v1/identity/verification_sessions/{id}",
		Update:   "/v1/identity/verification_sessions/{id}",
		List:     "/v1/identity/verification_sessions",
	},
	Delete:       engine.Refuse,
	RefuseReason: "Stripe cannot delete a verification session; set `status` to `canceled` to cancel one that still requires input.",
	Transitions:  []engine.Transition{{Field: "status", Value: "canceled", Path: "/v1/identity/verification_sessions/{id}/cancel"}},
	// Stripe returns the client secret and the URL while the session requires input, and
	// nulls both once it is canceled.
	OnceReturned: []string{"client_secret", "url"},
	// The verified outputs are the person's identity data.
	Sensitive: []string{"verified_outputs"},
	// Stripe accepts `provided_details` and never returns it (suite: "create with optional
	// fields"). The suite shows the options echoed on create and accepted on update, but not
	// that an update changes them, so they are treated as create-only.
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
