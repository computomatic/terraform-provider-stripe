package customer_test

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

const address = "stripe_customer.test"

func customerConfig(c acctest.Config, body string) string {
	return c.ProviderBlock() + "resource \"stripe_customer\" \"test\" {\n" + body + "}\n"
}

func idOf(dst *string) resource.TestCheckFunc {
	return resource.TestCheckResourceAttrWith(address, "id", func(v string) error {
		*dst = v
		return nil
	})
}

// named is the body for customerConfig that sets the customer's name, then the rest.
func named(c acctest.Config, name, rest string) string {
	return fmt.Sprintf("  name = %q\n%s", c.Name(name), rest)
}

func TestAccCustomer_create(t *testing.T) {
	acct := acctest.NewAccount(t)
	resource.ParallelTest(t, resource.TestCase{
		ProtoV6ProviderFactories: tfacc.ProviderFactories,
		Steps: []resource.TestStep{{
			Config: customerConfig(acct, fmt.Sprintf(`
  name        = %q
  email       = "billing@acme.example"
  description = "The Acme account"
  metadata    = { team = "a" }
  address = {
    line1       = "1 Main Street"
    city        = "Springfield"
    postal_code = "12345"
    country     = "US"
  }
`, acct.Name("Acme Inc."))) + `
data "stripe_customer" "test" {
  id = stripe_customer.test.id
}
`,
			Check: resource.ComposeAggregateTestCheckFunc(
				resource.TestCheckResourceAttrSet(address, "id"),
				resource.TestCheckResourceAttr(address, "name", acct.Name("Acme Inc.")),
				resource.TestCheckResourceAttr(address, "email", "billing@acme.example"),
				resource.TestCheckResourceAttr(address, "metadata.team", "a"),
				resource.TestCheckResourceAttr(address, "address.city", "Springfield"),
				resource.TestCheckResourceAttr(address, "livemode", "false"),
				resource.TestCheckNoResourceAttr(address, "phone"),
				resource.TestCheckResourceAttrPair("data.stripe_customer.test", "id", address, "id"),
				resource.TestCheckResourceAttr("data.stripe_customer.test", "email", "billing@acme.example"),
				resource.TestCheckResourceAttr("data.stripe_customer.test", "address.country", "US"),
			),
		}},
	})
}

func TestAccCustomer_update(t *testing.T) {
	acct := acctest.NewAccount(t)
	resource.ParallelTest(t, resource.TestCase{
		ProtoV6ProviderFactories: tfacc.ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: customerConfig(acct, named(acct, "Acme", "  email = \"a@acme.example\"\n  metadata = { team = \"a\", tier = \"1\" }\n")),
				Check:  resource.TestCheckResourceAttr(address, "email", "a@acme.example"),
			},
			{
				Config: customerConfig(acct, named(acct, "Acme v2", "  email = \"b@acme.example\"\n  metadata = { team = \"b\" }\n")),
				ConfigPlanChecks: resource.ConfigPlanChecks{PreApply: []plancheck.PlanCheck{
					plancheck.ExpectResourceAction(address, plancheck.ResourceActionUpdate),
				}},
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr(address, "name", acct.Name("Acme v2")),
					resource.TestCheckResourceAttr(address, "email", "b@acme.example"),
					resource.TestCheckResourceAttr(address, "metadata.%", "1"),
					resource.TestCheckResourceAttr(address, "metadata.team", "b"),
				),
			},
		},
	})
}

// tax_id_data is accepted on create only, so changing it replaces the customer. The fake
// serves no create-only customer parameter yet, so the test skips on its
// fake_not_implemented reply and runs once the fake serves tax_id_data.
func TestAccCustomer_replace(t *testing.T) {
	acct := acctest.NewAccount(t)
	var first string
	taxID := func(value string) string {
		return fmt.Sprintf("  name = %q\n  tax_id_data = [{ type = \"eu_vat\", value = %q }]\n", acct.Name("Acme"), value)
	}
	resource.ParallelTest(t, resource.TestCase{
		ProtoV6ProviderFactories: tfacc.ProviderFactories,
		ErrorCheck:               tfacc.SkipWhenFakeLacks(t, "tax_id_data"),
		Steps: []resource.TestStep{
			{Config: customerConfig(acct, taxID("DE123456789")), Check: idOf(&first)},
			{
				Config: customerConfig(acct, taxID("DE987654321")),
				ConfigPlanChecks: resource.ConfigPlanChecks{PreApply: []plancheck.PlanCheck{
					plancheck.ExpectResourceAction(address, plancheck.ResourceActionReplace),
				}},
				Check: resource.TestCheckResourceAttrWith(address, "id", func(v string) error {
					if v == first {
						return fmt.Errorf("the replacement kept the customer ID %s", v)
					}
					return nil
				}),
			},
		},
	})
}

func TestAccCustomer_import(t *testing.T) {
	acct := acctest.NewAccount(t)
	cfg := customerConfig(acct, named(acct, "Acme", "  email = \"a@acme.example\"\n  metadata = { team = \"a\" }\n  preferred_locales = [\"en\"]\n"))
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

func TestAccCustomer_delete(t *testing.T) {
	acct := acctest.NewAccount(t)
	var id string
	resource.ParallelTest(t, resource.TestCase{
		ProtoV6ProviderFactories: tfacc.ProviderFactories,
		CheckDestroy: func(*terraform.State) error {
			// Stripe retrieves a deleted customer as a stub with deleted=true.
			resp, err := tfacc.Client(acct).Do(t.Context(), stripeapi.Request{Method: "GET", Path: "/v1/customers/" + id})
			if stripeapi.IsNotFound(err) {
				return nil
			}
			if err != nil {
				return err
			}
			if deleted, _ := resp.Body.(wire.Object).GetBool("deleted"); !deleted {
				return fmt.Errorf("customer %s should be deleted after destroy", id)
			}
			return nil
		},
		Steps: []resource.TestStep{{
			Config: customerConfig(acct, named(acct, "Doomed", "")),
			Check:  idOf(&id),
		}},
	})
}

func TestAccCustomer_drift(t *testing.T) {
	acct := acctest.NewAccount(t)
	var id string
	cfg := customerConfig(acct, named(acct, "Acme", "  email = \"a@acme.example\"\n"))
	resource.ParallelTest(t, resource.TestCase{
		ProtoV6ProviderFactories: tfacc.ProviderFactories,
		Steps: []resource.TestStep{
			{Config: cfg, Check: idOf(&id)},
			{
				PreConfig: func() {
					tfacc.Post(t, acct, "/v1/customers/"+id, wire.Object{
						{Name: "name", Value: wire.String("Renamed")},
					})
				},
				Config: cfg,
				ConfigPlanChecks: resource.ConfigPlanChecks{PreApply: []plancheck.PlanCheck{
					plancheck.ExpectNonEmptyPlan(),
					plancheck.ExpectResourceAction(address, plancheck.ResourceActionUpdate),
				}},
				Check: resource.TestCheckResourceAttr(address, "name", acct.Name("Acme")),
			},
		},
	})
}
