package transfer_reversal_test

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

const resAddress = "stripe_transfer_reversal.test"

// newTransfer is a 1000 transfer to a connected account, made behind the provider's back.
func newTransfer(t *testing.T, acct acctest.Config) string {
	t.Helper()
	return tfacc.Transfer(t, acct, tfacc.TransfersAccount(t, acct), tfacc.AccountCurrency(t, acct), 1000)
}

// reversalConfig is a reversal of transfer. extra lands inside it.
func reversalConfig(c acctest.Config, transfer, extra string) string {
	return c.ProviderBlock() + fmt.Sprintf(`
resource "stripe_transfer_reversal" "test" {
  transfer = %q
%s}
`, transfer, extra)
}

// reversalImportID is the transfer ID and the reversal ID, as the import ID wants them.
func reversalImportID(s *terraform.State) (string, error) {
	rs, ok := s.RootModule().Resources[resAddress]
	if !ok {
		return "", fmt.Errorf("%s is not in state", resAddress)
	}
	return rs.Primary.Attributes["transfer"] + "/" + rs.Primary.ID, nil
}

func reversalPath(transfer string, dst *string) resource.TestCheckFunc {
	return resource.TestCheckResourceAttrWith(resAddress, "id", func(v string) error {
		*dst = "/v1/transfers/" + transfer + "/reversals/" + v
		return nil
	})
}

// A partial reversal (suite: "create partial"), which the transfer then counts.
func TestAccTransferReversal_create(t *testing.T) {
	acct := acctest.NewAccount(t)
	transfer := newTransfer(t, acct)
	var transferPath = "/v1/transfers/" + transfer
	resource.ParallelTest(t, resource.TestCase{
		ProtoV6ProviderFactories: tfacc.ProviderFactories,
		Steps: acctest.Forget(t, []resource.TestStep{{
			Config: reversalConfig(acct, transfer, "  amount = 300\n  metadata = { reason = \"returned\" }\n") + `
data "stripe_transfer_reversal" "by_id" {
  transfer = stripe_transfer_reversal.test.transfer
  id       = stripe_transfer_reversal.test.id
}
`,
			Check: resource.ComposeAggregateTestCheckFunc(
				resource.TestCheckResourceAttr(resAddress, "amount", "300"),
				resource.TestCheckResourceAttr(resAddress, "transfer", transfer),
				resource.TestCheckResourceAttr(resAddress, "metadata.reason", "returned"),
				resource.TestMatchResourceAttr(resAddress, "balance_transaction", regexp.MustCompile(`^txn_`)),
				resource.TestCheckResourceAttrPair("data.stripe_transfer_reversal.by_id", "amount", resAddress, "amount"),
				tfacc.Holds(t, acct, &transferPath, "amount_reversed", wire.Int(300)),
			),
		}}, nil, "stripe_transfer_reversal"),
	})
}

// Without `amount`, the reversal is for what remains of the transfer (suite: "create the rest").
func TestAccTransferReversal_rest(t *testing.T) {
	acct := acctest.NewAccount(t)
	transfer := newTransfer(t, acct)
	resource.ParallelTest(t, resource.TestCase{
		ProtoV6ProviderFactories: tfacc.ProviderFactories,
		Steps: acctest.Forget(t, []resource.TestStep{{
			Config: reversalConfig(acct, transfer, ""),
			Check:  resource.TestCheckResourceAttr(resAddress, "amount", "1000"),
		}}, nil, "stripe_transfer_reversal"),
	})
}

// Only `metadata` changes in place (suite: "update").
func TestAccTransferReversal_update(t *testing.T) {
	acct := acctest.NewAccount(t)
	transfer := newTransfer(t, acct)
	resource.ParallelTest(t, resource.TestCase{
		ProtoV6ProviderFactories: tfacc.ProviderFactories,
		Steps: acctest.Forget(t, []resource.TestStep{
			{Config: reversalConfig(acct, transfer, "  amount = 300\n  metadata = { keep = \"me\", drop = \"me\" }\n")},
			{
				Config: reversalConfig(acct, transfer, "  amount = 300\n  metadata = { keep = \"me\", new = \"v\" }\n"),
				ConfigPlanChecks: resource.ConfigPlanChecks{PreApply: []plancheck.PlanCheck{
					plancheck.ExpectResourceAction(resAddress, plancheck.ResourceActionUpdate),
				}},
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr(resAddress, "metadata.%", "2"),
					resource.TestCheckResourceAttr(resAddress, "metadata.new", "v"),
				),
			},
		}, nil, "stripe_transfer_reversal"),
	})
}

// `amount` cannot change (suite: "immutable amount") and the reversal belongs to its transfer,
// and a reversal cannot be destroyed, so changing either fails the plan, and a taint at apply.
func TestAccTransferReversal_replace(t *testing.T) {
	acct := acctest.NewAccount(t)
	transfer := newTransfer(t, acct)
	cfg := reversalConfig(acct, transfer, "  amount = 300\n")
	resource.ParallelTest(t, resource.TestCase{
		ProtoV6ProviderFactories: tfacc.ProviderFactories,
		Steps: acctest.Forget(t, []resource.TestStep{
			{Config: cfg},
			{Config: reversalConfig(acct, transfer, "  amount = 400\n"), PlanOnly: true, ExpectError: tfacc.RefusedReplace},
			{Config: reversalConfig(acct, "tr_other", "  amount = 300\n"), PlanOnly: true, ExpectError: tfacc.RefusedReplace},
			{Config: cfg, Taint: []string{resAddress}, ExpectError: tfacc.RefusedDestroy},
		}, nil, "stripe_transfer_reversal"),
	})
}

func TestAccTransferReversal_import(t *testing.T) {
	acct := acctest.NewAccount(t)
	transfer := newTransfer(t, acct)
	cfg := reversalConfig(acct, transfer, "  amount = 300\n  metadata = { team = \"a\" }\n")
	resource.ParallelTest(t, resource.TestCase{
		ProtoV6ProviderFactories: tfacc.ProviderFactories,
		Steps: acctest.Forget(t, []resource.TestStep{
			{Config: cfg},
			{ResourceName: resAddress, ImportState: true, ImportStateIdFunc: reversalImportID, ImportStateVerify: true},
			{ResourceName: resAddress, ImportState: true, ImportStateKind: resource.ImportBlockWithID, ImportStateIdFunc: reversalImportID, Config: cfg},
		}, nil, "stripe_transfer_reversal"),
	})
}

// Stripe cannot delete or undo a reversal, so dropping it from the configuration, and
// `terraform destroy`, fail at plan time, and the reversal stays as it was.
func TestAccTransferReversal_delete(t *testing.T) {
	acct := acctest.NewAccount(t)
	transfer := newTransfer(t, acct)
	var path string
	var mark int
	cfg := reversalConfig(acct, transfer, "  amount = 300\n")
	resource.ParallelTest(t, resource.TestCase{
		ProtoV6ProviderFactories: tfacc.ProviderFactories,
		Steps: acctest.Forget(t, []resource.TestStep{
			{Config: cfg, Check: resource.ComposeTestCheckFunc(reversalPath(transfer, &path), tfacc.LogMark(t, acct, &mark))},
			{Config: acctest.Without(t, cfg, "stripe_transfer_reversal"), PlanOnly: true, ExpectError: tfacc.RefusedDestroy},
			{Config: cfg, Destroy: true, ExpectError: tfacc.RefusedDestroy},
		}, tfacc.Untouched(t, acct, &path, &mark, "amount", wire.Int(300)), "stripe_transfer_reversal"),
	})
}

func TestAccTransferReversal_drift(t *testing.T) {
	acct := acctest.NewAccount(t)
	transfer := newTransfer(t, acct)
	var path string
	cfg := reversalConfig(acct, transfer, "  amount = 300\n  metadata = { reason = \"returned\" }\n")
	resource.ParallelTest(t, resource.TestCase{
		ProtoV6ProviderFactories: tfacc.ProviderFactories,
		Steps: acctest.Forget(t, []resource.TestStep{
			{Config: cfg, Check: reversalPath(transfer, &path)},
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
		}, nil, "stripe_transfer_reversal"),
	})
}
