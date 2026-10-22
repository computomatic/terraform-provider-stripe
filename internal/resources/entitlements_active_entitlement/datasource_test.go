package entitlements_active_entitlement_test

import (
	"os"
	"regexp"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"

	"github.com/computomatic/terraform-provider-stripe/internal/acctest"
	"github.com/computomatic/terraform-provider-stripe/internal/acctest/tfacc"
	"github.com/computomatic/terraform-provider-stripe/internal/stripeapi"
	"github.com/computomatic/terraform-provider-stripe/internal/wire"
)

func TestMain(m *testing.M) { os.Exit(acctest.Main(m)) }

const address = "data.stripe_entitlements_active_entitlement.test"

func config(c acctest.Config, id string) string {
	return c.ProviderBlock() + `
data "stripe_entitlements_active_entitlement" "test" {
  id = "` + id + `"
}
`
}

// entitled subscribes a new customer to a product carrying one new feature, invoiced so that
// the subscription is active without a payment method, and returns the feature's ID and
// lookup key and the entitlement Stripe grants a moment later (suite: "Active entitlements").
// The subscription is canceled when the test ends.
func entitled(t *testing.T, acct acctest.Config, base string) (feature, lookupKey, entitlement string) {
	t.Helper()
	lookupKey = acct.Unique(base)
	feature = tfacc.Create(t, acct, "/v1/entitlements/features", wire.Object{
		{Name: "name", Value: wire.String(acct.Name(base))},
		{Name: "lookup_key", Value: wire.String(lookupKey)},
	})
	product := tfacc.Create(t, acct, "/v1/products", wire.Object{{Name: "name", Value: wire.String(acct.Name(base))}})
	tfacc.Create(t, acct, "/v1/products/"+product+"/features", wire.Object{{Name: "entitlement_feature", Value: wire.String(feature)}})
	price := tfacc.Create(t, acct, "/v1/prices", wire.Object{
		{Name: "currency", Value: wire.String(tfacc.AccountCurrency(t, acct))},
		{Name: "unit_amount", Value: wire.Int(1000)},
		{Name: "product", Value: wire.String(product)},
		{Name: "recurring", Value: wire.Object{{Name: "interval", Value: wire.String("month")}}},
	})
	customer := tfacc.Create(t, acct, "/v1/customers", nil)
	subscription := tfacc.Create(t, acct, "/v1/subscriptions", wire.Object{
		{Name: "customer", Value: wire.String(customer)},
		{Name: "items", Value: wire.Array{wire.Object{{Name: "price", Value: wire.String(price)}}}},
		{Name: "collection_method", Value: wire.String("send_invoice")},
		{Name: "days_until_due", Value: wire.Int(30)},
	})
	t.Cleanup(func() {
		_, _ = tfacc.Client(acct).Do(t.Context(), stripeapi.Request{Method: "DELETE", Path: "/v1/subscriptions/" + subscription})
	})
	items := tfacc.AwaitListed(t, acct, "/v1/entitlements/active_entitlements", wire.Object{{Name: "customer", Value: wire.String(customer)}},
		func(items []wire.Object) bool { return len(items) > 0 })
	entitlement, _ = items[0].GetString("id")
	return feature, lookupKey, entitlement
}

func TestAccEntitlementsActiveEntitlement_read(t *testing.T) {
	acct := acctest.NewAccount(t)
	feature, lookupKey, id := entitled(t, acct, "tfacc_entitlement_read")
	resource.ParallelTest(t, resource.TestCase{
		ProtoV6ProviderFactories: tfacc.ProviderFactories,
		Steps: []resource.TestStep{{
			Config: config(acct, id),
			Check: resource.ComposeAggregateTestCheckFunc(
				resource.TestCheckResourceAttr(address, "id", id),
				resource.TestCheckResourceAttr(address, "feature", feature),
				resource.TestCheckResourceAttr(address, "lookup_key", lookupKey),
				resource.TestCheckResourceAttr(address, "livemode", "false"),
			),
		}},
	})
}

// An unknown entitlement is a 404 `resource_missing` (suite: "retrieve unknown", an altered
// real ID).
func TestAccEntitlementsActiveEntitlement_notFound(t *testing.T) {
	acct := acctest.NewAccount(t)
	_, _, id := entitled(t, acct, "tfacc_entitlement_missing")
	missing := tfacc.AlteredID(id)
	resource.ParallelTest(t, resource.TestCase{
		ProtoV6ProviderFactories: tfacc.ProviderFactories,
		Steps: []resource.TestStep{{
			Config:      config(acct, missing),
			ExpectError: regexp.MustCompile(`no stripe_entitlements_active_entitlement found for\s+id=` + missing),
		}},
	})
}
