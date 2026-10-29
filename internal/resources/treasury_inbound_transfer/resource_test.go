package treasury_inbound_transfer_test

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
	resAddress = "stripe_treasury_inbound_transfer.test"
	// amount is each transfer's, in cents. A transfer cannot be deleted, so it stays at a dollar.
	amount = 100
)

// scope is a connected account whose financial account can pull by ACH from bank, a US bank
// account attached to the connected account itself (suite: "from the account's own bank
// account"). A sandbox that refuses the attach skips the test before anything is created.
type scope struct {
	acct, conn acctest.Config
	fa, bank   string
}

func newScope(t *testing.T) scope {
	t.Helper()
	acct := acctest.NewAccount(t)
	conn := tfacc.IssuingAccount(t, acct)
	fa := tfacc.IssuingFinancialAccount(t, conn)
	tfacc.TreasuryFeatures(t, conn, fa, "inbound_transfers.ach")
	return scope{acct: acct, conn: conn, fa: fa, bank: tfacc.SelfBankAccount(t, acct, conn)}
}

// config is a transfer of cents from the scope's bank account. extra lands inside it.
func (s scope) config(cents int, extra string) string {
	return s.acct.ProviderBlock() + s.conn.ConnectedProviderBlock() + fmt.Sprintf(`
resource "stripe_treasury_inbound_transfer" "test" {
  provider              = stripe.connected
  financial_account     = %q
  amount                = %d
  currency              = "usd"
  origin_payment_method = %q
%s}
`, s.fa, cents, s.bank, extra)
}

func idOf(dst *string) resource.TestCheckFunc {
	return resource.TestCheckResourceAttrWith(resAddress, "id", func(v string) error {
		*dst = v
		return nil
	})
}

func pathOf(dst *string) resource.TestCheckFunc {
	return resource.TestCheckResourceAttrWith(resAddress, "id", func(v string) error {
		*dst = "/v1/treasury/inbound_transfers/" + v
		return nil
	})
}

// A new inbound transfer is processing and cancelable, with no transaction yet (suite:
// "create").
func TestAccTreasuryInboundTransfer_create(t *testing.T) {
	s := newScope(t)
	resource.ParallelTest(t, resource.TestCase{
		ProtoV6ProviderFactories: tfacc.ProviderFactories,
		Steps: acctest.Forget(t, []resource.TestStep{{
			Config: s.config(amount, "  metadata = { env = \"test\" }\n") + `
data "stripe_treasury_inbound_transfer" "by_id" {
  provider = stripe.connected
  id       = stripe_treasury_inbound_transfer.test.id
}
`,
			Check: resource.ComposeAggregateTestCheckFunc(
				resource.TestMatchResourceAttr(resAddress, "id", regexp.MustCompile(`^ibt_`)),
				resource.TestCheckResourceAttr(resAddress, "amount", fmt.Sprint(amount)),
				resource.TestCheckResourceAttr(resAddress, "financial_account", s.fa),
				resource.TestCheckResourceAttr(resAddress, "origin_payment_method", s.bank),
				resource.TestCheckResourceAttr(resAddress, "origin_payment_method_details.type", "us_bank_account"),
				resource.TestCheckResourceAttr(resAddress, "metadata.env", "test"),
				resource.TestCheckResourceAttr(resAddress, "statement_descriptor", "transfer"),
				resource.TestCheckResourceAttr(resAddress, "status", "processing"),
				resource.TestCheckResourceAttr(resAddress, "cancelable", "true"),
				resource.TestCheckResourceAttrPair("data.stripe_treasury_inbound_transfer.by_id", "id", resAddress, "id"),
			),
		}}, nil, "stripe_treasury_inbound_transfer"),
	})
}

// `status = "canceled"` cancels a processing transfer in place (suite: "cancel"), and the same
// value again changes nothing.
func TestAccTreasuryInboundTransfer_update(t *testing.T) {
	s := newScope(t)
	var first, path string
	canceled := s.config(amount, "  status = \"canceled\"\n")
	resource.ParallelTest(t, resource.TestCase{
		ProtoV6ProviderFactories: tfacc.ProviderFactories,
		Steps: acctest.Forget(t, []resource.TestStep{
			{Config: s.config(amount, ""), Check: resource.ComposeTestCheckFunc(idOf(&first), pathOf(&path))},
			{
				Config: canceled,
				ConfigPlanChecks: resource.ConfigPlanChecks{PreApply: []plancheck.PlanCheck{
					plancheck.ExpectResourceAction(resAddress, plancheck.ResourceActionUpdate),
				}},
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttrWith(resAddress, "id", func(v string) error {
						if v != first {
							return fmt.Errorf("the cancel changed the transfer from %s to %s", first, v)
						}
						return nil
					}),
					resource.TestCheckResourceAttr(resAddress, "status", "canceled"),
					resource.TestCheckResourceAttr(resAddress, "cancelable", "false"),
					resource.TestCheckResourceAttrSet(resAddress, "status_transitions.canceled_at"),
					tfacc.Holds(t, s.conn, &path, "status", wire.String("canceled")),
				),
			},
			{Config: canceled, PlanOnly: true},
		}, nil, "stripe_treasury_inbound_transfer"),
	})
}

// Every argument is create-only and a transfer cannot be destroyed, so a new `amount` fails the
// plan, and so does a taint at apply.
func TestAccTreasuryInboundTransfer_replace(t *testing.T) {
	s := newScope(t)
	cfg := s.config(amount, "")
	resource.ParallelTest(t, resource.TestCase{
		ProtoV6ProviderFactories: tfacc.ProviderFactories,
		Steps: acctest.Forget(t, []resource.TestStep{
			{Config: cfg},
			{Config: s.config(2*amount, ""), PlanOnly: true, ExpectError: tfacc.RefusedReplace},
			{Config: cfg, Taint: []string{resAddress}, ExpectError: tfacc.RefusedDestroy},
		}, nil, "stripe_treasury_inbound_transfer"),
	})
}

// Stripe signs a new receipt URL on every read.
func TestAccTreasuryInboundTransfer_import(t *testing.T) {
	s := newScope(t)
	cfg := s.config(amount, "  metadata = { env = \"test\" }\n")
	resource.ParallelTest(t, resource.TestCase{
		ProtoV6ProviderFactories: tfacc.ProviderFactories,
		Steps: acctest.Forget(t, []resource.TestStep{
			{Config: cfg},
			{ResourceName: resAddress, ImportState: true, ImportStateVerify: true, ImportStateVerifyIgnore: []string{"hosted_regulatory_receipt_url"}},
			{ResourceName: resAddress, ImportState: true, ImportStateKind: resource.ImportBlockWithID, Config: cfg},
		}, nil, "stripe_treasury_inbound_transfer"),
	})
}

// Stripe cannot delete an inbound transfer, and destroy never cancels one, so dropping it from
// the configuration, and `terraform destroy`, fail at plan time, and the transfer stays
// processing.
func TestAccTreasuryInboundTransfer_delete(t *testing.T) {
	s := newScope(t)
	var path string
	var mark int
	cfg := s.config(amount, "")
	resource.ParallelTest(t, resource.TestCase{
		ProtoV6ProviderFactories: tfacc.ProviderFactories,
		Steps: acctest.Forget(t, []resource.TestStep{
			{Config: cfg, Check: resource.ComposeTestCheckFunc(pathOf(&path), tfacc.LogMark(t, s.conn, &mark))},
			{Config: acctest.Without(t, cfg, "stripe_treasury_inbound_transfer"), PlanOnly: true, ExpectError: tfacc.RefusedDestroy},
			{Config: cfg, Destroy: true, ExpectError: tfacc.RefusedDestroy},
		}, tfacc.Untouched(t, s.conn, &path, &mark, "status", wire.String("processing")), "stripe_treasury_inbound_transfer"),
	})
}

// A transfer canceled out of band reads `canceled`, and a configuration that leaves `status`
// unset plans nothing for it.
func TestAccTreasuryInboundTransfer_drift(t *testing.T) {
	s := newScope(t)
	var path string
	cfg := s.config(amount, "")
	resource.ParallelTest(t, resource.TestCase{
		ProtoV6ProviderFactories: tfacc.ProviderFactories,
		Steps: acctest.Forget(t, []resource.TestStep{
			{Config: cfg, Check: pathOf(&path)},
			{
				PreConfig: func() { tfacc.Post(t, s.conn, path+"/cancel", nil) },
				Config:    cfg,
				ConfigPlanChecks: resource.ConfigPlanChecks{PreApply: []plancheck.PlanCheck{
					plancheck.ExpectEmptyPlan(),
				}},
				Check: resource.TestCheckResourceAttr(resAddress, "status", "canceled"),
			},
		}, nil, "stripe_treasury_inbound_transfer"),
	})
}

// Stripe refuses to cancel a transfer that has succeeded (suite: "cancel after succeed"), so once
// the state reads `succeeded`, a configuration that sets `canceled` fails the plan.
func TestAccTreasuryInboundTransfer_cancelSucceeded(t *testing.T) {
	s := newScope(t)
	var id string
	resource.ParallelTest(t, resource.TestCase{
		ProtoV6ProviderFactories: tfacc.ProviderFactories,
		Steps: acctest.Forget(t, []resource.TestStep{
			{Config: s.config(amount, ""), Check: idOf(&id)},
			// An apply that changes nothing refreshes the state to `succeeded`.
			{
				PreConfig: func() { tfacc.Post(t, s.conn, "/v1/test_helpers/treasury/inbound_transfers/"+id+"/succeed", nil) },
				Config:    s.config(amount, ""),
				Check:     resource.TestCheckResourceAttr(resAddress, "status", "succeeded"),
			},
			{
				Config:      s.config(amount, "  status = \"canceled\"\n"),
				PlanOnly:    true,
				ExpectError: regexp.MustCompile("Cannot change `status` from `succeeded`"),
			},
		}, nil, "stripe_treasury_inbound_transfer"),
	})
}
