// Package financial_connections_session declares the stripe_financial_connections_session data source. There is no resource.
package financial_connections_session

import (
	"github.com/hashicorp/terraform-plugin-framework/datasource"

	"github.com/computomatic/terraform-provider-stripe/internal/engine"
	"github.com/computomatic/terraform-provider-stripe/internal/generated/financial_connections_session"
)

// Declaration is what the spec cannot say about this object. A session is created for the browser flow that links accounts and cannot be updated, canceled or deleted: an unused one expires on its own (suite: "createFinancialConnectionsSession"). Its `client_secret` launches that flow, so it is sensitive. The provider reads sessions only.
var Declaration = engine.Declaration{
	TypeName:    "stripe_financial_connections_session",
	IDPrefix:    "fcsess_",
	Description: "A Financial Connections session, which a customer completes in the browser to link bank accounts. Look one up by its `id`. `client_secret` is sensitive.",
	Paths:       engine.Paths{Retrieve: "/v1/financial_connections/sessions/{id}", List: "/v1/financial_connections/sessions"},
	Delete:      engine.NoDelete,
	Sensitive:   []string{"client_secret"},
}

// NewDataSource returns the data source constructor the provider registers. It looks the object up by `id` only: Stripe documents no field of it as unique.
func NewDataSource() (func() datasource.DataSource, error) {
	return engine.NewDataSource(Declaration, financial_connections_session.Table)
}
