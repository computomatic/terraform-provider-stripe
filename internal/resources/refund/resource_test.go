package refund_test

import (
	"fmt"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/plancheck"

	"github.com/computomatic/terraform-provider-stripe/internal/acctest"
	"github.com/computomatic/terraform-provider-stripe/internal/acctest/tfacc"
	"github.com/computomatic/terraform-provider-stripe/internal/wire"
)

const resAddress = "stripe_refund.test"

// refundConfig is a refund of intent. extra lands inside the refund.
func refundConfig(c acctest.Config, intent, extra string) string {
	return c.ProviderBlock() + fmt.Sprintf(`
resource "stripe_refund" "test" {
  payment_intent = %q
%s}
`, intent, extra)
}

// refundPath records the refund's API path.
func refundPath(dst *string) resource.TestCheckFunc {
	return resource.TestCheckResourceAttrWith(resAddress, "id", func(v string) error {
		*dst = "/v1/refunds/" + v
		return nil
	})
}

// A partial refund of a succeeded payment (suite: "create partial").
func TestAccRefund_create(t *testing.T) {
	acct := acctest.NewAccount(t)
	intent, charge := tfacc.SucceededPaymentIntent(t, acct, "pm_card_visa")
	resource.ParallelTest(t, resource.TestCase{
		ProtoV6ProviderFactories: tfacc.ProviderFactories,
		Steps: acctest.Forget(t, []resource.TestStep{{
			Config: refundConfig(acct, intent, "  amount = 500\n  reason = \"requested_by_customer\"\n  metadata = { order = \"1\" }\n") + `
data "stripe_refund" "by_id" {
  id = stripe_refund.test.id
}
`,
			Check: resource.ComposeAggregateTestCheckFunc(
				resource.TestCheckResourceAttr(resAddress, "amount", "500"),
				resource.TestCheckResourceAttr(resAddress, "currency", "usd"),
				resource.TestCheckResourceAttr(resAddress, "status", "succeeded"),
				resource.TestCheckResourceAttr(resAddress, "reason", "requested_by_customer"),
				resource.TestCheckResourceAttr(resAddress, "charge", charge),
				resource.TestCheckResourceAttr(resAddress, "metadata.order", "1"),
				resource.TestCheckResourceAttrPair("data.stripe_refund.by_id", "amount", resAddress, "amount"),
			),
		}}, nil, "stripe_refund"),
	})
}

// Without `amount`, the refund is for what remains of the payment (suite: "create the rest").
func TestAccRefund_rest(t *testing.T) {
	acct := acctest.NewAccount(t)
	intent, _ := tfacc.SucceededPaymentIntent(t, acct, "pm_card_visa")
	resource.ParallelTest(t, resource.TestCase{
		ProtoV6ProviderFactories: tfacc.ProviderFactories,
		Steps: acctest.Forget(t, []resource.TestStep{{
			Config: refundConfig(acct, intent, ""),
			Check:  resource.TestCheckResourceAttr(resAddress, "amount", "2000"),
		}}, nil, "stripe_refund"),
	})
}

// Only `metadata` changes in place (suite: "update metadata").
func TestAccRefund_update(t *testing.T) {
	acct := acctest.NewAccount(t)
	intent, _ := tfacc.SucceededPaymentIntent(t, acct, "pm_card_visa")
	resource.ParallelTest(t, resource.TestCase{
		ProtoV6ProviderFactories: tfacc.ProviderFactories,
		Steps: acctest.Forget(t, []resource.TestStep{
			{Config: refundConfig(acct, intent, "  amount = 500\n  metadata = { keep = \"me\", drop = \"me\" }\n")},
			{
				Config: refundConfig(acct, intent, "  amount = 500\n  metadata = { keep = \"me\", new = \"v\" }\n"),
				ConfigPlanChecks: resource.ConfigPlanChecks{PreApply: []plancheck.PlanCheck{
					plancheck.ExpectResourceAction(resAddress, plancheck.ResourceActionUpdate),
				}},
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr(resAddress, "metadata.%", "2"),
					resource.TestCheckResourceAttr(resAddress, "metadata.new", "v"),
				),
			},
		}, nil, "stripe_refund"),
	})
}

// `amount` cannot change (suite: "immutable amount"), and a refund cannot be destroyed, so
// changing it fails the plan, and a taint fails at apply.
func TestAccRefund_replace(t *testing.T) {
	acct := acctest.NewAccount(t)
	intent, _ := tfacc.SucceededPaymentIntent(t, acct, "pm_card_visa")
	cfg := refundConfig(acct, intent, "  amount = 500\n")
	resource.ParallelTest(t, resource.TestCase{
		ProtoV6ProviderFactories: tfacc.ProviderFactories,
		Steps: acctest.Forget(t, []resource.TestStep{
			{Config: cfg},
			{Config: refundConfig(acct, intent, "  amount = 600\n"), PlanOnly: true, ExpectError: tfacc.RefusedReplace},
			{Config: cfg, Taint: []string{resAddress}, ExpectError: tfacc.RefusedDestroy},
		}, nil, "stripe_refund"),
	})
}

func TestAccRefund_import(t *testing.T) {
	acct := acctest.NewAccount(t)
	intent, _ := tfacc.SucceededPaymentIntent(t, acct, "pm_card_visa")
	cfg := refundConfig(acct, intent, "  amount = 500\n  metadata = { team = \"a\" }\n")
	resource.ParallelTest(t, resource.TestCase{
		ProtoV6ProviderFactories: tfacc.ProviderFactories,
		Steps: acctest.Forget(t, []resource.TestStep{
			{Config: cfg},
			{ResourceName: resAddress, ImportState: true, ImportStateVerify: true},
			{ResourceName: resAddress, ImportState: true, ImportStateKind: resource.ImportBlockWithID, Config: cfg},
		}, nil, "stripe_refund"),
	})
}

// Stripe cannot delete or undo a refund, so dropping it from the configuration, and `terraform
// destroy`, fail at plan time, and the refund stays as it was.
func TestAccRefund_delete(t *testing.T) {
	acct := acctest.NewAccount(t)
	intent, _ := tfacc.SucceededPaymentIntent(t, acct, "pm_card_visa")
	var path string
	var mark int
	cfg := refundConfig(acct, intent, "  amount = 500\n")
	resource.ParallelTest(t, resource.TestCase{
		ProtoV6ProviderFactories: tfacc.ProviderFactories,
		Steps: acctest.Forget(t, []resource.TestStep{
			{Config: cfg, Check: resource.ComposeTestCheckFunc(refundPath(&path), tfacc.LogMark(t, acct, &mark))},
			{Config: acctest.Without(t, cfg, "stripe_refund"), PlanOnly: true, ExpectError: tfacc.RefusedDestroy},
			{Config: cfg, Destroy: true, ExpectError: tfacc.RefusedDestroy},
		}, tfacc.Untouched(t, acct, &path, &mark, "status", wire.String("succeeded")), "stripe_refund"),
	})
}

func TestAccRefund_drift(t *testing.T) {
	acct := acctest.NewAccount(t)
	intent, _ := tfacc.SucceededPaymentIntent(t, acct, "pm_card_visa")
	var path string
	cfg := refundConfig(acct, intent, "  amount = 500\n  metadata = { order = \"1\" }\n")
	resource.ParallelTest(t, resource.TestCase{
		ProtoV6ProviderFactories: tfacc.ProviderFactories,
		Steps: acctest.Forget(t, []resource.TestStep{
			{Config: cfg, Check: refundPath(&path)},
			{
				PreConfig: func() {
					tfacc.Post(t, acct, path, wire.Object{{Name: "metadata", Value: wire.Object{{Name: "order", Value: wire.String("2")}}}})
				},
				Config: cfg,
				ConfigPlanChecks: resource.ConfigPlanChecks{PreApply: []plancheck.PlanCheck{
					plancheck.ExpectNonEmptyPlan(),
					plancheck.ExpectResourceAction(resAddress, plancheck.ResourceActionUpdate),
				}},
				Check: resource.TestCheckResourceAttr(resAddress, "metadata.order", "1"),
			},
		}, nil, "stripe_refund"),
	})
}
