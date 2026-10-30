package account_test

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"

	"github.com/hashicorp/go-version"
	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/plancheck"
	"github.com/hashicorp/terraform-plugin-testing/terraform"
	"github.com/hashicorp/terraform-plugin-testing/tfjsonpath"
	"github.com/hashicorp/terraform-plugin-testing/tfversion"

	"github.com/computomatic/terraform-provider-stripe/internal/acctest"
	"github.com/computomatic/terraform-provider-stripe/internal/acctest/tfacc"
	"github.com/computomatic/terraform-provider-stripe/internal/resources/account"
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
	cfg := customConfig(acct, capabilities+"  lifecycle {\n    ignore_changes = [capabilities]\n  }\n")
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

// onboarding holds the onboarding values a test varies between steps.
type onboarding struct {
	name, mcc, firstName, idNumber, issuingIP string
}

// verified is the onboarding Stripe documents as always verifying, which the suite sends
// ("verifiedIndividualForm", "issuingAccountForm"). Its business name goes through Name.
var verified = onboarding{name: "Rosen Software", mcc: "5734", firstName: "Jenny", idNumber: "000000000", issuingIP: "8.8.8.8"}

// descriptor is the statement descriptor Stripe fills in from a business name (suite: "How a
// business name becomes the account's descriptor settings"): the name trimmed, in capitals
// and cut to 22 characters. The names here have no apostrophe or accent, which Stripe changes
// too.
func descriptor(name string) string {
	d := strings.ToUpper(strings.TrimSpace(name))
	if len(d) > 22 {
		d = d[:22]
	}
	return d
}

// onboardedConfig is a US Custom individual account onboarded through the resource alone. It
// requests card_issuing, transfers and treasury and accepts the Stripe Services Agreement and
// the Issuing and Treasury terms. extra lands inside the account.
func onboardedConfig(c acctest.Config, o onboarding, extra string) string {
	return customConfig(c, fmt.Sprintf(`  business_type = "individual"
  email         = %[1]q
  capabilities = {
    card_issuing = { requested = true }
    transfers    = { requested = true }
    treasury     = { requested = true }
  }
  business_profile = {
    name                   = %[7]q
    mcc                    = %[2]q
    url                    = "https://accessible.stripe.com"
    product_description    = "Acceptance test onboarding"
    estimated_worker_count = 1
    annual_revenue         = { amount = 10000000, currency = "usd", fiscal_year_end = "2025-12-31" }
  }
  individual = {
    first_name   = %[3]q
    last_name    = "Rosen"
    email        = %[1]q
    phone        = "0000000000"
    id_number    = %[4]q
    ssn_last_4   = "0000"
    dob          = { day = 1, month = 1, year = 1901 }
    address      = { line1 = "address_full_match", city = "South San Francisco", state = "CA", postal_code = "94080" }
    verification = { document = { front = "file_identity_document_success" } }
  }
  external_account = "btok_us_verified"
  tos_acceptance   = { date = 1609459200, ip = "8.8.8.8" }
  settings = {
    card_issuing = { tos_acceptance = { date = 1609459200, ip = %[5]q } }
    treasury     = { tos_acceptance = { date = 1609459200, ip = "8.8.8.8" } }
  }
%[6]s`, c.Name("jenny")+"@example.com", o.mcc, o.firstName, o.idNumber, o.issuingIP, extra, c.Name(o.name)))
}

// capabilityData reads the status of each onboarded capability.
const capabilityData = `
data "stripe_account_capability" "card_issuing" {
  account = stripe_account.test.id
  id      = "card_issuing"
}

data "stripe_account_capability" "transfers" {
  account = stripe_account.test.id
  id      = "transfers"
}

data "stripe_account_capability" "treasury" {
  account = stripe_account.test.id
  id      = "treasury"
}
`

// An account created with the onboarding values through the resource alone has card_issuing,
// transfers and treasury active (suite: "Treasury reads on a US connected account (first
// run)", "A transfers-only connected account activates at once"); the fake activates them in
// the create. Stripe verifies card_issuing for minutes, so a smoke run checks the request
// only. The plan after apply is empty. The individual is read back, with what Stripe adds to
// it and to `company`, but for the ID number and SSN, which Stripe never returns, and the
// phone and the document, which state keeps as configured while a data source shows Stripe's
// form. An import reads everything back that Stripe returns.
func TestAccAccount_onboard(t *testing.T) {
	acct := acctest.NewAccount(t)
	cfg := onboardedConfig(acct, verified, "")
	active := func(*terraform.State) error { return nil }
	if !acct.Smoke {
		active = resource.ComposeAggregateTestCheckFunc(
			resource.TestCheckResourceAttr("data.stripe_account_capability.card_issuing", "status", "active"),
			resource.TestCheckResourceAttr("data.stripe_account_capability.transfers", "status", "active"),
			resource.TestCheckResourceAttr("data.stripe_account_capability.treasury", "status", "active"),
		)
	}
	resource.ParallelTest(t, resource.TestCase{
		ProtoV6ProviderFactories: tfacc.ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: cfg + capabilityData + `
data "stripe_account" "read" {
  id = stripe_account.test.id
}
`,
				ConfigPlanChecks: resource.ConfigPlanChecks{PostApplyPostRefresh: []plancheck.PlanCheck{
					plancheck.ExpectEmptyPlan(),
				}},
				Check: resource.ComposeAggregateTestCheckFunc(
					active,
					resource.TestCheckResourceAttr("data.stripe_account_capability.card_issuing", "requested", "true"),
					resource.TestCheckResourceAttr("data.stripe_account_capability.treasury", "requested", "true"),
					resource.TestCheckResourceAttr(address, "business_type", "individual"),
					resource.TestCheckResourceAttr(address, "business_profile.mcc", "5734"),
					resource.TestCheckResourceAttr(address, "business_profile.annual_revenue.currency", "usd"),
					resource.TestCheckResourceAttr(address, "business_profile.estimated_worker_count", "1"),
					resource.TestCheckResourceAttr(address, "tos_acceptance.date", "1609459200"),
					resource.TestCheckResourceAttr(address, "tos_acceptance.ip", "8.8.8.8"),
					resource.TestCheckResourceAttr(address, "settings.card_issuing.tos_acceptance.ip", "8.8.8.8"),
					resource.TestCheckResourceAttr(address, "settings.treasury.tos_acceptance.date", "1609459200"),
					// Stripe fills these in from the business name.
					resource.TestCheckResourceAttr(address, "settings.dashboard.display_name", acct.Name(verified.name)),
					resource.TestCheckResourceAttr(address, "settings.payments.statement_descriptor", descriptor(acct.Name(verified.name))),
					resource.TestCheckResourceAttr(address, "individual.first_name", "Jenny"),
					resource.TestCheckResourceAttr(address, "individual.id_number", "000000000"),
					resource.TestCheckResourceAttr(address, "individual.phone", "0000000000"),
					resource.TestCheckResourceAttr(address, "individual.address.country", "US"),
					resource.TestCheckResourceAttr(address, "company.address.country", "US"),
					resource.TestCheckResourceAttr(address, "company.phone", "+10000000000"),
					resource.TestCheckResourceAttr(address, "external_account", "btok_us_verified"),
					resource.TestCheckResourceAttr("data.stripe_account.read", "individual.first_name", "Jenny"),
					resource.TestCheckResourceAttr("data.stripe_account.read", "individual.phone", "+10000000000"),
					resource.TestCheckNoResourceAttr("data.stripe_account.read", "individual.id_number"),
				),
			},
			{
				ResourceName:            address,
				ImportState:             true,
				ImportStateVerify:       true,
				ImportStateVerifyIgnore: []string{"capabilities", "external_account", "individual.id_number", "individual.ssn_last_4", "individual.phone", "individual.verification.document.front"},
			},
			// The `ignore_changes` of the import note leaves no difference to plan.
			{
				ResourceName:    address,
				ImportState:     true,
				ImportStateKind: resource.ImportBlockWithID,
				Config:          onboardedConfig(acct, verified, "  lifecycle {\n    ignore_changes = [capabilities, external_account, individual.id_number, individual.ssn_last_4, individual.phone, individual.verification.document.front]\n  }\n"),
			},
		},
	})
}

// A company's name is read back, with what Stripe adds to `company`, an import recovers it,
// and a name changed outside Terraform plans an update back to the configuration.
func TestAccAccount_companyReadBack(t *testing.T) {
	acct := acctest.NewAccount(t)
	var id string
	cfg := customConfig(acct, capabilities+fmt.Sprintf("  business_type = \"company\"\n  company = {\n    name = %q\n  }\n", acct.Name("Read Back Inc")))
	// Stripe moves these with the name, which the fake does not show. The fake also holds no
	// business profile to leave the name unknown in.
	renamed := []plancheck.PlanCheck{
		plancheck.ExpectResourceAction(address, plancheck.ResourceActionUpdate),
		plancheck.ExpectUnknownValue(address, tfjsonpath.New("settings")),
	}
	if acct.Smoke {
		renamed = append(renamed, plancheck.ExpectUnknownValue(address, tfjsonpath.New("business_profile").AtMapKey("name")))
	}
	resource.ParallelTest(t, resource.TestCase{
		ProtoV6ProviderFactories: tfacc.ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: cfg,
				ConfigPlanChecks: resource.ConfigPlanChecks{PostApplyPostRefresh: []plancheck.PlanCheck{
					plancheck.ExpectEmptyPlan(),
				}},
				Check: resource.ComposeAggregateTestCheckFunc(
					idOf(&id),
					resource.TestCheckResourceAttr(address, "company.name", acct.Name("Read Back Inc")),
					resource.TestCheckResourceAttr(address, "company.address.country", "US"),
					resource.TestCheckResourceAttr(address, "company.owners_provided", "false"),
				),
			},
			{
				ResourceName:            address,
				ImportState:             true,
				ImportStateVerify:       true,
				ImportStateVerifyIgnore: []string{"capabilities"},
			},
			{
				PreConfig: func() {
					tfacc.Post(t, acct, "/v1/accounts/"+id, wire.Object{{Name: "company", Value: wire.Object{{Name: "name", Value: wire.String(acct.Name("Renamed Inc"))}}}})
				},
				Config:           cfg,
				ConfigPlanChecks: resource.ConfigPlanChecks{PreApply: renamed},
				Check:            resource.TestCheckResourceAttr(address, "company.name", acct.Name("Read Back Inc")),
			},
		},
	})
}

// Stripe replaces a test token with a `file_...` ID of its own, and the record's uploaded files
// return as sent, so state keeps the configured value of a document reference: another value
// from Stripe plans nothing. The fake stores a token as sent, so it gets the other value from a
// request, which the dev sandbox needs no help for.
func TestAccAccount_documentKeepsConfiguredValue(t *testing.T) {
	acct := acctest.NewAccount(t)
	var id string
	cfg := customConfig(acct, capabilities+`  business_type = "individual"
  individual = {
    first_name   = "Jenny"
    verification = { document = { front = "file_identity_document_success" } }
  }
`)
	resource.ParallelTest(t, resource.TestCase{
		ProtoV6ProviderFactories: tfacc.ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: cfg,
				ConfigPlanChecks: resource.ConfigPlanChecks{PostApplyPostRefresh: []plancheck.PlanCheck{
					plancheck.ExpectEmptyPlan(),
				}},
				Check: resource.ComposeAggregateTestCheckFunc(
					idOf(&id),
					resource.TestCheckResourceAttr(address, "individual.verification.document.front", "file_identity_document_success"),
				),
			},
			{
				PreConfig: func() {
					if acct.Smoke {
						return
					}
					tfacc.Post(t, acct, "/v1/accounts/"+id, wire.Object{{Name: "individual", Value: wire.Object{{Name: "verification", Value: wire.Object{
						{Name: "document", Value: wire.Object{{Name: "front", Value: wire.String("file_stored_by_stripe")}}},
					}}}}})
				},
				Config: cfg,
				ConfigPlanChecks: resource.ConfigPlanChecks{PreApply: []plancheck.PlanCheck{
					plancheck.ExpectEmptyPlan(),
				}},
				Check: resource.TestCheckResourceAttr(address, "individual.verification.document.front", "file_identity_document_success"),
			},
		},
	})
}

// An individual changed outside Terraform plans an update back to the configuration. A phone
// number, which state keeps as configured, does not.
func TestAccAccount_individualDrift(t *testing.T) {
	acct := acctest.NewAccount(t)
	var id string
	cfg := customConfig(acct, capabilities+`  business_type = "individual"
  individual = {
    first_name = "Jenny"
    last_name  = "Rosen"
    phone      = "0000000000"
  }
`)
	individual := func(name, value string) wire.Object {
		return wire.Object{{Name: "individual", Value: wire.Object{{Name: name, Value: wire.String(value)}}}}
	}
	resource.ParallelTest(t, resource.TestCase{
		ProtoV6ProviderFactories: tfacc.ProviderFactories,
		Steps: []resource.TestStep{
			{Config: cfg, Check: idOf(&id)},
			{
				PreConfig: func() { tfacc.Post(t, acct, "/v1/accounts/"+id, individual("phone", "+15555550100")) },
				Config:    cfg,
				ConfigPlanChecks: resource.ConfigPlanChecks{PreApply: []plancheck.PlanCheck{
					plancheck.ExpectEmptyPlan(),
				}},
			},
			{
				PreConfig: func() { tfacc.Post(t, acct, "/v1/accounts/"+id, individual("last_name", "Changed")) },
				Config:    cfg,
				ConfigPlanChecks: resource.ConfigPlanChecks{PreApply: []plancheck.PlanCheck{
					plancheck.ExpectResourceAction(address, plancheck.ResourceActionUpdate),
				}},
				Check: resource.TestCheckResourceAttr(address, "individual.last_name", "Rosen"),
			},
		},
	})
}

// The members of `individual` change in place, and the update sends only what changed: a date
// of birth whole, as Stripe takes it, never the values Stripe filled in, and a member removed
// from the configuration as an empty value, which clears it.
func TestAccAccount_individualUpdate(t *testing.T) {
	acct := acctest.NewAccount(t)
	var id string
	cfg := func(members string) string {
		return customConfig(acct, capabilities+"  business_type = \"individual\"\n  individual = {\n"+members+"  }\n")
	}
	// sent checks what the newest update sent of `individual`.
	sent := func(members wire.Object) resource.TestCheckFunc {
		return func(*terraform.State) error {
			if acct.Smoke {
				return nil
			}
			for _, entry := range acctest.RequestLog(t, acct) {
				if entry.Method == "POST" && entry.Path == "/v1/accounts/"+id {
					if got, want := fmt.Sprint(entry.Params), fmt.Sprint(wire.Object{{Name: "individual", Value: members}}); got != want {
						return fmt.Errorf("the update sent %s, want %s", got, want)
					}
					return nil
				}
			}
			return fmt.Errorf("no update of %s", id)
		}
	}
	empty := resource.ConfigPlanChecks{PostApplyPostRefresh: []plancheck.PlanCheck{plancheck.ExpectEmptyPlan()}}
	resource.ParallelTest(t, resource.TestCase{
		ProtoV6ProviderFactories: tfacc.ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: cfg("    first_name = \"Jenny\"\n    last_name = \"Rosen\"\n    dob = { day = 1, month = 1, year = 1901 }\n"),
				Check:  idOf(&id),
			},
			{
				Config: cfg("    first_name = \"Jane\"\n    last_name = \"Rosen\"\n    dob = { day = 2, month = 1, year = 1901 }\n"),
				ConfigPlanChecks: resource.ConfigPlanChecks{
					// Stripe moves its descriptors with the name, which the fake does not show.
					PreApply:             []plancheck.PlanCheck{plancheck.ExpectUnknownValue(address, tfjsonpath.New("settings"))},
					PostApplyPostRefresh: []plancheck.PlanCheck{plancheck.ExpectEmptyPlan()},
				},
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttrPtr(address, "id", &id),
					resource.TestCheckResourceAttr(address, "individual.first_name", "Jane"),
					resource.TestCheckResourceAttr(address, "individual.dob.day", "2"),
					resource.TestCheckResourceAttr(address, "individual.last_name", "Rosen"),
					sent(wire.Object{
						{Name: "dob", Value: wire.Object{{Name: "day", Value: wire.String("2")}, {Name: "month", Value: wire.String("1")}, {Name: "year", Value: wire.String("1901")}}},
						{Name: "first_name", Value: wire.String("Jane")},
					}),
				),
			},
			{
				Config:           cfg("    first_name = \"Jane\"\n    dob = { day = 2, month = 1, year = 1901 }\n"),
				ConfigPlanChecks: empty,
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckNoResourceAttr(address, "individual.last_name"),
					sent(wire.Object{{Name: "last_name", Value: wire.String("")}}),
				),
			},
		},
	})
}

// On an individual account with no `company` in the configuration, Stripe copies the individual's
// address and phone into `company`, and the copy follows the individual's (the record's
// `filled_from`).
func TestAccAccount_companyCopiesIndividual(t *testing.T) {
	acct := acctest.NewAccount(t)
	cfg := func(city string) string {
		return customConfig(acct, capabilities+fmt.Sprintf(`  business_type = "individual"
  individual = {
    first_name = "Jenny"
    phone      = "0000000000"
    address    = { line1 = "1 Home St", city = %q, state = "CA", postal_code = "94080" }
  }
`, city))
	}
	empty := resource.ConfigPlanChecks{PostApplyPostRefresh: []plancheck.PlanCheck{plancheck.ExpectEmptyPlan()}}
	resource.ParallelTest(t, resource.TestCase{
		ProtoV6ProviderFactories: tfacc.ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config:           cfg("South San Francisco"),
				ConfigPlanChecks: empty,
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr(address, "company.address.line1", "1 Home St"),
					resource.TestCheckResourceAttr(address, "company.address.city", "South San Francisco"),
					resource.TestCheckResourceAttr(address, "company.phone", "+10000000000"),
				),
			},
			{
				Config:           cfg("Daly City"),
				ConfigPlanChecks: empty,
				Check:            resource.TestCheckResourceAttr(address, "company.address.city", "Daly City"),
			},
		},
	})
}

// The suite never sends `company` beside `individual`, so nothing records what Stripe does with
// the rest of an address a request sends in part: the dev sandbox leaves it null and the fake
// copies the individual's. Either way the configured part stays, an unchanged configuration
// plans nothing, and a change of the individual is one update that leaves nothing to plan.
func TestAccAccount_companyAddressBesideIndividual(t *testing.T) {
	acct := acctest.NewAccount(t)
	var id string
	cfg := func(city string) string {
		return customConfig(acct, capabilities+fmt.Sprintf(`  business_type = "individual"
  individual = {
    first_name = "Jenny"
    address    = { line1 = "1 Home St", city = %q, state = "CA", postal_code = "94080" }
  }
  company = {
    address = { line1 = "2 Office St" }
  }
`, city))
	}
	empty := resource.ConfigPlanChecks{PostApplyPostRefresh: []plancheck.PlanCheck{plancheck.ExpectEmptyPlan()}}
	resource.ParallelTest(t, resource.TestCase{
		ProtoV6ProviderFactories: tfacc.ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config:           cfg("South San Francisco"),
				ConfigPlanChecks: empty,
				Check: resource.ComposeAggregateTestCheckFunc(
					idOf(&id),
					resource.TestCheckResourceAttr(address, "company.address.line1", "2 Office St"),
				),
			},
			{
				Config: cfg("Daly City"),
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{
						plancheck.ExpectResourceAction(address, plancheck.ResourceActionUpdate),
						plancheck.ExpectUnknownValue(address, tfjsonpath.New("company").AtMapKey("address").AtMapKey("city")),
					},
					PostApplyPostRefresh: []plancheck.PlanCheck{plancheck.ExpectEmptyPlan()},
				},
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttrPtr(address, "id", &id),
					resource.TestCheckResourceAttr(address, "company.address.line1", "2 Office St"),
				),
			},
		},
	})
}

// Every member of `individual` and `company` changes in place where the pinned evidence verifies
// its update, and is refused where it does not.
func TestRefusedMembersAreThoseWithNoVerifiedUpdate(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join("..", "..", "generated", "coverage.json"))
	if err != nil {
		t.Fatal(err)
	}
	var coverage struct {
		Leaves []struct {
			Table, Side, Operation, Path, State, Class string
		} `json:"leaves"`
	}
	if err := json.Unmarshal(raw, &coverage); err != nil {
		t.Fatal(err)
	}
	var unverified []string
	for _, l := range coverage.Leaves {
		if l.Table == "account" && l.Operation == "PostAccountsAccount" && l.State == "exposed" && l.Class != "verified" &&
			(strings.HasPrefix(l.Path, "individual.") || strings.HasPrefix(l.Path, "company.")) {
			unverified = append(unverified, l.Path)
		}
	}
	slices.Sort(unverified)
	var refused []string
	for _, name := range account.Declaration.RefuseChange {
		if strings.HasPrefix(name, "individual") || strings.HasPrefix(name, "company") {
			refused = append(refused, name)
		}
	}
	slices.Sort(refused)
	if !slices.Equal(refused, unverified) {
		t.Errorf("RefuseChange holds %v of `individual` and `company`, want the members whose update is not verified: %v", refused, unverified)
	}
}

// The onboarding values are proven on create only, and replacing an account deletes it, so a
// plan that changes one fails, field by field: the profile's MCC beside its updatable name and
// the Issuing terms. A change to the name alone is an update.
func TestAccAccount_refuseOnboardingChange(t *testing.T) {
	acct := acctest.NewAccount(t)
	var first string
	otherMCC, otherIP, renamed := verified, verified, verified
	otherMCC.mcc, otherIP.issuingIP, renamed.name = "5045", "8.8.4.4", "Renamed"
	resource.ParallelTest(t, resource.TestCase{
		ProtoV6ProviderFactories: tfacc.ProviderFactories,
		Steps: []resource.TestStep{
			{Config: onboardedConfig(acct, verified, ""), Check: idOf(&first)},
			{
				Config:      onboardedConfig(acct, otherMCC, ""),
				ExpectError: regexp.MustCompile(refused("business_profile.mcc") + `(?s).*ignore_changes`),
			},
			{
				Config:      onboardedConfig(acct, otherIP, ""),
				ExpectError: regexp.MustCompile(refused("settings.card_issuing.tos_acceptance")),
			},
			// Removing the onboarding arguments is a change of those Stripe does not return.
			// Stripe holds the rest, such as `individual`, so removing them changes nothing.
			{
				Config:      customConfig(acct, "  lifecycle {\n    ignore_changes = [capabilities]\n  }\n"),
				ExpectError: regexp.MustCompile(refused("external_account")),
			},
			{
				Config: onboardedConfig(acct, otherMCC, "  lifecycle {\n    ignore_changes = [business_profile.mcc]\n  }\n"),
				ConfigPlanChecks: resource.ConfigPlanChecks{PreApply: []plancheck.PlanCheck{
					plancheck.ExpectEmptyPlan(),
				}},
			},
			{
				Config: onboardedConfig(acct, renamed, ""),
				ConfigPlanChecks: resource.ConfigPlanChecks{PreApply: []plancheck.PlanCheck{
					plancheck.ExpectResourceAction(address, plancheck.ResourceActionUpdate),
					// The configuration sets other `settings`, so only what it leaves unset is Stripe's.
					plancheck.ExpectUnknownValue(address, tfjsonpath.New("settings").AtMapKey("payments")),
				}},
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr(address, "business_profile.name", acct.Name(renamed.name)),
					resource.TestCheckResourceAttrWith(address, "id", func(v string) error {
						if v != first {
							return fmt.Errorf("the account ID changed from %s to %s", first, v)
						}
						return nil
					}),
					// The update sends the name alone, never the refused profile fields beside it.
					func(*terraform.State) error {
						if acct.Smoke {
							return nil
						}
						var updates int
						for _, entry := range acctest.RequestLog(t, acct) {
							if entry.Method != "POST" || entry.Path != "/v1/accounts/"+first {
								continue
							}
							updates++
							if got, want := fmt.Sprint(entry.Params), fmt.Sprint(wire.Object{{Name: "business_profile", Value: wire.Object{{Name: "name", Value: wire.String(renamed.name)}}}}); got != want {
								return fmt.Errorf("the update sent %s, want only business_profile[name]: %s", got, want)
							}
						}
						if updates != 1 {
							return fmt.Errorf("%d updates of %s, want 1", updates, first)
						}
						return nil
					},
				),
			},
		},
	})
}

// Stripe never returns the individual's ID number or SSN, so state keeps the configured
// values: they are sensitive in the plan, the plan after apply is empty, and a refused change
// to one says which attribute without its value.
func TestAccAccount_onboardingSensitive(t *testing.T) {
	acct := acctest.NewAccount(t)
	otherID := verified
	otherID.idNumber = "111111111"
	resource.ParallelTest(t, resource.TestCase{
		ProtoV6ProviderFactories: tfacc.ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: onboardedConfig(acct, verified, ""),
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{
						plancheck.ExpectSensitiveValue(address, tfjsonpath.New("individual").AtMapKey("id_number")),
						plancheck.ExpectSensitiveValue(address, tfjsonpath.New("individual").AtMapKey("ssn_last_4")),
					},
					PostApplyPostRefresh: []plancheck.PlanCheck{plancheck.ExpectEmptyPlan()},
				},
			},
			{
				Config:      onboardedConfig(acct, otherID, ""),
				ExpectError: regexp.MustCompile(refused("individual.id_number")),
			},
		},
	})
}

// refused matches the plan error for a change to the attribute at path, which Terraform may
// wrap across lines.
func refused(path string) string {
	return "Changing\\s+`" + regexp.QuoteMeta(path) + "`\\s+is\\s+not\\s+supported\\s+yet"
}
