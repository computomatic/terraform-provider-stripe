package account_person_test

import (
	"bytes"
	"fmt"
	"image"
	"image/png"
	"os"
	"path/filepath"
	"regexp"
	"testing"
	"time"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/plancheck"

	"github.com/computomatic/terraform-provider-stripe/internal/acctest"
	"github.com/computomatic/terraform-provider-stripe/internal/acctest/tfacc"
)

// requirementFiles uploads the two inert `account_requirement` files the suite's document probe
// names, one for the create and one for the update (TestAccountPersonAttributes, batch R02).
func requirementFiles(t *testing.T) string {
	out := ""
	for i, name := range []string{"requirement", "requirement_update"} {
		var b bytes.Buffer
		if err := png.Encode(&b, image.NewGray(image.Rect(0, 0, i+1, i+1))); err != nil {
			t.Fatal(err)
		}
		path := filepath.Join(t.TempDir(), name+".png")
		if err := os.WriteFile(path, b.Bytes(), 0o600); err != nil {
			t.Fatal(err)
		}
		out += fmt.Sprintf("\nresource \"stripe_file\" %q {\n  source  = %q\n  purpose = \"account_requirement\"\n}\n", name, path)
	}
	return out
}

// The suite's user agents of an acceptance, a create's and an update's.
const (
	userAgent       = "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) Conformance/1.0"
	userAgentUpdate = "Mozilla/5.0 (X11; Linux x86_64) Conformance/2.0"
)

// onboardingConfig holds the persons of batch R02's probes: on a US custom company
// (`companyAccountForm`), a person with every document (`isolated/documents`), one with a
// nationality (`isolated/nationality_create`), one given one by an update
// (`isolated/nationality_update`) and one with the CFPB data (`isolated/cfpb`); on a company with a
// representative (`representedCompanyAccount`), a legal guardian with the account's additional
// terms (`isolated/terms`), and on another such company, one made a legal guardian by an update
// (`isolated/legal_guardian_update`): Stripe allows an account one legal guardian, and each probe
// adds its own. Where updated, each holds the probe's update.
func onboardingConfig(t *testing.T, c acctest.Config, now time.Time, updated bool) string {
	file, nationality, terms := "requirement", "", fmt.Sprintf(`{ date = %d, ip = "8.8.8.8", user_agent = %q }`, now.Add(-24*time.Hour).Unix(), userAgent)
	cfpb := `{
    ethnicity_details      = { ethnicity = ["other_hispanic_or_latino"], ethnicity_other = "Dominican" }
    race_details           = { race = ["other_asian"], race_other = "Hmong" }
    self_identified_gender = "Woman"
  }`
	guardian := ""
	if updated {
		file, nationality = "requirement_update", `  nationality = "GB"`
		terms = fmt.Sprintf(`{ date = %d, ip = "8.8.4.4", user_agent = %q }`, now.Add(-48*time.Hour).Unix(), userAgentUpdate)
		cfpb = `{
    ethnicity_details      = { ethnicity = ["mexican", "other_hispanic_or_latino"], ethnicity_other = "Salvadoran" }
    race_details           = { race = ["chinese", "other_asian"], race_other = "Laotian" }
    self_identified_gender = "Non-binary"
  }`
		guardian = "  relationship = { legal_guardian = true }"
	}
	return c.ProviderBlock() + requirementFiles(t) + fmt.Sprintf(`
resource "stripe_account" "company" {
  type          = "custom"
  country       = "US"
  business_type = "company"
  capabilities  = { transfers = { requested = true } }
  company       = { name = %[1]q }
}

resource "stripe_account" "represented" {
  type          = "custom"
  country       = "US"
  business_type = "company"
  capabilities  = { transfers = { requested = true } }
  company       = { name = %[2]q }
  depends_on    = [stripe_account.company]
}

resource "stripe_account_person" "representative" {
  account      = stripe_account.represented.id
  first_name   = "Jenny"
  last_name    = "Rosen"
  relationship = { representative = true, title = "CEO" }
}

resource "stripe_account" "guarded" {
  type          = "custom"
  country       = "US"
  business_type = "company"
  capabilities  = { transfers = { requested = true } }
  company       = { name = %[8]q }
  depends_on    = [stripe_account.represented]
}

resource "stripe_account_person" "guarded_representative" {
  account      = stripe_account.guarded.id
  first_name   = "Jenny"
  last_name    = "Rosen"
  relationship = { representative = true, title = "CEO" }
}

resource "stripe_account_person" "documents" {
  account = stripe_account.company.id
  documents = {
    company_authorization = { files = [stripe_file.%[3]s.id] }
    passport              = { files = [stripe_file.%[3]s.id] }
    visa                  = { files = [stripe_file.%[3]s.id] }
  }
}

resource "stripe_account_person" "nationality" {
  account     = stripe_account.company.id
  nationality = "US"
}

resource "stripe_account_person" "nationality_added" {
  account = stripe_account.company.id
%[4]s
}

resource "stripe_account_person" "cfpb" {
  account      = stripe_account.company.id
  us_cfpb_data = %[5]s
}

resource "stripe_account_person" "terms" {
  account                    = stripe_account.represented.id
  relationship               = { legal_guardian = true }
  additional_tos_acceptances = { account = %[6]s }
  depends_on                 = [stripe_account_person.representative]
}

resource "stripe_account_person" "guardian" {
  account    = stripe_account.guarded.id
%[7]s
  depends_on = [stripe_account_person.guarded_representative]
}
`, c.Name("Conformance company"), c.Name("Conformance represented"), file, nationality, cfpb, terms, guardian, c.Name("Conformance guarded"))
}

// What onboarding writes on a person is taken on create and changes in place, as batch R02's
// probes write it: the documents, which Stripe never returns, so state keeps them from
// configuration; the nationality; the CFPB data, whose lists Stripe returns as sent; and, on a
// company with a representative, a legal guardian and the account's additional terms of service.
func TestAccAccountPerson_onboarding(t *testing.T) {
	acct := acctest.NewAccount(t)
	now := time.Now()
	var updates []plancheck.PlanCheck
	for _, name := range []string{"documents", "nationality_added", "cfpb", "terms", "guardian"} {
		updates = append(updates, plancheck.ExpectResourceAction("stripe_account_person."+name, plancheck.ResourceActionUpdate))
	}
	resource.ParallelTest(t, resource.TestCase{
		ProtoV6ProviderFactories: tfacc.ProviderFactories,
		Steps: acctest.Forget(t, []resource.TestStep{
			{
				Config: onboardingConfig(t, acct, now, false),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttrPair("stripe_account_person.documents", "documents.passport.files.0", "stripe_file.requirement", "id"),
					resource.TestCheckResourceAttr("stripe_account_person.nationality", "nationality", "US"),
					resource.TestCheckResourceAttr("stripe_account_person.cfpb", "us_cfpb_data.race_details.race_other", "Hmong"),
					resource.TestCheckResourceAttr("stripe_account_person.cfpb", "us_cfpb_data.ethnicity_details.ethnicity.0", "other_hispanic_or_latino"),
					resource.TestCheckResourceAttr("stripe_account_person.terms", "relationship.legal_guardian", "true"),
					resource.TestCheckResourceAttr("stripe_account_person.terms", "additional_tos_acceptances.account.user_agent", userAgent),
				),
				ConfigPlanChecks: resource.ConfigPlanChecks{PostApplyPostRefresh: []plancheck.PlanCheck{plancheck.ExpectEmptyPlan()}},
			},
			{
				Config: onboardingConfig(t, acct, now, true),
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply:             updates,
					PostApplyPostRefresh: []plancheck.PlanCheck{plancheck.ExpectEmptyPlan()},
				},
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttrPair("stripe_account_person.documents", "documents.visa.files.0", "stripe_file.requirement_update", "id"),
					resource.TestCheckResourceAttr("stripe_account_person.nationality_added", "nationality", "GB"),
					resource.TestCheckResourceAttr("stripe_account_person.cfpb", "us_cfpb_data.self_identified_gender", "Non-binary"),
					resource.TestCheckResourceAttr("stripe_account_person.cfpb", "us_cfpb_data.race_details.race.0", "chinese"),
					resource.TestCheckResourceAttr("stripe_account_person.terms", "additional_tos_acceptances.account.ip", "8.8.4.4"),
					resource.TestCheckResourceAttr("stripe_account_person.guardian", "relationship.legal_guardian", "true"),
				),
			},
		}, nil, "stripe_file"),
	})
}

// The plan refuses the authorizer role on a US company's person, as Stripe refused it (probe
// `isolated/authorizer_create`).
func TestAccAccountPerson_authorizerRefused(t *testing.T) {
	acct := acctest.NewAccount(t)
	resource.ParallelTest(t, resource.TestCase{
		ProtoV6ProviderFactories: tfacc.ProviderFactories,
		Steps: []resource.TestStep{{
			Config: acct.ProviderBlock() + accountBlock(acct, "test", "authorizer") + `
resource "stripe_account_person" "test" {
  account      = stripe_account.test.id
  relationship = { authorizer = true }
}
`,
			PlanOnly:    true,
			ExpectError: regexp.MustCompile(`authorizer role is not supported`),
		}},
	})
}
