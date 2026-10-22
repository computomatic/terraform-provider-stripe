package payment_record_test

import (
	"os"
	"regexp"
	"testing"
	"time"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"

	"github.com/computomatic/terraform-provider-stripe/internal/acctest"
	"github.com/computomatic/terraform-provider-stripe/internal/acctest/tfacc"
	"github.com/computomatic/terraform-provider-stripe/internal/wire"
)

func TestMain(m *testing.M) { os.Exit(acctest.Main(m)) }

const address = "data.stripe_payment_record.test"

func config(c acctest.Config, id string) string {
	return c.ProviderBlock() + `
data "stripe_payment_record" "test" {
  id = "` + id + `"
}
`
}

// A guaranteed payment of 1500, taken with a custom payment method and processor, is reported
// and read back by its id (suite: "Payment records are read by id only").
func TestAccPaymentRecord_read(t *testing.T) {
	acct := acctest.NewAccount(t)
	currency := tfacc.AccountCurrency(t, acct)
	now := wire.Int(time.Now().Unix())
	id := tfacc.Create(t, acct, "/v1/payment_records/report_payment", wire.Object{
		{Name: "amount_requested", Value: wire.Object{
			{Name: "currency", Value: wire.String(currency)},
			{Name: "value", Value: wire.Int(1500)},
		}},
		{Name: "initiated_at", Value: now},
		{Name: "outcome", Value: wire.String("guaranteed")},
		{Name: "guaranteed", Value: wire.Object{{Name: "guaranteed_at", Value: now}}},
		{Name: "customer_presence", Value: wire.String("on_session")},
		{Name: "payment_method_details", Value: wire.Object{
			{Name: "type", Value: wire.String("custom")},
			{Name: "custom", Value: wire.Object{{Name: "display_name", Value: wire.String("Till")}}},
		}},
		{Name: "processor_details", Value: wire.Object{
			{Name: "type", Value: wire.String("custom")},
			{Name: "custom", Value: wire.Object{{Name: "payment_reference", Value: wire.String(acct.Unique("tfacc-payment-record"))}}},
		}},
		{Name: "metadata", Value: wire.Object{{Name: "team", Value: wire.String("a")}}},
	})
	resource.ParallelTest(t, resource.TestCase{
		ProtoV6ProviderFactories: tfacc.ProviderFactories,
		Steps: []resource.TestStep{{
			Config: config(acct, id),
			Check: resource.ComposeAggregateTestCheckFunc(
				resource.TestCheckResourceAttr(address, "id", id),
				resource.TestCheckResourceAttr(address, "amount_requested.value", "1500"),
				resource.TestCheckResourceAttr(address, "amount_requested.currency", currency),
				resource.TestCheckResourceAttr(address, "customer_presence", "on_session"),
				resource.TestCheckResourceAttr(address, "reported_by", "self"),
				resource.TestCheckResourceAttr(address, "metadata.team", "a"),
				resource.TestCheckResourceAttrSet(address, "latest_payment_attempt_record"),
			),
		}},
	})
}

// An unknown record, well formed or not, is a 404 `resource_missing` with no param (suite:
// "retrieve unknown").
func TestAccPaymentRecord_notFound(t *testing.T) {
	acct := acctest.NewAccount(t)
	resource.ParallelTest(t, resource.TestCase{
		ProtoV6ProviderFactories: tfacc.ProviderFactories,
		Steps: []resource.TestStep{{
			Config:      config(acct, "pr_doesnotexist"),
			ExpectError: regexp.MustCompile(`no stripe_payment_record found for\s+id=pr_doesnotexist`),
		}},
	})
}
