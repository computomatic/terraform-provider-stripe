package treasury_outbound_transfer_test

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

const address = "data.stripe_treasury_outbound_transfer.test"

// config reads the object with the given ID through a provider configuration that acts on the
// connected account.
func config(acct, conn acctest.Config, id string) string {
	return acct.ProviderBlock() + conn.ConnectedProviderBlock() + `
data "stripe_treasury_outbound_transfer" "test" {
  provider = stripe.connected
  id       = "` + id + `"
}
`
}

// Nothing in the API makes an outbound transfer: making one needs a verified US bank account payment method on the connected account, which no test helper creates (suite: "outbound transfers"). The test reads the first one
// the financial account lists and skips when there is none, as on a new account.
func TestAccTreasuryOutboundTransfer_read(t *testing.T) {
	acct := acctest.NewAccount(t)
	conn := tfacc.IssuingAccount(t, acct)
	fa := tfacc.IssuingFinancialAccount(t, conn)
	resp, err := tfacc.Client(conn).Do(t.Context(), stripeapi.Request{Method: "GET", Path: "/v1/treasury/outbound_transfers", Params: wire.Object{{Name: "financial_account", Value: wire.String(fa)}}})
	if err != nil {
		t.Fatalf("listing outbound transfers: %v", err)
	}
	data, _ := resp.Body.(wire.Object).Get("data")
	items, _ := data.(wire.Array)
	if len(items) == 0 {
		t.Skip("the financial account has no outbound transfers, and making one needs a verified US bank account payment method on the connected account, which no test helper creates")
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

// An unknown outbound transfer is a 404 `resource_missing` (suite: "retrieve unknown").
func TestAccTreasuryOutboundTransfer_notFound(t *testing.T) {
	acct := acctest.NewAccount(t)
	conn := tfacc.IssuingAccount(t, acct)
	resource.ParallelTest(t, resource.TestCase{
		ProtoV6ProviderFactories: tfacc.ProviderFactories,
		Steps: []resource.TestStep{{
			Config:      config(acct, conn, "obt_doesnotexist"),
			ExpectError: regexp.MustCompile("no stripe_treasury_outbound_transfer found for id=obt_doesnotexist"),
		}},
	})
}
