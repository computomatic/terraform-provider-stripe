package account_external_account_test

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
	"github.com/computomatic/terraform-provider-stripe/internal/stripeapi"
	"github.com/computomatic/terraform-provider-stripe/internal/wire"
)

var regexpInvalidImportID = regexp.MustCompile(`expected .<account id>/<id>.`)

func TestMain(m *testing.M) { os.Exit(acctest.Main(m)) }

const (
	address = "stripe_account_external_account.test"
	second  = "stripe_account_external_account.second"

	// Stripe's documented US test bank account numbers and routing number.
	routingNumber = "110000000"
	number        = "000123456789"
	otherNumber   = "000111111116"
)

// accountBlock is the connected account the bank accounts belong to.
func accountBlock(c acctest.Config) string {
	return c.ProviderBlock() + fmt.Sprintf(`
resource "stripe_account" "test" {
  type     = "custom"
  country  = "US"
  metadata = { run = %q }
}
`, c.Name("external accounts"))
}

// bankBlock is a US bank account of the account. extra lands inside it.
func bankBlock(name, accountNumber, extra string) string {
	return fmt.Sprintf(`
resource "stripe_account_external_account" %q {
  account        = stripe_account.test.id
  country        = "US"
  currency       = "usd"
  routing_number = %q
  account_number = %q
%s}
`, name, routingNumber, accountNumber, extra)
}

// after orders a second bank account behind the first: bank accounts created in one apply
// are added in no fixed order, and the first added is the default.
const after = "  depends_on = [stripe_account_external_account.test]\n"

func config(c acctest.Config, extra string) string {
	return accountBlock(c) + bankBlock("test", number, extra)
}

// importID is the account ID and the bank account ID, as the import ID wants them.
func importID(s *terraform.State) (string, error) {
	rs, ok := s.RootModule().Resources[address]
	if !ok {
		return "", fmt.Errorf("%s is not in state", address)
	}
	return rs.Primary.Attributes["account"] + "/" + rs.Primary.ID, nil
}

// ids records the account and bank account IDs of the resource at addr.
func ids(addr string, account, id *string) resource.TestCheckFunc {
	return func(s *terraform.State) error {
		rs, ok := s.RootModule().Resources[addr]
		if !ok {
			return fmt.Errorf("%s is not in state", addr)
		}
		*account, *id = rs.Primary.Attributes["account"], rs.Primary.ID
		return nil
	}
}

func bankPath(account, id string) string {
	return "/v1/accounts/" + account + "/external_accounts/" + id
}

func TestAccAccountExternalAccount_create(t *testing.T) {
	acct := acctest.NewAccount(t)
	resource.ParallelTest(t, resource.TestCase{
		ProtoV6ProviderFactories: tfacc.ProviderFactories,
		Steps: acctest.Forget(t, []resource.TestStep{{
			Config: config(acct, "") + `
data "stripe_account_external_account" "test" {
  account = stripe_account.test.id
  id      = stripe_account_external_account.test.id
}
`,
			Check: resource.ComposeAggregateTestCheckFunc(
				resource.TestCheckResourceAttrWith(address, "id", func(v string) error {
					if !regexp.MustCompile(`^ba_`).MatchString(v) {
						return fmt.Errorf("id %q does not start with ba_", v)
					}
					return nil
				}),
				resource.TestCheckResourceAttrPair(address, "account", "stripe_account.test", "id"),
				resource.TestCheckResourceAttr(address, "country", "US"),
				resource.TestCheckResourceAttr(address, "currency", "usd"),
				resource.TestCheckResourceAttr(address, "routing_number", routingNumber),
				resource.TestCheckResourceAttr(address, "last4", "6789"),
				resource.TestCheckResourceAttr(address, "bank_name", "STRIPE TEST BANK"),
				resource.TestCheckResourceAttr(address, "status", "new"),
				// The first bank account of a currency is its default.
				resource.TestCheckResourceAttr(address, "default_for_currency", "true"),
				// The account number is never returned; state keeps the configured one.
				resource.TestCheckResourceAttr(address, "account_number", number),
				resource.TestCheckResourceAttrPair("data.stripe_account_external_account.test", "id", address, "id"),
				resource.TestCheckResourceAttr("data.stripe_account_external_account.test", "last4", "6789"),
				resource.TestCheckResourceAttr("data.stripe_account_external_account.test", "default_for_currency", "true"),
				resource.TestCheckNoResourceAttr("data.stripe_account_external_account.test", "account_number"),
				func(*terraform.State) error {
					if acct.Smoke {
						return nil
					}
					// Create sends the bank account as the hash Stripe takes, with the object
					// kind, and the provider's own `id` never goes into a request.
					for _, entry := range acctest.RequestLog(t, acct) {
						if entry.Method != "POST" || !regexp.MustCompile(`/external_accounts$`).MatchString(entry.Path) {
							continue
						}
						params, _ := entry.Params.(wire.Object)
						hash, ok := params.Get("external_account")
						if !ok {
							return fmt.Errorf("create did not send external_account: %s", entry.Path)
						}
						inner, _ := hash.(wire.Object)
						for name, want := range map[string]string{"object": "bank_account", "country": "US", "currency": "usd", "routing_number": routingNumber, "account_number": number} {
							if got, _ := inner.GetString(name); got != want {
								return fmt.Errorf("external_account[%s] = %q, want %q", name, got, want)
							}
						}
						if _, top := params.Get("routing_number"); top {
							return fmt.Errorf("routing_number must be inside the hash only")
						}
					}
					return nil
				},
			),
		}}, nil, "stripe_account_external_account.test"),
	})
}

// Stripe takes the holder name and type, the metadata and the default for the currency on
// update (suite: "update", "update default for currency") and refuses the routing number,
// account number, country and currency, so the provider never sends them.
func TestAccAccountExternalAccount_update(t *testing.T) {
	acct := acctest.NewAccount(t)
	var account, first, other string
	resource.ParallelTest(t, resource.TestCase{
		ProtoV6ProviderFactories: tfacc.ProviderFactories,
		Steps: acctest.Forget(t, []resource.TestStep{
			{
				Config: accountBlock(acct) + bankBlock("test", number, "") + bankBlock("second", otherNumber, after),
				Check: resource.ComposeAggregateTestCheckFunc(
					ids(address, &account, &first),
					ids(second, &account, &other),
					resource.TestCheckResourceAttr(address, "default_for_currency", "true"),
					resource.TestCheckResourceAttr(second, "default_for_currency", "false"),
				),
			},
			{
				Config: accountBlock(acct) +
					bankBlock("test", number, fmt.Sprintf("  account_holder_name = %q\n  account_holder_type = \"company\"\n  metadata            = { k = \"v\" }\n", acct.Name("Acme Holdings"))) +
					bankBlock("second", otherNumber, after+"  default_for_currency = true\n"),
				ConfigPlanChecks: resource.ConfigPlanChecks{PreApply: []plancheck.PlanCheck{
					plancheck.ExpectResourceAction(address, plancheck.ResourceActionUpdate),
					plancheck.ExpectResourceAction(second, plancheck.ResourceActionUpdate),
				}},
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr(address, "account_holder_name", acct.Name("Acme Holdings")),
					resource.TestCheckResourceAttr(address, "account_holder_type", "company"),
					resource.TestCheckResourceAttr(address, "metadata.k", "v"),
					resource.TestCheckResourceAttr(second, "default_for_currency", "true"),
					resource.TestCheckResourceAttrWith(address, "id", func(v string) error {
						if v != first {
							return fmt.Errorf("the in-place update changed the ID from %s to %s", first, v)
						}
						return nil
					}),
					func(*terraform.State) error {
						if acct.Smoke {
							return nil
						}
						for _, entry := range acctest.RequestLog(t, acct) {
							params, ok := entry.Params.(wire.Object)
							if entry.Method != "POST" || !ok || entry.Path != bankPath(account, first) {
								continue
							}
							for _, createOnly := range []string{"routing_number", "account_number", "country", "currency", "external_account", "object"} {
								if _, sent := params.Get(createOnly); sent {
									return fmt.Errorf("POST %s sent the create-only field %s", entry.Path, createOnly)
								}
							}
						}
						return nil
					},
				),
			},
			// The first bank account stopped being the default when the second became it, and
			// its configuration does not say otherwise.
			{
				Config: accountBlock(acct) +
					bankBlock("test", number, fmt.Sprintf("  account_holder_name = %q\n  account_holder_type = \"company\"\n  metadata            = { k = \"v\" }\n", acct.Name("Acme Holdings"))) +
					bankBlock("second", otherNumber, after+"  default_for_currency = true\n"),
				Check: resource.TestCheckResourceAttr(address, "default_for_currency", "false"),
			},
		}, nil, "stripe_account_external_account"),
	})
}

// The routing number, account number, country and currency are create-only (suite: "update
// immutable fields"), so changing one would replace the bank account. This one is the default,
// which Stripe will not delete, so the plan fails, and so does a taint, at apply.
func TestAccAccountExternalAccount_replace(t *testing.T) {
	acct := acctest.NewAccount(t)
	cfg := config(acct, "  default_for_currency = true\n")
	resource.ParallelTest(t, resource.TestCase{
		ProtoV6ProviderFactories: tfacc.ProviderFactories,
		Steps: acctest.Forget(t, []resource.TestStep{
			{Config: cfg},
			{Config: accountBlock(acct) + bankBlock("test", otherNumber, "  default_for_currency = true\n"), PlanOnly: true, ExpectError: tfacc.RefusedReplace},
			{Config: cfg, Taint: []string{address}, ExpectError: tfacc.RefusedDestroy},
		}, nil, address),
	})
}

// The import ID is `<account_id>/<bank_account_id>`. Stripe never returns the account number,
// so an import leaves it out of state and a configuration that sets it ignores changes to it.
func TestAccAccountExternalAccount_import(t *testing.T) {
	acct := acctest.NewAccount(t)
	extra := fmt.Sprintf("  account_holder_name = %q\n  metadata = { k = \"v\" }\n", acct.Name("Acme Holdings"))
	cfg := config(acct, extra)
	imported := config(acct, extra+"  lifecycle {\n    ignore_changes = [account_number]\n  }\n")
	resource.ParallelTest(t, resource.TestCase{
		ProtoV6ProviderFactories: tfacc.ProviderFactories,
		Steps: acctest.Forget(t, []resource.TestStep{
			{Config: cfg},
			{
				ResourceName:            address,
				ImportState:             true,
				ImportStateIdFunc:       importID,
				ImportStateVerify:       true,
				ImportStateVerifyIgnore: []string{"account_number"},
			},
			{
				ResourceName:      address,
				ImportState:       true,
				ImportStateKind:   resource.ImportBlockWithID,
				ImportStateIdFunc: importID,
				Config:            imported,
			},
			{
				ResourceName:  address,
				ImportState:   true,
				ImportStateId: "not-a-compound-id",
				ExpectError:   regexpInvalidImportID,
			},
		}, nil, "stripe_account_external_account.test"),
	})
}

// The identity form needs Terraform or OpenTofu 1.12.0 or later. The identity carries the
// account as well as the bank account ID.
func TestAccAccountExternalAccount_importIdentity(t *testing.T) {
	acct := acctest.NewAccount(t)
	cfg := config(acct, "  lifecycle {\n    ignore_changes = [account_number]\n  }\n")
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
		}, nil, "stripe_account_external_account.test"),
	})
}

// Stripe deletes a bank account that is not the default with DELETE and answers a later GET
// with a 404 (suite: "delete").
func TestAccAccountExternalAccount_delete(t *testing.T) {
	acct := acctest.NewAccount(t)
	var account, id string
	both := accountBlock(acct) + bankBlock("test", number, "") + bankBlock("second", otherNumber, after)
	resource.ParallelTest(t, resource.TestCase{
		ProtoV6ProviderFactories: tfacc.ProviderFactories,
		Steps: acctest.Forget(t, []resource.TestStep{
			{Config: both, Check: ids(second, &account, &id)},
			// Removing the second bank account deletes it and leaves the first.
			{
				Config: config(acct, ""),
				Check: func(*terraform.State) error {
					_, err := tfacc.Client(acct).Do(t.Context(), stripeapi.Request{Method: "GET", Path: bankPath(account, id)})
					if !stripeapi.IsNotFound(err) {
						return fmt.Errorf("bank account %s should be deleted, GET gave %v", id, err)
					}
					return nil
				},
			},
		}, nil, "stripe_account_external_account.test"),
	})
}

// Stripe refuses to delete the default bank account of a currency, so dropping it from the
// configuration, and `terraform destroy`, fail at plan time, and it stays the default. Once it
// is out of the state, destroying the account deletes it.
func TestAccAccountExternalAccount_deleteDefault(t *testing.T) {
	acct := acctest.NewAccount(t)
	var account, id, path string
	var mark int
	cfg := config(acct, "")
	resource.ParallelTest(t, resource.TestCase{
		ProtoV6ProviderFactories: tfacc.ProviderFactories,
		Steps: acctest.Forget(t, []resource.TestStep{
			{Config: cfg, Check: resource.ComposeTestCheckFunc(ids(address, &account, &id), func(*terraform.State) error {
				path = bankPath(account, id)
				return nil
			}, tfacc.LogMark(t, acct, &mark))},
			{Config: accountBlock(acct), PlanOnly: true, ExpectError: tfacc.RefusedDestroy},
			{Config: cfg, Destroy: true, ExpectError: tfacc.RefusedDestroy},
		}, tfacc.Untouched(t, acct, &path, &mark, "default_for_currency", wire.Bool(true)), address),
	})
}

func TestAccAccountExternalAccount_drift(t *testing.T) {
	acct := acctest.NewAccount(t)
	var account, id string
	cfg := config(acct, fmt.Sprintf("  account_holder_name = %q\n", acct.Name("Acme Holdings")))
	resource.ParallelTest(t, resource.TestCase{
		ProtoV6ProviderFactories: tfacc.ProviderFactories,
		Steps: acctest.Forget(t, []resource.TestStep{
			{Config: cfg, Check: ids(address, &account, &id)},
			{
				PreConfig: func() {
					tfacc.Post(t, acct, bankPath(account, id), wire.Object{{Name: "account_holder_name", Value: wire.String("Renamed")}})
				},
				Config: cfg,
				ConfigPlanChecks: resource.ConfigPlanChecks{PreApply: []plancheck.PlanCheck{
					plancheck.ExpectNonEmptyPlan(),
					plancheck.ExpectResourceAction(address, plancheck.ResourceActionUpdate),
				}},
				Check: resource.TestCheckResourceAttr(address, "account_holder_name", acct.Name("Acme Holdings")),
			},
		}, nil, "stripe_account_external_account.test"),
	})
}

// A bank account deleted out of band is a 404, so refresh drops it and the plan adds it again.
func TestAccAccountExternalAccount_driftDeleted(t *testing.T) {
	acct := acctest.NewAccount(t)
	var account, id string
	cfg := accountBlock(acct) + bankBlock("test", number, "") + bankBlock("second", otherNumber, after)
	resource.ParallelTest(t, resource.TestCase{
		ProtoV6ProviderFactories: tfacc.ProviderFactories,
		Steps: acctest.Forget(t, []resource.TestStep{
			{Config: cfg, Check: ids(second, &account, &id)},
			{
				PreConfig: func() {
					if _, err := tfacc.Client(acct).Do(t.Context(), stripeapi.Request{Method: "DELETE", Path: bankPath(account, id)}); err != nil {
						t.Fatal(err)
					}
				},
				Config: cfg,
				ConfigPlanChecks: resource.ConfigPlanChecks{PreApply: []plancheck.PlanCheck{
					plancheck.ExpectNonEmptyPlan(),
					plancheck.ExpectResourceAction(second, plancheck.ResourceActionCreate),
				}},
				Check: resource.TestCheckResourceAttrWith(second, "id", func(v string) error {
					if v == id {
						return fmt.Errorf("the bank account kept the deleted ID %s", v)
					}
					return nil
				}),
			},
		}, nil, "stripe_account_external_account.test"),
	})
}

// When the account is deleted out of band, Stripe answers its bank accounts with 403
// `account_invalid`. Refresh takes that for gone, so both are created again.
func TestAccAccountExternalAccount_driftAccountDeleted(t *testing.T) {
	acct := acctest.NewAccount(t)
	var account, id string
	cfg := config(acct, "")
	resource.ParallelTest(t, resource.TestCase{
		ProtoV6ProviderFactories: tfacc.ProviderFactories,
		Steps: acctest.Forget(t, []resource.TestStep{
			{Config: cfg, Check: ids(address, &account, &id)},
			{
				PreConfig: func() {
					if _, err := tfacc.Client(acct).Do(t.Context(), stripeapi.Request{Method: "DELETE", Path: "/v1/accounts/" + account}); err != nil {
						t.Fatal(err)
					}
				},
				Config: cfg,
				ConfigPlanChecks: resource.ConfigPlanChecks{PreApply: []plancheck.PlanCheck{
					plancheck.ExpectNonEmptyPlan(),
					plancheck.ExpectResourceAction("stripe_account.test", plancheck.ResourceActionCreate),
					plancheck.ExpectResourceAction(address, plancheck.ResourceActionCreate),
				}},
			},
		}, nil, "stripe_account_external_account.test"),
	})
}

// Stripe refuses a routing number that fails its check and an account number that is not a
// test number in test mode (suite: "create errors"); the error reaches the user unchanged.
func TestAccAccountExternalAccount_createError(t *testing.T) {
	acct := acctest.NewAccount(t)
	resource.ParallelTest(t, resource.TestCase{
		ProtoV6ProviderFactories: tfacc.ProviderFactories,
		Steps: []resource.TestStep{{
			Config:      accountBlock(acct) + bankBlock("test", "12", ""),
			ExpectError: regexp.MustCompile(`account_number_invalid`),
		}},
	})
}
