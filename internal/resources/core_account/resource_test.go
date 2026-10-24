package core_account_test

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

func TestMain(m *testing.M) { os.Exit(acctest.Main(m)) }

const (
	address  = "stripe_core_account.test"
	basePath = "/v2/core/accounts"
)

// The configurations the suite creates, each requesting its one capability.
const (
	customer  = "    customer  = { capabilities = { automatic_indirect_tax = { requested = true } } }\n"
	merchant  = "    merchant  = { capabilities = { card_payments = { requested = true } } }\n"
	recipient = "    recipient = { capabilities = { stripe_balance = { stripe_transfers = { requested = true } } } }\n"
)

// managed is what a merchant or recipient needs (suite: newV2AccountBody): no Stripe
// dashboard, and the platform collecting fees and covering losses.
const managed = `  dashboard = "none"
  defaults = {
    currency         = "usd"
    responsibilities = { fees_collector = "application", losses_collector = "application" }
  }
`

// details are the account's fields the update changes.
type details struct {
	display, registered, email, metadata string
}

func created(c acctest.Config) details {
	return details{
		display:    fmt.Sprintf("display_name  = %q\n", c.Name("Acme Supplies")),
		registered: c.Name("Acme Supplies Inc."),
		email:      "payments@example.com",
		metadata:   fmt.Sprintf("{ run = %q, stage = \"created\" }", c.Name("v2 account")),
	}
}

// accountConfig is a US company with the given configurations, the country in lower case as
// the suite sends it. body holds the dashboard and defaults, and anything else.
func accountConfig(c acctest.Config, d details, configurations, body string) string {
	return c.ProviderBlock() + fmt.Sprintf(`
resource "stripe_core_account" "test" {
  %s  contact_email = %q
  identity = {
    country          = "us"
    entity_type      = "company"
    business_details = { registered_name = %q }
  }
  configuration = {
%s  }
  metadata = %s
%s}
`, d.display, d.email, d.registered, configurations, d.metadata, body)
}

func merchantConfig(c acctest.Config, extra string) string {
	return accountConfig(c, created(c), customer+merchant, managed+extra)
}

// pathOf records the account's path.
func pathOf(dst *string) resource.TestCheckFunc {
	return func(s *terraform.State) error {
		rs, ok := s.RootModule().Resources[address]
		if !ok {
			return fmt.Errorf("%s is not in state", address)
		}
		*dst = basePath + "/" + rs.Primary.ID
		return nil
	}
}

func jsonIs(v wire.Value, want string) bool {
	if v == nil {
		return false
	}
	got, err := wire.EncodeJSON(v)
	return err == nil && string(got) == want
}

// everyInclude is the include list the engine sends: every includable field of a v2 account.
const everyInclude = `["configuration.customer","configuration.merchant","configuration.recipient","defaults","future_requirements","identity","requirements"]`

// Stripe returns every includable field, but null unless included (suite: "retrieve with
// include"), so the engine includes them all on create and read and state holds each applied
// configuration. The country comes back in upper case and state keeps the configured one; the
// data source shows Stripe's. The account is also a v1 account (suite: "v2 account is a v1
// account").
func TestAccCoreAccount_create(t *testing.T) {
	acct := acctest.NewAccount(t)
	var path string
	resource.ParallelTest(t, resource.TestCase{
		ProtoV6ProviderFactories: tfacc.ProviderFactories,
		Steps: acctest.Forget(t, []resource.TestStep{{
			Config: merchantConfig(acct, "") + `
data "stripe_core_account" "test" {
  id = stripe_core_account.test.id
}

data "stripe_account" "v1" {
  id = stripe_core_account.test.id
}
`,
			Check: resource.ComposeAggregateTestCheckFunc(
				pathOf(&path),
				resource.TestMatchResourceAttr(address, "id", regexp.MustCompile(`^acct_`)),
				resource.TestMatchResourceAttr(address, "created", regexp.MustCompile(`^\d{4}-\d\d-\d\dT`)),
				resource.TestCheckResourceAttr(address, "closed", "false"),
				resource.TestCheckResourceAttr(address, "applied_configurations.#", "2"),
				resource.TestCheckResourceAttr(address, "applied_configurations.0", "customer"),
				resource.TestCheckResourceAttr(address, "applied_configurations.1", "merchant"),
				resource.TestCheckResourceAttr(address, "dashboard", "none"),
				resource.TestCheckResourceAttr(address, "identity.country", "us"),
				resource.TestCheckResourceAttr(address, "identity.entity_type", "company"),
				resource.TestCheckResourceAttr(address, "configuration.customer.applied", "true"),
				resource.TestCheckResourceAttr(address, "configuration.customer.capabilities.automatic_indirect_tax.requested", "true"),
				resource.TestCheckResourceAttrSet(address, "configuration.customer.capabilities.automatic_indirect_tax.status"),
				resource.TestCheckResourceAttr(address, "configuration.merchant.applied", "true"),
				resource.TestCheckResourceAttrSet(address, "configuration.merchant.capabilities.card_payments.status"),
				resource.TestCheckResourceAttrSet(address, "configuration.merchant.capabilities.stripe_balance.payouts.status"),
				resource.TestCheckNoResourceAttr(address, "configuration.recipient"),
				resource.TestCheckResourceAttr(address, "defaults.currency", "usd"),
				resource.TestCheckResourceAttr(address, "defaults.responsibilities.requirements_collector", "application"),
				resource.TestCheckResourceAttr(address, "metadata.run", acct.Name("v2 account")),
				resource.TestCheckResourceAttr(address, "display_name", acct.Name("Acme Supplies")),
				resource.TestCheckResourceAttrPair("data.stripe_core_account.test", "id", address, "id"),
				resource.TestCheckResourceAttr("data.stripe_core_account.test", "identity.country", "US"),
				resource.TestCheckResourceAttr("data.stripe_core_account.test", "configuration.merchant.applied", "true"),
				resource.TestCheckResourceAttr("data.stripe_core_account.test", "defaults.currency", "usd"),
				resource.TestCheckNoResourceAttr("data.stripe_core_account.test", "configuration.merchant.capabilities.card_payments.requested"),
				resource.TestCheckResourceAttr("data.stripe_account.v1", "country", "US"),
				func(*terraform.State) error {
					if acct.Smoke {
						return nil
					}
					var sawCreate, sawRead bool
					for _, entry := range acctest.RequestLog(t, acct) {
						params, _ := entry.Params.(wire.Object)
						include, _ := params.Get("include")
						switch {
						case entry.Method == "POST" && entry.Path == basePath:
							sawCreate = jsonIs(include, everyInclude)
						case entry.Method == "GET" && entry.Path == path:
							sawRead = sawRead || jsonIs(include, everyInclude)
						}
					}
					if !sawCreate || !sawRead {
						return fmt.Errorf("the create (%v) and a read (%v) must include every includable field", sawCreate, sawRead)
					}
					return nil
				},
			),
		}}, nil, "stripe_core_account"),
	})
}

// A recipient requests `stripe_balance.stripe_transfers`, and Stripe adds its payouts (suite:
// "create recipient").
func TestAccCoreAccount_recipient(t *testing.T) {
	acct := acctest.NewAccount(t)
	resource.ParallelTest(t, resource.TestCase{
		ProtoV6ProviderFactories: tfacc.ProviderFactories,
		Steps: acctest.Forget(t, []resource.TestStep{{
			Config: accountConfig(acct, created(acct), recipient, managed),
			Check: resource.ComposeAggregateTestCheckFunc(
				resource.TestCheckResourceAttr(address, "applied_configurations.#", "1"),
				resource.TestCheckResourceAttr(address, "applied_configurations.0", "recipient"),
				resource.TestCheckResourceAttr(address, "configuration.recipient.capabilities.stripe_balance.stripe_transfers.requested", "true"),
				resource.TestCheckResourceAttrSet(address, "configuration.recipient.capabilities.stripe_balance.stripe_transfers.status"),
				resource.TestCheckResourceAttrSet(address, "configuration.recipient.capabilities.stripe_balance.payouts.status"),
				resource.TestCheckNoResourceAttr(address, "configuration.merchant"),
			),
		}}, nil, "stripe_core_account"),
	})
}

// An account that is only a customer has no dashboard (suite: "create customer only"), and
// Stripe refuses a dashboard on one (suite: "dashboard on a customer only account"). Stripe
// fills its `defaults.responsibilities`, which the fake leaves out.
func TestAccCoreAccount_customerOnly(t *testing.T) {
	acct := acctest.NewAccount(t)
	resource.ParallelTest(t, resource.TestCase{
		ProtoV6ProviderFactories: tfacc.ProviderFactories,
		Steps: acctest.Forget(t, []resource.TestStep{
			{
				Config:      accountConfig(acct, created(acct), customer, "  dashboard = \"none\"\n  defaults  = { currency = \"usd\" }\n"),
				ExpectError: regexp.MustCompile("(?s)unsupported_field_for_configs.*You cannot set `dashboard` unless the account is configured as a merchant"),
			},
			{
				Config: accountConfig(acct, created(acct), customer, "  defaults = { currency = \"usd\" }\n"),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr(address, "applied_configurations.#", "1"),
					resource.TestCheckNoResourceAttr(address, "dashboard"),
				),
			},
		}, nil, "stripe_core_account"),
	})
}

// Stripe updates the display name, the contact email and the registered name and merges the
// identity and the metadata, removing a key sent as null (suite: "update", "update unsets
// metadata key with null"). The update never sends the create-only identity fields, the
// configurations, the dashboard or the defaults. Removing the display name fails, because
// nothing proves Stripe clears it with null.
func TestAccCoreAccount_update(t *testing.T) {
	acct := acctest.NewAccount(t)
	var path string
	renamed := details{
		display:    fmt.Sprintf("display_name  = %q\n", acct.Name("Acme Renamed")),
		registered: acct.Name("Acme Renamed Inc."),
		email:      "billing@example.com",
		metadata:   fmt.Sprintf("{ run = %q }", acct.Name("v2 account")),
	}
	cleared := renamed
	cleared.display = ""
	resource.ParallelTest(t, resource.TestCase{
		ProtoV6ProviderFactories: tfacc.ProviderFactories,
		Steps: acctest.Forget(t, []resource.TestStep{
			{Config: merchantConfig(acct, ""), Check: pathOf(&path)},
			{
				Config: accountConfig(acct, renamed, customer+merchant, managed),
				ConfigPlanChecks: resource.ConfigPlanChecks{PreApply: []plancheck.PlanCheck{
					plancheck.ExpectResourceAction(address, plancheck.ResourceActionUpdate),
				}},
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr(address, "display_name", acct.Name("Acme Renamed")),
					resource.TestCheckResourceAttr(address, "contact_email", "billing@example.com"),
					resource.TestCheckResourceAttr(address, "identity.business_details.registered_name", acct.Name("Acme Renamed Inc.")),
					resource.TestCheckResourceAttr(address, "identity.entity_type", "company"),
					resource.TestCheckResourceAttr(address, "identity.country", "us"),
					resource.TestCheckNoResourceAttr(address, "metadata.stage"),
					resource.TestCheckResourceAttr(address, "metadata.run", acct.Name("v2 account")),
					resource.TestCheckResourceAttr(address, "configuration.merchant.applied", "true"),
					func(*terraform.State) error {
						if acct.Smoke {
							return nil
						}
						var updates []wire.Object
						for _, entry := range acctest.RequestLog(t, acct) {
							if params, ok := entry.Params.(wire.Object); ok && entry.Method == "POST" && entry.Path == path {
								updates = append(updates, params)
							}
						}
						if len(updates) != 1 {
							return fmt.Errorf("want one update, got %d", len(updates))
						}
						u := updates[0]
						for _, never := range []string{"configuration", "dashboard", "defaults"} {
							if _, sent := u.Get(never); sent {
								return fmt.Errorf("the update sent %s: %+v", never, u)
							}
						}
						if identity, _ := u.Get("identity"); !jsonIs(identity, `{"business_details":{"registered_name":"`+acct.Name("Acme Renamed Inc.")+`"}}`) {
							return fmt.Errorf("the update must send only the identity's registered name: %+v", identity)
						}
						if meta, _ := u.Get("metadata"); !jsonIs(meta, `{"stage":null}`) {
							return fmt.Errorf("the removed metadata key must be sent as null: %+v", meta)
						}
						if include, _ := u.Get("include"); !jsonIs(include, everyInclude) {
							return fmt.Errorf("the update must include every includable field: %+v", include)
						}
						return nil
					},
				),
			},
			{
				Config:      accountConfig(acct, cleared, customer+merchant, managed),
				ExpectError: regexp.MustCompile("clearing `display_name` is not supported"),
			},
		}, nil, "stripe_core_account"),
	})
}

// A change to the configurations, the dashboard or the defaults is refused at plan time,
// because nothing proves Stripe applies one on update. The error says to remove the account
// from the state, because its destroy is refused too.
func TestAccCoreAccount_refuseChange(t *testing.T) {
	acct := acctest.NewAccount(t)
	resource.ParallelTest(t, resource.TestCase{
		ProtoV6ProviderFactories: tfacc.ProviderFactories,
		Steps: acctest.Forget(t, []resource.TestStep{
			{Config: merchantConfig(acct, "")},
			{
				Config:      accountConfig(acct, created(acct), customer+merchant+recipient, managed),
				PlanOnly:    true,
				ExpectError: regexp.MustCompile("(?s)Changing `configuration` is not supported yet.*removed block"),
			},
		}, nil, "stripe_core_account"),
	})
}

// The identity's country and entity type are create-only, and Stripe cannot delete the
// account, so a change to one fails the plan, and a taint fails at apply.
func TestAccCoreAccount_replace(t *testing.T) {
	acct := acctest.NewAccount(t)
	cfg := merchantConfig(acct, "")
	resource.ParallelTest(t, resource.TestCase{
		ProtoV6ProviderFactories: tfacc.ProviderFactories,
		Steps: acctest.Forget(t, []resource.TestStep{
			{Config: cfg},
			{Config: regexp.MustCompile(`"company"`).ReplaceAllString(cfg, `"non_profit"`), PlanOnly: true, ExpectError: tfacc.RefusedReplace},
			{Config: cfg, Taint: []string{address}, ExpectError: tfacc.RefusedDestroy},
		}, nil, "stripe_core_account"),
	})
}

// Stripe does not return the requested capabilities, and returns the country in upper case,
// so an import has no `requested` flags and Stripe's country, and a configuration ignores
// both (see the import note).
func TestAccCoreAccount_import(t *testing.T) {
	acct := acctest.NewAccount(t)
	cfg := merchantConfig(acct, "")
	imported := merchantConfig(acct, "  lifecycle {\n    ignore_changes = [configuration, identity.country]\n  }\n")
	resource.ParallelTest(t, resource.TestCase{
		ProtoV6ProviderFactories: tfacc.ProviderFactories,
		Steps: acctest.Forget(t, []resource.TestStep{
			{Config: cfg},
			{
				ResourceName:      address,
				ImportState:       true,
				ImportStateVerify: true,
				ImportStateVerifyIgnore: []string{
					"configuration.customer.capabilities.automatic_indirect_tax.requested",
					"configuration.merchant.capabilities.card_payments.requested",
					"identity.country",
				},
			},
			{
				ResourceName:    address,
				ImportState:     true,
				ImportStateKind: resource.ImportBlockWithID,
				Config:          imported,
			},
		}, nil, "stripe_core_account"),
	})
}

// The identity form needs Terraform or OpenTofu 1.12.0 or later.
func TestAccCoreAccount_importIdentity(t *testing.T) {
	acct := acctest.NewAccount(t)
	cfg := merchantConfig(acct, "  lifecycle {\n    ignore_changes = [configuration, identity.country]\n  }\n")
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
		}, nil, "stripe_core_account"),
	})
}

// Stripe has no DELETE for a v2 account, so dropping it from the configuration, and `terraform
// destroy`, fail at plan time, and the account stays open: destroy never closes it.
func TestAccCoreAccount_delete(t *testing.T) {
	acct := acctest.NewAccount(t)
	var path string
	var mark int
	cfg := merchantConfig(acct, "")
	resource.ParallelTest(t, resource.TestCase{
		ProtoV6ProviderFactories: tfacc.ProviderFactories,
		Steps: acctest.Forget(t, []resource.TestStep{
			{Config: cfg, Check: resource.ComposeTestCheckFunc(pathOf(&path), tfacc.LogMark(t, acct, &mark))},
			{Config: acct.ProviderBlock(), PlanOnly: true, ExpectError: tfacc.RefusedDestroy},
			{Config: cfg, Destroy: true, ExpectError: tfacc.RefusedDestroy},
		}, tfacc.Untouched(t, acct, &path, &mark, "closed", wire.Bool(false)), "stripe_core_account"),
	})
}

func TestAccCoreAccount_drift(t *testing.T) {
	acct := acctest.NewAccount(t)
	var path string
	cfg := merchantConfig(acct, "")
	resource.ParallelTest(t, resource.TestCase{
		ProtoV6ProviderFactories: tfacc.ProviderFactories,
		Steps: acctest.Forget(t, []resource.TestStep{
			{Config: cfg, Check: pathOf(&path)},
			{
				PreConfig: func() {
					tfacc.Post(t, acct, path, wire.Object{{Name: "display_name", Value: wire.String("Renamed")}})
				},
				Config: cfg,
				ConfigPlanChecks: resource.ConfigPlanChecks{PreApply: []plancheck.PlanCheck{
					plancheck.ExpectNonEmptyPlan(),
					plancheck.ExpectResourceAction(address, plancheck.ResourceActionUpdate),
				}},
				Check: resource.TestCheckResourceAttr(address, "display_name", acct.Name("Acme Supplies")),
			},
		}, nil, "stripe_core_account"),
	})
}

// Setting `closed` closes the account through its action endpoint, which must name exactly the
// account's applied configurations (suite: "close", "close without the applied
// configurations"), so the engine sends the ones the account holds. A closed account keeps
// them. Its identity no longer returns the country and entity type (suite: "close"), so state
// keeps them and the next plan replaces nothing.
func TestAccCoreAccount_close(t *testing.T) {
	acct := acctest.NewAccount(t)
	var path string
	resource.ParallelTest(t, resource.TestCase{
		ProtoV6ProviderFactories: tfacc.ProviderFactories,
		Steps: acctest.Forget(t, []resource.TestStep{
			{Config: merchantConfig(acct, ""), Check: pathOf(&path)},
			{
				Config: merchantConfig(acct, "  closed = true\n"),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr(address, "closed", "true"),
					resource.TestCheckResourceAttr(address, "applied_configurations.#", "2"),
					resource.TestCheckResourceAttr(address, "configuration.merchant.applied", "true"),
					resource.TestCheckResourceAttr(address, "identity.entity_type", "company"),
					resource.TestCheckResourceAttr(address, "identity.country", "us"),
					tfacc.Holds(t, acct, &path, "closed", wire.Bool(true)),
					func(*terraform.State) error {
						if acct.Smoke {
							return nil
						}
						var closes []wire.Object
						for _, entry := range acctest.RequestLog(t, acct) {
							if params, ok := entry.Params.(wire.Object); ok && entry.Method == "POST" && entry.Path == path+"/close" {
								closes = append(closes, params)
							}
						}
						if len(closes) != 1 || !jsonIs(wire.Value(closes[0]), `{"applied_configurations":["customer","merchant"]}`) {
							return fmt.Errorf("want one close naming the applied configurations and nothing else, got %+v", closes)
						}
						return nil
					},
				),
			},
			// The configuration that says closed plans nothing more, after a refresh that
			// reads the identity without its country and entity type.
			{Config: merchantConfig(acct, "  closed = true\n"), PlanOnly: true},
			{
				Config:      merchantConfig(acct, "  closed = false\n"),
				PlanOnly:    true,
				ExpectError: regexp.MustCompile("`closed` can be set to `true`"),
			},
		}, nil, "stripe_core_account"),
	})
}
