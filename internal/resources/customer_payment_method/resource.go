// Package customer_payment_method declares the stripe_customer_payment_method resource and
// data source: a payment method attached to a customer.
package customer_payment_method

import (
	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/resource"

	"github.com/computomatic/terraform-provider-stripe/internal/engine"
	"github.com/computomatic/terraform-provider-stripe/internal/generated/customer_payment_method"
	"github.com/computomatic/terraform-provider-stripe/internal/wire"
)

// Declaration is what the spec cannot say about the attachment of a payment method to a
// customer. The ADR names a nested object by its universe entry, `customers.payment_methods`,
// so the type is `stripe_customer_payment_method`, as `products.features` is
// `stripe_product_feature`. The resource is the attachment: creating it attaches an existing
// payment method (suite: "attach"), and destroy detaches it (suite: "detach"). A detached
// payment method cannot be attached again (suite: "detached cannot be reattached"). Stripe
// answers the customer-scoped retrieve with a 404 for a method that is detached or belongs
// to another customer (suite: "retrieve customer payment method errors"), which the engine
// reads as gone. Detaching a method that is not attached is refused, and the engine reads the
// 404 first, so destroy of an attachment that is already gone succeeds.
var Declaration = engine.Declaration{
	TypeName:    "stripe_customer_payment_method",
	IDPrefix:    "pm_",
	Description: "A payment method attached to a customer. The resource is the attachment: `terraform destroy` detaches the payment method from the customer, and Stripe never lets a detached payment method be attached again. The payment method itself is not created here, because creating one takes raw card data: create it with Stripe.js or a SetupIntent, or use a Stripe test payment method such as `pm_card_visa`, and give its ID as `payment_method`. Attaching a test payment method returns a new payment method, so the `id` can differ from `payment_method`. Only `billing_details`, `metadata` and `card.exp_year` change in place; Stripe's other updatable payment method fields are read-only here. Import with `<customer_id>/<payment_method_id>`.",
	Parent:      "customer",
	Attach:      "payment_method",
	Paths: engine.Paths{
		Create:   "/v1/payment_methods/{payment_method}/attach",
		Retrieve: "/v1/customers/{customer}/payment_methods/{id}",
		Update:   "/v1/payment_methods/{id}",
		List:     "/v1/customers/{customer}/payment_methods",
	},
	Delete: engine.Archive,
	Archive: &engine.ArchiveAction{
		Field: "customer",
		Value: wire.Null{},
		Path:  "/v1/payment_methods/{id}/detach",
	},
}

// NewResource returns the resource constructor the provider registers.
func NewResource() (func() resource.Resource, error) {
	return engine.NewResource(Declaration, customer_payment_method.Table)
}

// NewDataSource returns the data source constructor. It needs the `customer` and the `id` of the payment method. Stripe documents no payment method field as unique.
func NewDataSource() (func() datasource.DataSource, error) {
	return engine.NewDataSource(Declaration, customer_payment_method.Table)
}
