package treasury_debit_reversal_test

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
	resAddress = "stripe_treasury_debit_reversal.test"
	// amount is each received debit's, in cents. A reversal cannot be deleted, so it stays at a
	// dollar.
	amount = 100
)

// scope is a connected account's financial account and a way to make ACH received debits in it,
// which Stripe can reverse (suite: "Credit and debit reversals").
type scope struct {
	acct, conn acctest.Config
	fa         string
}

func newScope(t *testing.T) scope {
	t.Helper()
	acct := acctest.NewAccount(t)
	conn := tfacc.IssuingAccount(t, acct)
	fa := tfacc.IssuingFinancialAccount(t, conn)
	// A received debit takes money out, so the account holds enough for a few.
	tfacc.ReceivedCredit(t, conn, fa, 10*amount)
	return scope{acct: acct, conn: conn, fa: fa}
}

// received makes a new received debit to reverse.
func (s scope) received(t *testing.T) string {
	t.Helper()
	id, _ := tfacc.ReceivedDebit(t, s.conn, s.fa, amount)
	return id
}

// config reverses the received debit. extra lands inside it.
func (s scope) config(received, extra string) string {
	return s.acct.ProviderBlock() + s.conn.ConnectedProviderBlock() + fmt.Sprintf(`
resource "stripe_treasury_debit_reversal" "test" {
  provider       = stripe.connected
  received_debit = %q
%s}
`, received, extra)
}

func idOf(dst *string) resource.TestCheckFunc {
	return resource.TestCheckResourceAttrWith(resAddress, "id", func(v string) error {
		*dst = v
		return nil
	})
}

func pathOf(dst *string) resource.TestCheckFunc {
	return resource.TestCheckResourceAttrWith(resAddress, "id", func(v string) error {
		*dst = "/v1/treasury/debit_reversals/" + v
		return nil
	})
}

// A reversal takes the received debit's amount and currency and starts `processing` (suite:
// "Credit and debit reversals").
func TestAccTreasuryDebitReversal_create(t *testing.T) {
	s := newScope(t)
	received := s.received(t)
	resource.ParallelTest(t, resource.TestCase{
		ProtoV6ProviderFactories: tfacc.ProviderFactories,
		Steps: acctest.Forget(t, []resource.TestStep{{
			Config: s.config(received, "  metadata = { env = \"test\" }\n") + `
data "stripe_treasury_debit_reversal" "by_id" {
  provider = stripe.connected
  id       = stripe_treasury_debit_reversal.test.id
}
`,
			Check: resource.ComposeAggregateTestCheckFunc(
				resource.TestMatchResourceAttr(resAddress, "id", regexp.MustCompile(`^debrev_`)),
				resource.TestCheckResourceAttr(resAddress, "received_debit", received),
				resource.TestCheckResourceAttr(resAddress, "amount", fmt.Sprint(amount)),
				resource.TestCheckResourceAttr(resAddress, "currency", "usd"),
				resource.TestCheckResourceAttr(resAddress, "financial_account", s.fa),
				resource.TestCheckResourceAttr(resAddress, "network", "ach"),
				resource.TestCheckResourceAttr(resAddress, "metadata.env", "test"),
				resource.TestCheckResourceAttrWith(resAddress, "status", func(v string) error {
					if v != "processing" && v != "succeeded" {
						return fmt.Errorf("status %s", v)
					}
					return nil
				}),
				resource.TestCheckResourceAttrPair("data.stripe_treasury_debit_reversal.by_id", "id", resAddress, "id"),
			),
		}}, nil, "stripe_treasury_debit_reversal"),
	})
}

// Stripe never updates a reversal, so nothing in place can change but the provider's own
// timeouts, which a plan applies without a replacement.
func TestAccTreasuryDebitReversal_update(t *testing.T) {
	s := newScope(t)
	received := s.received(t)
	var first string
	resource.ParallelTest(t, resource.TestCase{
		ProtoV6ProviderFactories: tfacc.ProviderFactories,
		Steps: acctest.Forget(t, []resource.TestStep{
			{Config: s.config(received, ""), Check: idOf(&first)},
			{
				Config: s.config(received, "  timeouts = { create = \"10m\" }\n"),
				ConfigPlanChecks: resource.ConfigPlanChecks{PreApply: []plancheck.PlanCheck{
					plancheck.ExpectResourceAction(resAddress, plancheck.ResourceActionUpdate),
				}},
				Check: resource.TestCheckResourceAttrWith(resAddress, "id", func(v string) error {
					if v != first {
						return fmt.Errorf("the in-place update changed the reversal from %s to %s", first, v)
					}
					return nil
				}),
			},
		}, nil, "stripe_treasury_debit_reversal"),
	})
}

// Reversing another received debit needs a new reversal, which fails the plan because a reversal
// cannot be destroyed, and so does a taint at apply.
func TestAccTreasuryDebitReversal_replace(t *testing.T) {
	s := newScope(t)
	received, other := s.received(t), s.received(t)
	cfg := s.config(received, "")
	resource.ParallelTest(t, resource.TestCase{
		ProtoV6ProviderFactories: tfacc.ProviderFactories,
		Steps: acctest.Forget(t, []resource.TestStep{
			{Config: cfg},
			{Config: s.config(other, ""), PlanOnly: true, ExpectError: tfacc.RefusedReplace},
			{Config: cfg, Taint: []string{resAddress}, ExpectError: tfacc.RefusedDestroy},
		}, nil, "stripe_treasury_debit_reversal"),
	})
}

// Stripe signs a new receipt URL on every read, and the reversal may move on between the reads.
func TestAccTreasuryDebitReversal_import(t *testing.T) {
	s := newScope(t)
	cfg := s.config(s.received(t), "  metadata = { env = \"test\" }\n")
	resource.ParallelTest(t, resource.TestCase{
		ProtoV6ProviderFactories: tfacc.ProviderFactories,
		Steps: acctest.Forget(t, []resource.TestStep{
			{Config: cfg},
			{
				ResourceName: resAddress, ImportState: true, ImportStateVerify: true,
				ImportStateVerifyIgnore: []string{"hosted_regulatory_receipt_url", "status", "status_transitions", "transaction"},
			},
			{ResourceName: resAddress, ImportState: true, ImportStateKind: resource.ImportBlockWithID, Config: cfg},
		}, nil, "stripe_treasury_debit_reversal"),
	})
}

// Stripe cannot delete a reversal, so dropping it from the configuration, and `terraform
// destroy`, fail at plan time, and the reversal stays.
func TestAccTreasuryDebitReversal_delete(t *testing.T) {
	s := newScope(t)
	var path string
	var mark int
	cfg := s.config(s.received(t), "")
	resource.ParallelTest(t, resource.TestCase{
		ProtoV6ProviderFactories: tfacc.ProviderFactories,
		Steps: acctest.Forget(t, []resource.TestStep{
			{Config: cfg, Check: resource.ComposeTestCheckFunc(pathOf(&path), tfacc.LogMark(t, s.conn, &mark))},
			{Config: acctest.Without(t, cfg, "stripe_treasury_debit_reversal"), PlanOnly: true, ExpectError: tfacc.RefusedDestroy},
			{Config: cfg, Destroy: true, ExpectError: tfacc.RefusedDestroy},
		}, tfacc.Untouched(t, s.conn, &path, &mark, "amount", wire.Int(amount)), "stripe_treasury_debit_reversal"),
	})
}

// Stripe moves a new reversal to `succeeded` by itself (suite: "Credit and debit reversals"). The
// state reads it and the plan changes nothing.
func TestAccTreasuryDebitReversal_drift(t *testing.T) {
	s := newScope(t)
	var path string
	cfg := s.config(s.received(t), "")
	resource.ParallelTest(t, resource.TestCase{
		ProtoV6ProviderFactories: tfacc.ProviderFactories,
		Steps: acctest.Forget(t, []resource.TestStep{
			{Config: cfg, Check: pathOf(&path)},
			{
				PreConfig: func() {
					tfacc.AwaitRetrieved(t, s.conn, path, func(o wire.Object) bool {
						status, _ := o.GetString("status")
						return status == "succeeded"
					})
				},
				Config: cfg,
				ConfigPlanChecks: resource.ConfigPlanChecks{PreApply: []plancheck.PlanCheck{
					plancheck.ExpectEmptyPlan(),
				}},
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr(resAddress, "status", "succeeded"),
					resource.TestCheckResourceAttrSet(resAddress, "status_transitions.completed_at"),
					resource.TestCheckResourceAttrSet(resAddress, "transaction"),
				),
			},
		}, nil, "stripe_treasury_debit_reversal"),
	})
}
