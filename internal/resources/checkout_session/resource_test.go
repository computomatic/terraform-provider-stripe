package checkout_session_test

import (
	"fmt"
	"regexp"
	"strings"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/plancheck"

	"github.com/computomatic/terraform-provider-stripe/internal/acctest"
	"github.com/computomatic/terraform-provider-stripe/internal/acctest/tfacc"
	"github.com/computomatic/terraform-provider-stripe/internal/wire"
)

const resAddress = "stripe_checkout_session.test"

// sessionConfig is a product, its price and a payment-mode session for two of it. extra lands
// inside the session.
func sessionConfig(c acctest.Config, extra string) string {
	return c.ProviderBlock() + fmt.Sprintf(`
resource "stripe_product" "test" {
  name = %q
}

resource "stripe_price" "test" {
  product     = stripe_product.test.id
  currency    = "usd"
  unit_amount = 2000
}

resource "stripe_checkout_session" "test" {
  mode        = "payment"
  success_url = "https://example.com/thanks"

  line_items = [{
    price    = stripe_price.test.id
    quantity = 2
  }]
%s}
`, c.Name("Checkout product"), extra)
}

func sessionPath(dst *string) resource.TestCheckFunc {
	return resource.TestCheckResourceAttrWith(resAddress, "id", func(v string) error {
		*dst = "/v1/checkout/sessions/" + v
		return nil
	})
}

// An open, unpaid session with a hosted page (suite: "create minimal", "create with optional
// fields").
func TestAccCheckoutSession_create(t *testing.T) {
	acct := acctest.NewAccount(t)
	resource.ParallelTest(t, resource.TestCase{
		ProtoV6ProviderFactories: tfacc.ProviderFactories,
		Steps: acctest.Forget(t, []resource.TestStep{{
			Config: sessionConfig(acct, "  cancel_url = \"https://example.com/cart\"\n  client_reference_id = \"order-1\"\n  metadata = { env = \"test\" }\n") + `
data "stripe_checkout_session" "by_id" {
  id = stripe_checkout_session.test.id
}
`,
			Check: resource.ComposeAggregateTestCheckFunc(
				resource.TestCheckResourceAttr(resAddress, "status", "open"),
				resource.TestCheckResourceAttr(resAddress, "payment_status", "unpaid"),
				resource.TestCheckResourceAttr(resAddress, "amount_total", "4000"),
				resource.TestCheckResourceAttrSet(resAddress, "url"),
				resource.TestCheckResourceAttr(resAddress, "cancel_url", "https://example.com/cart"),
				resource.TestCheckResourceAttr(resAddress, "client_reference_id", "order-1"),
				resource.TestCheckResourceAttr(resAddress, "metadata.env", "test"),
				resource.TestCheckResourceAttr(resAddress, "line_items.#", "1"),
				resource.TestCheckResourceAttr(resAddress, "line_items.0.quantity", "2"),
				resource.TestCheckResourceAttrPair(resAddress, "line_items.0.price", "stripe_price.test", "id"),
				resource.TestCheckResourceAttrPair("data.stripe_checkout_session.by_id", "id", resAddress, "id"),
			),
		}}, nil, "stripe_checkout_session"),
	})
}

// `metadata` changes in place (suite: "update metadata").
func TestAccCheckoutSession_update(t *testing.T) {
	acct := acctest.NewAccount(t)
	resource.ParallelTest(t, resource.TestCase{
		ProtoV6ProviderFactories: tfacc.ProviderFactories,
		Steps: acctest.Forget(t, []resource.TestStep{
			{Config: sessionConfig(acct, "  metadata = { keep = \"me\", drop = \"me\" }\n")},
			{
				Config: sessionConfig(acct, "  metadata = { keep = \"me\", new = \"v\" }\n"),
				ConfigPlanChecks: resource.ConfigPlanChecks{PreApply: []plancheck.PlanCheck{
					plancheck.ExpectResourceAction(resAddress, plancheck.ResourceActionUpdate),
				}},
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr(resAddress, "metadata.%", "2"),
					resource.TestCheckResourceAttr(resAddress, "metadata.new", "v"),
				),
			},
		}, nil, "stripe_checkout_session"),
	})
}

// `success_url` is create-only and the line items are not proven to update, and a session
// cannot be destroyed, so changing either fails the plan, and a taint fails at apply.
func TestAccCheckoutSession_replace(t *testing.T) {
	acct := acctest.NewAccount(t)
	cfg := sessionConfig(acct, "")
	resource.ParallelTest(t, resource.TestCase{
		ProtoV6ProviderFactories: tfacc.ProviderFactories,
		Steps: acctest.Forget(t, []resource.TestStep{
			{Config: cfg},
			{Config: strings.Replace(cfg, "https://example.com/thanks", "https://example.com/done", 1), PlanOnly: true, ExpectError: tfacc.RefusedReplace},
			{Config: strings.Replace(cfg, "quantity = 2", "quantity = 3", 1), PlanOnly: true, ExpectError: tfacc.RefusedReplace},
			{Config: cfg, Taint: []string{resAddress}, ExpectError: tfacc.RefusedDestroy},
		}, nil, "stripe_checkout_session"),
	})
}

func TestAccCheckoutSession_import(t *testing.T) {
	acct := acctest.NewAccount(t)
	cfg := sessionConfig(acct, "  client_reference_id = \"order-1\"\n  metadata = { team = \"a\" }\n")
	resource.ParallelTest(t, resource.TestCase{
		ProtoV6ProviderFactories: tfacc.ProviderFactories,
		Steps: acctest.Forget(t, []resource.TestStep{
			{Config: cfg},
			{ResourceName: resAddress, ImportState: true, ImportStateVerify: true},
			{ResourceName: resAddress, ImportState: true, ImportStateKind: resource.ImportBlockWithID, Config: cfg},
		}, nil, "stripe_checkout_session"),
	})
}

// Stripe cannot delete a checkout session, so dropping it from the configuration, and
// `terraform destroy`, fail at plan time, and the session stays open.
func TestAccCheckoutSession_delete(t *testing.T) {
	acct := acctest.NewAccount(t)
	var path string
	var mark int
	cfg := sessionConfig(acct, "")
	resource.ParallelTest(t, resource.TestCase{
		ProtoV6ProviderFactories: tfacc.ProviderFactories,
		Steps: acctest.Forget(t, []resource.TestStep{
			{Config: cfg, Check: resource.ComposeTestCheckFunc(sessionPath(&path), tfacc.LogMark(t, acct, &mark))},
			{Config: acctest.Without(t, cfg, "stripe_checkout_session"), PlanOnly: true, ExpectError: tfacc.RefusedDestroy},
			{Config: cfg, Destroy: true, ExpectError: tfacc.RefusedDestroy},
		}, tfacc.Untouched(t, acct, &path, &mark, "status", wire.String("open")), "stripe_checkout_session"),
	})
}

func TestAccCheckoutSession_drift(t *testing.T) {
	acct := acctest.NewAccount(t)
	var path string
	cfg := sessionConfig(acct, "  metadata = { order = \"1\" }\n")
	resource.ParallelTest(t, resource.TestCase{
		ProtoV6ProviderFactories: tfacc.ProviderFactories,
		Steps: acctest.Forget(t, []resource.TestStep{
			{Config: cfg, Check: sessionPath(&path)},
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
		}, nil, "stripe_checkout_session"),
	})
}

// Setting `status` to `expired` expires the session (suite: "expire"), after which a plan
// that keeps it expired is empty.
func TestAccCheckoutSession_expire(t *testing.T) {
	acct := acctest.NewAccount(t)
	var path string
	expired := sessionConfig(acct, "  status = \"expired\"\n")
	resource.ParallelTest(t, resource.TestCase{
		ProtoV6ProviderFactories: tfacc.ProviderFactories,
		Steps: acctest.Forget(t, []resource.TestStep{
			{Config: sessionConfig(acct, ""), Check: sessionPath(&path)},
			{
				Config: expired,
				Check: resource.ComposeTestCheckFunc(
					resource.TestCheckResourceAttr(resAddress, "status", "expired"),
					resource.TestCheckNoResourceAttr(resAddress, "url"),
					tfacc.Holds(t, acct, &path, "status", wire.String("expired")),
				),
			},
			{Config: expired, PlanOnly: true},
		}, nil, "stripe_checkout_session"),
	})
}

// A session that expires on its own, outside Terraform, reads back expired with no planned
// change when the configuration leaves `status` unset.
func TestAccCheckoutSession_expiredElsewhere(t *testing.T) {
	acct := acctest.NewAccount(t)
	var path string
	cfg := sessionConfig(acct, "")
	resource.ParallelTest(t, resource.TestCase{
		ProtoV6ProviderFactories: tfacc.ProviderFactories,
		Steps: acctest.Forget(t, []resource.TestStep{
			{Config: cfg, Check: sessionPath(&path)},
			{
				PreConfig: func() { tfacc.Post(t, acct, path+"/expire", nil) },
				Config:    cfg,
				PlanOnly:  true,
			},
			{Config: cfg, Check: resource.TestCheckResourceAttr(resAddress, "status", "expired")},
			{Config: sessionConfig(acct, "  status = \"expired\"\n"), PlanOnly: true},
			{Config: sessionConfig(acct, ""), Check: resource.TestMatchResourceAttr(resAddress, "id", regexp.MustCompile(`^cs_`))},
		}, nil, "stripe_checkout_session"),
	})
}
