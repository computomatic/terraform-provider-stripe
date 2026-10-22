package issuing_authorization_test

import (
	"fmt"
	"os"
	"regexp"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"

	"github.com/computomatic/terraform-provider-stripe/internal/acctest"
	"github.com/computomatic/terraform-provider-stripe/internal/acctest/tfacc"
)

func TestMain(m *testing.M) { os.Exit(acctest.Main(m)) }

const (
	pending  = "data.stripe_issuing_authorization.pending"
	captured = "data.stripe_issuing_authorization.captured"
	amount   = 500
)

// The test helper approves an authorization, which holds its amount and embeds the card until
// it is captured; a captured one is closed and embeds its capture (suite: "create with the test
// helper", "retrieve", "capture"). A full reversal leaves it reversed, holding nothing
// ("reverse").
func TestAccIssuingAuthorization_read(t *testing.T) {
	acct := acctest.NewAccount(t)
	conn := tfacc.IssuingAccount(t, acct)
	card, cardholder := tfacc.ActiveCard(t, conn, tfacc.IssuingFinancialAccount(t, conn))
	open := tfacc.Authorization(t, conn, card, amount)
	closed, txn := tfacc.CapturedTransaction(t, conn, card, amount)
	cfg := acct.ProviderBlock() + conn.ConnectedProviderBlock() + fmt.Sprintf(`
data "stripe_issuing_authorization" "pending" {
  provider = stripe.connected
  id       = %q
}

data "stripe_issuing_authorization" "captured" {
  provider = stripe.connected
  id       = %q
}
`, open, closed)
	resource.ParallelTest(t, resource.TestCase{
		ProtoV6ProviderFactories: tfacc.ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: cfg,
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestMatchResourceAttr(pending, "id", regexp.MustCompile(`^iauth_`)),
					resource.TestCheckResourceAttr(pending, "status", "pending"),
					resource.TestCheckResourceAttr(pending, "approved", "true"),
					resource.TestCheckResourceAttr(pending, "amount", fmt.Sprint(amount)),
					resource.TestCheckResourceAttr(pending, "currency", "usd"),
					resource.TestCheckResourceAttr(pending, "merchant_amount", fmt.Sprint(amount)),
					resource.TestCheckResourceAttr(pending, "authorization_method", "online"),
					resource.TestCheckResourceAttr(pending, "card.id", card),
					resource.TestCheckResourceAttr(pending, "cardholder", cardholder),
					resource.TestCheckNoResourceAttr(pending, "card.number"),
					resource.TestCheckResourceAttr(pending, "transactions.#", "0"),
					resource.TestCheckResourceAttr(pending, "request_history.#", "1"),
					resource.TestCheckResourceAttr(pending, "request_history.0.approved", "true"),
					resource.TestCheckResourceAttr(pending, "livemode", "false"),
					resource.TestCheckResourceAttrSet(pending, "treasury.transaction"),
					resource.TestCheckResourceAttr(captured, "status", "closed"),
					resource.TestCheckResourceAttr(captured, "transactions.#", "1"),
					resource.TestCheckResourceAttr(captured, "transactions.0.id", txn),
					resource.TestCheckResourceAttr(captured, "transactions.0.type", "capture"),
					resource.TestCheckResourceAttr(captured, "transactions.0.amount", fmt.Sprint(-amount)),
				),
			},
			{
				PreConfig: func() { tfacc.Post(t, conn, "/v1/test_helpers/issuing/authorizations/"+open+"/reverse", nil) },
				Config:    cfg,
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr(pending, "status", "reversed"),
					resource.TestCheckResourceAttr(pending, "amount", "0"),
				),
			},
		},
	})
}

// An unknown authorization is a 404 `resource_missing` (suite: "retrieve unknown").
func TestAccIssuingAuthorization_notFound(t *testing.T) {
	acct := acctest.NewAccount(t)
	conn := tfacc.IssuingAccount(t, acct)
	resource.ParallelTest(t, resource.TestCase{
		ProtoV6ProviderFactories: tfacc.ProviderFactories,
		Steps: []resource.TestStep{{
			Config: acct.ProviderBlock() + conn.ConnectedProviderBlock() + `
data "stripe_issuing_authorization" "test" {
  provider = stripe.connected
  id       = "iauth_doesnotexist"
}
`,
			ExpectError: regexp.MustCompile("no stripe_issuing_authorization found for id=iauth_doesnotexist"),
		}},
	})
}
