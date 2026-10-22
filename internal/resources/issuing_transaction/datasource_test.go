package issuing_transaction_test

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

const (
	address = "data.stripe_issuing_transaction.test"
	amount  = 500
)

// A captured authorization makes a capture transaction of the negative amount, linked to the
// authorization, card and cardholder, with no dispute (suite: "capture of an authorization").
// A dispute of it shows on the transaction ("the transaction names the dispute").
func TestAccIssuingTransaction_read(t *testing.T) {
	acct := acctest.NewAccount(t)
	conn := tfacc.IssuingAccount(t, acct)
	card, cardholder := tfacc.ActiveCard(t, conn, tfacc.IssuingFinancialAccount(t, conn))
	auth, txn := tfacc.CapturedTransaction(t, conn, card, amount)
	cfg := acct.ProviderBlock() + conn.ConnectedProviderBlock() + fmt.Sprintf(`
data "stripe_issuing_transaction" "test" {
  provider = stripe.connected
  id       = %q
}
`, txn)
	var dispute string
	resource.ParallelTest(t, resource.TestCase{
		ProtoV6ProviderFactories: tfacc.ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: cfg,
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestMatchResourceAttr(address, "id", regexp.MustCompile(`^ipi_`)),
					resource.TestCheckResourceAttr(address, "type", "capture"),
					resource.TestCheckResourceAttr(address, "amount", fmt.Sprint(-amount)),
					resource.TestCheckResourceAttr(address, "currency", "usd"),
					resource.TestCheckResourceAttr(address, "merchant_amount", fmt.Sprint(-amount)),
					resource.TestCheckResourceAttr(address, "merchant_currency", "usd"),
					resource.TestCheckResourceAttr(address, "authorization", auth),
					resource.TestCheckResourceAttr(address, "card", card),
					resource.TestCheckResourceAttr(address, "cardholder", cardholder),
					resource.TestCheckNoResourceAttr(address, "dispute"),
					resource.TestCheckResourceAttr(address, "livemode", "false"),
					resource.TestCheckResourceAttrSet(address, "created"),
				),
			},
			{
				PreConfig: func() {
					dispute = tfacc.Create(t, conn, "/v1/issuing/disputes", wire.Object{
						{Name: "transaction", Value: wire.String(txn)},
						{Name: "evidence", Value: wire.Object{{Name: "reason", Value: wire.String("fraudulent")}}},
					})
				},
				Config: cfg,
				Check: resource.TestCheckResourceAttrWith(address, "dispute", func(v string) error {
					if v != dispute {
						return fmt.Errorf("dispute = %q, want %s", v, dispute)
					}
					return nil
				}),
			},
		},
	})
}

// An unknown transaction is a 404 `resource_missing` (suite: "retrieve unknown").
func TestAccIssuingTransaction_notFound(t *testing.T) {
	acct := acctest.NewAccount(t)
	conn := tfacc.IssuingAccount(t, acct)
	resource.ParallelTest(t, resource.TestCase{
		ProtoV6ProviderFactories: tfacc.ProviderFactories,
		Steps: []resource.TestStep{{
			Config: acct.ProviderBlock() + conn.ConnectedProviderBlock() + `
data "stripe_issuing_transaction" "test" {
  provider = stripe.connected
  id       = "ipi_doesnotexist"
}
`,
			ExpectError: regexp.MustCompile("no stripe_issuing_transaction found for id=ipi_doesnotexist"),
		}},
	})
}
