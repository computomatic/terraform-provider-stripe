package customer_balance_transaction_test

import (
	"fmt"
	"os"
	"regexp"
	"testing"

	"github.com/hashicorp/go-version"
	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/plancheck"
	"github.com/hashicorp/terraform-plugin-testing/terraform"
	"github.com/hashicorp/terraform-plugin-testing/tfversion"

	"github.com/computomatic/terraform-provider-stripe/internal/acctest"
	"github.com/computomatic/terraform-provider-stripe/internal/acctest/tfacc"
	"github.com/computomatic/terraform-provider-stripe/internal/wire"
)

var regexpInvalidImportID = regexp.MustCompile(`expected .<customer id>/<id>.`)

func TestMain(m *testing.M) { os.Exit(acctest.Main(m)) }

const address = "stripe_customer_balance_transaction.test"

// transactionConfig is a customer and a balance transaction on it. extra lands inside the
// transaction.
func transactionConfig(c acctest.Config, amount int, extra string) string {
	return c.ProviderBlock() + fmt.Sprintf(`
resource "stripe_customer" "test" {
  name = %q
}

resource "stripe_customer_balance_transaction" "test" {
  customer = stripe_customer.test.id
  amount   = %d
  currency = "usd"
%s}
`, c.Name("Balance customer"), amount, extra)
}

// importID is the customer ID and the transaction ID, as the import ID wants them.
func importID(s *terraform.State) (string, error) {
	rs, ok := s.RootModule().Resources[address]
	if !ok {
		return "", fmt.Errorf("%s is not in state", address)
	}
	return rs.Primary.Attributes["customer"] + "/" + rs.Primary.ID, nil
}

// ids records the customer and transaction IDs.
func ids(customer, id *string) resource.TestCheckFunc {
	return func(s *terraform.State) error {
		rs, ok := s.RootModule().Resources[address]
		if !ok {
			return fmt.Errorf("%s is not in state", address)
		}
		*customer, *id = rs.Primary.Attributes["customer"], rs.Primary.ID
		return nil
	}
}

func transactionPath(customer, id string) string {
	return "/v1/customers/" + customer + "/balance_transactions/" + id
}

func TestAccCustomerBalanceTransaction_create(t *testing.T) {
	acct := acctest.NewAccount(t)
	resource.ParallelTest(t, resource.TestCase{
		ProtoV6ProviderFactories: tfacc.ProviderFactories,
		Steps: acctest.Forget(t, []resource.TestStep{{
			Config: transactionConfig(acct, -500, "  description = \"Goodwill credit\"\n  metadata    = { reason = \"outage\" }\n") + `
data "stripe_customer_balance_transaction" "test" {
  customer = stripe_customer.test.id
  id       = stripe_customer_balance_transaction.test.id
}
`,
			Check: resource.ComposeAggregateTestCheckFunc(
				resource.TestCheckResourceAttrSet(address, "id"),
				resource.TestCheckResourceAttrPair(address, "customer", "stripe_customer.test", "id"),
				resource.TestCheckResourceAttr(address, "amount", "-500"),
				resource.TestCheckResourceAttr(address, "currency", "usd"),
				resource.TestCheckResourceAttr(address, "description", "Goodwill credit"),
				resource.TestCheckResourceAttr(address, "metadata.reason", "outage"),
				resource.TestCheckResourceAttr(address, "type", "adjustment"),
				resource.TestCheckResourceAttr(address, "ending_balance", "-500"),
				resource.TestCheckResourceAttrPair("data.stripe_customer_balance_transaction.test", "id", address, "id"),
				resource.TestCheckResourceAttr("data.stripe_customer_balance_transaction.test", "amount", "-500"),
				resource.TestCheckResourceAttr("data.stripe_customer_balance_transaction.test", "description", "Goodwill credit"),
			),
		}}, nil, "stripe_customer_balance_transaction"),
	})
}

// `description` and `metadata` change in place (suite: "update") and the amount stays.
func TestAccCustomerBalanceTransaction_update(t *testing.T) {
	acct := acctest.NewAccount(t)
	var first string
	resource.ParallelTest(t, resource.TestCase{
		ProtoV6ProviderFactories: tfacc.ProviderFactories,
		Steps: acctest.Forget(t, []resource.TestStep{
			{
				Config: transactionConfig(acct, -500, "  description = \"Goodwill credit\"\n  metadata    = { reason = \"outage\" }\n"),
				Check:  resource.TestCheckResourceAttrWith(address, "id", func(v string) error { first = v; return nil }),
			},
			{
				Config: transactionConfig(acct, -500, "  description = \"Goodwill credit, edited\"\n  metadata    = { note = \"added\" }\n"),
				ConfigPlanChecks: resource.ConfigPlanChecks{PreApply: []plancheck.PlanCheck{
					plancheck.ExpectResourceAction(address, plancheck.ResourceActionUpdate),
				}},
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr(address, "description", "Goodwill credit, edited"),
					resource.TestCheckResourceAttr(address, "metadata.%", "1"),
					resource.TestCheckResourceAttr(address, "metadata.note", "added"),
					resource.TestCheckResourceAttr(address, "amount", "-500"),
					resource.TestCheckResourceAttrWith(address, "id", func(v string) error {
						if v != first {
							return fmt.Errorf("the in-place update changed the transaction from %s to %s", first, v)
						}
						return nil
					}),
				),
			},
		}, nil, "stripe_customer_balance_transaction"),
	})
}

// `amount` is create-only, so changing it would replace the transaction, which Stripe cannot
// delete or reverse: the plan fails, and so does a taint, at apply.
func TestAccCustomerBalanceTransaction_replace(t *testing.T) {
	acct := acctest.NewAccount(t)
	cfg := transactionConfig(acct, -500, "")
	resource.ParallelTest(t, resource.TestCase{
		ProtoV6ProviderFactories: tfacc.ProviderFactories,
		Steps: acctest.Forget(t, []resource.TestStep{
			{Config: cfg},
			{Config: transactionConfig(acct, -700, ""), PlanOnly: true, ExpectError: tfacc.RefusedReplace},
			{Config: cfg, Taint: []string{address}, ExpectError: tfacc.RefusedDestroy},
		}, nil, "stripe_customer_balance_transaction"),
	})
}

// The import ID is `<customer_id>/<transaction_id>`.
func TestAccCustomerBalanceTransaction_import(t *testing.T) {
	acct := acctest.NewAccount(t)
	cfg := transactionConfig(acct, -500, "  description = \"Goodwill credit\"\n  metadata    = { reason = \"outage\" }\n")
	resource.ParallelTest(t, resource.TestCase{
		ProtoV6ProviderFactories: tfacc.ProviderFactories,
		Steps: acctest.Forget(t, []resource.TestStep{
			{Config: cfg},
			{
				ResourceName:      address,
				ImportState:       true,
				ImportStateIdFunc: importID,
				ImportStateVerify: true,
			},
			{
				ResourceName:      address,
				ImportState:       true,
				ImportStateKind:   resource.ImportBlockWithID,
				ImportStateIdFunc: importID,
				Config:            cfg,
			},
			{
				ResourceName:  address,
				ImportState:   true,
				ImportStateId: "not-a-compound-id",
				ExpectError:   regexpInvalidImportID,
			},
		}, nil, "stripe_customer_balance_transaction"),
	})
}

// The identity form needs Terraform or OpenTofu 1.12.0 or later. The identity carries the
// customer as well as the transaction ID.
func TestAccCustomerBalanceTransaction_importIdentity(t *testing.T) {
	acct := acctest.NewAccount(t)
	cfg := transactionConfig(acct, -500, "")
	resource.ParallelTest(t, resource.TestCase{
		ProtoV6ProviderFactories: tfacc.ProviderFactories,
		TerraformVersionChecks:   []tfversion.TerraformVersionCheck{tfversion.RequireAbove(version.Must(version.NewVersion("1.12.0")))},
		Steps: acctest.Forget(t, []resource.TestStep{
			{Config: cfg},
			{
				ResourceName:    address,
				ImportState:     true,
				ImportStateKind: resource.ImportBlockWithResourceIdentity,
				Config:          cfg,
			},
		}, nil, "stripe_customer_balance_transaction"),
	})
}

// Stripe cannot delete a balance transaction (suite: "delete is not served"), so dropping it
// from the configuration, and `terraform destroy`, fail at plan time, and the transaction
// stays on the customer's balance.
func TestAccCustomerBalanceTransaction_delete(t *testing.T) {
	acct := acctest.NewAccount(t)
	var customer, id, path string
	var mark int
	cfg := transactionConfig(acct, -500, "")
	resource.ParallelTest(t, resource.TestCase{
		ProtoV6ProviderFactories: tfacc.ProviderFactories,
		Steps: acctest.Forget(t, []resource.TestStep{
			{Config: cfg, Check: resource.ComposeTestCheckFunc(ids(&customer, &id), func(*terraform.State) error {
				path = transactionPath(customer, id)
				return nil
			}, tfacc.LogMark(t, acct, &mark))},
			{Config: acctest.Without(t, cfg, "stripe_customer_balance_transaction"), PlanOnly: true, ExpectError: tfacc.RefusedDestroy},
			{Config: cfg, Destroy: true, ExpectError: tfacc.RefusedDestroy},
		}, tfacc.Untouched(t, acct, &path, &mark, "amount", wire.Int(-500)), "stripe_customer_balance_transaction"),
	})
}

// A description changed out of band shows as drift and the apply restores it.
func TestAccCustomerBalanceTransaction_drift(t *testing.T) {
	acct := acctest.NewAccount(t)
	var customer, id string
	cfg := transactionConfig(acct, -500, "  description = \"Goodwill credit\"\n")
	resource.ParallelTest(t, resource.TestCase{
		ProtoV6ProviderFactories: tfacc.ProviderFactories,
		Steps: acctest.Forget(t, []resource.TestStep{
			{Config: cfg, Check: ids(&customer, &id)},
			{
				PreConfig: func() {
					tfacc.Post(t, acct, transactionPath(customer, id), wire.Object{{Name: "description", Value: wire.String("changed elsewhere")}})
				},
				Config: cfg,
				ConfigPlanChecks: resource.ConfigPlanChecks{PreApply: []plancheck.PlanCheck{
					plancheck.ExpectNonEmptyPlan(),
					plancheck.ExpectResourceAction(address, plancheck.ResourceActionUpdate),
				}},
				Check: resource.TestCheckResourceAttr(address, "description", "Goodwill credit"),
			},
		}, nil, "stripe_customer_balance_transaction"),
	})
}
