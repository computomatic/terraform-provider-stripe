package climate_supplier_test

import (
	"os"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"

	"github.com/computomatic/terraform-provider-stripe/internal/acctest"
	"github.com/computomatic/terraform-provider-stripe/internal/acctest/tfacc"
	"github.com/computomatic/terraform-provider-stripe/internal/stripeapi"
	"github.com/computomatic/terraform-provider-stripe/internal/wire"
)

func TestMain(m *testing.M) { os.Exit(acctest.Main(m)) }

// The Climate catalogue is Stripe's, so the test reads the first entry the list serves (suite:
// "TestClimateProductAndSupplier").
func TestAccClimateSupplier_read(t *testing.T) {
	acct := acctest.NewAccount(t)
	resp, err := tfacc.Client(acct).Do(t.Context(), stripeapi.Request{Method: "GET", Path: "/v1/climate/suppliers", Params: wire.Object{{Name: "limit", Value: wire.Int(1)}}})
	if err != nil {
		t.Fatalf("listing climate suppliers: %v", err)
	}
	list, _ := resp.Body.(wire.Object).Get("data")
	items, _ := list.(wire.Array)
	if len(items) == 0 {
		t.Skip("no climate suppliers are listed")
	}
	id, _ := items[0].(wire.Object).GetString("id")
	resource.ParallelTest(t, resource.TestCase{
		ProtoV6ProviderFactories: tfacc.ProviderFactories,
		Steps: []resource.TestStep{{
			Config: acct.ProviderBlock() + `
data "stripe_climate_supplier" "test" {
  id = "` + id + `"
}
`,
			Check: resource.ComposeAggregateTestCheckFunc(
				resource.TestCheckResourceAttr("data.stripe_climate_supplier.test", "id", id),
				resource.TestCheckResourceAttrSet("data.stripe_climate_supplier.test", "name"),
				resource.TestCheckResourceAttrSet("data.stripe_climate_supplier.test", "removal_pathway"),
				resource.TestCheckResourceAttrSet("data.stripe_climate_supplier.test", "locations.#"),
			),
		}},
	})
}
