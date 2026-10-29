package treasury_outbound_payment_test

import (
	"fmt"
	"regexp"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/plancheck"

	"github.com/computomatic/terraform-provider-stripe/internal/acctest"
	"github.com/computomatic/terraform-provider-stripe/internal/acctest/tfacc"
	"github.com/computomatic/terraform-provider-stripe/internal/wire"
)

const (
	resAddress = "stripe_treasury_outbound_payment.test"
	// amount is each payment's, in cents. A payment cannot be deleted, so it stays at a dollar.
	amount = 100
)

// scope is a connected account whose financial account can pay out by ACH and holds enough for
// a few payments.
type scope struct {
	acct, conn acctest.Config
	fa         string
}

func newScope(t *testing.T) scope {
	t.Helper()
	acct := acctest.NewAccount(t)
	conn := tfacc.IssuingAccount(t, acct)
	return scope{acct: acct, conn: conn, fa: tfacc.FundedFinancialAccount(t, conn, 10*amount, "outbound_payments.ach")}
}

// config is a payment of cents to Stripe's test bank account, given inline, which needs no
// customer (suite: "create to inline us_bank_account data"). extra lands inside it.
func (s scope) config(cents int, extra string) string {
	return s.acct.ProviderBlock() + s.conn.ConnectedProviderBlock() + fmt.Sprintf(`
resource "stripe_treasury_outbound_payment" "test" {
  provider          = stripe.connected
  financial_account = %q
  amount            = %d
  currency          = "usd"

  destination_payment_method_data = {
    type = "us_bank_account"
    us_bank_account = {
      routing_number      = "110000000"
      account_number      = "000123456789"
      account_holder_type = "individual"
    }
    billing_details = {
      name = "Jenny Rosen"
    }
  }
%s}
`, s.fa, cents, extra)
}

func idOf(dst *string) resource.TestCheckFunc {
	return resource.TestCheckResourceAttrWith(resAddress, "id", func(v string) error {
		*dst = v
		return nil
	})
}

func pathOf(dst *string) resource.TestCheckFunc {
	return resource.TestCheckResourceAttrWith(resAddress, "id", func(v string) error {
		*dst = "/v1/treasury/outbound_payments/" + v
		return nil
	})
}

// processingOrPosted accepts the status of a new payment, which Stripe posts by itself within
// about a second (suite: "cancel").
var processingOrPosted = resource.TestCheckResourceAttrWith(resAddress, "status", func(v string) error {
	if v != "processing" && v != "posted" {
		return fmt.Errorf("status %s", v)
	}
	return nil
})

// awaitPosted waits until Stripe has posted the payment at *path by itself.
func (s scope) awaitPosted(t *testing.T, path *string) {
	t.Helper()
	tfacc.AwaitRetrieved(t, s.conn, *path, func(o wire.Object) bool {
		status, _ := o.GetString("status")
		return status == "posted"
	})
}

// A payment to inline bank details answers with the bank account Stripe made of them, and Stripe
// fills in `end_user_details` (suite: "create to inline us_bank_account data").
func TestAccTreasuryOutboundPayment_create(t *testing.T) {
	s := newScope(t)
	resource.ParallelTest(t, resource.TestCase{
		ProtoV6ProviderFactories: tfacc.ProviderFactories,
		Steps: acctest.Forget(t, []resource.TestStep{{
			Config: s.config(amount, "  metadata = { env = \"test\" }\n") + `
data "stripe_treasury_outbound_payment" "by_id" {
  provider = stripe.connected
  id       = stripe_treasury_outbound_payment.test.id
}
`,
			Check: resource.ComposeAggregateTestCheckFunc(
				resource.TestMatchResourceAttr(resAddress, "id", regexp.MustCompile(`^obp_`)),
				resource.TestCheckResourceAttr(resAddress, "amount", fmt.Sprint(amount)),
				resource.TestCheckResourceAttr(resAddress, "financial_account", s.fa),
				resource.TestCheckResourceAttr(resAddress, "metadata.env", "test"),
				resource.TestCheckResourceAttr(resAddress, "statement_descriptor", "payment"),
				resource.TestCheckResourceAttr(resAddress, "end_user_details.present", "false"),
				resource.TestCheckResourceAttr(resAddress, "destination_payment_method_details.type", "us_bank_account"),
				resource.TestCheckResourceAttr(resAddress, "destination_payment_method_details.us_bank_account.last4", "6789"),
				resource.TestCheckResourceAttr(resAddress, "destination_payment_method_data.us_bank_account.account_number", "000123456789"),
				processingOrPosted,
				resource.TestCheckResourceAttrPair("data.stripe_treasury_outbound_payment.by_id", "id", resAddress, "id"),
			),
		}}, nil, "stripe_treasury_outbound_payment"),
	})
}

// Stripe never updates an outbound payment, so nothing in place can change but `status` and the
// provider's own timeouts, which a plan applies without a replacement.
func TestAccTreasuryOutboundPayment_update(t *testing.T) {
	s := newScope(t)
	var first string
	resource.ParallelTest(t, resource.TestCase{
		ProtoV6ProviderFactories: tfacc.ProviderFactories,
		Steps: acctest.Forget(t, []resource.TestStep{
			{Config: s.config(amount, ""), Check: idOf(&first)},
			{
				Config: s.config(amount, "  timeouts = { create = \"10m\" }\n"),
				ConfigPlanChecks: resource.ConfigPlanChecks{PreApply: []plancheck.PlanCheck{
					plancheck.ExpectResourceAction(resAddress, plancheck.ResourceActionUpdate),
				}},
				Check: resource.TestCheckResourceAttrWith(resAddress, "id", func(v string) error {
					if v != first {
						return fmt.Errorf("the in-place update changed the payment from %s to %s", first, v)
					}
					return nil
				}),
			},
		}, nil, "stripe_treasury_outbound_payment"),
	})
}

// Every argument is create-only and a payment cannot be destroyed, so a new `amount` fails the
// plan, and so does a taint at apply.
func TestAccTreasuryOutboundPayment_replace(t *testing.T) {
	s := newScope(t)
	cfg := s.config(amount, "")
	resource.ParallelTest(t, resource.TestCase{
		ProtoV6ProviderFactories: tfacc.ProviderFactories,
		Steps: acctest.Forget(t, []resource.TestStep{
			{Config: cfg},
			{Config: s.config(2*amount, ""), PlanOnly: true, ExpectError: tfacc.RefusedReplace},
			{Config: cfg, Taint: []string{resAddress}, ExpectError: tfacc.RefusedDestroy},
		}, nil, "stripe_treasury_outbound_payment"),
	})
}

// Stripe does not return the inline bank details, so the import block's configuration ignores
// them, as the import note says. Stripe signs a new receipt URL on every read, and the payment
// may post between the reads.
func TestAccTreasuryOutboundPayment_import(t *testing.T) {
	s := newScope(t)
	cfg := s.config(amount, "  lifecycle {\n    ignore_changes = [destination_payment_method_data]\n  }\n")
	resource.ParallelTest(t, resource.TestCase{
		ProtoV6ProviderFactories: tfacc.ProviderFactories,
		Steps: acctest.Forget(t, []resource.TestStep{
			{Config: cfg},
			{
				Config: cfg, ResourceName: resAddress, ImportState: true, ImportStateVerify: true,
				ImportStateVerifyIgnore: []string{"destination_payment_method_data", "hosted_regulatory_receipt_url", "status", "cancelable", "status_transitions", "expected_arrival_date"},
			},
			{ResourceName: resAddress, ImportState: true, ImportStateKind: resource.ImportBlockWithID, Config: cfg},
		}, nil, "stripe_treasury_outbound_payment"),
	})
}

// Stripe cannot delete an outbound payment, so dropping it from the configuration, and
// `terraform destroy`, fail at plan time, and the payment stays.
func TestAccTreasuryOutboundPayment_delete(t *testing.T) {
	s := newScope(t)
	var path string
	var mark int
	cfg := s.config(amount, "")
	resource.ParallelTest(t, resource.TestCase{
		ProtoV6ProviderFactories: tfacc.ProviderFactories,
		Steps: acctest.Forget(t, []resource.TestStep{
			{Config: cfg, Check: resource.ComposeTestCheckFunc(pathOf(&path), tfacc.LogMark(t, s.conn, &mark))},
			{Config: acctest.Without(t, cfg, "stripe_treasury_outbound_payment"), PlanOnly: true, ExpectError: tfacc.RefusedDestroy},
			{Config: cfg, Destroy: true, ExpectError: tfacc.RefusedDestroy},
		}, tfacc.Untouched(t, s.conn, &path, &mark, "amount", wire.Int(amount)), "stripe_treasury_outbound_payment"),
	})
}

// Stripe posts a new payment by itself (suite: "cancel"). The state reads `posted` and the plan
// does not try to put `status` back.
func TestAccTreasuryOutboundPayment_drift(t *testing.T) {
	s := newScope(t)
	var path string
	cfg := s.config(amount, "")
	resource.ParallelTest(t, resource.TestCase{
		ProtoV6ProviderFactories: tfacc.ProviderFactories,
		Steps: acctest.Forget(t, []resource.TestStep{
			{Config: cfg, Check: pathOf(&path)},
			{
				PreConfig: func() { s.awaitPosted(t, &path) },
				Config:    cfg,
				ConfigPlanChecks: resource.ConfigPlanChecks{PreApply: []plancheck.PlanCheck{
					plancheck.ExpectEmptyPlan(),
				}},
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr(resAddress, "status", "posted"),
					resource.TestCheckResourceAttr(resAddress, "cancelable", "false"),
				),
			},
		}, nil, "stripe_treasury_outbound_payment"),
	})
}

// Setting `status` to `canceled` with the create cancels the payment before Stripe posts it,
// which the fake does five seconds after the create (suite: "cancel"). Stripe posts one within
// about a second, so on Stripe the cancel races the posting; TestAccTreasuryOutboundPayment_cancelPosted
// covers what a cancel of a posted payment does.
func TestAccTreasuryOutboundPayment_cancel(t *testing.T) {
	if acctest.Smoke() {
		t.Skip("Stripe posts a new outbound payment within about a second, so whether a cancel sent with the create wins depends on timing")
	}
	s := newScope(t)
	var path string
	resource.ParallelTest(t, resource.TestCase{
		ProtoV6ProviderFactories: tfacc.ProviderFactories,
		Steps: acctest.Forget(t, []resource.TestStep{{
			Config: s.config(amount, "  status = \"canceled\"\n"),
			Check: resource.ComposeAggregateTestCheckFunc(
				pathOf(&path),
				resource.TestCheckResourceAttr(resAddress, "status", "canceled"),
				resource.TestCheckResourceAttr(resAddress, "cancelable", "false"),
				resource.TestCheckResourceAttrSet(resAddress, "status_transitions.canceled_at"),
				tfacc.Holds(t, s.conn, &path, "status", wire.String("canceled")),
			),
		}}, nil, "stripe_treasury_outbound_payment"),
	})
}

// Stripe refuses to cancel a posted payment (suite: "cancel after post"), so once the state reads
// `posted`, a configuration that sets `canceled` fails the plan.
func TestAccTreasuryOutboundPayment_cancelPosted(t *testing.T) {
	s := newScope(t)
	var path string
	resource.ParallelTest(t, resource.TestCase{
		ProtoV6ProviderFactories: tfacc.ProviderFactories,
		Steps: acctest.Forget(t, []resource.TestStep{
			{Config: s.config(amount, ""), Check: pathOf(&path)},
			// An apply that changes nothing refreshes the state to `posted`.
			{PreConfig: func() { s.awaitPosted(t, &path) }, Config: s.config(amount, "")},
			{
				Config:      s.config(amount, "  status = \"canceled\"\n"),
				PlanOnly:    true,
				ExpectError: regexp.MustCompile("Cannot change `status` from `posted`"),
			},
		}, nil, "stripe_treasury_outbound_payment"),
	})
}
