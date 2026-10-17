package product_test

import (
	"fmt"
	"os"
	"slices"
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

const address = "stripe_product.test"

func config(c acctest.Config, body string) string {
	return c.ProviderBlock() + body
}

func productConfig(c acctest.Config, id, name, extra string) string {
	return config(c, fmt.Sprintf("resource \"stripe_product\" \"test\" {\n  id   = %q\n  name = %q\n%s}\n", id, name, extra))
}

// idOf returns a check that stores the product's ID for the out-of-band calls.
func idOf(dst *string) resource.TestCheckFunc {
	return resource.TestCheckResourceAttrWith(address, "id", func(v string) error {
		*dst = v
		return nil
	})
}

func TestAccProduct_create(t *testing.T) {
	acct := acctest.NewAccount(t)
	resource.ParallelTest(t, resource.TestCase{
		ProtoV6ProviderFactories: tfacc.ProviderFactories,
		Steps: []resource.TestStep{{
			Config: config(acct, `
resource "stripe_product" "test" {
  name        = "Gold plan"
  description = "The gold one"
  metadata    = { team = "a" }
  package_dimensions = {
    height = 1.5
    length = 2
    weight = 3
    width  = 4
  }
}

data "stripe_product" "test" {
  id = stripe_product.test.id
}
`),
			Check: resource.ComposeAggregateTestCheckFunc(
				resource.TestCheckResourceAttrSet(address, "id"),
				resource.TestCheckResourceAttr(address, "name", "Gold plan"),
				resource.TestCheckResourceAttr(address, "active", "true"),
				resource.TestCheckResourceAttr(address, "metadata.team", "a"),
				resource.TestCheckResourceAttr(address, "package_dimensions.height", "1.5"),
				resource.TestCheckResourceAttr(address, "livemode", "false"),
				resource.TestCheckNoResourceAttr(address, "url"),
				resource.TestCheckResourceAttrPair("data.stripe_product.test", "id", address, "id"),
				resource.TestCheckResourceAttr("data.stripe_product.test", "name", "Gold plan"),
				resource.TestCheckResourceAttr("data.stripe_product.test", "metadata.team", "a"),
			),
		}},
	})
}

func TestAccProduct_update(t *testing.T) {
	acct := acctest.NewAccount(t)
	resource.ParallelTest(t, resource.TestCase{
		ProtoV6ProviderFactories: tfacc.ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: config(acct, `
resource "stripe_product" "test" {
  name        = "Gold plan"
  description = "The gold one"
  metadata    = { team = "a", tier = "1" }
}
`),
				Check: resource.TestCheckResourceAttr(address, "description", "The gold one"),
			},
			{
				Config: config(acct, `
resource "stripe_product" "test" {
  name     = "Gold plan v2"
  metadata = { team = "b" }
}
`),
				ConfigPlanChecks: resource.ConfigPlanChecks{PreApply: []plancheck.PlanCheck{
					plancheck.ExpectResourceAction(address, plancheck.ResourceActionUpdate),
				}},
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr(address, "name", "Gold plan v2"),
					resource.TestCheckNoResourceAttr(address, "description"),
					resource.TestCheckResourceAttr(address, "metadata.%", "1"),
					resource.TestCheckResourceAttr(address, "metadata.team", "b"),
					func(*terraform.State) error {
						// The provider never sent the create-only `id` on update, and every
						// POST carried an idempotency key.
						for _, entry := range acctest.RequestLog(t, acct) {
							if entry.Method != "POST" {
								continue
							}
							if entry.IdempotencyKey == "" {
								return fmt.Errorf("POST %s had no idempotency key", entry.Path)
							}
							if params, ok := entry.Params.(wire.Object); ok && entry.Path != "/v1/products" {
								if _, sent := params.Get("id"); sent {
									return fmt.Errorf("POST %s sent the create-only field id", entry.Path)
								}
							}
						}
						return nil
					},
				),
			},
		},
	})
}

func TestAccProduct_replace(t *testing.T) {
	acct := acctest.NewAccount(t)
	resource.ParallelTest(t, resource.TestCase{
		ProtoV6ProviderFactories: tfacc.ProviderFactories,
		Steps: []resource.TestStep{
			{Config: productConfig(acct, "prod_first", "Gold plan", "")},
			{
				Config: productConfig(acct, "prod_second", "Gold plan", ""),
				ConfigPlanChecks: resource.ConfigPlanChecks{PreApply: []plancheck.PlanCheck{
					plancheck.ExpectResourceAction(address, plancheck.ResourceActionReplace),
				}},
				Check: resource.TestCheckResourceAttr(address, "id", "prod_second"),
			},
		},
	})
}

func TestAccProduct_import(t *testing.T) {
	acct := acctest.NewAccount(t)
	body := `
resource "stripe_product" "test" {
  name        = "Gold plan"
  description = "The gold one"
  metadata    = { team = "a" }
  images      = ["https://example.com/gold.png"]
  marketing_features = [{ name = "Priority support" }]
}
`
	resource.ParallelTest(t, resource.TestCase{
		ProtoV6ProviderFactories: tfacc.ProviderFactories,
		Steps: []resource.TestStep{
			{Config: config(acct, body)},
			{
				ResourceName:      address,
				ImportState:       true,
				ImportStateVerify: true,
			},
			{
				ResourceName:    address,
				ImportState:     true,
				ImportStateKind: resource.ImportBlockWithID,
				Config:          config(acct, body),
			},
		},
	})
}

func TestAccProduct_delete(t *testing.T) {
	acct := acctest.NewAccount(t)
	var id string
	resource.ParallelTest(t, resource.TestCase{
		ProtoV6ProviderFactories: tfacc.ProviderFactories,
		CheckDestroy: func(*terraform.State) error {
			_, err := tfacc.Client(acct).Do(t.Context(), stripeapi.Request{Method: "GET", Path: "/v1/products/" + id})
			if !stripeapi.IsNotFound(err) {
				return fmt.Errorf("product %s should be gone after destroy, got %v", id, err)
			}
			return nil
		},
		Steps: []resource.TestStep{{
			Config: productConfig(acct, "prod_doomed", "Doomed", ""),
			Check:  idOf(&id),
		}},
	})
}

// Stripe refuses to delete a product that has a price. Destroy archives the product
// instead.
func TestAccProduct_destroyWithPrices(t *testing.T) {
	acct := acctest.NewAccount(t)
	var id string
	resource.ParallelTest(t, resource.TestCase{
		ProtoV6ProviderFactories: tfacc.ProviderFactories,
		CheckDestroy: func(*terraform.State) error {
			resp, err := tfacc.Client(acct).Do(t.Context(), stripeapi.Request{Method: "GET", Path: "/v1/products/" + id})
			if err != nil {
				return fmt.Errorf("an archived product still retrieves: %w", err)
			}
			if active, _ := resp.Body.(wire.Object).GetBool("active"); active {
				return fmt.Errorf("product %s should be archived after destroy", id)
			}
			var deleteTried, archived bool
			// The log lists the newest request first.
			log := acctest.RequestLog(t, acct)
			slices.Reverse(log)
			for _, entry := range log {
				switch {
				case entry.Method == "DELETE" && entry.Path == "/v1/products/"+id:
					deleteTried = entry.Status == 400
				case entry.Method == "POST" && entry.Path == "/v1/products/"+id:
					if params, ok := entry.Params.(wire.Object); ok && deleteTried {
						_, archived = params.Get("active")
					}
				}
			}
			if !deleteTried || !archived {
				return fmt.Errorf("destroy must try DELETE (refused with 400) and then set active=false: delete refused %v, archived %v", deleteTried, archived)
			}
			return nil
		},
		Steps: []resource.TestStep{
			{Config: productConfig(acct, "prod_priced", "Gold plan", ""), Check: idOf(&id)},
			{
				// The price is made behind the provider's back, so this test does not depend
				// on stripe_price.
				PreConfig: func() {
					tfacc.Post(t, acct, "/v1/prices", wire.Object{
						{Name: "product", Value: wire.String(id)},
						{Name: "currency", Value: wire.String("usd")},
						{Name: "unit_amount", Value: wire.Int(2000)},
					})
				},
				Config: productConfig(acct, "prod_priced", "Gold plan", ""),
			},
		},
	})
}

func TestAccProduct_drift(t *testing.T) {
	acct := acctest.NewAccount(t)
	var id string
	cfg := productConfig(acct, "prod_drift", "Gold plan", "  description = \"The gold one\"\n")
	resource.ParallelTest(t, resource.TestCase{
		ProtoV6ProviderFactories: tfacc.ProviderFactories,
		Steps: []resource.TestStep{
			{Config: cfg, Check: idOf(&id)},
			{
				PreConfig: func() {
					tfacc.Post(t, acct, "/v1/products/"+id, wire.Object{
						{Name: "name", Value: wire.String("Renamed")},
					})
				},
				Config: cfg,
				ConfigPlanChecks: resource.ConfigPlanChecks{PreApply: []plancheck.PlanCheck{
					plancheck.ExpectNonEmptyPlan(),
					plancheck.ExpectResourceAction(address, plancheck.ResourceActionUpdate),
				}},
				Check: resource.TestCheckResourceAttr(address, "name", "Gold plan"),
			},
		},
	})
}
