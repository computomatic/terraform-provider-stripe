// Package payment_record declares the stripe_payment_record data source. There is no resource.
package payment_record

import (
	"github.com/hashicorp/terraform-plugin-framework/datasource"

	"github.com/computomatic/terraform-provider-stripe/internal/engine"
	"github.com/computomatic/terraform-provider-stripe/internal/generated/payment_record"
)

// Declaration is what the spec cannot say about this object. A record is made by reporting a payment taken outside Stripe and changes by reporting its attempts, which are actions, and Stripe answers no list of records (suite: "Payment records are read by id only"), so only the data source exists.
var Declaration = engine.Declaration{
	TypeName:    "stripe_payment_record",
	IDPrefix:    "pr_",
	Description: "A record of a payment taken outside Stripe and reported to it. Look one up by its `id`.",
	Paths:       engine.Paths{Retrieve: "/v1/payment_records/{id}"},
	Delete:      engine.NoDelete,
}

// NewDataSource returns the data source constructor the provider registers. It looks the object up by `id` only: Stripe answers no list of records.
func NewDataSource() (func() datasource.DataSource, error) {
	return engine.NewDataSource(Declaration, payment_record.Table)
}
