package account_test

import (
	"fmt"
	"regexp"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/plancheck"

	"github.com/computomatic/terraform-provider-stripe/internal/acctest"
	"github.com/computomatic/terraform-provider-stripe/internal/acctest/tfacc"
	"github.com/computomatic/terraform-provider-stripe/internal/wire"
)

// The token and the tax ID of the suite's sweep (TestAccountAttributes, `accountUnknownToken` and
// `accountUnknownTaxID`), which no one made.
const (
	unknownToken = "ct_1AttrsNoSuchAccountToken"
	unknownTaxID = "atxi_1AttrsNoSuchTaxId"
)

// controlledAccount is the base of the suite's token probes, a US account whose controller
// properties leave everything to the platform and that requests `transfers` (`accountControlled`),
// with the given extra arguments.
func controlledAccount(name, extra string) string {
	return fmt.Sprintf(`
resource "stripe_account" %q {
  country = "US"
  controller = {
    fees                   = { payer = "application" }
    losses                 = { payments = "application" }
    requirement_collection = "application"
    stripe_dashboard       = { type = "none" }
  }
  capabilities = { transfers = { requested = true } }
%s}
`, name, extra)
}

// Stripe refuses a token it cannot find on create (probe `isolated/account_token_create`), and the
// plan leaves that to Stripe, which the apply then reports.
func TestAccAccount_accountTokenOnCreate(t *testing.T) {
	acct := acctest.NewAccount(t)
	resource.ParallelTest(t, resource.TestCase{
		ProtoV6ProviderFactories: tfacc.ProviderFactories,
		Steps: []resource.TestStep{{
			Config:      acct.ProviderBlock() + controlledAccount("test", fmt.Sprintf("  account_token = %q\n", unknownToken)),
			ExpectError: regexp.MustCompile(`No such token`),
		}},
	})
}

// Stripe refuses a token it cannot find on an update (probe `isolated/account_token_update`), and
// a tax ID the account does not have as an invoice default (`isolated/default_tax_ids_update`); the
// plan leaves both to Stripe, which the apply then reports.
func TestAccAccount_unknownObjectsOnUpdate(t *testing.T) {
	acct := acctest.NewAccount(t)
	taxIDs := fmt.Sprintf("  settings      = { invoices = { default_account_tax_ids = [%q] } }\n", unknownTaxID)
	resource.ParallelTest(t, resource.TestCase{
		ProtoV6ProviderFactories: tfacc.ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config:           acct.ProviderBlock() + controlledAccount("token", "") + companyAccount("tax", "token", ""),
				ConfigPlanChecks: resource.ConfigPlanChecks{PostApplyPostRefresh: []plancheck.PlanCheck{plancheck.ExpectEmptyPlan()}},
			},
			{
				Config:      acct.ProviderBlock() + controlledAccount("token", fmt.Sprintf("  account_token = %q\n", unknownToken)) + companyAccount("tax", "token", ""),
				ExpectError: regexp.MustCompile(`No such token`),
			},
			{
				Config:      acct.ProviderBlock() + controlledAccount("token", "") + companyAccount("tax", "token", taxIDs),
				ExpectError: regexp.MustCompile(`No such account_tax_ids`),
			},
		},
	})
}

// A US company takes the `full` service agreement on create and on update (probes
// `isolated/service_agreement_full` and `isolated/service_agreement_full_update`). Stripe never
// returns it, so state keeps it, and the update sends it.
func TestAccAccount_serviceAgreement(t *testing.T) {
	acct := acctest.NewAccount(t)
	full := "  tos_acceptance = { service_agreement = \"full\" }\n"
	path := new(string)
	config := func(later string) string {
		return acct.ProviderBlock() + companyAccount("created", "", full) + companyAccount("updated", "created", later)
	}
	resource.ParallelTest(t, resource.TestCase{
		ProtoV6ProviderFactories: tfacc.ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: config(""),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("stripe_account.created", "tos_acceptance.service_agreement", "full"),
					resource.TestCheckNoResourceAttr("stripe_account.updated", "tos_acceptance.service_agreement"),
					resource.TestCheckResourceAttrWith("stripe_account.updated", "id", func(v string) error {
						*path = "/v1/accounts/" + v
						return nil
					}),
				),
				ConfigPlanChecks: resource.ConfigPlanChecks{PostApplyPostRefresh: []plancheck.PlanCheck{plancheck.ExpectEmptyPlan()}},
			},
			{
				Config: config(full),
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{
						plancheck.ExpectResourceAction("stripe_account.created", plancheck.ResourceActionNoop),
						plancheck.ExpectResourceAction("stripe_account.updated", plancheck.ResourceActionUpdate),
					},
					PostApplyPostRefresh: []plancheck.PlanCheck{plancheck.ExpectEmptyPlan()},
				},
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("stripe_account.updated", "tos_acceptance.service_agreement", "full"),
					tfacc.Sent(t, acct, "POST", path, map[string]string{"tos_acceptance.service_agreement": "full"}),
				),
			},
		},
	})
}

// countryCompany is the suite's base of a company outside the US (`accountCountryCompany`), a
// custom company requesting `card_payments` and `transfers`, with the given extra arguments,
// created after previous.
func countryCompany(name, country, previous, extra string) string {
	return fmt.Sprintf(`
resource "stripe_account" %q {
  type          = "custom"
  country       = %q
  business_type = "company"
  capabilities  = { card_payments = { requested = true }, transfers = { requested = true } }
%s%s}
`, name, country, extra, after(previous))
}

// A Singapore company takes its ownership exemption reason on create and on update and reads it
// back (probes `isolated/ownership_exemption_sg_create` and `_update`), with the value Stripe's
// changelog gives as its example. A US account refuses it when you apply, which no test here sends.
func TestAccAccount_ownershipExemptionReason(t *testing.T) {
	acct := acctest.NewAccount(t)
	reason := "  company = { ownership_exemption_reason = \"qualified_entity_exceeds_ownership_threshold\" }\n"
	config := func(later string) string {
		return acct.ProviderBlock() + countryCompany("created", "SG", "", reason) + countryCompany("updated", "SG", "created", later)
	}
	resource.ParallelTest(t, resource.TestCase{
		ProtoV6ProviderFactories: tfacc.ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: config(""),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("stripe_account.created", "company.ownership_exemption_reason", "qualified_entity_exceeds_ownership_threshold"),
					resource.TestCheckNoResourceAttr("stripe_account.updated", "company.ownership_exemption_reason"),
				),
				ConfigPlanChecks: resource.ConfigPlanChecks{PostApplyPostRefresh: []plancheck.PlanCheck{plancheck.ExpectEmptyPlan()}},
			},
			{
				Config: config(reason),
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{
						plancheck.ExpectResourceAction("stripe_account.created", plancheck.ResourceActionNoop),
						plancheck.ExpectResourceAction("stripe_account.updated", plancheck.ResourceActionUpdate),
					},
					PostApplyPostRefresh: []plancheck.PlanCheck{plancheck.ExpectEmptyPlan()},
				},
				Check: resource.TestCheckResourceAttr("stripe_account.updated", "company.ownership_exemption_reason", "qualified_entity_exceeds_ownership_threshold"),
			},
		},
	})
}

// An individual's reads the sweep proves: a Thai individual created with a secondary ID number reads
// that Stripe has it (state `individual_secondary_id_th`), with the number of the suite's Thai
// persons (`personTHSecondaryID`), and a US individual reads the nationality the persons API gives
// its own person (state `individual_profile`), as a refresh then shows.
func TestAccAccount_individualReads(t *testing.T) {
	acct := acctest.NewAccount(t)
	config := acct.ProviderBlock() + `
resource "stripe_account" "thai" {
  type          = "custom"
  country       = "TH"
  business_type = "individual"
  capabilities  = { card_payments = { requested = true }, transfers = { requested = true } }
  individual    = { first_name = "Jenny", last_name = "Rosen", id_number_secondary = "AB0000000000" }
}

resource "stripe_account" "profile" {
  type          = "custom"
  country       = "US"
  business_type = "individual"
  capabilities  = { transfers = { requested = true } }
  individual    = { first_name = "Jenny", last_name = "Rosen" }
  depends_on    = [stripe_account.thai]
}

data "stripe_account" "profile" {
  id = stripe_account.profile.id
}
`
	var account, person string
	resource.ParallelTest(t, resource.TestCase{
		ProtoV6ProviderFactories: tfacc.ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: config,
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("stripe_account.thai", "individual.id_number_secondary_provided", "true"),
					resource.TestCheckNoResourceAttr("stripe_account.profile", "individual.nationality"),
					resource.TestCheckResourceAttrWith("stripe_account.profile", "id", func(v string) error {
						account = v
						return nil
					}),
					resource.TestCheckResourceAttrWith("stripe_account.profile", "individual.id", func(v string) error {
						person = v
						return nil
					}),
				),
				ConfigPlanChecks: resource.ConfigPlanChecks{PostApplyPostRefresh: []plancheck.PlanCheck{plancheck.ExpectEmptyPlan()}},
			},
			{
				PreConfig: func() {
					tfacc.Post(t, acct, "/v1/accounts/"+account+"/persons/"+person, wire.Object{{Name: "nationality", Value: wire.String("IE")}})
				},
				Config: config,
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{plancheck.ExpectEmptyPlan()},
				},
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("stripe_account.profile", "individual.nationality", "IE"),
					resource.TestCheckResourceAttr("data.stripe_account.profile", "individual.nationality", "IE"),
				),
			},
		},
	})
}
