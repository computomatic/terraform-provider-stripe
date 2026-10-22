package account_test

import (
	"errors"
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

func TestMain(m *testing.M) { os.Exit(acctest.Main(m)) }

const address = "stripe_account.test"

// customConfig is a custom account in the US, the one country the suite proves. extra lands
// inside the account.
func customConfig(c acctest.Config, extra string) string {
	return c.ProviderBlock() + fmt.Sprintf(`
resource "stripe_account" "test" {
  type    = "custom"
  country = "US"
%s}
`, extra)
}

// controllerConfig is an account created from controller properties.
func controllerConfig(c acctest.Config, extra string) string {
	return c.ProviderBlock() + fmt.Sprintf(`
resource "stripe_account" "test" {
  country = "US"
  controller = {
    fees                   = { payer = "application" }
    losses                 = { payments = "application" }
    requirement_collection = "stripe"
    stripe_dashboard       = { type = "express" }
  }
%s}
`, extra)
}

const capabilities = `  capabilities = {
    card_payments = { requested = true }
    transfers     = { requested = true }
  }
`

func idOf(dst *string) resource.TestCheckFunc {
	return resource.TestCheckResourceAttrWith(address, "id", func(v string) error {
		*dst = v
		return nil
	})
}

// isGone reports Stripe's answer for an account that was deleted: 403 `account_invalid`.
func isGone(err error) bool {
	var apiErr *stripeapi.APIError
	return errors.As(err, &apiErr) && apiErr.Status == 403 && apiErr.Code == "account_invalid"
}

func TestAccAccount_create(t *testing.T) {
	acct := acctest.NewAccount(t)
	run := acct.Name("create")
	resource.ParallelTest(t, resource.TestCase{
		ProtoV6ProviderFactories: tfacc.ProviderFactories,
		Steps: []resource.TestStep{{
			Config: customConfig(acct, capabilities+fmt.Sprintf("  metadata = { run = %q }\n", run)) + `
data "stripe_account" "test" {
  id = stripe_account.test.id
}

data "stripe_account_capability" "card_payments" {
  account = stripe_account.test.id
  id      = "card_payments"
}
`,
			Check: resource.ComposeAggregateTestCheckFunc(
				resource.TestCheckResourceAttrWith(address, "id", func(v string) error {
					if !regexp.MustCompile(`^acct_`).MatchString(v) {
						return fmt.Errorf("id %q does not start with acct_", v)
					}
					return nil
				}),
				resource.TestCheckResourceAttr(address, "type", "custom"),
				resource.TestCheckResourceAttr(address, "country", "US"),
				resource.TestCheckResourceAttr(address, "charges_enabled", "false"),
				resource.TestCheckResourceAttr(address, "payouts_enabled", "false"),
				resource.TestCheckResourceAttr(address, "details_submitted", "false"),
				resource.TestCheckResourceAttr(address, "default_currency", "usd"),
				resource.TestCheckNoResourceAttr(address, "email"),
				resource.TestCheckNoResourceAttr(address, "controller"),
				resource.TestCheckResourceAttr(address, "metadata.run", run),
				resource.TestCheckResourceAttr(address, "capabilities.card_payments.requested", "true"),
				resource.TestCheckResourceAttrPair("data.stripe_account.test", "id", address, "id"),
				resource.TestCheckResourceAttr("data.stripe_account.test", "type", "custom"),
				resource.TestCheckResourceAttr("data.stripe_account.test", "metadata.run", run),
				// The capability data source reads what the account asked for.
				resource.TestCheckResourceAttr("data.stripe_account_capability.card_payments", "requested", "true"),
				resource.TestCheckResourceAttr("data.stripe_account_capability.card_payments", "status", "inactive"),
			),
		}},
	})
}

// An account created from controller properties has the top-level type `none`, and Stripe
// echoes the controller (suite: "create controller based express").
func TestAccAccount_createController(t *testing.T) {
	acct := acctest.NewAccount(t)
	resource.ParallelTest(t, resource.TestCase{
		ProtoV6ProviderFactories: tfacc.ProviderFactories,
		Steps: []resource.TestStep{{
			Config: controllerConfig(acct, capabilities),
			Check: resource.ComposeAggregateTestCheckFunc(
				resource.TestCheckResourceAttr(address, "type", "none"),
				resource.TestCheckResourceAttr(address, "controller.requirement_collection", "stripe"),
				resource.TestCheckResourceAttr(address, "controller.stripe_dashboard.type", "express"),
				resource.TestCheckResourceAttr(address, "controller.fees.payer", "application"),
				resource.TestCheckResourceAttr(address, "controller.losses.payments", "application"),
			),
		}},
	})
}

// A configuration sets exactly one of `type` and `controller`, which the plan enforces
// before Stripe is called.
func TestAccAccount_oneShape(t *testing.T) {
	acct := acctest.NewAccount(t)
	resource.ParallelTest(t, resource.TestCase{
		ProtoV6ProviderFactories: tfacc.ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config:      acct.ProviderBlock() + "resource \"stripe_account\" \"test\" {\n  country = \"US\"\n}\n",
				PlanOnly:    true,
				ExpectError: regexp.MustCompile(`(?s)exactly one of.*type.*controller`),
			},
			{
				Config: acct.ProviderBlock() + `
resource "stripe_account" "test" {
  type       = "custom"
  country    = "US"
  controller = { requirement_collection = "stripe" }
}
`,
				PlanOnly:    true,
				ExpectError: regexp.MustCompile(`(?s)exactly one of.*type.*controller`),
			},
		},
	})
}

// Stripe takes email and the business profile name on update (suite: "update") and merges
// metadata; it refuses country, type and controller there, so the provider never sends them.
func TestAccAccount_update(t *testing.T) {
	acct := acctest.NewAccount(t)
	name := acct.Name("Acme Supplies")
	var first string
	resource.ParallelTest(t, resource.TestCase{
		ProtoV6ProviderFactories: tfacc.ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: customConfig(acct, capabilities+fmt.Sprintf("  metadata = { run = %q }\n", acct.Name("update"))),
				Check:  idOf(&first),
			},
			{
				Config: customConfig(acct, capabilities+fmt.Sprintf(`  email            = "owner@example.com"
  business_profile = { name = %q }
  metadata         = { run = %q, stage = "updated" }
`, name, acct.Name("update"))),
				ConfigPlanChecks: resource.ConfigPlanChecks{PreApply: []plancheck.PlanCheck{
					plancheck.ExpectResourceAction(address, plancheck.ResourceActionUpdate),
				}},
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr(address, "email", "owner@example.com"),
					resource.TestCheckResourceAttr(address, "business_profile.name", name),
					resource.TestCheckResourceAttr(address, "metadata.stage", "updated"),
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
							if entry.Method != "POST" || !ok || entry.Path == "/v1/accounts" {
								continue
							}
							for _, createOnly := range []string{"country", "type", "controller", "capabilities"} {
								if _, sent := params.Get(createOnly); sent {
									return fmt.Errorf("POST %s sent the create-only field %s", entry.Path, createOnly)
								}
							}
						}
						return nil
					},
				),
			},
			// Metadata keys are unset one by one.
			{
				Config: customConfig(acct, capabilities+fmt.Sprintf(`  email            = "owner@example.com"
  business_profile = { name = %q }
  metadata         = { run = %q }
`, name, acct.Name("update"))),
				Check: resource.TestCheckNoResourceAttr(address, "metadata.stage"),
			},
			// Removing email and the profile leaves them on the account: a plain string cleared
			// with an empty value is not something Stripe is proven to take.
			{
				Config: customConfig(acct, capabilities+fmt.Sprintf("  metadata = { run = %q }\n", acct.Name("update"))),
				ConfigPlanChecks: resource.ConfigPlanChecks{PreApply: []plancheck.PlanCheck{
					plancheck.ExpectEmptyPlan(),
				}},
				Check: resource.TestCheckResourceAttr(address, "email", "owner@example.com"),
			},
		},
	})
}

// Country, type and controller are create-only (suite: "update
// immutable fields"). Moving from a custom account to a controller-based one replaces it.
func TestAccAccount_replace(t *testing.T) {
	acct := acctest.NewAccount(t)
	var first string
	resource.ParallelTest(t, resource.TestCase{
		ProtoV6ProviderFactories: tfacc.ProviderFactories,
		Steps: []resource.TestStep{
			{Config: customConfig(acct, capabilities), Check: idOf(&first)},
			{
				Config: controllerConfig(acct, capabilities),
				ConfigPlanChecks: resource.ConfigPlanChecks{PreApply: []plancheck.PlanCheck{
					plancheck.ExpectResourceAction(address, plancheck.ResourceActionReplace),
				}},
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr(address, "type", "none"),
					resource.TestCheckResourceAttrWith(address, "id", func(v string) error {
						if v == first {
							return fmt.Errorf("the replacement kept the account ID %s", v)
						}
						return nil
					}),
				),
			},
			// A change of capabilities together with a create-only change is a replace, whose
			// create requests the new capabilities, so the refusal does not apply.
			{
				Config: customConfig(acct, "  capabilities = { card_payments = { requested = true }, transfers = { requested = true }, us_bank_account_ach_payments = { requested = true } }\n"),
				ConfigPlanChecks: resource.ConfigPlanChecks{PreApply: []plancheck.PlanCheck{
					plancheck.ExpectResourceAction(address, plancheck.ResourceActionReplace),
				}},
				Check: resource.TestCheckResourceAttr(address, "capabilities.us_bank_account_ach_payments.requested", "true"),
			},
		},
	})
}

// Nothing proves that an account update applies a capability request, so a plan that changes
// `capabilities` of an existing account fails, and ignoring the change is the way out. Neither
// touches the account.
func TestAccAccount_refuseCapabilityChange(t *testing.T) {
	acct := acctest.NewAccount(t)
	var first string
	more := "  capabilities = { card_payments = { requested = true }, transfers = { requested = true }, us_bank_account_ach_payments = { requested = true } }\n"
	resource.ParallelTest(t, resource.TestCase{
		ProtoV6ProviderFactories: tfacc.ProviderFactories,
		Steps: []resource.TestStep{
			{Config: customConfig(acct, capabilities), Check: idOf(&first)},
			{
				Config:      customConfig(acct, more),
				ExpectError: regexp.MustCompile(`(?s)not supported yet.*ignore_changes`),
			},
			{
				Config:      customConfig(acct, ""),
				ExpectError: regexp.MustCompile(`not supported yet`),
			},
			{
				Config: customConfig(acct, more+"  lifecycle {\n    ignore_changes = [capabilities]\n  }\n"),
				ConfigPlanChecks: resource.ConfigPlanChecks{PreApply: []plancheck.PlanCheck{
					plancheck.ExpectEmptyPlan(),
				}},
				Check: resource.TestCheckResourceAttrWith(address, "id", func(v string) error {
					if v != first {
						return fmt.Errorf("the account ID changed from %s to %s", first, v)
					}
					return nil
				}),
			},
		},
	})
}

// Stripe returns no capability requests, so an import leaves `capabilities` out of state
// and a configuration that sets it ignores changes to it.
func TestAccAccount_import(t *testing.T) {
	acct := acctest.NewAccount(t)
	extra := capabilities + fmt.Sprintf("  email    = \"owner@example.com\"\n  metadata = { run = %q }\n", acct.Name("import"))
	cfg := customConfig(acct, extra)
	imported := customConfig(acct, extra+"  lifecycle {\n    ignore_changes = [capabilities]\n  }\n")
	resource.ParallelTest(t, resource.TestCase{
		ProtoV6ProviderFactories: tfacc.ProviderFactories,
		Steps: []resource.TestStep{
			{Config: cfg},
			{
				ResourceName:            address,
				ImportState:             true,
				ImportStateVerify:       true,
				ImportStateVerifyIgnore: []string{"capabilities"},
			},
			{
				ResourceName:    address,
				ImportState:     true,
				ImportStateKind: resource.ImportBlockWithID,
				Config:          imported,
			},
			{
				ResourceName:  address,
				ImportState:   true,
				ImportStateId: "acct_1DoesNotExist0000",
				ExpectError:   regexp.MustCompile(`(?i)cannot import non-existent remote object`),
			},
		},
	})
}

// The identity form needs Terraform or OpenTofu 1.12.0 or later.
func TestAccAccount_importIdentity(t *testing.T) {
	acct := acctest.NewAccount(t)
	cfg := customConfig(acct, "  lifecycle {\n    ignore_changes = [capabilities]\n  }\n")
	resource.ParallelTest(t, resource.TestCase{
		ProtoV6ProviderFactories: tfacc.ProviderFactories,
		TerraformVersionChecks:   []tfversion.TerraformVersionCheck{tfversion.RequireAbove(version.Must(version.NewVersion("1.12.0")))},
		Steps: []resource.TestStep{
			{Config: cfg},
			{
				ResourceName:    address,
				ImportState:     true,
				ImportStateKind: resource.ImportBlockWithResourceIdentity,
				Config:          cfg,
			},
		},
	})
}

// Stripe deletes an account with DELETE, and a deleted account answers 403 `account_invalid`
// (suite: "delete").
func TestAccAccount_delete(t *testing.T) {
	acct := acctest.NewAccount(t)
	var id string
	resource.ParallelTest(t, resource.TestCase{
		ProtoV6ProviderFactories: tfacc.ProviderFactories,
		CheckDestroy: func(*terraform.State) error {
			_, err := tfacc.Client(acct).Do(t.Context(), stripeapi.Request{Method: "GET", Path: "/v1/accounts/" + id})
			if !isGone(err) {
				return fmt.Errorf("account %s should be deleted after destroy, GET gave %v", id, err)
			}
			return nil
		},
		Steps: []resource.TestStep{{Config: customConfig(acct, capabilities), Check: idOf(&id)}},
	})
}

func TestAccAccount_drift(t *testing.T) {
	acct := acctest.NewAccount(t)
	var id string
	cfg := customConfig(acct, capabilities+"  email = \"owner@example.com\"\n")
	resource.ParallelTest(t, resource.TestCase{
		ProtoV6ProviderFactories: tfacc.ProviderFactories,
		Steps: []resource.TestStep{
			{Config: cfg, Check: idOf(&id)},
			{
				PreConfig: func() {
					tfacc.Post(t, acct, "/v1/accounts/"+id, wire.Object{{Name: "email", Value: wire.String("changed@example.com")}})
				},
				Config: cfg,
				ConfigPlanChecks: resource.ConfigPlanChecks{PreApply: []plancheck.PlanCheck{
					plancheck.ExpectNonEmptyPlan(),
					plancheck.ExpectResourceAction(address, plancheck.ResourceActionUpdate),
				}},
				Check: resource.TestCheckResourceAttr(address, "email", "owner@example.com"),
			},
		},
	})
}

// Stripe answers 403 `account_invalid`, not 404, for an account deleted out of band, and
// refresh takes it for gone: the resource leaves the state and the plan creates it again.
func TestAccAccount_driftDeleted(t *testing.T) {
	acct := acctest.NewAccount(t)
	var id string
	cfg := customConfig(acct, capabilities)
	resource.ParallelTest(t, resource.TestCase{
		ProtoV6ProviderFactories: tfacc.ProviderFactories,
		Steps: []resource.TestStep{
			{Config: cfg, Check: idOf(&id)},
			{
				PreConfig: func() {
					if _, err := tfacc.Client(acct).Do(t.Context(), stripeapi.Request{Method: "DELETE", Path: "/v1/accounts/" + id}); err != nil {
						t.Fatal(err)
					}
				},
				Config: cfg,
				ConfigPlanChecks: resource.ConfigPlanChecks{PreApply: []plancheck.PlanCheck{
					plancheck.ExpectNonEmptyPlan(),
					plancheck.ExpectResourceAction(address, plancheck.ResourceActionCreate),
				}},
				Check: resource.TestCheckResourceAttrWith(address, "id", func(v string) error {
					if v == id {
						return fmt.Errorf("the account kept the deleted ID %s", v)
					}
					return nil
				}),
			},
		},
	})
}

// A data source for a deleted or unknown account says so.
func TestAccAccount_readMissing(t *testing.T) {
	acct := acctest.NewAccount(t)
	resource.ParallelTest(t, resource.TestCase{
		ProtoV6ProviderFactories: tfacc.ProviderFactories,
		Steps: []resource.TestStep{{
			Config: acct.ProviderBlock() + `
data "stripe_account" "test" {
  id = "acct_1DoesNotExist0000"
}
`,
			ExpectError: regexp.MustCompile(`no stripe_account found for id=acct_1DoesNotExist0000`),
		}},
	})
}
