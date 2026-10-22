package payout_test

import (
	"os"
	"regexp"
	"strconv"
	"testing"
	"time"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"

	"github.com/computomatic/terraform-provider-stripe/internal/acctest"
	"github.com/computomatic/terraform-provider-stripe/internal/acctest/tfacc"
	"github.com/computomatic/terraform-provider-stripe/internal/wire"
)

func TestMain(m *testing.M) { os.Exit(acctest.Main(m)) }

// No payout can be made on the suite's account, which has no external account in its currency
// and cannot add one (suite: "Payouts need an external account"), so the test seeds one and
// runs on the fake only.
func TestAccPayout_read(t *testing.T) {
	acctest.RequireFake(t)
	acct := acctest.NewAccount(t)
	currency := tfacc.AccountCurrency(t, acct)
	arrival := time.Now().Add(48 * time.Hour).Unix()
	id := acctest.Seed(t, acct, wire.Object{
		{Name: "object", Value: wire.String("payout")},
		{Name: "amount", Value: wire.Int(1200)},
		{Name: "arrival_date", Value: wire.Int(arrival)},
		{Name: "automatic", Value: wire.Bool(false)},
		{Name: "currency", Value: wire.String(currency)},
		{Name: "livemode", Value: wire.Bool(false)},
		{Name: "method", Value: wire.String("standard")},
		{Name: "reconciliation_status", Value: wire.String("not_applicable")},
		{Name: "source_type", Value: wire.String("card")},
		{Name: "status", Value: wire.String("pending")},
		{Name: "type", Value: wire.String("bank_account")},
	})[0]
	resource.ParallelTest(t, resource.TestCase{
		ProtoV6ProviderFactories: tfacc.ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: acct.ProviderBlock() + `
data "stripe_payout" "test" {
  id = "` + id + `"
}
`,
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("data.stripe_payout.test", "id", id),
					resource.TestCheckResourceAttr("data.stripe_payout.test", "amount", "1200"),
					resource.TestCheckResourceAttr("data.stripe_payout.test", "arrival_date", strconv.FormatInt(arrival, 10)),
					resource.TestCheckResourceAttr("data.stripe_payout.test", "currency", currency),
					resource.TestCheckResourceAttr("data.stripe_payout.test", "method", "standard"),
					resource.TestCheckResourceAttr("data.stripe_payout.test", "status", "pending"),
					resource.TestCheckResourceAttr("data.stripe_payout.test", "automatic", "false"),
				),
			},
		},
	})
}

// An unknown payout is a 404 `resource_missing` (suite: "Unknown payout ids").
func TestAccPayout_notFound(t *testing.T) {
	acct := acctest.NewAccount(t)
	resource.ParallelTest(t, resource.TestCase{
		ProtoV6ProviderFactories: tfacc.ProviderFactories,
		Steps: []resource.TestStep{{
			Config: acct.ProviderBlock() + `
data "stripe_payout" "test" {
  id = "po_doesnotexist"
}
`,
			ExpectError: regexp.MustCompile("no stripe_payout found for id=po_doesnotexist"),
		}},
	})
}
