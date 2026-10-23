package application_fee_refund_test

import (
	"fmt"
	"regexp"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/plancheck"
	"github.com/hashicorp/terraform-plugin-testing/terraform"

	"github.com/computomatic/terraform-provider-stripe/internal/acctest"
	"github.com/computomatic/terraform-provider-stripe/internal/acctest/tfacc"
	"github.com/computomatic/terraform-provider-stripe/internal/wire"
)

const resAddress = "stripe_application_fee_refund.test"

// newFee is the 150 application fee of a destination charge, made behind the provider's back.
// The platform's available balance covers a refund of all of it, which the fake proves only
// within the balance.
func newFee(t *testing.T, acct acctest.Config) string {
	t.Helper()
	currency := tfacc.AccountCurrency(t, acct)
	_, fee := tfacc.DestinationCharge(t, acct, tfacc.TransfersAccount(t, acct), currency, 1000, 150)
	tfacc.FundAvailableBalance(t, acct, currency, 1000)
	return fee
}

// feeRefundConfig is a refund of fee. extra lands inside it.
func feeRefundConfig(c acctest.Config, fee, extra string) string {
	return c.ProviderBlock() + fmt.Sprintf(`
resource "stripe_application_fee_refund" "test" {
  fee = %q
%s}
`, fee, extra)
}

// feeRefundImportID is the fee ID and the refund ID, as the import ID wants them.
func feeRefundImportID(s *terraform.State) (string, error) {
	rs, ok := s.RootModule().Resources[resAddress]
	if !ok {
		return "", fmt.Errorf("%s is not in state", resAddress)
	}
	return rs.Primary.Attributes["fee"] + "/" + rs.Primary.ID, nil
}

func feeRefundPath(fee string, dst *string) resource.TestCheckFunc {
	return resource.TestCheckResourceAttrWith(resAddress, "id", func(v string) error {
		*dst = "/v1/application_fees/" + fee + "/refunds/" + v
		return nil
	})
}

// A partial refund (suite: "create partial"), which the fee then counts.
func TestAccApplicationFeeRefund_create(t *testing.T) {
	acct := acctest.NewAccount(t)
	fee := newFee(t, acct)
	feePath := "/v1/application_fees/" + fee
	resource.ParallelTest(t, resource.TestCase{
		ProtoV6ProviderFactories: tfacc.ProviderFactories,
		Steps: acctest.Forget(t, []resource.TestStep{{
			Config: feeRefundConfig(acct, fee, "  amount = 50\n  metadata = { reason = \"returned\" }\n") + `
data "stripe_application_fee_refund" "by_id" {
  fee = stripe_application_fee_refund.test.fee
  id  = stripe_application_fee_refund.test.id
}
`,
			Check: resource.ComposeAggregateTestCheckFunc(
				resource.TestCheckResourceAttr(resAddress, "amount", "50"),
				resource.TestCheckResourceAttr(resAddress, "fee", fee),
				resource.TestCheckResourceAttr(resAddress, "metadata.reason", "returned"),
				resource.TestMatchResourceAttr(resAddress, "balance_transaction", regexp.MustCompile(`^txn_`)),
				resource.TestCheckResourceAttrPair("data.stripe_application_fee_refund.by_id", "amount", resAddress, "amount"),
				tfacc.Holds(t, acct, &feePath, "amount_refunded", wire.Int(50)),
			),
		}}, nil, "stripe_application_fee_refund"),
	})
}

// Without `amount`, the refund is for what remains of the fee (suite: "create the rest").
func TestAccApplicationFeeRefund_rest(t *testing.T) {
	acct := acctest.NewAccount(t)
	fee := newFee(t, acct)
	resource.ParallelTest(t, resource.TestCase{
		ProtoV6ProviderFactories: tfacc.ProviderFactories,
		Steps: acctest.Forget(t, []resource.TestStep{{
			Config: feeRefundConfig(acct, fee, ""),
			Check:  resource.TestCheckResourceAttr(resAddress, "amount", "150"),
		}}, nil, "stripe_application_fee_refund"),
	})
}

// Only `metadata` changes in place (suite: "update").
func TestAccApplicationFeeRefund_update(t *testing.T) {
	acct := acctest.NewAccount(t)
	fee := newFee(t, acct)
	resource.ParallelTest(t, resource.TestCase{
		ProtoV6ProviderFactories: tfacc.ProviderFactories,
		Steps: acctest.Forget(t, []resource.TestStep{
			{Config: feeRefundConfig(acct, fee, "  amount = 50\n  metadata = { keep = \"me\", drop = \"me\" }\n")},
			{
				Config: feeRefundConfig(acct, fee, "  amount = 50\n  metadata = { keep = \"me\", new = \"v\" }\n"),
				ConfigPlanChecks: resource.ConfigPlanChecks{PreApply: []plancheck.PlanCheck{
					plancheck.ExpectResourceAction(resAddress, plancheck.ResourceActionUpdate),
				}},
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr(resAddress, "metadata.%", "2"),
					resource.TestCheckResourceAttr(resAddress, "metadata.new", "v"),
				),
			},
		}, nil, "stripe_application_fee_refund"),
	})
}

// `amount` cannot change (suite: "immutable amount") and the refund belongs to its fee, and a
// refund cannot be destroyed, so changing either fails the plan, and a taint at apply.
func TestAccApplicationFeeRefund_replace(t *testing.T) {
	acct := acctest.NewAccount(t)
	fee := newFee(t, acct)
	cfg := feeRefundConfig(acct, fee, "  amount = 50\n")
	resource.ParallelTest(t, resource.TestCase{
		ProtoV6ProviderFactories: tfacc.ProviderFactories,
		Steps: acctest.Forget(t, []resource.TestStep{
			{Config: cfg},
			{Config: feeRefundConfig(acct, fee, "  amount = 60\n"), PlanOnly: true, ExpectError: tfacc.RefusedReplace},
			{Config: feeRefundConfig(acct, "fee_other", "  amount = 50\n"), PlanOnly: true, ExpectError: tfacc.RefusedReplace},
			{Config: cfg, Taint: []string{resAddress}, ExpectError: tfacc.RefusedDestroy},
		}, nil, "stripe_application_fee_refund"),
	})
}

func TestAccApplicationFeeRefund_import(t *testing.T) {
	acct := acctest.NewAccount(t)
	fee := newFee(t, acct)
	cfg := feeRefundConfig(acct, fee, "  amount = 50\n  metadata = { team = \"a\" }\n")
	resource.ParallelTest(t, resource.TestCase{
		ProtoV6ProviderFactories: tfacc.ProviderFactories,
		Steps: acctest.Forget(t, []resource.TestStep{
			{Config: cfg},
			{ResourceName: resAddress, ImportState: true, ImportStateIdFunc: feeRefundImportID, ImportStateVerify: true},
			{ResourceName: resAddress, ImportState: true, ImportStateKind: resource.ImportBlockWithID, ImportStateIdFunc: feeRefundImportID, Config: cfg},
		}, nil, "stripe_application_fee_refund"),
	})
}

// Stripe cannot delete or undo a refund, so dropping it from the configuration, and `terraform
// destroy`, fail at plan time, and the refund stays as it was.
func TestAccApplicationFeeRefund_delete(t *testing.T) {
	acct := acctest.NewAccount(t)
	fee := newFee(t, acct)
	var path string
	var mark int
	cfg := feeRefundConfig(acct, fee, "  amount = 50\n")
	resource.ParallelTest(t, resource.TestCase{
		ProtoV6ProviderFactories: tfacc.ProviderFactories,
		Steps: acctest.Forget(t, []resource.TestStep{
			{Config: cfg, Check: resource.ComposeTestCheckFunc(feeRefundPath(fee, &path), tfacc.LogMark(t, acct, &mark))},
			{Config: acctest.Without(t, cfg, "stripe_application_fee_refund"), PlanOnly: true, ExpectError: tfacc.RefusedDestroy},
			{Config: cfg, Destroy: true, ExpectError: tfacc.RefusedDestroy},
		}, tfacc.Untouched(t, acct, &path, &mark, "amount", wire.Int(50)), "stripe_application_fee_refund"),
	})
}

func TestAccApplicationFeeRefund_drift(t *testing.T) {
	acct := acctest.NewAccount(t)
	fee := newFee(t, acct)
	var path string
	cfg := feeRefundConfig(acct, fee, "  amount = 50\n  metadata = { reason = \"returned\" }\n")
	resource.ParallelTest(t, resource.TestCase{
		ProtoV6ProviderFactories: tfacc.ProviderFactories,
		Steps: acctest.Forget(t, []resource.TestStep{
			{Config: cfg, Check: feeRefundPath(fee, &path)},
			{
				PreConfig: func() {
					tfacc.Post(t, acct, path, wire.Object{{Name: "metadata", Value: wire.Object{{Name: "reason", Value: wire.String("changed")}}}})
				},
				Config: cfg,
				ConfigPlanChecks: resource.ConfigPlanChecks{PreApply: []plancheck.PlanCheck{
					plancheck.ExpectNonEmptyPlan(),
					plancheck.ExpectResourceAction(resAddress, plancheck.ResourceActionUpdate),
				}},
				Check: resource.TestCheckResourceAttr(resAddress, "metadata.reason", "returned"),
			},
		}, nil, "stripe_application_fee_refund"),
	})
}
