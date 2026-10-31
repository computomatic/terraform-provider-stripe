package transfer_test

import (
	"fmt"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/knownvalue"
	"github.com/hashicorp/terraform-plugin-testing/plancheck"
	"github.com/hashicorp/terraform-plugin-testing/tfjsonpath"

	"github.com/computomatic/terraform-provider-stripe/internal/acctest"
	"github.com/computomatic/terraform-provider-stripe/internal/acctest/tfacc"
	"github.com/computomatic/terraform-provider-stripe/internal/wire"
)

const resAddress = "stripe_transfer.test"

// transferSetup funds the platform and returns a connected account that takes transfers, and
// the account's currency.
func transferSetup(t *testing.T, acct acctest.Config) (destination, currency string) {
	t.Helper()
	currency = tfacc.AccountCurrency(t, acct)
	destination = tfacc.TransfersAccount(t, acct)
	tfacc.FundAvailableBalance(t, acct, currency, 5000)
	return destination, currency
}

// transferConfig is a transfer of amount to destination. extra lands inside it.
func transferConfig(c acctest.Config, destination, currency string, amount int, extra string) string {
	return c.ProviderBlock() + fmt.Sprintf(`
resource "stripe_transfer" "test" {
  amount      = %d
  currency    = %q
  destination = %q
%s}
`, amount, currency, destination, extra)
}

func transferPath(dst *string) resource.TestCheckFunc {
	return resource.TestCheckResourceAttrWith(resAddress, "id", func(v string) error {
		*dst = "/v1/transfers/" + v
		return nil
	})
}

// A transfer funded by a payment takes the transfer group of the payment's intent, which it holds
// through an update, as the suite's fixture of it does (TestTransferAttributes, probe
// `every_field/2`, whose `source_transaction` Stripe refuses beside a `source_type`).
func TestAccTransfer_sourceTransaction(t *testing.T) {
	acct := acctest.NewAccount(t)
	destination, currency := transferSetup(t, acct)
	intent, charge := tfacc.SourceCharge(t, acct, currency, 2000)
	group := "group_" + intent
	source := fmt.Sprintf("  source_transaction = %q\n", charge)
	resource.ParallelTest(t, resource.TestCase{
		ProtoV6ProviderFactories: tfacc.ProviderFactories,
		Steps: acctest.Forget(t, []resource.TestStep{
			{
				Config: transferConfig(acct, destination, currency, 100, source),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr(resAddress, "source_transaction", charge),
					resource.TestCheckResourceAttr(resAddress, "transfer_group", group),
				),
			},
			{Config: transferConfig(acct, destination, currency, 100, source), PlanOnly: true},
			{
				Config: transferConfig(acct, destination, currency, 100, source+"  description = \"Seller share\"\n"),
				ConfigPlanChecks: resource.ConfigPlanChecks{PreApply: []plancheck.PlanCheck{
					plancheck.ExpectResourceAction(resAddress, plancheck.ResourceActionUpdate),
					plancheck.ExpectKnownValue(resAddress, tfjsonpath.New("transfer_group"), knownvalue.StringExact(group)),
				}},
				Check: resource.TestCheckResourceAttr(resAddress, "transfer_group", group),
			},
		}, nil, "stripe_transfer"),
	})
}

// A transfer of the balance has no group, and an update plans that as known.
func TestAccTransfer_noGroup(t *testing.T) {
	acct := acctest.NewAccount(t)
	destination, currency := transferSetup(t, acct)
	resource.ParallelTest(t, resource.TestCase{
		ProtoV6ProviderFactories: tfacc.ProviderFactories,
		Steps: acctest.Forget(t, []resource.TestStep{
			{
				Config: transferConfig(acct, destination, currency, 100, ""),
				Check:  resource.TestCheckNoResourceAttr(resAddress, "transfer_group"),
			},
			{
				Config: transferConfig(acct, destination, currency, 100, "  description = \"Seller share\"\n"),
				ConfigPlanChecks: resource.ConfigPlanChecks{PreApply: []plancheck.PlanCheck{
					plancheck.ExpectResourceAction(resAddress, plancheck.ResourceActionUpdate),
					plancheck.ExpectKnownValue(resAddress, tfjsonpath.New("transfer_group"), knownvalue.Null()),
				}},
			},
		}, nil, "stripe_transfer"),
	})
}

// A transfer with every argument the suite proves (suite: "create").
func TestAccTransfer_create(t *testing.T) {
	acct := acctest.NewAccount(t)
	destination, currency := transferSetup(t, acct)
	resource.ParallelTest(t, resource.TestCase{
		ProtoV6ProviderFactories: tfacc.ProviderFactories,
		Steps: acctest.Forget(t, []resource.TestStep{{
			Config: transferConfig(acct, destination, currency, 500, "  description = \"Seller share\"\n  transfer_group = \"order-1\"\n  metadata = { order = \"1\" }\n") + `
data "stripe_transfer" "by_id" {
  id = stripe_transfer.test.id
}
`,
			Check: resource.ComposeAggregateTestCheckFunc(
				resource.TestCheckResourceAttr(resAddress, "amount", "500"),
				resource.TestCheckResourceAttr(resAddress, "destination", destination),
				resource.TestCheckResourceAttr(resAddress, "description", "Seller share"),
				resource.TestCheckResourceAttr(resAddress, "transfer_group", "order-1"),
				resource.TestCheckResourceAttr(resAddress, "metadata.order", "1"),
				resource.TestCheckResourceAttr(resAddress, "amount_reversed", "0"),
				resource.TestCheckResourceAttr(resAddress, "reversed", "false"),
				resource.TestCheckResourceAttrPair("data.stripe_transfer.by_id", "id", resAddress, "id"),
			),
		}}, nil, "stripe_transfer"),
	})
}

// `description` and `metadata` change in place (suite: "update").
func TestAccTransfer_update(t *testing.T) {
	acct := acctest.NewAccount(t)
	destination, currency := transferSetup(t, acct)
	resource.ParallelTest(t, resource.TestCase{
		ProtoV6ProviderFactories: tfacc.ProviderFactories,
		Steps: acctest.Forget(t, []resource.TestStep{
			{Config: transferConfig(acct, destination, currency, 500, "  description = \"one\"\n  metadata = { keep = \"me\", drop = \"me\" }\n")},
			{
				Config: transferConfig(acct, destination, currency, 500, "  description = \"two\"\n  metadata = { keep = \"me\", new = \"v\" }\n"),
				ConfigPlanChecks: resource.ConfigPlanChecks{PreApply: []plancheck.PlanCheck{
					plancheck.ExpectResourceAction(resAddress, plancheck.ResourceActionUpdate),
				}},
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr(resAddress, "description", "two"),
					resource.TestCheckResourceAttr(resAddress, "metadata.%", "2"),
					resource.TestCheckResourceAttr(resAddress, "metadata.new", "v"),
				),
			},
		}, nil, "stripe_transfer"),
	})
}

// `amount` cannot change (suite: "immutable"), and a transfer cannot be destroyed.
func TestAccTransfer_replace(t *testing.T) {
	acct := acctest.NewAccount(t)
	destination, currency := transferSetup(t, acct)
	cfg := transferConfig(acct, destination, currency, 500, "")
	resource.ParallelTest(t, resource.TestCase{
		ProtoV6ProviderFactories: tfacc.ProviderFactories,
		Steps: acctest.Forget(t, []resource.TestStep{
			{Config: cfg},
			{Config: transferConfig(acct, destination, currency, 600, ""), PlanOnly: true, ExpectError: tfacc.RefusedReplace},
			{Config: cfg, Taint: []string{resAddress}, ExpectError: tfacc.RefusedDestroy},
		}, nil, "stripe_transfer"),
	})
}

func TestAccTransfer_import(t *testing.T) {
	acct := acctest.NewAccount(t)
	destination, currency := transferSetup(t, acct)
	cfg := transferConfig(acct, destination, currency, 500, "  description = \"Seller share\"\n  metadata = { team = \"a\" }\n")
	resource.ParallelTest(t, resource.TestCase{
		ProtoV6ProviderFactories: tfacc.ProviderFactories,
		Steps: acctest.Forget(t, []resource.TestStep{
			{Config: cfg},
			{ResourceName: resAddress, ImportState: true, ImportStateVerify: true},
			{ResourceName: resAddress, ImportState: true, ImportStateKind: resource.ImportBlockWithID, Config: cfg},
		}, nil, "stripe_transfer"),
	})
}

// Stripe cannot delete a transfer, and destroy never reverses one, so dropping it from the
// configuration, and `terraform destroy`, fail at plan time, and nothing is reversed.
func TestAccTransfer_delete(t *testing.T) {
	acct := acctest.NewAccount(t)
	destination, currency := transferSetup(t, acct)
	var path string
	var mark int
	cfg := transferConfig(acct, destination, currency, 500, "")
	resource.ParallelTest(t, resource.TestCase{
		ProtoV6ProviderFactories: tfacc.ProviderFactories,
		Steps: acctest.Forget(t, []resource.TestStep{
			{Config: cfg, Check: resource.ComposeTestCheckFunc(transferPath(&path), tfacc.LogMark(t, acct, &mark))},
			{Config: acctest.Without(t, cfg, "stripe_transfer"), PlanOnly: true, ExpectError: tfacc.RefusedDestroy},
			{Config: cfg, Destroy: true, ExpectError: tfacc.RefusedDestroy},
		}, tfacc.Untouched(t, acct, &path, &mark, "amount_reversed", wire.Int(0)), "stripe_transfer"),
	})
}

func TestAccTransfer_drift(t *testing.T) {
	acct := acctest.NewAccount(t)
	destination, currency := transferSetup(t, acct)
	var path string
	cfg := transferConfig(acct, destination, currency, 500, "  description = \"Seller share\"\n")
	resource.ParallelTest(t, resource.TestCase{
		ProtoV6ProviderFactories: tfacc.ProviderFactories,
		Steps: acctest.Forget(t, []resource.TestStep{
			{Config: cfg, Check: transferPath(&path)},
			{
				PreConfig: func() {
					tfacc.Post(t, acct, path, wire.Object{{Name: "description", Value: wire.String("changed")}})
				},
				Config: cfg,
				ConfigPlanChecks: resource.ConfigPlanChecks{PreApply: []plancheck.PlanCheck{
					plancheck.ExpectNonEmptyPlan(),
					plancheck.ExpectResourceAction(resAddress, plancheck.ResourceActionUpdate),
				}},
				Check: resource.TestCheckResourceAttr(resAddress, "description", "Seller share"),
			},
		}, nil, "stripe_transfer"),
	})
}
