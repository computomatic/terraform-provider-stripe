package account_v2_test

import (
	"fmt"
	"os"
	"regexp"
	"strings"
	"testing"

	"github.com/hashicorp/go-version"
	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/knownvalue"
	"github.com/hashicorp/terraform-plugin-testing/plancheck"
	"github.com/hashicorp/terraform-plugin-testing/statecheck"
	"github.com/hashicorp/terraform-plugin-testing/terraform"
	"github.com/hashicorp/terraform-plugin-testing/tfjsonpath"
	"github.com/hashicorp/terraform-plugin-testing/tfversion"

	"github.com/computomatic/terraform-provider-stripe/internal/acctest"
	"github.com/computomatic/terraform-provider-stripe/internal/acctest/tfacc"
	"github.com/computomatic/terraform-provider-stripe/internal/wire"
)

func TestMain(m *testing.M) { os.Exit(acctest.Main(m)) }

const (
	address  = "stripe_account_v2.test"
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
resource "stripe_account_v2" "test" {
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
func TestAccAccountV2_create(t *testing.T) {
	acct := acctest.NewAccount(t)
	var path string
	resource.ParallelTest(t, resource.TestCase{
		ProtoV6ProviderFactories: tfacc.ProviderFactories,
		Steps: acctest.Forget(t, []resource.TestStep{{
			Config: merchantConfig(acct, "") + `
data "stripe_account_v2" "test" {
  id = stripe_account_v2.test.id
}

data "stripe_account" "v1" {
  id = stripe_account_v2.test.id
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
				resource.TestCheckResourceAttrPair("data.stripe_account_v2.test", "id", address, "id"),
				resource.TestCheckResourceAttr("data.stripe_account_v2.test", "identity.country", "US"),
				resource.TestCheckResourceAttr("data.stripe_account_v2.test", "configuration.merchant.applied", "true"),
				resource.TestCheckResourceAttr("data.stripe_account_v2.test", "defaults.currency", "usd"),
				resource.TestCheckNoResourceAttr("data.stripe_account_v2.test", "configuration.merchant.capabilities.card_payments.requested"),
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
		}}, nil, "stripe_account_v2"),
	})
}

// A recipient requests `stripe_balance.stripe_transfers`, and Stripe adds its payouts (suite:
// "create recipient").
func TestAccAccountV2_recipient(t *testing.T) {
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
		}}, nil, "stripe_account_v2"),
	})
}

// An account that is only a customer has no dashboard (suite: "create customer only"), and
// Stripe refuses a dashboard on one (suite: "dashboard on a customer only account"). Stripe
// fills its `defaults.responsibilities`, which the fake leaves out.
func TestAccAccountV2_customerOnly(t *testing.T) {
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
		}, nil, "stripe_account_v2"),
	})
}

// Stripe updates the display name, the contact email and the registered name and merges the
// identity and the metadata, removing a key sent as null (suite: "update", "update unsets
// metadata key with null"). The update never sends the create-only identity fields, the
// configurations, the dashboard or the defaults. Removing the display name fails, because
// nothing proves Stripe clears it with null.
func TestAccAccountV2_update(t *testing.T) {
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
		}, nil, "stripe_account_v2"),
	})
}

// A change Stripe refuses or that nothing proves is refused at plan time: the dashboard and the
// collectors once set, which Stripe refuses to change, and the customer and merchant
// configurations. The error says to remove the account from the state, because its destroy is
// refused too.
func TestAccAccountV2_refuseChange(t *testing.T) {
	acct := acctest.NewAccount(t)
	withoutTax := "    customer  = { capabilities = { automatic_indirect_tax = { requested = false } } }\n"
	cfg := merchantConfig(acct, "")
	refused := func(config, field string) resource.TestStep {
		return resource.TestStep{
			Config:      config,
			PlanOnly:    true,
			ExpectError: regexp.MustCompile("(?s)Changing `" + field + "` is not supported yet.*removed block"),
		}
	}
	resource.ParallelTest(t, resource.TestCase{
		ProtoV6ProviderFactories: tfacc.ProviderFactories,
		Steps: acctest.Forget(t, []resource.TestStep{
			{Config: cfg},
			refused(accountConfig(acct, created(acct), withoutTax+merchant, managed), "configuration.customer"),
			refused(strings.ReplaceAll(cfg, `dashboard = "none"`, `dashboard = "express"`), "dashboard"),
			refused(strings.ReplaceAll(cfg, `fees_collector = "application"`, `fees_collector = "stripe"`), "defaults.responsibilities"),
		}, nil, "stripe_account_v2"),
	})
}

// bareConfig is an account with no configuration, as the record's change probes create it, with
// the identity they name and a display name, which the test runs name their objects by. body holds
// the rest.
func bareConfig(c acctest.Config, identity, body string) string {
	return c.ProviderBlock() + fmt.Sprintf(`
resource "stripe_account_v2" "test" {
  display_name = %q
%s%s}
`, c.Name("Bare"), identity, body)
}

// sentOnUpdate checks what the account's last update sent of field.
func sentOnUpdate(t *testing.T, c acctest.Config, path *string, field, want string) resource.TestCheckFunc {
	return func(*terraform.State) error {
		if c.Smoke {
			return nil
		}
		if got, _ := lastUpdate(t, c, *path).Get(field); !jsonIs(got, want) {
			return fmt.Errorf("the update sent %s as %+v, want %s", field, got, want)
		}
		return nil
	}
}

// Stripe takes a country on an account with none, filling the default currency from it, and a
// new country on one with another (the record's probes `isolated/change_country_from_none` and
// `isolated/change_country`, with their values), so the country changes in place and goes alone.
func TestAccAccountV2_changeCountry(t *testing.T) {
	acct := acctest.NewAccount(t)
	var path string
	country := func(c string) string { return fmt.Sprintf("  identity = { country = %q }\n", c) }
	resource.ParallelTest(t, resource.TestCase{
		ProtoV6ProviderFactories: tfacc.ProviderFactories,
		Steps: acctest.Forget(t, []resource.TestStep{
			{
				Config: bareConfig(acct, "", ""),
				Check:  resource.ComposeAggregateTestCheckFunc(pathOf(&path), resource.TestCheckNoResourceAttr(address, "identity.country")),
			},
			{
				Config: bareConfig(acct, country("US"), ""),
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply:             []plancheck.PlanCheck{plancheck.ExpectResourceAction(address, plancheck.ResourceActionUpdate)},
					PostApplyPostRefresh: []plancheck.PlanCheck{plancheck.ExpectEmptyPlan()},
				},
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr(address, "identity.country", "US"),
					resource.TestCheckResourceAttr(address, "defaults.currency", "usd"),
					sentOnUpdate(t, acct, &path, "identity", `{"country":"US"}`),
				),
			},
			{
				Config: bareConfig(acct, country("GB"), ""),
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply:             []plancheck.PlanCheck{plancheck.ExpectResourceAction(address, plancheck.ResourceActionUpdate)},
					PostApplyPostRefresh: []plancheck.PlanCheck{plancheck.ExpectEmptyPlan()},
				},
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr(address, "identity.country", "GB"),
					sentOnUpdate(t, acct, &path, "identity", `{"country":"GB"}`),
				),
			},
		}, nil, "stripe_account_v2"),
	})
}

// Stripe refused `GB` on the suite's recipient account in `US`, which requests `stripe_transfers`
// (the record's rule on the update's `identity.country`, probe
// `isolated/change_country_on_recipient`, with its values), so the plan refuses the update that
// would send it, with Stripe's message, and nothing reaches Stripe.
func TestAccAccountV2_changeCountryOnRecipient(t *testing.T) {
	acct := acctest.NewAccount(t)
	managedRecipient := "  dashboard = \"none\"\n  defaults = {\n    responsibilities = { fees_collector = \"application\", losses_collector = \"application\" }\n  }\n"
	resource.ParallelTest(t, resource.TestCase{
		ProtoV6ProviderFactories: tfacc.ProviderFactories,
		Steps: acctest.Forget(t, []resource.TestStep{
			{
				Config: recipientConfig(acct, "    country = \"US\"\n", requested, managedRecipient),
				Check:  resource.TestCheckResourceAttr(address, "identity.country", "US"),
			},
			{
				Config:      recipientConfig(acct, "    country = \"GB\"\n", requested, managedRecipient),
				PlanOnly:    true,
				ExpectError: regexp.MustCompile(`(?s)Stripe refuses this update.*identity\.country\s+=\s+"GB".*Stripe\s+account\s+in\s+US.*When\s+requesting\s+the\s+` + "`transfers`" + `\s+capability.*isolated/change_country_on_recipient`),
			},
		}, nil, "stripe_account_v2"),
	})
}

// Stripe takes a new default currency on a US account (the record's probe
// `isolated/change_currency`, with its values), so it changes in place.
func TestAccAccountV2_changeCurrency(t *testing.T) {
	acct := acctest.NewAccount(t)
	var path string
	us := "  identity = { country = \"US\" }\n"
	resource.ParallelTest(t, resource.TestCase{
		ProtoV6ProviderFactories: tfacc.ProviderFactories,
		Steps: acctest.Forget(t, []resource.TestStep{
			{
				Config: bareConfig(acct, us, ""),
				Check:  resource.ComposeAggregateTestCheckFunc(pathOf(&path), resource.TestCheckResourceAttr(address, "defaults.currency", "usd")),
			},
			{
				Config: bareConfig(acct, us, "  defaults = { currency = \"eur\" }\n"),
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply:             []plancheck.PlanCheck{plancheck.ExpectResourceAction(address, plancheck.ResourceActionUpdate)},
					PostApplyPostRefresh: []plancheck.PlanCheck{plancheck.ExpectEmptyPlan()},
				},
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr(address, "defaults.currency", "eur"),
					sentOnUpdate(t, acct, &path, "defaults", `{"currency":"eur"}`),
				),
			},
		}, nil, "stripe_account_v2"),
	})
}

// Stripe refuses the recipient's `stripe_transfers` request on an account with no dashboard
// unless the same update sets the dashboard and both collectors, and then takes it (the record's
// probes `isolated/change_adds_recipient` and `isolated/change_adds_recipient_with_collectors`,
// with their values). The plan sets the dashboard and the collectors while the account has none
// (SetOnce), and refuses to change them once set. The update sends the recipient's one member the
// configuration sets, as the probe did, so adding the recipient with no member set, which would
// send nothing of it, is refused at plan.
func TestAccAccountV2_addRecipient(t *testing.T) {
	acct := acctest.NewAccount(t)
	var path string
	// The probes' account sends a contact email (`$gen`), which the test generates as the suite does.
	us := fmt.Sprintf("  contact_email = \"e2e+%s@example.com\"\n  identity = { country = \"US\" }\n", acct.Name("recipient"))
	recipient := "  configuration = {\n    recipient = {\n" + requested + "\n    }\n  }\n"
	managedRecipient := "  dashboard = \"none\"\n  defaults = {\n    responsibilities = { fees_collector = \"application\", losses_collector = \"application\" }\n  }\n"
	resource.ParallelTest(t, resource.TestCase{
		ProtoV6ProviderFactories: tfacc.ProviderFactories,
		Steps: acctest.Forget(t, []resource.TestStep{
			{
				Config: bareConfig(acct, us, ""),
				Check:  resource.ComposeAggregateTestCheckFunc(pathOf(&path), resource.TestCheckNoResourceAttr(address, "dashboard")),
			},
			{
				Config:      bareConfig(acct, us, recipient),
				ExpectError: regexp.MustCompile("You must set dashboard"),
			},
			{
				Config:      bareConfig(acct, us, "  configuration = {\n    recipient = {}\n  }\n"+managedRecipient),
				PlanOnly:    true,
				ExpectError: regexp.MustCompile("(?s)Cannot add `configuration.recipient` with no\\s+member set"),
			},
			{
				Config: bareConfig(acct, us, recipient+managedRecipient),
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply:             []plancheck.PlanCheck{plancheck.ExpectResourceAction(address, plancheck.ResourceActionUpdate)},
					PostApplyPostRefresh: []plancheck.PlanCheck{plancheck.ExpectEmptyPlan()},
				},
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr(address, "dashboard", "none"),
					resource.TestCheckResourceAttr(address, "defaults.responsibilities.fees_collector", "application"),
					resource.TestCheckResourceAttr(address, "configuration.recipient.applied", "true"),
					resource.TestCheckTypeSetElemAttr(address, "applied_configurations.*", "recipient"),
					sentOnUpdate(t, acct, &path, "dashboard", `"none"`),
					sentOnUpdate(t, acct, &path, "configuration", `{"recipient":{"capabilities":{"stripe_balance":{"stripe_transfers":{"requested":true}}}}}`),
				),
			},
			{
				Config:      bareConfig(acct, us, recipient+strings.ReplaceAll(managedRecipient, `"none"`, `"express"`)),
				PlanOnly:    true,
				ExpectError: regexp.MustCompile("(?s)Changing `dashboard` is not supported yet"),
			},
		}, nil, "stripe_account_v2"),
	})
}

// Stripe takes `applied` and the `stripe_transfers` request together, both true, on an account
// with neither, beside the dashboard and both collectors (the record's `together` answer to probe
// `isolated/recipient_pair_added`, on an account created with a contact email and a US identity),
// and applies the recipient, whose capabilities read `restricted` (its side effects). The update
// sends both members, as the probe did.
func TestAccAccountV2_addRecipientApplied(t *testing.T) {
	acct := acctest.NewAccount(t)
	var path string
	us := fmt.Sprintf("  contact_email = \"e2e+%s@example.com\"\n  identity = { country = \"US\" }\n", acct.Name("recipient"))
	applied := "  configuration = {\n    recipient = {\n      applied      = true\n" + requested + "\n    }\n  }\n"
	managedRecipient := "  dashboard = \"none\"\n  defaults = {\n    responsibilities = { fees_collector = \"application\", losses_collector = \"application\" }\n  }\n"
	resource.ParallelTest(t, resource.TestCase{
		ProtoV6ProviderFactories: tfacc.ProviderFactories,
		Steps: acctest.Forget(t, []resource.TestStep{
			{
				Config: bareConfig(acct, us, ""),
				Check:  pathOf(&path),
			},
			{
				Config: bareConfig(acct, us, applied+managedRecipient),
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply:             []plancheck.PlanCheck{plancheck.ExpectResourceAction(address, plancheck.ResourceActionUpdate)},
					PostApplyPostRefresh: []plancheck.PlanCheck{plancheck.ExpectEmptyPlan()},
				},
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr(address, "configuration.recipient.applied", "true"),
					resource.TestCheckResourceAttr(address, "configuration.recipient.capabilities.stripe_balance.stripe_transfers.status", "restricted"),
					resource.TestCheckResourceAttr(address, "configuration.recipient.capabilities.stripe_balance.payouts.status", "restricted"),
					resource.TestCheckTypeSetElemAttr(address, "applied_configurations.*", "recipient"),
					sentOnUpdate(t, acct, &path, "configuration", `{"recipient":{"applied":true,"capabilities":{"stripe_balance":{"stripe_transfers":{"requested":true}}}}}`),
				),
			},
		}, nil, "stripe_account_v2"),
	})
}

// Stripe takes `applied = false` on a recipient (the record's probe
// `isolated/change_applied_to_false`, on the suite's recipient account), so it changes in place,
// and the account's applied configurations follow it. The update sends `applied` alone, as that
// probe did, because Stripe keeps the recipient's members an update leaves out.
func TestAccAccountV2_unapplyRecipient(t *testing.T) {
	acct := acctest.NewAccount(t)
	var path string
	managedRecipient := "  dashboard = \"none\"\n  defaults = {\n    responsibilities = { fees_collector = \"application\", losses_collector = \"application\" }\n  }\n"
	resource.ParallelTest(t, resource.TestCase{
		ProtoV6ProviderFactories: tfacc.ProviderFactories,
		Steps: acctest.Forget(t, []resource.TestStep{
			{
				Config: recipientConfig(acct, "    country = \"US\"\n", requested, managedRecipient),
				Check:  pathOf(&path),
			},
			{
				Config: recipientConfig(acct, "    country = \"US\"\n", "      applied      = false\n"+requested, managedRecipient),
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply:             []plancheck.PlanCheck{plancheck.ExpectResourceAction(address, plancheck.ResourceActionUpdate)},
					PostApplyPostRefresh: []plancheck.PlanCheck{plancheck.ExpectEmptyPlan()},
				},
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr(address, "configuration.recipient.applied", "false"),
					sentOnUpdate(t, acct, &path, "configuration", `{"recipient":{"applied":false}}`),
				),
			},
		}, nil, "stripe_account_v2"),
	})
}

// Stripe takes `applied = false` and the `stripe_transfers` request at false together on a
// recipient that holds both (the record's `together` answer to probe
// `isolated/recipient_pair_changed`), removing the configuration and its capabilities (its side
// effects). The update sends both members, as the probe did.
func TestAccAccountV2_withdrawRecipient(t *testing.T) {
	acct := acctest.NewAccount(t)
	var path string
	managedRecipient := "  dashboard = \"none\"\n  defaults = {\n    responsibilities = { fees_collector = \"application\", losses_collector = \"application\" }\n  }\n"
	resource.ParallelTest(t, resource.TestCase{
		ProtoV6ProviderFactories: tfacc.ProviderFactories,
		Steps: acctest.Forget(t, []resource.TestStep{
			{
				Config: recipientConfig(acct, "    country = \"US\"\n", requested, managedRecipient),
				Check:  pathOf(&path),
			},
			{
				Config: recipientConfig(acct, "    country = \"US\"\n", "      applied      = false\n"+strings.Replace(requested, "true", "false", 1), managedRecipient),
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply:             []plancheck.PlanCheck{plancheck.ExpectResourceAction(address, plancheck.ResourceActionUpdate)},
					PostApplyPostRefresh: []plancheck.PlanCheck{plancheck.ExpectEmptyPlan()},
				},
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr(address, "configuration.recipient.applied", "false"),
					resource.TestCheckResourceAttr(address, "configuration.recipient.capabilities.stripe_balance.stripe_transfers.requested", "false"),
					resource.TestCheckNoResourceAttr(address, "configuration.recipient.capabilities.stripe_balance.stripe_transfers.status"),
					resource.TestCheckResourceAttr(address, "applied_configurations.#", "0"),
					sentOnUpdate(t, acct, &path, "configuration", `{"recipient":{"applied":false,"capabilities":{"stripe_balance":{"stripe_transfers":{"requested":false}}}}}`),
				),
			},
		}, nil, "stripe_account_v2"),
	})
}

// recipientConfig is the suite's recipient account: a US identity, a contact email and a
// recipient configuration. body holds what the test changes, and the identity goes in full, so
// that the test can change one member of it. Where the record generates a value, as it does the
// name and the email (`$gen`), so does the test.
func recipientConfig(c acctest.Config, identity, requested, body string) string {
	return c.ProviderBlock() + fmt.Sprintf(`
resource "stripe_account_v2" "test" {
  display_name  = %q
  contact_email = "e2e+%s@example.com"
  identity = {
%s  }
  configuration = {
    recipient = {
%s
    }
  }
%s}
`, c.Name("Recipient"), c.Name("recipient"), identity, requested, body)
}

// profileConfig is the account of the record's probe `update/1`: no configuration and no
// identity, on which the suite proves the contact phone and the defaults' locales and profile.
func profileConfig(c acctest.Config, body string) string {
	return c.ProviderBlock() + fmt.Sprintf(`
resource "stripe_account_v2" "test" {
  display_name = %q
%s}
`, c.Name("Profile"), body)
}

// noConfiguration checks that an account with no configuration holds `configuration` null, as
// Stripe reads it under every include, which the engine asks for (the record's `hash_reads`,
// condition `no_configuration`), and not a hash of three nulls.
var noConfiguration = statecheck.ExpectKnownValue(address, tfjsonpath.New("configuration"), knownvalue.Null())

const (
	usCompany = "    country     = \"US\"\n    entity_type = \"company\"\n"
	requested = "      capabilities = { stripe_balance = { stripe_transfers = { requested = true } } }"
)

// The record's probes `update/1` and `unset/1`, on the same account and with the same values: the
// contact phone, the locales reordered then shortened, and the profile. An update sends only what
// changed. Removing a member the record shows null unsets clears it. Stripe judges a business URL
// by its value (the record's probes `isolated/url_*`): an update of an account with no
// configuration takes `https://accessible.stripe.com`, which the record holds on no such create,
// so the create leaves the URL out and the update adds it, and it stays in every later step,
// because Stripe keeps the profile's members an update leaves out.
func TestAccAccountV2_updateDefaults(t *testing.T) {
	acct := acctest.NewAccount(t)
	var path string
	body := func(phone, locales, url, profile string) string {
		if url != "" {
			url = fmt.Sprintf("      business_url = %q\n", url)
		}
		return phone + `  defaults = {
    locales = ` + locales + `
    profile = {
` + url + profile + `    }
  }
`
	}
	const accessible = "https://accessible.stripe.com"
	doing, renamed, product := acct.Name("Acme"), acct.Name("Acme Trading"), acct.Name("Hardware")
	first := body("  contact_phone = \"+18008675309\"\n", `["ar-SA", "bg"]`, "", fmt.Sprintf(`      doing_business_as   = %q
      product_description = %q
`, doing, product))
	second := body("  contact_phone = \"+18008675310\"\n", `["bg", "ar-SA"]`, accessible, fmt.Sprintf(`      doing_business_as   = %q
      product_description = %q
`, renamed, product))
	removed := body("", `["ar-SA"]`, accessible, fmt.Sprintf(`      doing_business_as = %q
`, renamed))
	resource.ParallelTest(t, resource.TestCase{
		ProtoV6ProviderFactories: tfacc.ProviderFactories,
		Steps: acctest.Forget(t, []resource.TestStep{
			{
				Config: profileConfig(acct, first),
				ConfigPlanChecks: resource.ConfigPlanChecks{PostApplyPostRefresh: []plancheck.PlanCheck{
					plancheck.ExpectEmptyPlan(),
				}},
				ConfigStateChecks: []statecheck.StateCheck{noConfiguration},
				Check: resource.ComposeAggregateTestCheckFunc(
					pathOf(&path),
					resource.TestCheckResourceAttr(address, "contact_phone", "+18008675309"),
					resource.TestCheckResourceAttr(address, "defaults.locales.0", "ar-SA"),
					resource.TestCheckNoResourceAttr(address, "defaults.profile.business_url"),
					resource.TestCheckResourceAttr(address, "defaults.profile.doing_business_as", doing),
				),
			},
			{
				Config: profileConfig(acct, second),
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply:             []plancheck.PlanCheck{plancheck.ExpectResourceAction(address, plancheck.ResourceActionUpdate)},
					PostApplyPostRefresh: []plancheck.PlanCheck{plancheck.ExpectEmptyPlan()},
				},
				ConfigStateChecks: []statecheck.StateCheck{noConfiguration},
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr(address, "contact_phone", "+18008675310"),
					resource.TestCheckResourceAttr(address, "defaults.locales.0", "bg"),
					resource.TestCheckResourceAttr(address, "defaults.profile.doing_business_as", renamed),
					resource.TestCheckResourceAttr(address, "defaults.profile.business_url", accessible),
					func(*terraform.State) error {
						if acct.Smoke {
							return nil
						}
						u := lastUpdate(t, acct, path)
						// The record lists `defaults.profile` as keeping its siblings, so the
						// unchanged product description is not sent.
						if defaults, _ := u.Get("defaults"); !jsonIs(defaults, `{"locales":["bg","ar-SA"],"profile":{"business_url":"https://accessible.stripe.com","doing_business_as":"`+renamed+`"}}`) {
							return fmt.Errorf("the update must send only the defaults and the profile members that changed: %+v", defaults)
						}
						for _, never := range []string{"configuration", "identity"} {
							if _, sent := u.Get(never); sent {
								return fmt.Errorf("the update sent %s: %+v", never, u)
							}
						}
						return nil
					},
				),
			},
			{
				Config: profileConfig(acct, removed),
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PostApplyPostRefresh: []plancheck.PlanCheck{plancheck.ExpectEmptyPlan()},
				},
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckNoResourceAttr(address, "contact_phone"),
					resource.TestCheckResourceAttr(address, "defaults.locales.#", "1"),
					resource.TestCheckNoResourceAttr(address, "defaults.profile.product_description"),
					resource.TestCheckResourceAttr(address, "defaults.profile.business_url", "https://accessible.stripe.com"),
					resource.TestCheckResourceAttr(address, "defaults.profile.doing_business_as", renamed),
				),
			},
		}, nil, "stripe_account_v2"),
	})
}

// A contact phone Stripe holds that the configuration leaves out plans an update that clears it,
// because the record shows null unsets it.
func TestAccAccountV2_clearsWhatTheConfigurationLeavesOut(t *testing.T) {
	acct := acctest.NewAccount(t)
	var path string
	cfg := profileConfig(acct, "")
	resource.ParallelTest(t, resource.TestCase{
		ProtoV6ProviderFactories: tfacc.ProviderFactories,
		Steps: acctest.Forget(t, []resource.TestStep{
			{Config: cfg, Check: pathOf(&path)},
			{
				PreConfig: func() {
					tfacc.Post(t, acct, path, wire.Object{{Name: "contact_phone", Value: wire.String("+18008675310")}})
				},
				Config: cfg,
				ConfigPlanChecks: resource.ConfigPlanChecks{PreApply: []plancheck.PlanCheck{
					plancheck.ExpectResourceAction(address, plancheck.ResourceActionUpdate),
				}},
				Check: resource.TestCheckNoResourceAttr(address, "contact_phone"),
			},
		}, nil, "stripe_account_v2"),
	})
}

// lastUpdate is the parameters of the last POST to path. There is no log in smoke mode.
func lastUpdate(t *testing.T, c acctest.Config, path string) wire.Object {
	if c.Smoke {
		return nil
	}
	for _, entry := range acctest.RequestLog(t, c) {
		if params, ok := entry.Params.(wire.Object); ok && entry.Method == "POST" && entry.Path == path {
			return params
		}
	}
	return nil
}

// The record proves a change of the entity type, which goes alone: the country, which did not
// change, stays out of the request, and removing the country keeps its value, because Stripe
// refuses a null.
func TestAccAccountV2_updateIdentity(t *testing.T) {
	acct := acctest.NewAccount(t)
	var path string
	managedRecipient := "  dashboard = \"none\"\n  defaults = {\n    responsibilities = { fees_collector = \"application\", losses_collector = \"application\" }\n  }\n"
	resource.ParallelTest(t, resource.TestCase{
		ProtoV6ProviderFactories: tfacc.ProviderFactories,
		Steps: acctest.Forget(t, []resource.TestStep{
			{
				Config: recipientConfig(acct, usCompany, requested, managedRecipient),
				Check:  pathOf(&path),
			},
			{
				Config: recipientConfig(acct, "    country     = \"US\"\n    entity_type = \"non_profit\"\n", requested, managedRecipient),
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply:             []plancheck.PlanCheck{plancheck.ExpectResourceAction(address, plancheck.ResourceActionUpdate)},
					PostApplyPostRefresh: []plancheck.PlanCheck{plancheck.ExpectEmptyPlan()},
				},
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr(address, "identity.entity_type", "non_profit"),
					func(*terraform.State) error {
						if acct.Smoke {
							return nil
						}
						if identity, _ := lastUpdate(t, acct, path).Get("identity"); !jsonIs(identity, `{"entity_type":"non_profit"}`) {
							return fmt.Errorf("the update must send only what changed of the identity: %+v", identity)
						}
						return nil
					},
				),
			},
			{
				Config: recipientConfig(acct, "    entity_type = \"non_profit\"\n", requested, managedRecipient),
				ConfigPlanChecks: resource.ConfigPlanChecks{PreApply: []plancheck.PlanCheck{
					plancheck.ExpectEmptyPlan(),
				}},
				Check: resource.TestCheckResourceAttr(address, "identity.country", "US"),
			},
		}, nil, "stripe_account_v2"),
	})
}

// The record proves a change of a recipient's `stripe_transfers` request, whose capability
// Stripe then drops from its answer (probe `update/2`), sent alone (probe
// `isolated/transfers_requested_false`), as the update sends it. Stripe never returns
// `requested`, so state keeps the configured value.
func TestAccAccountV2_updateRecipient(t *testing.T) {
	acct := acctest.NewAccount(t)
	var path string
	managedRecipient := "  dashboard = \"none\"\n  defaults = {\n    responsibilities = { fees_collector = \"application\", losses_collector = \"application\" }\n  }\n"
	withdrawn := "      capabilities = { stripe_balance = { stripe_transfers = { requested = false } } }"
	resource.ParallelTest(t, resource.TestCase{
		ProtoV6ProviderFactories: tfacc.ProviderFactories,
		Steps: acctest.Forget(t, []resource.TestStep{
			{
				Config: recipientConfig(acct, usCompany, requested, managedRecipient),
				Check:  pathOf(&path),
			},
			{
				Config: recipientConfig(acct, usCompany, withdrawn, managedRecipient),
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply:             []plancheck.PlanCheck{plancheck.ExpectResourceAction(address, plancheck.ResourceActionUpdate)},
					PostApplyPostRefresh: []plancheck.PlanCheck{plancheck.ExpectEmptyPlan()},
				},
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr(address, "configuration.recipient.capabilities.stripe_balance.stripe_transfers.requested", "false"),
					func(*terraform.State) error {
						if acct.Smoke {
							return nil
						}
						if configuration, _ := lastUpdate(t, acct, path).Get("configuration"); !jsonIs(configuration, `{"recipient":{"capabilities":{"stripe_balance":{"stripe_transfers":{"requested":false}}}}}`) {
							return fmt.Errorf("the update must send only the transfers request: %+v", configuration)
						}
						return nil
					},
				),
			},
		}, nil, "stripe_account_v2"),
	})
}

// Nothing of the account is create-only now, so no change of an argument replaces it. Stripe
// cannot delete the account, so a taint fails at apply.
func TestAccAccountV2_replace(t *testing.T) {
	acct := acctest.NewAccount(t)
	cfg := merchantConfig(acct, "")
	resource.ParallelTest(t, resource.TestCase{
		ProtoV6ProviderFactories: tfacc.ProviderFactories,
		Steps: acctest.Forget(t, []resource.TestStep{
			{Config: cfg},
			{Config: cfg, Taint: []string{address}, ExpectError: tfacc.RefusedDestroy},
		}, nil, "stripe_account_v2"),
	})
}

// Stripe does not return the requested capabilities, and returns the country in upper case,
// so an import has no `requested` flags and Stripe's country, and a configuration ignores
// both (see the import note).
func TestAccAccountV2_import(t *testing.T) {
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
		}, nil, "stripe_account_v2"),
	})
}

// The identity form needs Terraform or OpenTofu 1.12.0 or later.
func TestAccAccountV2_importIdentity(t *testing.T) {
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
		}, nil, "stripe_account_v2"),
	})
}

// Stripe has no DELETE for a v2 account, so dropping it from the configuration, and `terraform
// destroy`, fail at plan time, and the account stays open: destroy never closes it.
func TestAccAccountV2_delete(t *testing.T) {
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
		}, tfacc.Untouched(t, acct, &path, &mark, "closed", wire.Bool(false)), "stripe_account_v2"),
	})
}

func TestAccAccountV2_drift(t *testing.T) {
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
		}, nil, "stripe_account_v2"),
	})
}

// Setting `closed` closes the account through its action endpoint, which must name exactly the
// account's applied configurations (suite: "close", "close without the applied
// configurations"), so the engine sends the ones the account holds. Stripe answers a closed
// account's identity, configuration, defaults and requirements as null, and on later reads
// its dashboard, contact email, display name and metadata too (suite: "close"), which the
// data source shows; state keeps them all, so the next plans change and replace nothing.
func TestAccAccountV2_close(t *testing.T) {
	acct := acctest.NewAccount(t)
	var path string
	renamed := created(acct)
	renamed.display = fmt.Sprintf("display_name  = %q\n", acct.Name("Renamed Supplies"))
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
			// reads every field Stripe nulls as null.
			{Config: merchantConfig(acct, "  closed = true\n"), PlanOnly: true},
			{
				Config: merchantConfig(acct, "  closed = true\n") + `
data "stripe_account_v2" "test" {
  id = stripe_account_v2.test.id
}
`,
				ConfigPlanChecks: resource.ConfigPlanChecks{PreApply: []plancheck.PlanCheck{
					plancheck.ExpectEmptyPlan(),
				}},
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr(address, "display_name", acct.Name("Acme Supplies")),
					resource.TestCheckResourceAttr(address, "contact_email", "payments@example.com"),
					resource.TestCheckResourceAttr(address, "dashboard", "none"),
					resource.TestCheckResourceAttr(address, "metadata.stage", "created"),
					resource.TestCheckResourceAttr(address, "defaults.currency", "usd"),
					resource.TestCheckResourceAttr(address, "configuration.merchant.applied", "true"),
					resource.TestCheckResourceAttr(address, "identity.country", "us"),
					resource.TestCheckResourceAttr("data.stripe_account_v2.test", "closed", "true"),
					resource.TestCheckNoResourceAttr("data.stripe_account_v2.test", "display_name"),
					resource.TestCheckNoResourceAttr("data.stripe_account_v2.test", "dashboard"),
					resource.TestCheckNoResourceAttr("data.stripe_account_v2.test", "identity.country"),
					resource.TestCheckNoResourceAttr("data.stripe_account_v2.test", "configuration.merchant.applied"),
				),
			},
			{Config: merchantConfig(acct, "  closed = true\n"), PlanOnly: true},
			{
				Config:      merchantConfig(acct, "  closed = false\n"),
				PlanOnly:    true,
				ExpectError: regexp.MustCompile("`closed` can be set to `true`"),
			},
			// Stripe refuses any update of a closed account (suite: "update a closed
			// account"), so a change fails at plan time, and so does one that would replace it.
			{Config: accountConfig(acct, renamed, customer+merchant, managed+"  closed = true\n"), PlanOnly: true, ExpectError: closedRefusal},
			{Config: strings.Replace(merchantConfig(acct, "  closed = true\n"), `"us"`, `"gb"`, 1), PlanOnly: true, ExpectError: closedRefusal},
		}, nil, "stripe_account_v2"),
	})
}

// closedRefusal is the plan-time refusal of a change to a closed account.
var closedRefusal = regexp.MustCompile(`Cannot change a closed stripe_account_v2`)

// An import of a closed account has none of what Stripe nulls (suite: "close"), so the
// configuration that made the account would change it, which fails at plan time like any change
// to a closed account, instead of planning a replacement for the configured `identity.country`.
// A configuration that ignores every change imports it (see the import note).
func TestAccAccountV2_importClosed(t *testing.T) {
	acct := acctest.NewAccount(t)
	cfg := merchantConfig(acct, "  closed = true\n")
	resource.ParallelTest(t, resource.TestCase{
		ProtoV6ProviderFactories: tfacc.ProviderFactories,
		Steps: acctest.Forget(t, []resource.TestStep{
			{Config: merchantConfig(acct, "")},
			{Config: cfg},
			{
				ResourceName:    address,
				ImportState:     true,
				ImportStateKind: resource.ImportBlockWithID,
				Config:          cfg,
				ExpectError:     closedRefusal,
			},
			{
				ResourceName:    address,
				ImportState:     true,
				ImportStateKind: resource.ImportBlockWithID,
				Config:          merchantConfig(acct, "  closed = true\n  lifecycle {\n    ignore_changes = all\n  }\n"),
			},
		}, nil, "stripe_account_v2"),
	})
}
