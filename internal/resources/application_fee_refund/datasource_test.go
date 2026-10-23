package application_fee_refund_test

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

const address = "data.stripe_application_fee_refund.test"

func config(c acctest.Config, fee, id string) string {
	return c.ProviderBlock() + fmt.Sprintf(`
data "stripe_application_fee_refund" "test" {
  fee = %q
  id  = %q
}
`, fee, id)
}

// A partial refund names its fee and a balance transaction, and keeps its metadata (suite:
// "Application fee refunds").
func TestAccApplicationFeeRefund_read(t *testing.T) {
	acct := acctest.NewAccount(t)
	currency := tfacc.AccountCurrency(t, acct)
	_, fee := tfacc.DestinationCharge(t, acct, tfacc.TransfersAccount(t, acct), currency, 1000, 150)
	id := tfacc.Create(t, acct, "/v1/application_fees/"+fee+"/refunds", wire.Object{
		{Name: "amount", Value: wire.Int(50)},
		{Name: "metadata", Value: wire.Object{{Name: "reason", Value: wire.String("goodwill")}}},
	})
	resource.ParallelTest(t, resource.TestCase{
		ProtoV6ProviderFactories: tfacc.ProviderFactories,
		Steps: []resource.TestStep{{
			Config: config(acct, fee, id),
			Check: resource.ComposeAggregateTestCheckFunc(
				resource.TestCheckResourceAttr(address, "id", id),
				resource.TestCheckResourceAttr(address, "fee", fee),
				resource.TestCheckResourceAttr(address, "amount", "50"),
				resource.TestCheckResourceAttr(address, "currency", currency),
				resource.TestMatchResourceAttr(address, "balance_transaction", regexp.MustCompile(`^txn_`)),
				resource.TestCheckResourceAttr(address, "metadata.%", "1"),
				resource.TestCheckResourceAttr(address, "metadata.reason", "goodwill"),
				resource.TestCheckResourceAttrSet(address, "created"),
			),
		}},
	})
}

// An unknown refund of a fee, and any refund under an unknown fee, is a 404
// `resource_missing` (suite: "Application fee refunds", "An unknown refund ... 404" and "an
// unknown fee is 404 `resource_missing` ... with param `fee` on retrieve of a refund").
func TestAccApplicationFeeRefund_notFound(t *testing.T) {
	acct := acctest.NewAccount(t)
	_, fee := tfacc.DestinationCharge(t, acct, tfacc.TransfersAccount(t, acct), tfacc.AccountCurrency(t, acct), 1000, 150)
	refund := tfacc.Create(t, acct, "/v1/application_fees/"+fee+"/refunds", wire.Object{{Name: "amount", Value: wire.Int(50)}})
	resource.ParallelTest(t, resource.TestCase{
		ProtoV6ProviderFactories: tfacc.ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config:      config(acct, fee, "fr_doesnotexist"),
				ExpectError: regexp.MustCompile("no stripe_application_fee_refund found for id=fr_doesnotexist"),
			},
			{
				Config:      config(acct, "fee_doesnotexist", refund),
				ExpectError: regexp.MustCompile("no stripe_application_fee_refund found for id=" + refund),
			},
		},
	})
}
