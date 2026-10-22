package climate_order_test

import (
	"fmt"
	"os"
	"regexp"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/plancheck"
	"github.com/hashicorp/terraform-plugin-testing/terraform"

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
	acctest.RequireFake(t)
	acct := acctest.NewAccount(t)
	currency := tfacc.AccountCurrency(t, acct)
	acctest.Seed(t, acct, wire.Object{
		{Name: "object", Value: wire.String("balance")},
		{Name: "livemode", Value: wire.Bool(false)},
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

func status(t *testing.T, acct acctest.Config, id string) (string, error) {
	t.Helper()
	resp, err := tfacc.Client(acct).Do(t.Context(), stripeapi.Request{Method: "GET", Path: "/v1/climate/orders/" + id})
	if err != nil {
		return "", err
	}
	s, _ := resp.Body.(wire.Object).GetString("status")
	return s, nil
}

func TestAccClimateOrder_create(t *testing.T) {
	acct := fundedAccount(t)
	p := product(t, acct)
	resource.ParallelTest(t, resource.TestCase{
		ProtoV6ProviderFactories: tfacc.ProviderFactories,
		Steps: []resource.TestStep{{
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
		}},
	})
}

// An order without a beneficiary is attributed to the account (suite: "A climate order
// without a beneficiary is attributed to the account"), which the plan reads back.
func TestAccClimateOrder_defaultBeneficiary(t *testing.T) {
	acct := fundedAccount(t)
	cfg := orderConfig(acct, product(t, acct), "  metric_tons = \"1\"\n")
	resource.ParallelTest(t, resource.TestCase{
		ProtoV6ProviderFactories: tfacc.ProviderFactories,
		Steps: []resource.TestStep{
			{Config: cfg, Check: resource.TestCheckResourceAttrSet(address, "beneficiary.public_name")},
			{Config: cfg, PlanOnly: true},
		},
	})
}

// An order can be sized by money instead of tons.
func TestAccClimateOrder_createByAmount(t *testing.T) {
	acct := fundedAccount(t)
	cfg := orderConfig(acct, product(t, acct), "  amount = 60000\n")
	resource.ParallelTest(t, resource.TestCase{
		ProtoV6ProviderFactories: tfacc.ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: cfg,
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr(address, "amount", "60000"),
					resource.TestCheckResourceAttrSet(address, "metric_tons"),
					resource.TestCheckResourceAttr(address, "status", "confirmed"),
				),
			},
			{Config: cfg, PlanOnly: true},
		},
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
		Steps: []resource.TestStep{
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
		},
	})
}

// A confirmed order cannot be resized (suite: "update errors", "metric tons on a confirmed
// order"), so new tons cancel the order and buy another.
func TestAccClimateOrder_replace(t *testing.T) {
	acct := fundedAccount(t)
	p := product(t, acct)
	var first string
	resource.ParallelTest(t, resource.TestCase{
		ProtoV6ProviderFactories: tfacc.ProviderFactories,
		Steps: []resource.TestStep{
			{Config: orderConfig(acct, p, "  metric_tons = \"1\"\n"), Check: idOf(&first)},
			{
				Config: orderConfig(acct, p, "  metric_tons = \"2\"\n"),
				ConfigPlanChecks: resource.ConfigPlanChecks{PreApply: []plancheck.PlanCheck{
					plancheck.ExpectResourceAction(address, plancheck.ResourceActionReplace),
				}},
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr(address, "metric_tons", "2"),
					resource.TestCheckResourceAttrWith(address, "id", func(v string) error {
						if v == first {
							return fmt.Errorf("the replacement kept the order %s", v)
						}
						got, err := status(t, acct, first)
						if err != nil || got != "canceled" {
							return fmt.Errorf("the replaced order %s is %q (%v), want canceled", first, got, err)
						}
						return nil
					}),
				),
			},
		},
	})
}

func TestAccClimateOrder_import(t *testing.T) {
	acct := fundedAccount(t)
	cfg := orderConfig(acct, product(t, acct), "  metric_tons = \"1\"\n  beneficiary = { public_name = \"Conformance Beneficiary\" }\n  metadata = { env = \"test\" }\n")
	resource.ParallelTest(t, resource.TestCase{
		ProtoV6ProviderFactories: tfacc.ProviderFactories,
		Steps: []resource.TestStep{
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
		},
	})
}

// Stripe cannot delete an order; a confirmed one cancels (suite: "cancel"), so destroy
// cancels it and the order stays in the account.
func TestAccClimateOrder_delete(t *testing.T) {
	acct := fundedAccount(t)
	var id string
	resource.ParallelTest(t, resource.TestCase{
		ProtoV6ProviderFactories: tfacc.ProviderFactories,
		CheckDestroy: func(*terraform.State) error {
			got, err := status(t, acct, id)
			if err != nil {
				return fmt.Errorf("a canceled order still retrieves: %w", err)
			}
			if got != "canceled" {
				return fmt.Errorf("order %s is %q after destroy, want canceled", id, got)
			}
			return nil
		},
		Steps: []resource.TestStep{{Config: orderConfig(acct, product(t, acct), "  metric_tons = \"1\"\n"), Check: idOf(&id)}},
	})
}

// An order canceled out of band reads as canceled, and destroy does not cancel it again
// (suite: "cancel again" is refused).
func TestAccClimateOrder_deleteAfterCancel(t *testing.T) {
	acct := fundedAccount(t)
	var id string
	cfg := orderConfig(acct, product(t, acct), "  metric_tons = \"1\"\n")
	resource.ParallelTest(t, resource.TestCase{
		ProtoV6ProviderFactories: tfacc.ProviderFactories,
		Steps: []resource.TestStep{
			{Config: cfg, Check: idOf(&id)},
			{
				PreConfig: func() { tfacc.Post(t, acct, "/v1/climate/orders/"+id+"/cancel", nil) },
				Config:    cfg,
				Check:     resource.TestCheckResourceAttr(address, "status", "canceled"),
			},
		},
	})
}

// Stripe refuses to cancel a delivered order (the fake answers "Order can not be canceled"
// for any status but confirmed), so destroy leaves it as it is after a retrieve shows it
// delivered. Only an admin seed reaches a delivered order.
func TestAccClimateOrder_deleteDelivered(t *testing.T) {
	acct := fundedAccount(t)
	p := product(t, acct)
	id := acctest.Seed(t, acct, wire.Object{
		{Name: "object", Value: wire.String("climate.order")},
		{Name: "amount_fees", Value: wire.Int(4700)},
		{Name: "amount_subtotal", Value: wire.Int(47000)},
		{Name: "amount_total", Value: wire.Int(51700)},
		{Name: "beneficiary", Value: wire.Object{{Name: "public_name", Value: wire.String("Seeded")}}},
		{Name: "canceled_at", Value: wire.Null{}},
		{Name: "cancellation_reason", Value: wire.Null{}},
		{Name: "certificate", Value: wire.String("https://example.com/certificate")},
		{Name: "confirmed_at", Value: wire.Int(1700000000)},
		{Name: "created", Value: wire.Int(1700000000)},
		{Name: "currency", Value: wire.String(tfacc.AccountCurrency(t, acct))},
		{Name: "delayed_at", Value: wire.Null{}},
		{Name: "delivered_at", Value: wire.Int(1700000100)},
		{Name: "delivery_details", Value: wire.Array{}},
		{Name: "expected_delivery_year", Value: wire.Int(2027)},
		{Name: "livemode", Value: wire.Bool(false)},
		{Name: "metadata", Value: wire.Object{}},
		{Name: "metric_tons", Value: wire.String("1")},
		{Name: "product", Value: wire.String(p)},
		{Name: "product_substituted_at", Value: wire.Null{}},
		{Name: "status", Value: wire.String("delivered")},
	})[0]
	resource.ParallelTest(t, resource.TestCase{
		ProtoV6ProviderFactories: tfacc.ProviderFactories,
		CheckDestroy: func(*terraform.State) error {
			if got, err := status(t, acct, id); err != nil || got != "delivered" {
				return fmt.Errorf("order %s is %q (%v) after destroy, want delivered", id, got, err)
			}
			return nil
		},
		Steps: []resource.TestStep{{
			Config:             orderConfig(acct, p, "  metric_tons = \"1\"\n"),
			ResourceName:       address,
			ImportState:        true,
			ImportStateId:      id,
			ImportStatePersist: true,
		}},
	})
}

func TestAccClimateOrder_drift(t *testing.T) {
	acct := fundedAccount(t)
	var id string
	cfg := orderConfig(acct, product(t, acct), "  metric_tons = \"1\"\n  metadata = { env = \"test\" }\n")
	resource.ParallelTest(t, resource.TestCase{
		ProtoV6ProviderFactories: tfacc.ProviderFactories,
		Steps: []resource.TestStep{
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
		},
	})
}
