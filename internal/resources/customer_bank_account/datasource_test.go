package customer_bank_account_test

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

const address = "data.stripe_customer_bank_account.test"

func config(c acctest.Config, customer, id string) string {
	return c.ProviderBlock() + fmt.Sprintf(`
data "stripe_customer_bank_account" "test" {
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

// `btok_us_verified` attaches a verified US dollar bank account ending 6789 with routing
// number 110000000 (suite: "Legacy customer sources"), which may carry a holder name. A holder
// name set afterwards reads back ("update").
func TestAccCustomerBankAccount_read(t *testing.T) {
	acct := acctest.NewAccount(t)
	cus := customer(t, acct, "tfacc-bank-read")
	id := tfacc.Create(t, acct, "/v1/customers/"+cus+"/sources", wire.Object{{Name: "source", Value: wire.String("btok_us_verified")}})
	resource.ParallelTest(t, resource.TestCase{
		ProtoV6ProviderFactories: tfacc.ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: config(acct, cus, id),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr(address, "id", id),
					resource.TestCheckResourceAttr(address, "customer", cus),
					resource.TestCheckResourceAttr(address, "status", "verified"),
					resource.TestCheckResourceAttr(address, "last4", "6789"),
					resource.TestCheckResourceAttr(address, "routing_number", "110000000"),
					resource.TestCheckResourceAttr(address, "country", "US"),
					resource.TestCheckResourceAttr(address, "currency", "usd"),
					resource.TestCheckResourceAttrSet(address, "bank_name"),
					resource.TestCheckResourceAttrSet(address, "fingerprint"),
					resource.TestCheckResourceAttr(address, "metadata.%", "0"),
				),
			},
			{
				PreConfig: func() {
					tfacc.Post(t, acct, "/v1/customers/"+cus+"/sources/"+id, wire.Object{{Name: "account_holder_name", Value: wire.String("Jordan Lee")}})
				},
				Config: config(acct, cus, id),
				Check:  resource.TestCheckResourceAttr(address, "account_holder_name", "Jordan Lee"),
			},
		},
	})
}

// An unknown bank account of a customer is a 404 on `/bank_accounts`, which carries no error
// code (suite: "Legacy customer sources", "`/bank_accounts/{id}` is 404 with **no code**").
// A 404 is not found whatever its code.
func TestAccCustomerBankAccount_notFound(t *testing.T) {
	acct := acctest.NewAccount(t)
	cus := customer(t, acct, "tfacc-bank-missing")
	resource.ParallelTest(t, resource.TestCase{
		ProtoV6ProviderFactories: tfacc.ProviderFactories,
		Steps: []resource.TestStep{{
			Config:      config(acct, cus, "ba_doesnotexist"),
			ExpectError: regexp.MustCompile("no stripe_customer_bank_account found for id=ba_doesnotexist"),
		}},
	})
}
