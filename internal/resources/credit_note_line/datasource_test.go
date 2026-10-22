package credit_note_line_test

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

// A pre-payment credit note on an open `send_invoice` invoice needs no card (suite: "Credit
// notes"). Stripe has no retrieve for its lines (suite: "Credit note lines"), so the data
// source finds the line by its id among the credit note's lines.
func TestAccCreditNoteLine_read(t *testing.T) {
	acct := acctest.NewAccount(t)
	_, invoice := tfacc.OpenInvoice(t, acct, "")
	note := tfacc.Create(t, acct, "/v1/credit_notes", wire.Object{
		{Name: "invoice", Value: wire.String(invoice)},
		{Name: "lines", Value: wire.Array{wire.Object{
			{Name: "type", Value: wire.String("custom_line_item")},
			{Name: "description", Value: wire.String("Goodwill credit")},
			{Name: "quantity", Value: wire.Int(2)},
			{Name: "unit_amount", Value: wire.Int(300)},
		}}},
	})
	line := tfacc.FirstListed(t, acct, "/v1/credit_notes/"+note+"/lines")
	resource.ParallelTest(t, resource.TestCase{
		ProtoV6ProviderFactories: tfacc.ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: acct.ProviderBlock() + `
data "stripe_credit_note_line" "test" {
  credit_note = "` + note + `"
  id          = "` + line + `"
}
`,
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("data.stripe_credit_note_line.test", "id", line),
					resource.TestCheckResourceAttr("data.stripe_credit_note_line.test", "credit_note", note),
					resource.TestCheckResourceAttr("data.stripe_credit_note_line.test", "type", "custom_line_item"),
					resource.TestCheckResourceAttr("data.stripe_credit_note_line.test", "description", "Goodwill credit"),
					resource.TestCheckResourceAttr("data.stripe_credit_note_line.test", "quantity", "2"),
					resource.TestCheckResourceAttr("data.stripe_credit_note_line.test", "unit_amount", "300"),
					resource.TestCheckResourceAttr("data.stripe_credit_note_line.test", "amount", "600"),
				),
			},
			{
				Config: acct.ProviderBlock() + `
data "stripe_credit_note_line" "test" {
  credit_note = "` + note + `"
  id          = "cnli_doesnotexist"
}
`,
				ExpectError: regexp.MustCompile("no stripe_credit_note_line found for id=cnli_doesnotexist"),
			},
		},
	})
}
