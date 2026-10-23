// Package topup declares the stripe_topup resource and data source.
package topup

import (
	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/resource"

	"github.com/computomatic/terraform-provider-stripe/internal/engine"
	"github.com/computomatic/terraform-provider-stripe/internal/generated/topup"
)

// Declaration is what the spec cannot say about a top-up (ADR
// 2026-10-22-money-moving-resources, D6.8). It moves money, so the live-mode guard applies.
// Stripe cannot delete one, so destroy is refused. `status` cancels a pending top-up (suite:
// "cancel", "already canceled"). `description` and `metadata` update (suite: "update"), as the
// spec's update says.
var Declaration = engine.Declaration{
	TypeName:              "stripe_topup",
	IDPrefix:              "tu_",
	Description:           "A top-up of your Stripe balance from your bank account. Set `status` to `canceled` to cancel a top-up that is still pending. Stripe moves a pending top-up to `succeeded` by itself. Only `description` and `metadata` change after creation.",
	DataSourceDescription: "A top-up of your Stripe balance from a bank account or source.",
	Paths: engine.Paths{
		Create:   "/v1/topups",
		Retrieve: "/v1/topups/{id}",
		Update:   "/v1/topups/{id}",
		List:     "/v1/topups",
	},
	MovesMoney:   true,
	Delete:       engine.Refuse,
	RefuseReason: "Stripe cannot delete a top-up; set `status` to `canceled` to cancel a pending one.",
	Transitions: []engine.Transition{
		{Field: "status", Value: "canceled", Path: "/v1/topups/{id}/cancel", From: []string{"pending"}, Final: []string{"succeeded", "failed", "reversed"}},
	},
}

// NewResource returns the resource constructor the provider registers.
func NewResource() (func() resource.Resource, error) {
	return engine.NewResource(Declaration, topup.Table)
}

// NewDataSource returns the data source constructor. It looks the object up by `id` only: Stripe documents no field of it as unique.
func NewDataSource() (func() datasource.DataSource, error) {
	return engine.NewDataSource(Declaration, topup.Table)
}
