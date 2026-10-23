package transfer_reversal_test

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

const address = "data.stripe_transfer_reversal.test"

func config(c acctest.Config, transfer, id string) string {
	return c.ProviderBlock() + fmt.Sprintf(`
data "stripe_transfer_reversal" "test" {
  transfer = %q
  id       = %q
}
`, transfer, id)
}

// A partial reversal names its transfer, a balance transaction and the refund of the
// destination payment, has no source refund, and keeps its metadata (suite: "Transfer
// reversals").
func TestAccTransferReversal_read(t *testing.T) {
	acct := acctest.NewAccount(t)
	currency := tfacc.AccountCurrency(t, acct)
	transfer := tfacc.Transfer(t, acct, tfacc.TransfersAccount(t, acct), currency, 500)
	id := tfacc.Create(t, acct, "/v1/transfers/"+transfer+"/reversals", wire.Object{
		{Name: "amount", Value: wire.Int(200)},
		{Name: "metadata", Value: wire.Object{{Name: "reason", Value: wire.String("returned")}}},
	})
	resource.ParallelTest(t, resource.TestCase{
		ProtoV6ProviderFactories: tfacc.ProviderFactories,
		Steps: []resource.TestStep{{
			Config: config(acct, transfer, id),
			Check: resource.ComposeAggregateTestCheckFunc(
				resource.TestCheckResourceAttr(address, "id", id),
				resource.TestCheckResourceAttr(address, "transfer", transfer),
				resource.TestCheckResourceAttr(address, "amount", "200"),
				resource.TestCheckResourceAttr(address, "currency", currency),
				resource.TestMatchResourceAttr(address, "balance_transaction", regexp.MustCompile(`^txn_`)),
				resource.TestMatchResourceAttr(address, "destination_payment_refund", regexp.MustCompile(`^pyr_`)),
				resource.TestCheckNoResourceAttr(address, "source_refund"),
				resource.TestCheckResourceAttr(address, "metadata.%", "1"),
				resource.TestCheckResourceAttr(address, "metadata.reason", "returned"),
				resource.TestCheckResourceAttrSet(address, "created"),
			),
		}},
	})
}

// An unknown reversal of a transfer, and any reversal under an unknown transfer, is a 404
// `resource_missing` (suite: "Transfer reversals", "An unknown reversal ... 404" and "An
// unknown transfer is 404 `resource_missing` with param `transfer` on retrieve of a
// reversal").
func TestAccTransferReversal_notFound(t *testing.T) {
	acct := acctest.NewAccount(t)
	transfer := tfacc.Transfer(t, acct, tfacc.TransfersAccount(t, acct), tfacc.AccountCurrency(t, acct), 300)
	reversal := tfacc.Create(t, acct, "/v1/transfers/"+transfer+"/reversals", wire.Object{{Name: "amount", Value: wire.Int(100)}})
	resource.ParallelTest(t, resource.TestCase{
		ProtoV6ProviderFactories: tfacc.ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config:      config(acct, transfer, "trr_doesnotexist"),
				ExpectError: regexp.MustCompile("no stripe_transfer_reversal found for id=trr_doesnotexist"),
			},
			{
				Config:      config(acct, "tr_doesnotexist", reversal),
				ExpectError: regexp.MustCompile("no stripe_transfer_reversal found for id=" + reversal),
			},
		},
	})
}
