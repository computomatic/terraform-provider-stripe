package account_test

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
	"github.com/hashicorp/terraform-plugin-testing/tfjsonpath"

	"github.com/computomatic/terraform-provider-stripe/internal/acctest"
	"github.com/computomatic/terraform-provider-stripe/internal/acctest/tfacc"
)

// upload is a stripe_file of a blank PNG of the given side, in pixels, as the suite's inert test
// images are (TestAccountAttributes, batch R02).
type upload struct {
	name, purpose string
	side          int
}

func uploads(t *testing.T, files ...upload) string {
	out := ""
	for _, f := range files {
		var b bytes.Buffer
		if err := png.Encode(&b, image.NewGray(image.Rect(0, 0, f.side, f.side))); err != nil {
			t.Fatal(err)
		}
		path := filepath.Join(t.TempDir(), f.name+".png")
		if err := os.WriteFile(path, b.Bytes(), 0o600); err != nil {
			t.Fatal(err)
		}
		out += fmt.Sprintf("\nresource \"stripe_file\" %q {\n  source  = %q\n  purpose = %q\n}\n", f.name, path, f.purpose)
	}
	return out
}

// companyAccount is the suite's base of batch R02's probes, a US custom company that requests
// `transfers` (`accountCompany`), with the given extra arguments, created after previous.
func companyAccount(name, previous, extra string) string {
	return fmt.Sprintf(`
resource "stripe_account" %q {
  type          = "custom"
  country       = "US"
  business_type = "company"
  capabilities  = { transfers = { requested = true } }
%s%s}
`, name, extra, after(previous))
}

// accountDocuments are the documents a create takes, each on an account of its own as the suite's
// probes `isolated/document_<name>_create` send it; the proof of registration goes only on an update.
var accountDocuments = []string{
	"bank_account_ownership_verification", "company_license", "company_memorandum_of_association", "company_ministerial_decree",
	"company_registration_verification", "company_tax_id_verification", "proof_of_address", "proof_of_ultimate_beneficial_ownership",
}

// The suite's user agents of an acceptance or a declaration, a create's and an update's.
const (
	userAgent       = "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) Conformance/1.0"
	userAgentUpdate = "Mozilla/5.0 (X11; Linux x86_64) Conformance/2.0"
)

// onboardingCreateConfig holds the creates of batch R02's probes, each on an account of its own:
// the branding and the account's terms of service (`isolated/branding` and `isolated/tos`, which an
// update changes where updated), Issuing's and Treasury's terms (`isolated/tos_card_issuing` and
// `isolated/tos_treasury`), each document, a bank account's ownership documents
// (`isolated/bank_account_documents_create`) and each side of the company's verification document
// (`isolated/company_verification_<side>_create`). The terms are dated a day before the test, and
// two where updated, as the probes date them.
func onboardingCreateConfig(t *testing.T, c acctest.Config, now time.Time, updated bool) string {
	day, ip, agent := now.Add(-24*time.Hour).Unix(), "8.8.8.8", userAgent
	icon, logo, primary, secondary := "icon", "logo", "#0a2540", "#635bff"
	if updated {
		day, ip, agent = now.Add(-48*time.Hour).Unix(), "8.8.4.4", userAgentUpdate
		icon, logo, primary, secondary = "icon_update", "logo_update", "#ff5733", "#33c1ff"
	}
	firstDay := now.Add(-24 * time.Hour).Unix()
	out := c.ProviderBlock() + uploads(t,
		upload{"requirement", "account_requirement", 1}, upload{"front", "additional_verification", 2}, upload{"back", "additional_verification", 3},
		upload{"icon", "business_icon", 128}, upload{"logo", "business_logo", 128},
		upload{"icon_update", "business_icon", 129}, upload{"logo_update", "business_logo", 129},
	)
	out += companyAccount("branding", "", fmt.Sprintf(`  settings = { branding = { icon = stripe_file.%s.id, logo = stripe_file.%s.id, primary_color = %q, secondary_color = %q } }
`, icon, logo, primary, secondary))
	out += companyAccount("tos", "branding", fmt.Sprintf("  tos_acceptance = { date = %d, ip = %q, user_agent = %q }\n", day, ip, agent))
	out += fmt.Sprintf(`
resource "stripe_account" "tos_card_issuing" {
  type          = "custom"
  country       = "US"
  business_type = "company"
  capabilities  = { card_issuing = { requested = true }, transfers = { requested = true } }
  settings      = { card_issuing = { tos_acceptance = { date = %[1]d, ip = "8.8.8.8", user_agent = %[2]q } } }
%[3]s}

resource "stripe_account" "tos_treasury" {
  type          = "custom"
  country       = "US"
  business_type = "company"
  capabilities  = { transfers = { requested = true }, treasury = { requested = true } }
  settings      = { treasury = { tos_acceptance = { date = %[1]d, ip = "8.8.8.8", user_agent = %[2]q } } }
%[4]s}
`, firstDay, userAgent, after("tos"), after("tos_card_issuing"))
	previous := "tos_treasury"
	for _, d := range accountDocuments {
		out += companyAccount(d, previous, fmt.Sprintf("  documents = { %s = { files = [stripe_file.requirement.id] } }\n", d))
		previous = d
	}
	out += companyAccount("bank_documents", previous, `  bank_account_data = {
    account_number = "000123456789"
    country        = "US"
    routing_number = "110000000"
    documents      = { bank_account_ownership_verification = { files = [stripe_file.requirement.id] } }
  }
`)
	out += companyAccount("verification_front", "bank_documents", "  company = { verification = { document = { front = stripe_file.front.id } } }\n")
	out += companyAccount("verification_back", "verification_front", "  company = { verification = { document = { back = stripe_file.back.id } } }\n")
	return out
}

// What onboarding writes on a create is taken there, as batch R02's probes write it: the branding,
// the terms of service of the account, of Issuing and of Treasury with their user agents, each
// document's files, a bank account's ownership documents and each side of the company's
// verification document. Stripe never returns the documents, so state keeps them from
// configuration. The branding and the account's terms change in place, as the probes change them.
func TestAccAccount_onboardingOnCreate(t *testing.T) {
	acct := acctest.NewAccount(t)
	now := time.Now()
	checks := []resource.TestCheckFunc{
		resource.TestCheckResourceAttrPair("stripe_account.branding", "settings.branding.icon", "stripe_file.icon", "id"),
		resource.TestCheckResourceAttr("stripe_account.branding", "settings.branding.primary_color", "#0a2540"),
		resource.TestCheckResourceAttr("stripe_account.tos", "tos_acceptance.user_agent", userAgent),
		resource.TestCheckResourceAttr("stripe_account.tos_card_issuing", "settings.card_issuing.tos_acceptance.user_agent", userAgent),
		resource.TestCheckResourceAttr("stripe_account.tos_treasury", "settings.treasury.tos_acceptance.user_agent", userAgent),
		resource.TestCheckResourceAttrPair("stripe_account.bank_documents", "bank_account_data.documents.bank_account_ownership_verification.files.0", "stripe_file.requirement", "id"),
		resource.TestCheckResourceAttrPair("stripe_account.verification_front", "company.verification.document.front", "stripe_file.front", "id"),
		resource.TestCheckResourceAttrPair("stripe_account.verification_back", "company.verification.document.back", "stripe_file.back", "id"),
	}
	for _, d := range accountDocuments {
		checks = append(checks, resource.TestCheckResourceAttrPair("stripe_account."+d, "documents."+d+".files.0", "stripe_file.requirement", "id"))
	}
	resource.ParallelTest(t, resource.TestCase{
		ProtoV6ProviderFactories: tfacc.ProviderFactories,
		Steps: acctest.Forget(t, []resource.TestStep{
			{
				Config:           onboardingCreateConfig(t, acct, now, false),
				Check:            resource.ComposeAggregateTestCheckFunc(checks...),
				ConfigPlanChecks: resource.ConfigPlanChecks{PostApplyPostRefresh: []plancheck.PlanCheck{plancheck.ExpectEmptyPlan()}},
			},
			{
				Config: onboardingCreateConfig(t, acct, now, true),
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{
						plancheck.ExpectResourceAction("stripe_account.branding", plancheck.ResourceActionUpdate),
						plancheck.ExpectResourceAction("stripe_account.tos", plancheck.ResourceActionUpdate),
					},
					PostApplyPostRefresh: []plancheck.PlanCheck{plancheck.ExpectEmptyPlan()},
				},
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttrPair("stripe_account.branding", "settings.branding.logo", "stripe_file.logo_update", "id"),
					resource.TestCheckResourceAttr("stripe_account.branding", "settings.branding.secondary_color", "#33c1ff"),
					resource.TestCheckResourceAttr("stripe_account.tos", "tos_acceptance.ip", "8.8.4.4"),
					resource.TestCheckResourceAttr("stripe_account.tos", "tos_acceptance.user_agent", userAgentUpdate),
				),
			},
		}, nil, "stripe_file"),
	})
}

// onboardingUpdateConfig holds batch R02's probes that write on an update of a plain company:
// every document's files (`isolated/documents_update`), both sides of the company's verification
// document (`isolated/company_verification_update`) and the representative declaration
// (`isolated/representative_declaration_update`), which Stripe takes once the company has a
// representative with a name, which the probe adds first (`addRepresentative`).
func onboardingUpdateConfig(t *testing.T, c acctest.Config, now time.Time, updated bool) string {
	documents, verification, declaration := "", "", ""
	if updated {
		documents = "  documents = {\n"
		for _, d := range append([]string{"proof_of_registration"}, accountDocuments...) {
			documents += fmt.Sprintf("    %s = { files = [stripe_file.requirement_update.id] }\n", d)
		}
		documents += "  }\n"
		verification = "  company = { verification = { document = { back = stripe_file.back.id, front = stripe_file.front.id } } }\n"
		declaration = fmt.Sprintf("  company = { representative_declaration = { date = %d, ip = \"8.8.4.4\", user_agent = %q } }\n", now.Add(-48*time.Hour).Unix(), userAgentUpdate)
	}
	return c.ProviderBlock() + uploads(t,
		upload{"requirement_update", "account_requirement", 1}, upload{"front", "additional_verification", 2}, upload{"back", "additional_verification", 3},
	) + companyAccount("documents", "", documents) + companyAccount("verification", "documents", verification) +
		companyAccount("declaration", "verification", declaration) + `
resource "stripe_account_person" "representative" {
  account      = stripe_account.declaration.id
  first_name   = "Jenny"
  last_name    = "Rosen"
  relationship = { representative = true, title = "CEO" }
}
`
}

// What onboarding writes on an update of an account created without it is taken there, as batch
// R02's probes write it: every document's files, the proof of registration's among them, which only
// an update takes, the company's verification document and, once the company has a representative,
// the representative declaration, which a create refuses.
func TestAccAccount_onboardingOnUpdate(t *testing.T) {
	acct := acctest.NewAccount(t)
	now := time.Now()
	resource.ParallelTest(t, resource.TestCase{
		ProtoV6ProviderFactories: tfacc.ProviderFactories,
		Steps: acctest.Forget(t, []resource.TestStep{
			{Config: onboardingUpdateConfig(t, acct, now, false)},
			{
				Config: onboardingUpdateConfig(t, acct, now, true),
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{
						plancheck.ExpectResourceAction("stripe_account.documents", plancheck.ResourceActionUpdate),
						plancheck.ExpectResourceAction("stripe_account.verification", plancheck.ResourceActionUpdate),
						plancheck.ExpectResourceAction("stripe_account.declaration", plancheck.ResourceActionUpdate),
					},
					PostApplyPostRefresh: []plancheck.PlanCheck{plancheck.ExpectEmptyPlan()},
				},
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttrPair("stripe_account.documents", "documents.proof_of_registration.files.0", "stripe_file.requirement_update", "id"),
					resource.TestCheckResourceAttrPair("stripe_account.documents", "documents.company_license.files.0", "stripe_file.requirement_update", "id"),
					resource.TestCheckResourceAttrPair("stripe_account.verification", "company.verification.document.front", "stripe_file.front", "id"),
					resource.TestCheckResourceAttr("stripe_account.declaration", "company.representative_declaration.ip", "8.8.4.4"),
					resource.TestCheckResourceAttr("stripe_account.declaration", "company.representative_declaration.user_agent", userAgentUpdate),
				),
			},
		}, nil, "stripe_file"),
	})
}

// The plan refuses a representative declaration on a create, as Stripe refused each member sent
// alone (probes `isolated/representative_declaration_ip_create` and `_user_agent_create`).
func TestAccAccount_representativeDeclarationOnCreate(t *testing.T) {
	acct := acctest.NewAccount(t)
	resource.ParallelTest(t, resource.TestCase{
		ProtoV6ProviderFactories: tfacc.ProviderFactories,
		Steps: []resource.TestStep{{
			Config:      acct.ProviderBlock() + companyAccount("test", "", "  company = { representative_declaration = { ip = \"8.8.8.8\" } }\n"),
			PlanOnly:    true,
			ExpectError: regexp.MustCompile(`only allowed during account updates`),
		}},
	})
}

// japaneseConfig holds the suite's probes of a Japanese account (batch C1): a company with its kana
// and kanji names and addresses (`isolated/jp_company`), an individual with its
// (`isolated/jp_individual`) and a company with its kana and kanji descriptors
// (`isolated/jp_descriptors`), each created with the probe's create or, where updated, changed to
// its update. The individual also sets the latin names of the suite's individuals (`Jenny Rosen`),
// which the probe leaves out and no update changes: on the dev
// sandbox Stripe filled them from the kanji names, which the record does not show (a suite
// question), and the provider plans a removed latin name as cleared, as the record proves.
func japaneseConfig(c acctest.Config, updated bool) string {
	kana := `{ city = "ｼﾌﾞﾔ", country = "JP", line1 = "27-15", line2 = "ﾃｽﾄﾋﾞﾙ 2F", postal_code = "1500001", state = "ﾄｳｷﾖｳﾄ", town = "ｼﾞﾝｸﾞｳﾏｴ 3-" }`
	kanji := `{ city = "渋谷区", country = "JP", line1 = "２７－１５", line2 = "テストビル　２階", postal_code = "１５００００１", state = "東京都", town = "神宮前　３丁目" }`
	company := `name_kana = "ｺﾝﾌｫｰﾏﾝｽ ｶﾌﾞｼｷｶﾞｲｼﾔ", name_kanji = "コンフォーマンス株式会社"`
	individual := `first_name = "Jenny", first_name_kana = "ハナコ", first_name_kanji = "花子", last_name = "Rosen", last_name_kana = "ヤマダ", last_name_kanji = "山田"`
	descriptors := `card_payments = { statement_descriptor_prefix_kana = "ｺﾝﾌｫ", statement_descriptor_prefix_kanji = "コンフォ" }
    payments      = { statement_descriptor_kana = "ｺﾝﾌｫｰﾏﾝｽ", statement_descriptor_kanji = "コンフォーマンス" }`
	if updated {
		kana = `{ city = "ｵｵｻｶｼｷﾀｸ", country = "JP", line1 = "1-1", line2 = "ｻﾝﾌﾟﾙﾋﾞﾙ 3F", postal_code = "5300001", state = "ｵｵｻｶﾌ", town = "ｳﾒﾀﾞ 1-" }`
		kanji = `{ city = "大阪市北区", country = "JP", line1 = "１－１", line2 = "サンプルビル　３階", postal_code = "５３００００１", state = "大阪府", town = "梅田　１丁目" }`
		company = `name_kana = "ﾃｽﾄ ｶﾌﾞｼｷｶﾞｲｼﾔ", name_kanji = "テスト株式会社"`
		individual = `first_name = "Jenny", first_name_kana = "タロウ", first_name_kanji = "太郎", last_name = "Rosen", last_name_kana = "サトウ", last_name_kanji = "佐藤"`
		descriptors = `card_payments = { statement_descriptor_prefix_kana = "ﾃｽﾄ", statement_descriptor_prefix_kanji = "テスト" }
    payments      = { statement_descriptor_kana = "ﾃｽﾄﾃﾝﾎﾟ", statement_descriptor_kanji = "テスト店舗" }`
	}
	return c.ProviderBlock() + fmt.Sprintf(`
resource "stripe_account" "company" {
  type          = "custom"
  country       = "JP"
  business_type = "company"
  capabilities  = { card_payments = { requested = true }, transfers = { requested = true } }
  company = {
    address_kana  = %[1]s
    address_kanji = %[2]s
    %[3]s
  }
}

resource "stripe_account" "individual" {
  type          = "custom"
  country       = "JP"
  business_type = "individual"
  capabilities  = { card_payments = { requested = true }, transfers = { requested = true } }
  individual = {
    address_kana  = %[1]s
    address_kanji = %[2]s
    %[4]s
  }
  depends_on = [stripe_account.company]
}

resource "stripe_account" "descriptors" {
  type          = "custom"
  country       = "JP"
  business_type = "company"
  capabilities  = { card_payments = { requested = true }, transfers = { requested = true } }
  settings = {
    %[5]s
  }
  depends_on = [stripe_account.individual]
}
`, kana, kanji, company, individual, descriptors)
}

// A Japanese account's kana and kanji names, addresses and descriptors are taken on create and
// change in place, as the suite's probes change them; the addresses go whole.
func TestAccAccount_japaneseNames(t *testing.T) {
	acct := acctest.NewAccount(t)
	resource.ParallelTest(t, resource.TestCase{
		ProtoV6ProviderFactories: tfacc.ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: japaneseConfig(acct, false),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("stripe_account.company", "company.address_kana.town", "ｼﾞﾝｸﾞｳﾏｴ 3-"),
					resource.TestCheckResourceAttr("stripe_account.company", "company.name_kanji", "コンフォーマンス株式会社"),
					resource.TestCheckResourceAttr("stripe_account.individual", "individual.address_kanji.line2", "テストビル　２階"),
					resource.TestCheckResourceAttr("stripe_account.individual", "individual.last_name_kana", "ヤマダ"),
					resource.TestCheckResourceAttr("stripe_account.descriptors", "settings.payments.statement_descriptor_kanji", "コンフォーマンス"),
					resource.TestCheckResourceAttr("stripe_account.descriptors", "settings.card_payments.statement_descriptor_prefix_kana", "ｺﾝﾌｫ"),
				),
				ConfigPlanChecks: resource.ConfigPlanChecks{PostApplyPostRefresh: []plancheck.PlanCheck{plancheck.ExpectEmptyPlan()}},
			},
			{
				Config: japaneseConfig(acct, true),
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{
						plancheck.ExpectResourceAction("stripe_account.company", plancheck.ResourceActionUpdate),
						plancheck.ExpectResourceAction("stripe_account.individual", plancheck.ResourceActionUpdate),
						plancheck.ExpectResourceAction("stripe_account.descriptors", plancheck.ResourceActionUpdate),
						// Stripe copies an individual's script addresses into the company's.
						plancheck.ExpectUnknownValue("stripe_account.individual", tfjsonpath.New("company").AtMapKey("address_kana")),
						plancheck.ExpectUnknownValue("stripe_account.individual", tfjsonpath.New("company").AtMapKey("address_kanji")),
					},
					PostApplyPostRefresh: []plancheck.PlanCheck{plancheck.ExpectEmptyPlan()},
				},
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("stripe_account.company", "company.address_kanji.city", "大阪市北区"),
					resource.TestCheckResourceAttr("stripe_account.company", "company.name_kana", "ﾃｽﾄ ｶﾌﾞｼｷｶﾞｲｼﾔ"),
					resource.TestCheckResourceAttr("stripe_account.individual", "individual.first_name_kanji", "太郎"),
					resource.TestCheckResourceAttr("stripe_account.descriptors", "settings.payments.statement_descriptor_kana", "ﾃｽﾄﾃﾝﾎﾟ"),
				),
			},
		},
	})
}

// countryCapabilities are the suite's probes `isolated/capability_<name>_create`
// (TestAccountAttributes): a custom company in a country that offers the capability, created
// requesting it beside `card_payments` and `transfers`.
var countryCapabilities = []struct{ capability, country string }{
	{"alma_payments", "FR"}, {"au_becs_debit_payments", "AU"}, {"bacs_debit_payments", "GB"}, {"billie_payments", "DE"},
	{"bizum_payments", "ES"}, {"blik_payments", "PL"}, {"fpx_payments", "MY"}, {"grabpay_payments", "SG"},
	{"jp_bank_transfer_payments", "JP"}, {"mobilepay_payments", "DK"}, {"oxxo_payments", "CA"}, {"pay_by_bank_payments", "GB"},
	{"paynow_payments", "SG"}, {"payto_payments", "AU"}, {"promptpay_payments", "TH"}, {"revolut_pay_payments", "GB"},
	{"sequra_payments", "ES"}, {"twint_payments", "CH"}, {"zip_payments", "AU"},
}

// Each country's payment method capability is requested on create, as the suite's probes request
// it, and the data source reads its status, which Stripe moves on its own, so only its form is
// checked.
func TestAccAccount_countryCapabilities(t *testing.T) {
	acct := acctest.NewAccount(t)
	status := regexp.MustCompile(`^(active|inactive|pending)$`)
	config := acct.ProviderBlock()
	var checks []resource.TestCheckFunc
	previous := ""
	for _, cc := range countryCapabilities {
		config += fmt.Sprintf(`
resource "stripe_account" %[1]q {
  type          = "custom"
  country       = %[2]q
  business_type = "company"
  capabilities  = { card_payments = { requested = true }, transfers = { requested = true }, %[1]s = { requested = true } }
%[3]s}

data "stripe_account" %[1]q {
  id = stripe_account.%[1]s.id
}
`, cc.capability, cc.country, after(previous))
		previous = cc.capability
		checks = append(checks, resource.TestMatchResourceAttr("data.stripe_account."+cc.capability, "capabilities."+cc.capability, status))
	}
	resource.ParallelTest(t, resource.TestCase{
		ProtoV6ProviderFactories: tfacc.ProviderFactories,
		Steps: []resource.TestStep{{
			Config:           config,
			Check:            resource.ComposeAggregateTestCheckFunc(checks...),
			ConfigPlanChecks: resource.ConfigPlanChecks{PostApplyPostRefresh: []plancheck.PlanCheck{plancheck.ExpectEmptyPlan()}},
		}},
	})
}
