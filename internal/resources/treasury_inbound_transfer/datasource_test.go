package treasury_inbound_transfer_test

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

const address = "data.stripe_treasury_inbound_transfer.test"

// config reads the object with the given ID through a provider configuration that acts on the
// connected account.
func config(acct, conn acctest.Config, id string) string {
	return acct.ProviderBlock() + conn.ConnectedProviderBlock() + `
data "stripe_treasury_inbound_transfer" "test" {
  provider = stripe.connected
  id       = "` + id + `"
}
`
}

// The test pulls a transfer from the bank account attached to the connected account through the
// API and reads it back.
func TestAccTreasuryInboundTransfer_read(t *testing.T) {
	s := newScope(t)
	acct, conn, fa := s.acct, s.conn, s.fa
	id := tfacc.Create(t, conn, "/v1/treasury/inbound_transfers", wire.Object{
		{Name: "financial_account", Value: wire.String(fa)},
		{Name: "amount", Value: wire.Int(amount)},
		{Name: "currency", Value: wire.String("usd")},
		{Name: "origin_payment_method", Value: wire.String(s.bank)},
	})
	resource.ParallelTest(t, resource.TestCase{
		ProtoV6ProviderFactories: tfacc.ProviderFactories,
		Steps: []resource.TestStep{{
			Config: config(acct, conn, id),
			Check: resource.ComposeAggregateTestCheckFunc(
				resource.TestCheckResourceAttr(address, "id", id),
				resource.TestCheckResourceAttr(address, "financial_account", fa),
				resource.TestCheckResourceAttrSet(address, "status"),
			),
		}},
	})
}

// An unknown inbound transfer is a 404 `resource_missing` (suite: "retrieve unknown").
func TestAccTreasuryInboundTransfer_notFound(t *testing.T) {
	acct := acctest.NewAccount(t)
	conn := tfacc.IssuingAccount(t, acct)
	resource.ParallelTest(t, resource.TestCase{
		ProtoV6ProviderFactories: tfacc.ProviderFactories,
		Steps: []resource.TestStep{{
			Config:      config(acct, conn, "ibt_doesnotexist"),
			ExpectError: regexp.MustCompile("no stripe_treasury_inbound_transfer found for id=ibt_doesnotexist"),
		}},
	})
}
