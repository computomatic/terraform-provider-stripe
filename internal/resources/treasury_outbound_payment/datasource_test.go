package treasury_outbound_payment_test

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

const address = "data.stripe_treasury_outbound_payment.test"

// config reads the object with the given ID through a provider configuration that acts on the
// connected account.
func config(acct, conn acctest.Config, id string) string {
	return acct.ProviderBlock() + conn.ConnectedProviderBlock() + `
data "stripe_treasury_outbound_payment" "test" {
  provider = stripe.connected
  id       = "` + id + `"
}
`
}

// The test pays Stripe's test bank account through the API and reads the payment back.
func TestAccTreasuryOutboundPayment_read(t *testing.T) {
	s := newScope(t)
	id := tfacc.Create(t, s.conn, "/v1/treasury/outbound_payments", wire.Object{
		{Name: "financial_account", Value: wire.String(s.fa)},
		{Name: "amount", Value: wire.Int(amount)},
		{Name: "currency", Value: wire.String("usd")},
		{Name: "destination_payment_method_data", Value: wire.Object{
			{Name: "type", Value: wire.String("us_bank_account")},
			{Name: "us_bank_account", Value: wire.Object{
				{Name: "routing_number", Value: wire.String("110000000")},
				{Name: "account_number", Value: wire.String("000123456789")},
				{Name: "account_holder_type", Value: wire.String("individual")},
			}},
			{Name: "billing_details", Value: wire.Object{{Name: "name", Value: wire.String("Jenny Rosen")}}},
		}},
	})
	resource.ParallelTest(t, resource.TestCase{
		ProtoV6ProviderFactories: tfacc.ProviderFactories,
		Steps: []resource.TestStep{{
			Config: config(s.acct, s.conn, id),
			Check: resource.ComposeAggregateTestCheckFunc(
				resource.TestCheckResourceAttr(address, "id", id),
				resource.TestCheckResourceAttr(address, "financial_account", s.fa),
				resource.TestCheckResourceAttrSet(address, "status"),
			),
		}},
	})
}

// An unknown outbound payment is a 404 `resource_missing` (suite: "retrieve unknown").
func TestAccTreasuryOutboundPayment_notFound(t *testing.T) {
	acct := acctest.NewAccount(t)
	conn := tfacc.IssuingAccount(t, acct)
	resource.ParallelTest(t, resource.TestCase{
		ProtoV6ProviderFactories: tfacc.ProviderFactories,
		Steps: []resource.TestStep{{
			Config:      config(acct, conn, "obp_doesnotexist"),
			ExpectError: regexp.MustCompile("no stripe_treasury_outbound_payment found for id=obp_doesnotexist"),
		}},
	})
}
