package promotion_code_test

import (
	"fmt"
	"os"
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

const address = "stripe_promotion_code.test"

// codeConfig is a coupon and a promotion code for it. extra lands inside the promotion code.
func codeConfig(c acctest.Config, code string, extra string) string {
	return c.ProviderBlock() + fmt.Sprintf(`
resource "stripe_coupon" "test" {
  percent_off = 10
  duration    = "once"
}

resource "stripe_promotion_code" "test" {
  code = %q
  promotion = {
    type   = "coupon"
    coupon = stripe_coupon.test.id
  }
%s}
`, code, extra)
}

func idOf(dst *string) resource.TestCheckFunc {
	return resource.TestCheckResourceAttrWith(address, "id", func(v string) error {
		*dst = v
		return nil
	})
}

func TestAccPromotionCode_create(t *testing.T) {
	acct := acctest.NewAccount(t)
	code := acct.Unique("SAVE10-create")
	resource.ParallelTest(t, resource.TestCase{
		ProtoV6ProviderFactories: tfacc.ProviderFactories,
		Steps: []resource.TestStep{{
			Config: codeConfig(acct, code, `
  max_redemptions = 5
  metadata        = { team = "a" }
  restrictions = {
    first_time_transaction  = true
    minimum_amount          = 1000
    minimum_amount_currency = "usd"
  }
`) + `
data "stripe_promotion_code" "by_id" {
  id = stripe_promotion_code.test.id
}

data "stripe_promotion_code" "by_code" {
  code = stripe_promotion_code.test.code
}
`,
			Check: resource.ComposeAggregateTestCheckFunc(
				resource.TestCheckResourceAttrSet(address, "id"),
				resource.TestCheckResourceAttr(address, "code", code),
				resource.TestCheckResourceAttr(address, "active", "true"),
				resource.TestCheckResourceAttrPair(address, "promotion.coupon", "stripe_coupon.test", "id"),
				resource.TestCheckResourceAttr(address, "promotion.type", "coupon"),
				resource.TestCheckResourceAttr(address, "max_redemptions", "5"),
				resource.TestCheckResourceAttr(address, "restrictions.first_time_transaction", "true"),
				resource.TestCheckResourceAttr(address, "restrictions.minimum_amount", "1000"),
				resource.TestCheckResourceAttr(address, "metadata.team", "a"),
				resource.TestCheckResourceAttrPair("data.stripe_promotion_code.by_id", "id", address, "id"),
				resource.TestCheckResourceAttrPair("data.stripe_promotion_code.by_code", "id", address, "id"),
				resource.TestCheckResourceAttr("data.stripe_promotion_code.by_code", "max_redemptions", "5"),
			),
		}},
	})
}

func TestAccPromotionCode_update(t *testing.T) {
	acct := acctest.NewAccount(t)
	code := acct.Unique("SAVE10-update")
	resource.ParallelTest(t, resource.TestCase{
		ProtoV6ProviderFactories: tfacc.ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: codeConfig(acct, code, "  metadata = { team = \"a\", tier = \"1\" }\n"),
				// Stripe fills `restrictions` in when the request omits it.
				Check: resource.TestCheckResourceAttr(address, "restrictions.first_time_transaction", "false"),
			},
			{
				Config: codeConfig(acct, code, "  metadata = { team = \"b\" }\n  active = false\n"),
				ConfigPlanChecks: resource.ConfigPlanChecks{PreApply: []plancheck.PlanCheck{
					plancheck.ExpectResourceAction(address, plancheck.ResourceActionUpdate),
				}},
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr(address, "active", "false"),
					resource.TestCheckResourceAttr(address, "metadata.%", "1"),
					resource.TestCheckResourceAttr(address, "metadata.team", "b"),
				),
			},
		},
	})
}

// code is accepted on create only, so changing it replaces the promotion code. The
// archived code keeps its `code`, but the replacement uses a new one.
func TestAccPromotionCode_replace(t *testing.T) {
	acct := acctest.NewAccount(t)
	code, other := acct.Unique("SAVE10-replace"), acct.Unique("SAVE20-replace")
	var first string
	resource.ParallelTest(t, resource.TestCase{
		ProtoV6ProviderFactories: tfacc.ProviderFactories,
		Steps: []resource.TestStep{
			{Config: codeConfig(acct, code, ""), Check: idOf(&first)},
			{
				Config: codeConfig(acct, other, ""),
				ConfigPlanChecks: resource.ConfigPlanChecks{PreApply: []plancheck.PlanCheck{
					plancheck.ExpectResourceAction(address, plancheck.ResourceActionReplace),
				}},
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr(address, "code", other),
					resource.TestCheckResourceAttrWith(address, "id", func(v string) error {
						if v == first {
							return fmt.Errorf("the replacement kept the promotion code ID %s", v)
						}
						return nil
					}),
				),
			},
		},
	})
}

func TestAccPromotionCode_import(t *testing.T) {
	acct := acctest.NewAccount(t)
	code := acct.Unique("SAVE10-import")
	cfg := codeConfig(acct, code, "  max_redemptions = 5\n  metadata = { team = \"a\" }\n")
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

// Stripe cannot delete a promotion code (suite: "Shipping rates, tax rates and promotion
// codes cannot be deleted"), so destroy archives it and the code stays in the account.
func TestAccPromotionCode_delete(t *testing.T) {
	acct := acctest.NewAccount(t)
	code := acct.Unique("SAVE10-delete")
	var id string
	resource.ParallelTest(t, resource.TestCase{
		ProtoV6ProviderFactories: tfacc.ProviderFactories,
		CheckDestroy: func(*terraform.State) error {
			resp, err := tfacc.Client(acct).Do(t.Context(), stripeapi.Request{Method: "GET", Path: "/v1/promotion_codes/" + id})
			if err != nil {
				return fmt.Errorf("an archived promotion code still retrieves: %w", err)
			}
			if active, _ := resp.Body.(wire.Object).GetBool("active"); active {
				return fmt.Errorf("promotion code %s should be archived after destroy", id)
			}
			return nil
		},
		Steps: []resource.TestStep{{Config: codeConfig(acct, code, ""), Check: idOf(&id)}},
	})
}

func TestAccPromotionCode_drift(t *testing.T) {
	acct := acctest.NewAccount(t)
	code := acct.Unique("SAVE10-drift")
	var id string
	cfg := codeConfig(acct, code, "  metadata = { team = \"a\" }\n")
	resource.ParallelTest(t, resource.TestCase{
		ProtoV6ProviderFactories: tfacc.ProviderFactories,
		Steps: []resource.TestStep{
			{Config: cfg, Check: idOf(&id)},
			{
				PreConfig: func() {
					tfacc.Post(t, acct, "/v1/promotion_codes/"+id, wire.Object{
						{Name: "metadata", Value: wire.Object{{Name: "team", Value: wire.String("b")}}},
					})
				},
				Config: cfg,
				ConfigPlanChecks: resource.ConfigPlanChecks{PreApply: []plancheck.PlanCheck{
					plancheck.ExpectNonEmptyPlan(),
					plancheck.ExpectResourceAction(address, plancheck.ResourceActionUpdate),
				}},
				Check: resource.TestCheckResourceAttr(address, "metadata.team", "a"),
			},
		},
	})
}
