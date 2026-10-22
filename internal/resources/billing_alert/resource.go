// Package billing_alert declares the stripe_billing_alert resource and data source.
package billing_alert

import (
	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/resource"

	"github.com/computomatic/terraform-provider-stripe/internal/engine"
	"github.com/computomatic/terraform-provider-stripe/internal/generated/billing_alert"
	"github.com/computomatic/terraform-provider-stripe/internal/wire"
)

// Declaration is what the spec cannot say about a billing alert. Stripe never updates an
// alert, so every argument but `status` forces replacement. `status` moves between `active`
// and `inactive` through action endpoints (suite: "deactivate and activate"), each a no-op
// when the alert is already there. Destroy deactivates the alert, which is reversible. Setting
// `status` to `archived` archives it (suite: "archive"), which is final (suite: "archived alert
// cannot be reactivated"), so destroy and replacement of an archived alert are refused; an
// archived alert can still be retrieved. A
// missing alert is a 404 with no error code, which the engine reads as gone by its status.
var Declaration = engine.Declaration{
	TypeName:              "stripe_billing_alert",
	IDPrefix:              "alrt_",
	Description:           "Notifies you when the usage a billing meter records crosses a threshold. `terraform destroy` deactivates the alert, because Stripe cannot delete one; it stays in the Stripe account. Stripe does not update alerts, so every argument except `status` forces replacement. `status` can be set to `active`, `inactive` or `archived`; an archived alert never triggers or activates again.",
	DataSourceDescription: "Notifies you when the usage a billing meter records crosses a threshold.",
	Paths: engine.Paths{
		Create:   "/v1/billing/alerts",
		Retrieve: "/v1/billing/alerts/{id}",
		List:     "/v1/billing/alerts",
	},
	Delete: engine.Archive,
	Archive: &engine.ArchiveAction{
		Field: "status",
		Value: wire.String("inactive"),
		Path:  "/v1/billing/alerts/{id}/deactivate",
	},
	RefuseWhen: &engine.RefuseWhen{
		Field:  "status",
		Value:  wire.String("archived"),
		Reason: "Stripe cannot deactivate an archived alert, because archiving one is final.",
	},
	Transitions: []engine.Transition{
		{Field: "status", Value: "active", Path: "/v1/billing/alerts/{id}/activate", Final: []string{"archived"}},
		{Field: "status", Value: "inactive", Path: "/v1/billing/alerts/{id}/deactivate", Final: []string{"archived"}},
		{Field: "status", Value: "archived", Path: "/v1/billing/alerts/{id}/archive"},
	},
}

// NewResource returns the resource constructor the provider registers.
func NewResource() (func() resource.Resource, error) {
	return engine.NewResource(Declaration, billing_alert.Table)
}

// NewDataSource returns the data source constructor. Stripe documents no alert field as unique, so the data source looks up by `id` only.
func NewDataSource() (func() datasource.DataSource, error) {
	return engine.NewDataSource(Declaration, billing_alert.Table)
}
