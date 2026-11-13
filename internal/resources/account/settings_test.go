package account_test

import (
	"fmt"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/plancheck"
	"github.com/hashicorp/terraform-plugin-testing/terraform"
	"github.com/hashicorp/terraform-plugin-testing/tfjsonpath"

	"github.com/computomatic/terraform-provider-stripe/internal/acctest"
	"github.com/computomatic/terraform-provider-stripe/internal/acctest/tfacc"
	"github.com/computomatic/terraform-provider-stripe/internal/wire"
)

// profile is the business profile and settings of the suite's probe `update/8`, which creates a
// US custom company account with one set and changes every member to the other. Stripe takes a
// fiscal year end only within the past year, so the probe sends one 60 days ago and then 90. The
// probe sends the payments descriptor and the card descriptor prefix too, but reads neither, as
// Stripe fills both from the business name, so they have a test of their own.
type profile struct {
	businessType, revenueEnd, name, product, termsURL, supportEmail, supportPhone, supportURL, url string
	revenue, workers                                                                               int
	mcc                                                                                            string
	designation                                                                                    string
	city, line1, line2, postalCode, state                                                          string
	declineChecks                                                                                  bool
	paymentSave, payoutDescriptor, interval, extraSettings                                         string
	debitNegative                                                                                  bool
}

func createdProfile(c acctest.Config) profile {
	return profile{
		businessType: "company", revenue: 10000000, revenueEnd: daysAgo(60), workers: 7, mcc: "5734",
		designation: `["lgbtqi_owned_business", "minority_owned_business"]`,
		name:        c.Name("Conformance Tours"), product: "Guided tours of the conformance suite.",
		termsURL: "https://accessible.stripe.com/legal", supportEmail: c.Name("support") + "@example.com",
		supportPhone: "+18008675309", supportURL: "https://accessible.stripe.com/support", url: "https://accessible.stripe.com",
		city: "San Francisco", line1: "510 Townsend St", line2: "Suite 100", postalCode: "94103", state: "CA",
		declineChecks: true, paymentSave: "always", payoutDescriptor: "CONFORM PAYOUT", interval: "manual",
	}
}

func updatedProfile(c acctest.Config) profile {
	return profile{
		businessType: "government_entity", revenue: 20000000, revenueEnd: daysAgo(90), workers: 8, mcc: "7372",
		designation: `["minority_owned_business", "lgbtqi_owned_business"]`,
		name:        c.Name("Harbour Tours"), product: "Guided tours of the harbour.",
		termsURL: "https://accessible.stripe.com/legal-notice", supportEmail: c.Name("help") + "@example.com",
		supportPhone: "+18008675310", supportURL: "https://accessible.stripe.com/help", url: "https://accessible.stripe.com/attrs",
		city: "Seattle", line1: "400 Pine St", line2: "Suite 200", postalCode: "98101", state: "WA",
		paymentSave: "never", payoutDescriptor: "HARBOUR PAYOUT",
		interval: "daily", debitNegative: true, extraSettings: "    sepa_debit_payments = { creditor_id = \"AT61ZZZ01234567890\" }\n",
	}
}

// daysAgo is the UTC date the given number of days before today, as Stripe takes a fiscal year
// end.
func daysAgo(days int) string {
	return time.Now().UTC().AddDate(0, 0, -days).Format(time.DateOnly)
}

func profileConfig(c acctest.Config, p profile) string {
	return customConfig(c, fmt.Sprintf(`  business_type    = %q
  default_currency = "usd"
  email            = %q
  external_account = "btok_us_verified"
  capabilities     = { transfers = { requested = true } }
  business_profile = {
    annual_revenue = { amount = %d, currency = "usd", fiscal_year_end = %q }
    estimated_worker_count                    = %d
    mcc                                       = %q
    minority_owned_business_designation       = %s
    name                                      = %q
    product_description                       = %q
    specified_commercial_transactions_act_url = %q
    support_address = { city = %q, country = "US", line1 = %q, line2 = %q, postal_code = %q, state = %q }
    support_email = %q
    support_phone = %q
    support_url   = %q
    url           = %q
  }
  settings = {
    card_payments = { decline_on = { avs_failure = %t, cvc_failure = %t } }
    invoices      = { hosted_payment_method_save = %q }
    payouts = {
      debit_negative_balances = %t
      schedule                = { interval = %q }
      statement_descriptor    = %q
    }
%s  }
`, p.businessType, c.Name("owner")+"@example.com", p.revenue, p.revenueEnd, p.workers, p.mcc, p.designation, p.name, p.product,
		p.termsURL, p.city, p.line1, p.line2, p.postalCode, p.state, p.supportEmail, p.supportPhone, p.supportURL, p.url,
		p.declineChecks, p.declineChecks, p.paymentSave, p.debitNegative, p.interval,
		p.payoutDescriptor, p.extraSettings))
}

func profileChecks(p profile) resource.TestCheckFunc {
	return resource.ComposeAggregateTestCheckFunc(
		resource.TestCheckResourceAttr(address, "business_type", p.businessType),
		resource.TestCheckResourceAttr(address, "default_currency", "usd"),
		resource.TestCheckResourceAttr(address, "business_profile.annual_revenue.amount", fmt.Sprint(p.revenue)),
		resource.TestCheckResourceAttr(address, "business_profile.annual_revenue.fiscal_year_end", p.revenueEnd),
		resource.TestCheckResourceAttr(address, "business_profile.estimated_worker_count", fmt.Sprint(p.workers)),
		resource.TestCheckResourceAttr(address, "business_profile.mcc", p.mcc),
		resource.TestCheckResourceAttr(address, "business_profile.minority_owned_business_designation.#", "2"),
		resource.TestCheckResourceAttr(address, "business_profile.support_address.city", p.city),
		resource.TestCheckResourceAttr(address, "business_profile.support_address.line2", p.line2),
		resource.TestCheckResourceAttr(address, "business_profile.support_email", p.supportEmail),
		resource.TestCheckResourceAttr(address, "business_profile.support_phone", p.supportPhone),
		resource.TestCheckResourceAttr(address, "business_profile.url", p.url),
		resource.TestCheckResourceAttr(address, "settings.card_payments.decline_on.avs_failure", fmt.Sprint(p.declineChecks)),
		resource.TestCheckResourceAttr(address, "settings.invoices.hosted_payment_method_save", p.paymentSave),
		resource.TestCheckResourceAttr(address, "settings.payouts.debit_negative_balances", fmt.Sprint(p.debitNegative)),
		resource.TestCheckResourceAttr(address, "settings.payouts.schedule.interval", p.interval),
		resource.TestCheckResourceAttr(address, "settings.payouts.statement_descriptor", p.payoutDescriptor),
	)
}

// The business profile, the account's own fields and its settings change in place, as the
// suite's probe `update/8` changes each (TestAccountAttributes): the business type, the MCC, the
// revenue and the worker count, which a create alone used to take, among them. The profile and
// the settings go whole, as the probe sent them, and the update never sends the create-only
// fields or the capabilities. The data source reads each requested capability's status, which
// Stripe moves on its own, so only its form is checked.
func TestAccAccount_profileAndSettings(t *testing.T) {
	acct := acctest.NewAccount(t)
	created, updated := createdProfile(acct), updatedProfile(acct)
	var first string
	resource.ParallelTest(t, resource.TestCase{
		ProtoV6ProviderFactories: tfacc.ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: profileConfig(acct, created) + `
data "stripe_account" "test" {
  id = stripe_account.test.id
}
`,
				Check: resource.ComposeAggregateTestCheckFunc(
					idOf(&first),
					profileChecks(created),
					resource.TestCheckResourceAttr(address, "capabilities.transfers.requested", "true"),
					resource.TestMatchResourceAttr("data.stripe_account.test", "capabilities.transfers", regexp.MustCompile(`^(active|inactive|pending)$`)),
					resource.TestCheckResourceAttr("data.stripe_account.test", "business_profile.support_address.postal_code", created.postalCode),
				),
				ConfigPlanChecks: resource.ConfigPlanChecks{PostApplyPostRefresh: []plancheck.PlanCheck{plancheck.ExpectEmptyPlan()}},
			},
			{
				Config: profileConfig(acct, updated),
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply:             []plancheck.PlanCheck{plancheck.ExpectResourceAction(address, plancheck.ResourceActionUpdate)},
					PostApplyPostRefresh: []plancheck.PlanCheck{plancheck.ExpectEmptyPlan()},
				},
				Check: resource.ComposeAggregateTestCheckFunc(
					profileChecks(updated),
					resource.TestCheckResourceAttr(address, "settings.sepa_debit_payments.creditor_id", "AT61ZZZ01234567890"),
					resource.TestCheckResourceAttrWith(address, "id", func(v string) error {
						if v != first {
							return fmt.Errorf("the account ID changed from %s to %s", first, v)
						}
						return nil
					}),
					func(*terraform.State) error {
						if acct.Smoke {
							return nil
						}
						var updates []wire.Object
						for _, entry := range acctest.RequestLog(t, acct) {
							if params, ok := entry.Params.(wire.Object); ok && entry.Method == "POST" && entry.Path == "/v1/accounts/"+first {
								updates = append(updates, params)
							}
						}
						if len(updates) != 1 {
							return fmt.Errorf("%d updates of %s, want 1", len(updates), first)
						}
						for _, createOnly := range []string{"country", "type", "capabilities", "external_account"} {
							if _, sent := updates[0].Get(createOnly); sent {
								return fmt.Errorf("the update sent %s: %v", createOnly, updates[0])
							}
						}
						for _, changed := range []string{"business_type", "business_profile", "settings"} {
							if _, sent := updates[0].Get(changed); !sent {
								return fmt.Errorf("the update did not send %s: %v", changed, updates[0])
							}
						}
						// The changed interval goes alone, as `update/8` sends it: the delay Stripe
						// read for the old schedule is its own, and goes with the interval.
						settings, _ := updates[0].GetObject("settings")
						payouts, _ := settings.GetObject("payouts")
						if schedule, _ := payouts.GetObject("schedule"); fmt.Sprint(schedule) != fmt.Sprint(wire.Object{{Name: "interval", Value: wire.String("daily")}}) {
							return fmt.Errorf("the update sent the payout schedule %v, want the interval alone", schedule)
						}
						return nil
					},
				),
			},
		},
	})
}

// after chains each account the test creates behind the one before it, so that they are created
// one at a time: Stripe refuses more than five account creates a second with a test key
// (`Sorry, you're creating accounts too quickly`), which parallel creates reached on the dev
// sandbox.
func after(previous string) string {
	if previous == "" {
		return ""
	}
	return fmt.Sprintf("  depends_on    = [stripe_account.%s]\n", previous)
}

// scheduleConfig is a custom company account, as the suite's payout schedule probes create it,
// with the given schedule, or none.
func scheduleConfig(c acctest.Config, schedules map[string]string) string {
	out := c.ProviderBlock()
	previous := ""
	for _, name := range []string{"minimum", "days", "monthly", "changed", "added"} {
		settings := ""
		if s := schedules[name]; s != "" {
			settings = fmt.Sprintf("  settings = { payouts = { schedule = %s } }\n", s)
		}
		out += fmt.Sprintf(`
resource "stripe_account" %q {
  type          = "custom"
  country       = "US"
  business_type = "company"
  capabilities  = { transfers = { requested = true } }
%s%s}
`, name, settings, after(previous))
		previous = name
	}
	return out
}

// A payout schedule takes its interval with its anchor or its payout days, each request one of
// the suite's (TestAccountAttributes): a delay of `minimum` (probe `every_field/10`), which Stripe
// answers with the number of days, and weekly payout days (`every_field/11`), which it answers
// in its own order, stay as configured; Stripe derives monthly payout days from the anchor
// (`every_field/12`); an update changes the delay and the anchor (`update/3`) and adds a monthly
// schedule to an account created without one (`isolated/interval_monthly`). A changed anchor
// leaves the payout days to Stripe, so the update sends no payout days beside it.
func TestAccAccount_payoutSchedule(t *testing.T) {
	acct := acctest.NewAccount(t)
	created := map[string]string{
		"minimum": `{ delay_days = "minimum", interval = "weekly", weekly_anchor = "friday" }`,
		"days":    `{ interval = "weekly", weekly_payout_days = ["friday", "monday"] }`,
		"monthly": `{ interval = "monthly", monthly_anchor = 7 }`,
		"changed": `{ delay_days = "7", interval = "weekly", weekly_anchor = "friday" }`,
	}
	updated := map[string]string{}
	for k, v := range created {
		updated[k] = v
	}
	updated["changed"] = `{ delay_days = "8", interval = "weekly", weekly_anchor = "monday" }`
	updated["added"] = `{ interval = "monthly", monthly_anchor = 15 }`
	var changedID string
	resource.ParallelTest(t, resource.TestCase{
		ProtoV6ProviderFactories: tfacc.ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: scheduleConfig(acct, created),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("stripe_account.minimum", "settings.payouts.schedule.delay_days", "minimum"),
					resource.TestCheckResourceAttr("stripe_account.minimum", "settings.payouts.schedule.weekly_payout_days.#", "1"),
					resource.TestCheckResourceAttr("stripe_account.minimum", "settings.payouts.schedule.weekly_payout_days.0", "friday"),
					resource.TestCheckResourceAttr("stripe_account.days", "settings.payouts.schedule.weekly_payout_days.0", "friday"),
					resource.TestCheckResourceAttr("stripe_account.days", "settings.payouts.schedule.weekly_payout_days.1", "monday"),
					resource.TestCheckResourceAttr("stripe_account.monthly", "settings.payouts.schedule.monthly_payout_days.#", "1"),
					resource.TestCheckResourceAttr("stripe_account.monthly", "settings.payouts.schedule.monthly_payout_days.0", "7"),
					resource.TestCheckResourceAttr("stripe_account.changed", "settings.payouts.schedule.delay_days", "7"),
					resource.TestCheckResourceAttrWith("stripe_account.changed", "id", func(v string) error {
						changedID = v
						return nil
					}),
				),
				ConfigPlanChecks: resource.ConfigPlanChecks{PostApplyPostRefresh: []plancheck.PlanCheck{plancheck.ExpectEmptyPlan()}},
			},
			{
				Config: scheduleConfig(acct, updated),
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{
						plancheck.ExpectResourceAction("stripe_account.changed", plancheck.ResourceActionUpdate),
						plancheck.ExpectResourceAction("stripe_account.added", plancheck.ResourceActionUpdate),
						plancheck.ExpectResourceAction("stripe_account.minimum", plancheck.ResourceActionNoop),
					},
					PostApplyPostRefresh: []plancheck.PlanCheck{plancheck.ExpectEmptyPlan()},
				},
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("stripe_account.changed", "settings.payouts.schedule.delay_days", "8"),
					resource.TestCheckResourceAttr("stripe_account.changed", "settings.payouts.schedule.weekly_anchor", "monday"),
					resource.TestCheckResourceAttr("stripe_account.changed", "settings.payouts.schedule.weekly_payout_days.0", "monday"),
					resource.TestCheckResourceAttr("stripe_account.added", "settings.payouts.schedule.interval", "monthly"),
					resource.TestCheckResourceAttr("stripe_account.added", "settings.payouts.schedule.monthly_anchor", "15"),
					func(*terraform.State) error {
						if acct.Smoke {
							return nil
						}
						for _, entry := range acctest.RequestLog(t, acct) {
							params, ok := entry.Params.(wire.Object)
							if !ok || entry.Method != "POST" || entry.Path != "/v1/accounts/"+changedID {
								continue
							}
							settings, _ := params.GetObject("settings")
							payouts, _ := settings.GetObject("payouts")
							schedule, ok := payouts.GetObject("schedule")
							if !ok {
								return fmt.Errorf("the update sent no payout schedule: %v", params)
							}
							if _, sent := schedule.Get("weekly_payout_days"); sent {
								return fmt.Errorf("the update sent payout days beside the changed anchor: %v", schedule)
							}
						}
						return nil
					},
				),
			},
		},
	})
}

// descriptorConfig is a custom company account for each of the suite's descriptor probes: the
// card descriptor prefix and the payments descriptor each sent alone on a create, and on an
// update of an account created without them, which updated holds.
func descriptorConfig(c acctest.Config, updated bool) string {
	prefix, payments := "", ""
	if updated {
		prefix = "  settings      = { card_payments = { statement_descriptor_prefix = \"HARBOUR\" } }\n"
		payments = "  settings      = { payments = { statement_descriptor = \"HARBOUR RIDES\" } }\n"
	}
	out := c.ProviderBlock()
	previous := ""
	for _, a := range []struct{ name, settings string }{
		{"prefix_create", "  settings      = { card_payments = { statement_descriptor_prefix = \"CONFORM\" } }\n"},
		{"payments_create", "  settings      = { payments = { statement_descriptor = \"CONFORMANCE TOURS\" } }\n"},
		{"prefix_update", prefix},
		{"payments_update", payments},
	} {
		out += fmt.Sprintf(`
resource "stripe_account" %q {
  type          = "custom"
  country       = "US"
  business_type = "company"
  capabilities  = { transfers = { requested = true } }
%s%s}
`, a.name, a.settings, after(previous))
		previous = a.name
	}
	return out
}

// Stripe keeps a card descriptor prefix sent alone, and fills it from a payments descriptor sent
// alone, on a create and on an update of an account created without either (TestAccountAttributes,
// probes `isolated/descriptor_prefix_*` and `isolated/descriptor_payments_*`), so an update that
// sets the payments descriptor leaves the prefix to Stripe.
func TestAccAccount_descriptors(t *testing.T) {
	acct := acctest.NewAccount(t)
	const prefix, descriptor = "settings.card_payments.statement_descriptor_prefix", "settings.payments.statement_descriptor"
	resource.ParallelTest(t, resource.TestCase{
		ProtoV6ProviderFactories: tfacc.ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: descriptorConfig(acct, false),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("stripe_account.prefix_create", prefix, "CONFORM"),
					resource.TestCheckResourceAttr("stripe_account.payments_create", descriptor, "CONFORMANCE TOURS"),
					resource.TestCheckResourceAttr("stripe_account.payments_create", prefix, "CONFORMANC"),
				),
				ConfigPlanChecks: resource.ConfigPlanChecks{PostApplyPostRefresh: []plancheck.PlanCheck{plancheck.ExpectEmptyPlan()}},
			},
			{
				Config: descriptorConfig(acct, true),
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{
						plancheck.ExpectUnknownValue("stripe_account.payments_update",
							tfjsonpath.New("settings").AtMapKey("card_payments").AtMapKey("statement_descriptor_prefix")),
					},
					PostApplyPostRefresh: []plancheck.PlanCheck{plancheck.ExpectEmptyPlan()},
				},
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("stripe_account.prefix_update", prefix, "HARBOUR"),
					resource.TestCheckResourceAttr("stripe_account.payments_update", descriptor, "HARBOUR RIDES"),
					resource.TestCheckResourceAttr("stripe_account.payments_update", prefix, "HARBOUR RI"),
				),
			},
		},
	})
}

// numbersConfig is a custom company account for each of three suite probes that create one
// (TestAccountAttributes): the company's registration, tax and VAT numbers (`every_field/21`),
// and the bank account as a hash (`every_field/18`) and as a token (`every_field/19`).
func numbersConfig(c acctest.Config) string {
	out := c.ProviderBlock()
	previous := ""
	for _, a := range []struct{ name, body string }{
		{"numbers", `  company = { registration_number = "REG123456", tax_id = "512345678", vat_id = "VAT123456" }`},
		{"bank_data", `  bank_account_data = {
    account_holder_name = "Jenny Rosen"
    account_holder_type = "company"
    account_number      = "000123456789"
    country             = "US"
    currency            = "usd"
    object              = "bank_account"
    routing_number      = "110000000"
  }`},
		{"bank_token", `  bank_account = "btok_us_verified"`},
	} {
		out += fmt.Sprintf(`
resource "stripe_account" %q {
  type          = "custom"
  country       = "US"
  business_type = "company"
  capabilities  = { transfers = { requested = true } }
%s
%s}
`, a.name, a.body, after(previous))
		previous = a.name
	}
	return out
}

// Stripe takes the company's numbers and a bank account and never returns them, so state keeps
// them from configuration: the tax ID is sensitive, and Stripe says only that one was provided. A
// change of a company number fails the plan, as of the individual's ID number, because the
// record proves no update of it that a read could confirm.
func TestAccAccount_companyNumbersAndBankAccount(t *testing.T) {
	acct := acctest.NewAccount(t)
	resource.ParallelTest(t, resource.TestCase{
		ProtoV6ProviderFactories: tfacc.ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: numbersConfig(acct),
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{
						plancheck.ExpectSensitiveValue("stripe_account.numbers", tfjsonpath.New("company").AtMapKey("tax_id")),
					},
					PostApplyPostRefresh: []plancheck.PlanCheck{plancheck.ExpectEmptyPlan()},
				},
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("stripe_account.numbers", "company.registration_number", "REG123456"),
					resource.TestCheckResourceAttr("stripe_account.numbers", "company.tax_id", "512345678"),
					resource.TestCheckResourceAttr("stripe_account.numbers", "company.vat_id", "VAT123456"),
					resource.TestCheckResourceAttr("stripe_account.bank_data", "bank_account_data.routing_number", "110000000"),
					resource.TestCheckResourceAttr("stripe_account.bank_token", "bank_account", "btok_us_verified"),
				),
			},
			{
				Config:      strings.Replace(numbersConfig(acct), "REG123456", "REG654321", 1),
				ExpectError: regexp.MustCompile(refused("company.registration_number")),
			},
		},
	})
}

// Stripe refuses every account type of a US bank account (the record's rule on
// `bank_account.account_type`, probes `isolated/bank_type_*`), so the plan refuses the probe's
// request on a US account, with Stripe's message, and nothing reaches Stripe.
func TestAccAccount_bankAccountTypeRefused(t *testing.T) {
	acct := acctest.NewAccount(t)
	resource.ParallelTest(t, resource.TestCase{
		ProtoV6ProviderFactories: tfacc.ProviderFactories,
		Steps: []resource.TestStep{{
			Config: customConfig(acct, `  business_type = "company"
  capabilities  = { transfers = { requested = true } }
  bank_account_data = {
    account_number = "000123456789"
    account_type   = "checking"
    country        = "US"
    routing_number = "110000000"
  }
`),
			PlanOnly:    true,
			ExpectError: regexp.MustCompile(`(?s)Stripe refuses this create.*bank_account.*Stripe\s+account\s+in\s+US.*Invalid\s+bank\s+account\s+type.*isolated/bank_type_checking`),
		}},
	})
}
