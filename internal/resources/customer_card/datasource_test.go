package customer_card_test

import (
	"fmt"
	"os"
	"regexp"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"

	"github.com/computomatic/terraform-provider-stripe/internal/acctest"
	"github.com/computomatic/terraform-provider-stripe/internal/acctest/tfacc"
	"github.com/computomatic/terraform-provider-stripe/internal/wire"
)

func TestMain(m *testing.M) { os.Exit(acctest.Main(m)) }

const address = "data.stripe_customer_card.test"

func config(c acctest.Config, customer, id string) string {
	return c.ProviderBlock() + fmt.Sprintf(`
data "stripe_customer_card" "test" {
  customer = %q
  id       = %q
}
`, customer, id)
}

// customer creates a customer named for base.
func customer(t *testing.T, c acctest.Config, base string) string {
	t.Helper()
	return tfacc.Create(t, c, "/v1/customers", wire.Object{{Name: "name", Value: wire.String(c.Name(base))}})
}

// `tok_visa` attaches a US credit Visa ending 4242 with no name or address (suite: "Legacy
// customer sources", "attach a card token"). A name set afterwards reads back ("update").
func TestAccCustomerCard_read(t *testing.T) {
	acct := acctest.NewAccount(t)
	cus := customer(t, acct, "tfacc-card-read")
	id := tfacc.Create(t, acct, "/v1/customers/"+cus+"/sources", wire.Object{{Name: "source", Value: wire.String("tok_visa")}})
	resource.ParallelTest(t, resource.TestCase{
		ProtoV6ProviderFactories: tfacc.ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: config(acct, cus, id),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr(address, "id", id),
					resource.TestCheckResourceAttr(address, "customer", cus),
					resource.TestCheckResourceAttr(address, "brand", "Visa"),
					resource.TestCheckResourceAttr(address, "last4", "4242"),
					resource.TestCheckResourceAttr(address, "funding", "credit"),
					resource.TestCheckResourceAttr(address, "country", "US"),
					resource.TestCheckResourceAttrSet(address, "exp_month"),
					resource.TestCheckResourceAttrSet(address, "exp_year"),
					resource.TestCheckResourceAttrSet(address, "fingerprint"),
					resource.TestCheckNoResourceAttr(address, "name"),
					resource.TestCheckNoResourceAttr(address, "address_zip"),
					resource.TestCheckResourceAttr(address, "metadata.%", "0"),
				),
			},
			{
				PreConfig: func() {
					tfacc.Post(t, acct, "/v1/customers/"+cus+"/sources/"+id, wire.Object{
						{Name: "name", Value: wire.String("Jenny Rosen")},
						{Name: "address_zip", Value: wire.String("94107")},
					})
				},
				Config: config(acct, cus, id),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr(address, "name", "Jenny Rosen"),
					resource.TestCheckResourceAttr(address, "address_zip", "94107"),
				),
			},
		},
	})
}

// An unknown card of a customer is a 404 `resource_missing` on `/cards` (suite: "Legacy
// customer sources", "`/cards/{id}` is 404 `resource_missing`, param `card`").
func TestAccCustomerCard_notFound(t *testing.T) {
	acct := acctest.NewAccount(t)
	cus := customer(t, acct, "tfacc-card-missing")
	resource.ParallelTest(t, resource.TestCase{
		ProtoV6ProviderFactories: tfacc.ProviderFactories,
		Steps: []resource.TestStep{{
			Config:      config(acct, cus, "card_doesnotexist"),
			ExpectError: regexp.MustCompile("no stripe_customer_card found for id=card_doesnotexist"),
		}},
	})
}
