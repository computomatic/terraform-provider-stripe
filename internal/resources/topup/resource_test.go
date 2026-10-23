package topup_test

import (
	"fmt"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/plancheck"
	"github.com/hashicorp/terraform-plugin-testing/terraform"

	"github.com/computomatic/terraform-provider-stripe/internal/acctest"
	"github.com/computomatic/terraform-provider-stripe/internal/acctest/tfacc"
	"github.com/computomatic/terraform-provider-stripe/internal/wire"
)

const resAddress = "stripe_topup.test"

// topupConfig is a top-up of amount cents. extra lands inside it.
func topupConfig(c acctest.Config, amount int, extra string) string {
	return c.ProviderBlock() + fmt.Sprintf(`
resource "stripe_topup" "test" {
  amount   = %d
  currency = "usd"
%s}
`, amount, extra)
}

func topupPath(dst *string) resource.TestCheckFunc {
	return resource.TestCheckResourceAttrWith(resAddress, "id", func(v string) error {
		*dst = "/v1/topups/" + v
		return nil
	})
}

// A top-up starts pending (suite: "create").
func TestAccTopup_create(t *testing.T) {
	acct := acctest.NewAccount(t)
	resource.ParallelTest(t, resource.TestCase{
		ProtoV6ProviderFactories: tfacc.ProviderFactories,
		Steps: acctest.Forget(t, []resource.TestStep{{
			Config: topupConfig(acct, 1000, "  description = \"Test funds\"\n  metadata = { env = \"test\" }\n") + `
data "stripe_topup" "by_id" {
  id = stripe_topup.test.id
}
`,
			Check: resource.ComposeAggregateTestCheckFunc(
				resource.TestCheckResourceAttr(resAddress, "amount", "1000"),
				resource.TestCheckResourceAttr(resAddress, "description", "Test funds"),
				resource.TestCheckResourceAttr(resAddress, "metadata.env", "test"),
				resource.TestCheckResourceAttrWith(resAddress, "status", func(v string) error {
					if v != "pending" && v != "succeeded" {
						return fmt.Errorf("status %s", v)
					}
					return nil
				}),
				resource.TestCheckResourceAttrPair("data.stripe_topup.by_id", "id", resAddress, "id"),
			),
		}}, nil, "stripe_topup"),
	})
}

// `description` and `metadata` change in place (suite: "update").
func TestAccTopup_update(t *testing.T) {
	acct := acctest.NewAccount(t)
	resource.ParallelTest(t, resource.TestCase{
		ProtoV6ProviderFactories: tfacc.ProviderFactories,
		Steps: acctest.Forget(t, []resource.TestStep{
			{Config: topupConfig(acct, 1000, "  description = \"one\"\n  metadata = { keep = \"me\", drop = \"me\" }\n")},
			{
				Config: topupConfig(acct, 1000, "  description = \"two\"\n  metadata = { keep = \"me\", new = \"v\" }\n"),
				ConfigPlanChecks: resource.ConfigPlanChecks{PreApply: []plancheck.PlanCheck{
					plancheck.ExpectResourceAction(resAddress, plancheck.ResourceActionUpdate),
				}},
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr(resAddress, "description", "two"),
					resource.TestCheckResourceAttr(resAddress, "metadata.%", "2"),
					resource.TestCheckResourceAttr(resAddress, "metadata.new", "v"),
				),
			},
		}, nil, "stripe_topup"),
	})
}

// `amount` cannot change (suite: "immutable amount"), and a top-up cannot be destroyed.
func TestAccTopup_replace(t *testing.T) {
	acct := acctest.NewAccount(t)
	cfg := topupConfig(acct, 1000, "")
	resource.ParallelTest(t, resource.TestCase{
		ProtoV6ProviderFactories: tfacc.ProviderFactories,
		Steps: acctest.Forget(t, []resource.TestStep{
			{Config: cfg},
			{Config: topupConfig(acct, 2000, ""), PlanOnly: true, ExpectError: tfacc.RefusedReplace},
			{Config: cfg, Taint: []string{resAddress}, ExpectError: tfacc.RefusedDestroy},
		}, nil, "stripe_topup"),
	})
}

func TestAccTopup_import(t *testing.T) {
	acct := acctest.NewAccount(t)
	cfg := topupConfig(acct, 1000, "  description = \"Test funds\"\n  metadata = { team = \"a\" }\n")
	resource.ParallelTest(t, resource.TestCase{
		ProtoV6ProviderFactories: tfacc.ProviderFactories,
		Steps: acctest.Forget(t, []resource.TestStep{
			{Config: cfg},
			{ResourceName: resAddress, ImportState: true, ImportStateVerify: true},
			{ResourceName: resAddress, ImportState: true, ImportStateKind: resource.ImportBlockWithID, Config: cfg},
		}, nil, "stripe_topup"),
	})
}

// Stripe cannot delete a top-up, so dropping it from the configuration, and `terraform
// destroy`, fail at plan time, and the top-up stays as it was.
func TestAccTopup_delete(t *testing.T) {
	acct := acctest.NewAccount(t)
	var path string
	var mark int
	cfg := topupConfig(acct, 1000, "  description = \"Test funds\"\n")
	resource.ParallelTest(t, resource.TestCase{
		ProtoV6ProviderFactories: tfacc.ProviderFactories,
		Steps: acctest.Forget(t, []resource.TestStep{
			{Config: cfg, Check: resource.ComposeTestCheckFunc(topupPath(&path), tfacc.LogMark(t, acct, &mark))},
			{Config: acctest.Without(t, cfg, "stripe_topup"), PlanOnly: true, ExpectError: tfacc.RefusedDestroy},
			{Config: cfg, Destroy: true, ExpectError: tfacc.RefusedDestroy},
		}, tfacc.Untouched(t, acct, &path, &mark, "description", wire.String("Test funds")), "stripe_topup"),
	})
}

func TestAccTopup_drift(t *testing.T) {
	acct := acctest.NewAccount(t)
	var path string
	cfg := topupConfig(acct, 1000, "  description = \"Test funds\"\n")
	resource.ParallelTest(t, resource.TestCase{
		ProtoV6ProviderFactories: tfacc.ProviderFactories,
		Steps: acctest.Forget(t, []resource.TestStep{
			{Config: cfg, Check: topupPath(&path)},
			{
				PreConfig: func() {
					tfacc.Post(t, acct, path, wire.Object{{Name: "description", Value: wire.String("changed")}})
				},
				Config: cfg,
				ConfigPlanChecks: resource.ConfigPlanChecks{PreApply: []plancheck.PlanCheck{
					plancheck.ExpectNonEmptyPlan(),
					plancheck.ExpectResourceAction(resAddress, plancheck.ResourceActionUpdate),
				}},
				Check: resource.TestCheckResourceAttr(resAddress, "description", "Test funds"),
			},
		}, nil, "stripe_topup"),
	})
}

// Setting `status` to `canceled` cancels a pending top-up (suite: "cancel"). The cancel
// succeeds only while the top-up is pending, which the fake keeps it.
func TestAccTopup_cancel(t *testing.T) {
	acct := acctest.NewAccount(t)
	var path string
	resource.ParallelTest(t, resource.TestCase{
		ProtoV6ProviderFactories: tfacc.ProviderFactories,
		Steps: acctest.Forget(t, []resource.TestStep{
			{
				Config: topupConfig(acct, 1000, ""),
				Check: resource.ComposeTestCheckFunc(topupPath(&path), func(s *terraform.State) error {
					if acctest.Smoke() {
						return nil
					}
					return resource.TestCheckResourceAttr(resAddress, "status", "pending")(s)
				}),
			},
			{
				Config: topupConfig(acct, 1000, "  status = \"canceled\"\n"),
				Check: resource.ComposeTestCheckFunc(
					resource.TestCheckResourceAttr(resAddress, "status", "canceled"),
					tfacc.Holds(t, acct, &path, "status", wire.String("canceled")),
				),
			},
		}, nil, "stripe_topup"),
	})
}
