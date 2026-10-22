package tax_location_test

import (
	"os"
	"regexp"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"

	"github.com/computomatic/terraform-provider-stripe/internal/acctest"
	"github.com/computomatic/terraform-provider-stripe/internal/acctest/tfacc"
	"github.com/computomatic/terraform-provider-stripe/internal/wire"
)

func TestMain(m *testing.M) { os.Exit(acctest.Main(m)) }

const address = "data.stripe_tax_location.test"

func config(c acctest.Config, id string) string {
	return c.ProviderBlock() + `
data "stripe_tax_location" "test" {
  id = "` + id + `"
}
`
}

// location creates a performance location at the suite's head office address (suite: "Tax
// locations are served"). Stripe cannot delete one.
func location(t *testing.T, acct acctest.Config, description string) string {
	t.Helper()
	return tfacc.Create(t, acct, "/v1/tax/locations", wire.Object{
		{Name: "type", Value: wire.String("performance")},
		{Name: "description", Value: wire.String(description)},
		{Name: "address", Value: wire.Object{
			{Name: "line1", Value: wire.String("354 Oyster Point Blvd")},
			{Name: "city", Value: wire.String("South San Francisco")},
			{Name: "state", Value: wire.String("CA")},
			{Name: "postal_code", Value: wire.String("94080")},
			{Name: "country", Value: wire.String("US")},
		}},
	})
}

func TestAccTaxLocation_read(t *testing.T) {
	acct := acctest.NewAccount(t)
	description := acct.Name("Concert hall")
	id := location(t, acct, description)
	resource.ParallelTest(t, resource.TestCase{
		ProtoV6ProviderFactories: tfacc.ProviderFactories,
		Steps: []resource.TestStep{{
			Config: config(acct, id),
			Check: resource.ComposeAggregateTestCheckFunc(
				resource.TestCheckResourceAttr(address, "id", id),
				resource.TestCheckResourceAttr(address, "type", "performance"),
				resource.TestCheckResourceAttr(address, "description", description),
				resource.TestCheckResourceAttr(address, "address.city", "South San Francisco"),
				resource.TestCheckResourceAttr(address, "address.country", "US"),
				resource.TestCheckResourceAttr(address, "livemode", "false"),
			),
		}},
	})
}

// An unknown location is a 404 `resource_missing` (suite: "retrieve unknown", an altered real
// ID).
func TestAccTaxLocation_notFound(t *testing.T) {
	acct := acctest.NewAccount(t)
	missing := tfacc.AlteredID(location(t, acct, acct.Name("Arena")))
	resource.ParallelTest(t, resource.TestCase{
		ProtoV6ProviderFactories: tfacc.ProviderFactories,
		Steps: []resource.TestStep{{
			Config:      config(acct, missing),
			ExpectError: regexp.MustCompile(`no stripe_tax_location found for\s+id=` + missing),
		}},
	})
}
