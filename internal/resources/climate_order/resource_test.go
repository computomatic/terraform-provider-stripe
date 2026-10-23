package climate_order_test

import (
	"fmt"
	"os"
	"regexp"
	"strings"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/plancheck"

	"github.com/computomatic/terraform-provider-stripe/internal/acctest"
	"github.com/computomatic/terraform-provider-stripe/internal/acctest/tfacc"
	"github.com/computomatic/terraform-provider-stripe/internal/stripeapi"
	"github.com/computomatic/terraform-provider-stripe/internal/wire"
)

func TestMain(m *testing.M) { os.Exit(acctest.Main(m)) }

const address = "stripe_climate_order.test"

// fundedAccount is an account whose available balance covers a few orders. A Climate order
// spends a balance in the account's currency (suite: "Climate orders spend the available
// balance", "Climate products price only in the account's currency"), which a fresh account on
// the fake lacks, so the test seeds one. It runs on the fake only: real Stripe has
// no seed, and the order is not in the smoke selection.
func fundedAccount(t *testing.T) acctest.Config {
	t.Helper()
	return seededAccount(t, false)
}

// seededAccount is fundedAccount with the balance reporting live mode when live is set.
func seededAccount(t *testing.T, live bool) acctest.Config {
	t.Helper()
	acctest.RequireFake(t)
	acct := acctest.NewAccount(t)
	currency := tfacc.AccountCurrency(t, acct)
	acctest.Seed(t, acct, wire.Object{
		{Name: "object", Value: wire.String("balance")},
		{Name: "livemode", Value: wire.Bool(live)},
		{Name: "available", Value: wire.Array{wire.Object{{Name: "amount", Value: wire.Int(10_000_000)}, {Name: "currency", Value: wire.String(currency)}}}},
		{Name: "pending", Value: wire.Array{wire.Object{{Name: "amount", Value: wire.Int(0)}, {Name: "currency", Value: wire.String(currency)}}}},
	})
	return acct
}

// product returns the ID of the first product in the Climate catalogue, as the suite does
// ("firstClimateProduct").
func product(t *testing.T, acct acctest.Config) string {
	t.Helper()
	resp, err := tfacc.Client(acct).Do(t.Context(), stripeapi.Request{Method: "GET", Path: "/v1/climate/products", Params: wire.Object{{Name: "limit", Value: wire.Int(1)}}})
	if err != nil {
		t.Fatalf("listing climate products: %v", err)
	}
	list, _ := resp.Body.(wire.Object).Get("data")
	items, _ := list.(wire.Array)
	if len(items) == 0 {
		t.Skip("no climate products are listed")
	}
	id, _ := items[0].(wire.Object).GetString("id")
	return id
}

// orderConfig is an order of the product. extra lands inside the order.
func orderConfig(c acctest.Config, product, extra string) string {
	return c.ProviderBlock() + fmt.Sprintf(`
resource "stripe_climate_order" "test" {
  product = %q
%s}
`, product, extra)
}

func idOf(dst *string) resource.TestCheckFunc {
	return resource.TestCheckResourceAttrWith(address, "id", func(v string) error {
		*dst = v
		return nil
	})
}

// pathOf records the order's API path.
func pathOf(dst *string) resource.TestCheckFunc {
	return resource.TestCheckResourceAttrWith(address, "id", func(v string) error {
		*dst = "/v1/climate/orders/" + v
		return nil
	})
}

func TestAccClimateOrder_create(t *testing.T) {
	acct := fundedAccount(t)
	p := product(t, acct)
	resource.ParallelTest(t, resource.TestCase{
		ProtoV6ProviderFactories: tfacc.ProviderFactories,
		Steps: acctest.Forget(t, []resource.TestStep{{
			Config: orderConfig(acct, p, "  metric_tons = \"1\"\n  beneficiary = { public_name = \"Conformance Beneficiary\" }\n  metadata = { env = \"test\" }\n") + `
data "stripe_climate_order" "test" {
  id = stripe_climate_order.test.id
}
`,
			Check: resource.ComposeAggregateTestCheckFunc(
				resource.TestMatchResourceAttr(address, "id", regexp.MustCompile(`^climorder_`)),
				resource.TestCheckResourceAttr(address, "product", p),
				resource.TestCheckResourceAttr(address, "metric_tons", "1"),
				resource.TestCheckResourceAttr(address, "status", "confirmed"),
				resource.TestCheckResourceAttr(address, "currency", tfacc.AccountCurrency(t, acct)),
				resource.TestCheckResourceAttr(address, "beneficiary.public_name", "Conformance Beneficiary"),
				resource.TestCheckResourceAttr(address, "metadata.env", "test"),
				resource.TestCheckResourceAttrSet(address, "amount_total"),
				resource.TestCheckResourceAttrPair("data.stripe_climate_order.test", "id", address, "id"),
				resource.TestCheckResourceAttrPair("data.stripe_climate_order.test", "amount_total", address, "amount_total"),
				resource.TestCheckResourceAttr("data.stripe_climate_order.test", "status", "confirmed"),
			),
		}}, nil, "stripe_climate_order"),
	})
}

// An order spends the account's balance, so in an account whose balance reports live mode it is
// refused at plan time until the provider allows live money movement, and so is a change to it
// (ADR 2026-10-22-money-moving-resources).
func TestAccClimateOrder_liveMode(t *testing.T) {
	acct := seededAccount(t, true)
	p := product(t, acct)
	refused := regexp.MustCompile(`Live-mode money movement is not allowed`)
	allow := strings.Replace(acct.ProviderBlock(), "provider \"stripe\" {\n", "provider \"stripe\" {\n  allow_live_money_movement = true\n", 1)
	order := strings.TrimPrefix(orderConfig(acct, p, "  metric_tons = \"1\"\n"), acct.ProviderBlock())
	resource.ParallelTest(t, resource.TestCase{
		ProtoV6ProviderFactories: tfacc.ProviderFactories,
		Steps: acctest.Forget(t, []resource.TestStep{
			{Config: orderConfig(acct, p, "  metric_tons = \"1\"\n"), PlanOnly: true, ExpectError: refused},
			{Config: allow + order, Check: resource.TestCheckResourceAttr(address, "status", "confirmed")},
			{Config: orderConfig(acct, p, "  metric_tons = \"1\"\n  metadata = { env = \"live\" }\n"), PlanOnly: true, ExpectError: refused},
		}, nil, "stripe_climate_order"),
	})
}

// An order without a beneficiary is attributed to the account (suite: "A climate order
// without a beneficiary is attributed to the account"), which the plan reads back.
func TestAccClimateOrder_defaultBeneficiary(t *testing.T) {
	acct := fundedAccount(t)
	cfg := orderConfig(acct, product(t, acct), "  metric_tons = \"1\"\n")
	resource.ParallelTest(t, resource.TestCase{
		ProtoV6ProviderFactories: tfacc.ProviderFactories,
		Steps: acctest.Forget(t, []resource.TestStep{
			{Config: cfg, Check: resource.TestCheckResourceAttrSet(address, "beneficiary.public_name")},
			{Config: cfg, PlanOnly: true},
		}, nil, "stripe_climate_order"),
	})
}

// An order can be sized by money instead of tons.
func TestAccClimateOrder_createByAmount(t *testing.T) {
	acct := fundedAccount(t)
	cfg := orderConfig(acct, product(t, acct), "  amount = 60000\n")
	resource.ParallelTest(t, resource.TestCase{
		ProtoV6ProviderFactories: tfacc.ProviderFactories,
		Steps: acctest.Forget(t, []resource.TestStep{
			{
				Config: cfg,
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr(address, "amount", "60000"),
					resource.TestCheckResourceAttrSet(address, "metric_tons"),
					resource.TestCheckResourceAttr(address, "status", "confirmed"),
				),
			},
			{Config: cfg, PlanOnly: true},
		}, nil, "stripe_climate_order"),
	})
}

// The order takes `metric_tons` or `amount`, never both and never neither (suite: "create
// errors"), which the plan refuses before Stripe is called.
func TestAccClimateOrder_oneOf(t *testing.T) {
	acct := fundedAccount(t)
	p := product(t, acct)
	resource.ParallelTest(t, resource.TestCase{
		ProtoV6ProviderFactories: tfacc.ProviderFactories,
		Steps: []resource.TestStep{
			{Config: orderConfig(acct, p, ""), PlanOnly: true, ExpectError: regexp.MustCompile(`exactly one of .amount., .metric_tons. must be set, got 0`)},
			{Config: orderConfig(acct, p, "  amount = 60000\n  metric_tons = \"1\"\n"), PlanOnly: true, ExpectError: regexp.MustCompile(`exactly one of .amount., .metric_tons. must be set, got 2`)},
		},
	})
}

// The beneficiary and the metadata update in place (suite: "update").
func TestAccClimateOrder_update(t *testing.T) {
	acct := fundedAccount(t)
	p := product(t, acct)
	var first string
	resource.ParallelTest(t, resource.TestCase{
		ProtoV6ProviderFactories: tfacc.ProviderFactories,
		Steps: acctest.Forget(t, []resource.TestStep{
			{
				Config: orderConfig(acct, p, "  metric_tons = \"1\"\n  beneficiary = { public_name = \"Before\" }\n  metadata = { keep = \"me\", drop = \"me\" }\n"),
				Check:  idOf(&first),
			},
			{
				Config: orderConfig(acct, p, "  metric_tons = \"1\"\n  beneficiary = { public_name = \"After\" }\n  metadata = { keep = \"me\", new = \"v\" }\n"),
				ConfigPlanChecks: resource.ConfigPlanChecks{PreApply: []plancheck.PlanCheck{
					plancheck.ExpectResourceAction(address, plancheck.ResourceActionUpdate),
				}},
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr(address, "beneficiary.public_name", "After"),
					resource.TestCheckResourceAttr(address, "metadata.%", "2"),
					resource.TestCheckResourceAttr(address, "metadata.new", "v"),
					resource.TestCheckResourceAttr(address, "status", "confirmed"),
					resource.TestCheckResourceAttrWith(address, "id", func(v string) error {
						if v != first {
							return fmt.Errorf("the in-place update changed the ID from %s to %s", first, v)
						}
						return nil
					}),
				),
			},
		}, nil, "stripe_climate_order"),
	})
}

// `metric_tons` is create-only, so changing it would replace the order, which Stripe cannot
// delete: the plan fails, and so does a taint, at apply.
func TestAccClimateOrder_replace(t *testing.T) {
	acct := fundedAccount(t)
	p := product(t, acct)
	cfg := orderConfig(acct, p, "  metric_tons = \"1\"\n")
	resource.ParallelTest(t, resource.TestCase{
		ProtoV6ProviderFactories: tfacc.ProviderFactories,
		Steps: acctest.Forget(t, []resource.TestStep{
			{Config: cfg},
			{Config: orderConfig(acct, p, "  metric_tons = \"2\"\n"), PlanOnly: true, ExpectError: tfacc.RefusedReplace},
			{Config: cfg, Taint: []string{address}, ExpectError: tfacc.RefusedDestroy},
		}, nil, "stripe_climate_order"),
	})
}

func TestAccClimateOrder_import(t *testing.T) {
	acct := fundedAccount(t)
	cfg := orderConfig(acct, product(t, acct), "  metric_tons = \"1\"\n  beneficiary = { public_name = \"Conformance Beneficiary\" }\n  metadata = { env = \"test\" }\n")
	resource.ParallelTest(t, resource.TestCase{
		ProtoV6ProviderFactories: tfacc.ProviderFactories,
		Steps: acctest.Forget(t, []resource.TestStep{
			{Config: cfg},
			{
				ResourceName:      address,
				ImportState:       true,
				ImportStateVerify: true,
			},
			{
				ResourceName:    address,
				ImportState:     true,
				ImportStateKind: resource.ImportBlockWithID,
				Config:          cfg,
			},
		}, nil, "stripe_climate_order"),
	})
}

// Stripe cannot delete an order, so dropping it from the configuration, and `terraform
// destroy`, fail at plan time, and the order stays as it was.
func TestAccClimateOrder_delete(t *testing.T) {
	acct := fundedAccount(t)
	var path string
	var mark int
	cfg := orderConfig(acct, product(t, acct), "  metric_tons = \"1\"\n")
	resource.ParallelTest(t, resource.TestCase{
		ProtoV6ProviderFactories: tfacc.ProviderFactories,
		Steps: acctest.Forget(t, []resource.TestStep{
			{Config: cfg, Check: resource.ComposeTestCheckFunc(pathOf(&path), tfacc.LogMark(t, acct, &mark))},
			{Config: acctest.Without(t, cfg, "stripe_climate_order"), PlanOnly: true, ExpectError: tfacc.RefusedDestroy},
			{Config: cfg, Destroy: true, ExpectError: tfacc.RefusedDestroy},
		}, tfacc.Untouched(t, acct, &path, &mark, "status", wire.String("confirmed")), "stripe_climate_order"),
	})
}

// Setting `status` to `canceled` cancels the order through its action endpoint (suite:
// "cancel"), after which it reads back canceled.
func TestAccClimateOrder_cancel(t *testing.T) {
	acct := fundedAccount(t)
	p := product(t, acct)
	var path string
	resource.ParallelTest(t, resource.TestCase{
		ProtoV6ProviderFactories: tfacc.ProviderFactories,
		Steps: acctest.Forget(t, []resource.TestStep{
			{Config: orderConfig(acct, p, "  metric_tons = \"1\"\n"), Check: pathOf(&path)},
			{
				Config: orderConfig(acct, p, "  metric_tons = \"1\"\n  status      = \"canceled\"\n"),
				Check: resource.ComposeTestCheckFunc(
					resource.TestCheckResourceAttr(address, "status", "canceled"),
					tfacc.Holds(t, acct, &path, "status", wire.String("canceled")),
				),
			},
		}, nil, "stripe_climate_order"),
	})
}

func TestAccClimateOrder_drift(t *testing.T) {
	acct := fundedAccount(t)
	var id string
	cfg := orderConfig(acct, product(t, acct), "  metric_tons = \"1\"\n  metadata = { env = \"test\" }\n")
	resource.ParallelTest(t, resource.TestCase{
		ProtoV6ProviderFactories: tfacc.ProviderFactories,
		Steps: acctest.Forget(t, []resource.TestStep{
			{Config: cfg, Check: idOf(&id)},
			{
				PreConfig: func() {
					tfacc.Post(t, acct, "/v1/climate/orders/"+id, wire.Object{{Name: "metadata", Value: wire.Object{{Name: "env", Value: wire.String("changed")}}}})
				},
				Config: cfg,
				ConfigPlanChecks: resource.ConfigPlanChecks{PreApply: []plancheck.PlanCheck{
					plancheck.ExpectNonEmptyPlan(),
					plancheck.ExpectResourceAction(address, plancheck.ResourceActionUpdate),
				}},
				Check: resource.TestCheckResourceAttr(address, "metadata.env", "test"),
			},
		}, nil, "stripe_climate_order"),
	})
}
