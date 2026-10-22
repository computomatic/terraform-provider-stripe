package treasury_outbound_payment_test

import (
	"os"
	"regexp"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"

	"github.com/computomatic/terraform-provider-stripe/internal/acctest"
	"github.com/computomatic/terraform-provider-stripe/internal/acctest/tfacc"
	"github.com/computomatic/terraform-provider-stripe/internal/stripeapi"
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

// Nothing in the API makes an outbound payment: making one needs a customer with a US bank account payment method on the connected account, which no test helper creates (suite: "outbound payments"). The test reads the first one
// the financial account lists and skips when there is none, as on a new account.
func TestAccTreasuryOutboundPayment_read(t *testing.T) {
	acct := acctest.NewAccount(t)
	conn := tfacc.IssuingAccount(t, acct)
	fa := tfacc.IssuingFinancialAccount(t, conn)
	resp, err := tfacc.Client(conn).Do(t.Context(), stripeapi.Request{Method: "GET", Path: "/v1/treasury/outbound_payments", Params: wire.Object{{Name: "financial_account", Value: wire.String(fa)}}})
	if err != nil {
		t.Fatalf("listing outbound payments: %v", err)
	}
	data, _ := resp.Body.(wire.Object).Get("data")
	items, _ := data.(wire.Array)
	if len(items) == 0 {
		t.Skip("the financial account has no outbound payments, and making one needs a customer with a US bank account payment method on the connected account, which no test helper creates")
	}
	id, _ := items[0].(wire.Object).GetString("id")
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
