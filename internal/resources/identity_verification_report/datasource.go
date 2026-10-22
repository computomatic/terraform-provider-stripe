// Package identity_verification_report declares the stripe_identity_verification_report data source. There is no resource.
package identity_verification_report

import (
	"github.com/hashicorp/terraform-plugin-framework/datasource"

	"github.com/computomatic/terraform-provider-stripe/internal/engine"
	"github.com/computomatic/terraform-provider-stripe/internal/generated/identity_verification_report"
)

// Declaration is what the spec cannot say about this object. A report is the result of a person completing a verification session's hosted flow (suite: "Invoice rendering templates and verification reports are empty on the dev sandbox"), so only the data source exists. The checks hold the person's identity data, as a session's verified outputs do, so they are sensitive.
var Declaration = engine.Declaration{
	TypeName:    "stripe_identity_verification_report",
	Description: "The result of the checks of a Stripe Identity verification session. Look one up by its `id`. The checks hold the person's identity data, so `document`, `email`, `id_number`, `phone` and `selfie` are sensitive.",
	Paths:       engine.Paths{Retrieve: "/v1/identity/verification_reports/{id}", List: "/v1/identity/verification_reports"},
	Sensitive:   []string{"document", "email", "id_number", "phone", "selfie"},
	Delete:      engine.NoDelete,
}

// NewDataSource returns the data source constructor the provider registers. It looks the object up by `id` only: Stripe documents no field of it as unique.
func NewDataSource() (func() datasource.DataSource, error) {
	return engine.NewDataSource(Declaration, identity_verification_report.Table)
}
