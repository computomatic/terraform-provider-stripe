package application_fee_test

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

const address = "data.stripe_application_fee.test"

func config(c acctest.Config, id string) string {
	return c.ProviderBlock() + fmt.Sprintf(`
data "stripe_application_fee" "test" {
  id = %q
}
`, id)
}

// A destination charge's fee names the connected account, the `ca_` application, the
// connected account's `py_` payment as `charge` and `fee_source`, and the platform's charge as
// `originating_transaction`, and embeds its refunds (suite: "Application fees from destination
// charges"). A refund made afterwards shows in `amount_refunded` and the embedded list.
func TestAccApplicationFee_read(t *testing.T) {
	acct := acctest.NewAccount(t)
	destination := tfacc.TransfersAccount(t, acct)
	currency := tfacc.AccountCurrency(t, acct)
	charge, id := tfacc.DestinationCharge(t, acct, destination, currency, 1000, 150)
	var refund string
	resource.ParallelTest(t, resource.TestCase{
		ProtoV6ProviderFactories: tfacc.ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: config(acct, id),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr(address, "id", id),
					resource.TestCheckResourceAttr(address, "amount", "150"),
					resource.TestCheckResourceAttr(address, "amount_refunded", "0"),
					resource.TestCheckResourceAttr(address, "refunded", "false"),
					resource.TestCheckResourceAttr(address, "currency", currency),
					resource.TestCheckResourceAttr(address, "account", destination),
					resource.TestMatchResourceAttr(address, "application", regexp.MustCompile(`^ca_`)),
					resource.TestMatchResourceAttr(address, "balance_transaction", regexp.MustCompile(`^txn_`)),
					resource.TestMatchResourceAttr(address, "charge", regexp.MustCompile(`^py_`)),
					resource.TestCheckResourceAttr(address, "originating_transaction", charge),
					resource.TestCheckResourceAttr(address, "fee_source.type", "charge"),
					resource.TestCheckResourceAttrPair(address, "fee_source.charge", address, "charge"),
					resource.TestCheckResourceAttr(address, "refunds.data.#", "0"),
					resource.TestCheckResourceAttr(address, "refunds.has_more", "false"),
					resource.TestCheckResourceAttr(address, "refunds.url", "/v1/application_fees/"+id+"/refunds"),
					resource.TestCheckResourceAttr(address, "livemode", "false"),
					resource.TestCheckResourceAttrSet(address, "created"),
				),
			},
			{
				PreConfig: func() {
					refund = tfacc.Create(t, acct, "/v1/application_fees/"+id+"/refunds", wire.Object{{Name: "amount", Value: wire.Int(50)}})
				},
				Config: config(acct, id),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr(address, "amount_refunded", "50"),
					resource.TestCheckResourceAttr(address, "refunded", "false"),
					resource.TestCheckResourceAttr(address, "refunds.data.#", "1"),
					resource.TestCheckResourceAttrWith(address, "refunds.data.0.id", func(v string) error {
						if v != refund {
							return fmt.Errorf("refunds.data.0.id = %q, want %s", v, refund)
						}
						return nil
					}),
					resource.TestCheckResourceAttr(address, "refunds.data.0.amount", "50"),
					resource.TestCheckResourceAttr(address, "refunds.data.0.fee", id),
				),
			},
		},
	})
}

// An unknown fee is a 404 `resource_missing` (suite: "Application fees from destination
// charges", "An unknown fee is 404 `resource_missing`, param `id`").
func TestAccApplicationFee_notFound(t *testing.T) {
	acct := acctest.NewAccount(t)
	resource.ParallelTest(t, resource.TestCase{
		ProtoV6ProviderFactories: tfacc.ProviderFactories,
		Steps: []resource.TestStep{{
			Config:      config(acct, "fee_doesnotexist"),
			ExpectError: regexp.MustCompile("no stripe_application_fee found for id=fee_doesnotexist"),
		}},
	})
}
