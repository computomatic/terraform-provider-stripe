package treasury_outbound_transfer_test

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
	resAddress = "stripe_treasury_outbound_transfer.test"
	// amount is each transfer's, in cents. A transfer cannot be deleted, so it stays at a dollar.
	amount = 100
)

// scope is a connected account whose financial account holds enough for a few transfers and can
// send to dest, a second financial account of the same connected account.
type scope struct {
	acct, conn acctest.Config
	fa, dest   string
}

func newScope(t *testing.T) scope {
	t.Helper()
	acct := acctest.NewAccount(t)
	conn := tfacc.IssuingAccount(t, acct)
	fa := tfacc.FundedFinancialAccount(t, conn, 10*amount, "intra_stripe_flows")
	return scope{acct: acct, conn: conn, fa: fa, dest: tfacc.DestinationFinancialAccount(t, conn)}
}

// destinationData sends to the scope's second financial account (suite: "create to a financial
// account").
func (s scope) destinationData() string {
	return fmt.Sprintf(`
  destination_payment_method_data = {
    type              = "financial_account"
    financial_account = %q
  }
`, s.dest)
}

// config is a transfer of cents to destination, the HCL that names it. extra lands inside it.
func (s scope) config(cents int, destination, extra string) string {
	return s.acct.ProviderBlock() + s.conn.ConnectedProviderBlock() + fmt.Sprintf(`
resource "stripe_treasury_outbound_transfer" "test" {
  provider          = stripe.connected
  financial_account = %q
  amount            = %d
  currency          = "usd"
%s%s}
`, s.fa, cents, destination, extra)
}

func idOf(dst *string) resource.TestCheckFunc {
	return resource.TestCheckResourceAttrWith(resAddress, "id", func(v string) error {
		*dst = v
		return nil
	})
}

func pathOf(dst *string) resource.TestCheckFunc {
	return resource.TestCheckResourceAttrWith(resAddress, "id", func(v string) error {
		*dst = "/v1/treasury/outbound_transfers/" + v
		return nil
	})
}

// A transfer to a financial account names it in its details and posts at once (suite: "create
// to a financial account", "posts itself").
func TestAccTreasuryOutboundTransfer_create(t *testing.T) {
	s := newScope(t)
	resource.ParallelTest(t, resource.TestCase{
		ProtoV6ProviderFactories: tfacc.ProviderFactories,
		Steps: acctest.Forget(t, []resource.TestStep{{
			Config: s.config(amount, s.destinationData(), "  metadata = { env = \"test\" }\n") + `
data "stripe_treasury_outbound_transfer" "by_id" {
  provider = stripe.connected
  id       = stripe_treasury_outbound_transfer.test.id
}
`,
			Check: resource.ComposeAggregateTestCheckFunc(
				resource.TestMatchResourceAttr(resAddress, "id", regexp.MustCompile(`^obt_`)),
				resource.TestCheckResourceAttr(resAddress, "amount", fmt.Sprint(amount)),
				resource.TestCheckResourceAttr(resAddress, "financial_account", s.fa),
				resource.TestCheckResourceAttr(resAddress, "metadata.env", "test"),
				resource.TestCheckResourceAttr(resAddress, "statement_descriptor", "transfer"),
				resource.TestCheckResourceAttr(resAddress, "destination_payment_method_details.type", "financial_account"),
				resource.TestCheckResourceAttr(resAddress, "destination_payment_method_details.financial_account.id", s.dest),
				resource.TestCheckResourceAttrWith(resAddress, "status", func(v string) error {
					if v != "processing" && v != "posted" {
						return fmt.Errorf("status %s", v)
					}
					return nil
				}),
				resource.TestCheckResourceAttrPair("data.stripe_treasury_outbound_transfer.by_id", "id", resAddress, "id"),
			),
		}}, nil, "stripe_treasury_outbound_transfer"),
	})
}

// Stripe never updates an outbound transfer, so nothing in place can change but `status` and the
// provider's own timeouts, which a plan applies without a replacement.
func TestAccTreasuryOutboundTransfer_update(t *testing.T) {
	s := newScope(t)
	var first string
	resource.ParallelTest(t, resource.TestCase{
		ProtoV6ProviderFactories: tfacc.ProviderFactories,
		Steps: acctest.Forget(t, []resource.TestStep{
			{Config: s.config(amount, s.destinationData(), ""), Check: idOf(&first)},
			{
				Config: s.config(amount, s.destinationData(), "  timeouts = { create = \"10m\" }\n"),
				ConfigPlanChecks: resource.ConfigPlanChecks{PreApply: []plancheck.PlanCheck{
					plancheck.ExpectResourceAction(resAddress, plancheck.ResourceActionUpdate),
				}},
				Check: resource.TestCheckResourceAttrWith(resAddress, "id", func(v string) error {
					if v != first {
						return fmt.Errorf("the in-place update changed the transfer from %s to %s", first, v)
					}
					return nil
				}),
			},
		}, nil, "stripe_treasury_outbound_transfer"),
	})
}

// Every argument is create-only and a transfer cannot be destroyed, so a new `amount` fails the
// plan, and so does a taint at apply.
func TestAccTreasuryOutboundTransfer_replace(t *testing.T) {
	s := newScope(t)
	cfg := s.config(amount, s.destinationData(), "")
	resource.ParallelTest(t, resource.TestCase{
		ProtoV6ProviderFactories: tfacc.ProviderFactories,
		Steps: acctest.Forget(t, []resource.TestStep{
			{Config: cfg},
			{Config: s.config(2*amount, s.destinationData(), ""), PlanOnly: true, ExpectError: tfacc.RefusedReplace},
			{Config: cfg, Taint: []string{resAddress}, ExpectError: tfacc.RefusedDestroy},
		}, nil, "stripe_treasury_outbound_transfer"),
	})
}

// Stripe does not return `destination_payment_method_data`, so the import block's configuration
// ignores it, as the import note says. Stripe signs a new receipt URL on every read, and the
// transfer posts between the create and the import.
func TestAccTreasuryOutboundTransfer_import(t *testing.T) {
	s := newScope(t)
	cfg := s.config(amount, s.destinationData(), "  lifecycle {\n    ignore_changes = [destination_payment_method_data]\n  }\n")
	resource.ParallelTest(t, resource.TestCase{
		ProtoV6ProviderFactories: tfacc.ProviderFactories,
		Steps: acctest.Forget(t, []resource.TestStep{
			{Config: cfg},
			{
				Config: cfg, ResourceName: resAddress, ImportState: true, ImportStateVerify: true,
				ImportStateVerifyIgnore: []string{"destination_payment_method_data", "hosted_regulatory_receipt_url", "status", "cancelable", "status_transitions"},
			},
			{ResourceName: resAddress, ImportState: true, ImportStateKind: resource.ImportBlockWithID, Config: cfg},
		}, nil, "stripe_treasury_outbound_transfer"),
	})
}

// Stripe cannot delete an outbound transfer, so dropping it from the configuration, and
// `terraform destroy`, fail at plan time, and the transfer stays.
func TestAccTreasuryOutboundTransfer_delete(t *testing.T) {
	s := newScope(t)
	var path string
	var mark int
	cfg := s.config(amount, s.destinationData(), "")
	resource.ParallelTest(t, resource.TestCase{
		ProtoV6ProviderFactories: tfacc.ProviderFactories,
		Steps: acctest.Forget(t, []resource.TestStep{
			{Config: cfg, Check: resource.ComposeTestCheckFunc(pathOf(&path), tfacc.LogMark(t, s.conn, &mark))},
			{Config: acctest.Without(t, cfg, "stripe_treasury_outbound_transfer"), PlanOnly: true, ExpectError: tfacc.RefusedDestroy},
			{Config: cfg, Destroy: true, ExpectError: tfacc.RefusedDestroy},
		}, tfacc.Untouched(t, s.conn, &path, &mark, "amount", wire.Int(amount)), "stripe_treasury_outbound_transfer"),
	})
}

// Stripe posts a transfer to a financial account by itself (suite: "posts itself"). The state
// reads `posted` and the plan does not try to put `status` back.
func TestAccTreasuryOutboundTransfer_drift(t *testing.T) {
	s := newScope(t)
	var path string
	cfg := s.config(amount, s.destinationData(), "")
	resource.ParallelTest(t, resource.TestCase{
		ProtoV6ProviderFactories: tfacc.ProviderFactories,
		Steps: acctest.Forget(t, []resource.TestStep{
			{Config: cfg, Check: pathOf(&path)},
			{
				PreConfig: func() {
					tfacc.AwaitRetrieved(t, s.conn, path, func(o wire.Object) bool {
						status, _ := o.GetString("status")
						return status == "posted"
					})
				},
				Config: cfg,
				ConfigPlanChecks: resource.ConfigPlanChecks{PreApply: []plancheck.PlanCheck{
					plancheck.ExpectEmptyPlan(),
				}},
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr(resAddress, "status", "posted"),
					resource.TestCheckResourceAttrSet(resAddress, "status_transitions.posted_at"),
				),
			},
		}, nil, "stripe_treasury_outbound_transfer"),
	})
}

// Stripe refuses to cancel a transfer to a financial account, which it posts at once (suite:
// "cancel to a financial account is refused"), so `status = "canceled"` fails the apply with
// Stripe's answer, and the state holds the status Stripe left.
func TestAccTreasuryOutboundTransfer_cancelRefused(t *testing.T) {
	s := newScope(t)
	resource.ParallelTest(t, resource.TestCase{
		ProtoV6ProviderFactories: tfacc.ProviderFactories,
		Steps: acctest.Forget(t, []resource.TestStep{{
			Config:      s.config(amount, s.destinationData(), "  status = \"canceled\"\n"),
			ExpectError: regexp.MustCompile("The OutboundTransfer is not cancelable"),
		}}, nil, "stripe_treasury_outbound_transfer"),
	})
}

// A transfer to the connected account's own bank account names it and its bank details, and
// posts within about a second (suite: "to the account's own bank account"). A cancel races that
// posting, on the fake too, which posts it a second after `created`, so no test sends one.
func TestAccTreasuryOutboundTransfer_toBankAccount(t *testing.T) {
	s := newScope(t)
	tfacc.TreasuryFeatures(t, s.conn, s.fa, "outbound_transfers.ach")
	bank := tfacc.SelfBankAccount(t, s.acct, s.conn)
	resource.ParallelTest(t, resource.TestCase{
		ProtoV6ProviderFactories: tfacc.ProviderFactories,
		Steps: acctest.Forget(t, []resource.TestStep{{
			Config: s.config(amount, fmt.Sprintf("  destination_payment_method = %q\n", bank), ""),
			Check: resource.ComposeAggregateTestCheckFunc(
				resource.TestCheckResourceAttr(resAddress, "destination_payment_method", bank),
				resource.TestCheckResourceAttr(resAddress, "destination_payment_method_details.type", "us_bank_account"),
				resource.TestCheckResourceAttr(resAddress, "destination_payment_method_details.us_bank_account.last4", "6789"),
				resource.TestCheckResourceAttrWith(resAddress, "status", func(v string) error {
					if v != "processing" && v != "posted" {
						return fmt.Errorf("status %s", v)
					}
					return nil
				}),
			),
		}}, nil, "stripe_treasury_outbound_transfer"),
	})
}
