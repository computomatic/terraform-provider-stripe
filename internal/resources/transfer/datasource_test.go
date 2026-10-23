package transfer_test

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

const address = "data.stripe_transfer.test"

func config(c acctest.Config, id string) string {
	return c.ProviderBlock() + fmt.Sprintf(`
data "stripe_transfer" "test" {
  id = %q
}
`, id)
}

// A transfer from the available balance pays the connected account with a `py_` payment, is
// funded by card balance with no source transaction, and embeds its reversals (suite:
// "Transfers"). A reversal made afterwards shows in `amount_reversed` and the embedded list.
func TestAccTransfer_read(t *testing.T) {
	acct := acctest.NewAccount(t)
	destination := tfacc.TransfersAccount(t, acct)
	currency := tfacc.AccountCurrency(t, acct)
	id := tfacc.Transfer(t, acct, destination, currency, 500)
	var reversal string
	resource.ParallelTest(t, resource.TestCase{
		ProtoV6ProviderFactories: tfacc.ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: config(acct, id),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr(address, "id", id),
					resource.TestCheckResourceAttr(address, "amount", "500"),
					resource.TestCheckResourceAttr(address, "amount_reversed", "0"),
					resource.TestCheckResourceAttr(address, "currency", currency),
					resource.TestCheckResourceAttr(address, "destination", destination),
					resource.TestMatchResourceAttr(address, "destination_payment", regexp.MustCompile(`^py_`)),
					resource.TestMatchResourceAttr(address, "balance_transaction", regexp.MustCompile(`^txn_`)),
					resource.TestCheckResourceAttr(address, "source_type", "card"),
					resource.TestCheckNoResourceAttr(address, "source_transaction"),
					resource.TestCheckResourceAttr(address, "reversed", "false"),
					resource.TestCheckResourceAttr(address, "reversals.data.#", "0"),
					resource.TestCheckResourceAttr(address, "reversals.has_more", "false"),
					resource.TestCheckResourceAttr(address, "reversals.url", "/v1/transfers/"+id+"/reversals"),
					resource.TestCheckResourceAttr(address, "livemode", "false"),
					resource.TestCheckResourceAttrSet(address, "created"),
				),
			},
			{
				PreConfig: func() {
					reversal = tfacc.Create(t, acct, "/v1/transfers/"+id+"/reversals", wire.Object{{Name: "amount", Value: wire.Int(200)}})
				},
				Config: config(acct, id),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr(address, "amount_reversed", "200"),
					resource.TestCheckResourceAttr(address, "reversed", "false"),
					resource.TestCheckResourceAttr(address, "reversals.data.#", "1"),
					resource.TestCheckResourceAttrWith(address, "reversals.data.0.id", func(v string) error {
						if v != reversal {
							return fmt.Errorf("reversals.data.0.id = %q, want %s", v, reversal)
						}
						return nil
					}),
					resource.TestCheckResourceAttr(address, "reversals.data.0.amount", "200"),
					resource.TestCheckResourceAttr(address, "reversals.data.0.transfer", id),
				),
			},
		},
	})
}

// An unknown transfer is a 404 `resource_missing` (suite: "Transfers", "An unknown id is 404
// `resource_missing`, param `transfer`").
func TestAccTransfer_notFound(t *testing.T) {
	acct := acctest.NewAccount(t)
	resource.ParallelTest(t, resource.TestCase{
		ProtoV6ProviderFactories: tfacc.ProviderFactories,
		Steps: []resource.TestStep{{
			Config:      config(acct, "tr_doesnotexist"),
			ExpectError: regexp.MustCompile("no stripe_transfer found for id=tr_doesnotexist"),
		}},
	})
}
