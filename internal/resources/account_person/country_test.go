package account_person_test

import (
	"fmt"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/plancheck"

	"github.com/computomatic/terraform-provider-stripe/internal/acctest"
	"github.com/computomatic/terraform-provider-stripe/internal/acctest/tfacc"
)

// countryCompany is a custom company account in country, as the fake's conformance run creates
// the suite's tagged country accounts (`country_jp_company`, `country_th_company`): requesting
// card payments and transfers.
func countryCompany(country string) string {
	return fmt.Sprintf(`
resource "stripe_account" "company" {
  type          = "custom"
  country       = %q
  business_type = "company"
  capabilities  = { card_payments = { requested = true }, transfers = { requested = true } }
}
`, country)
}

// japanesePerson is a person of the Japanese company with the suite's probe values (batch C1,
// `isolated/jp`): Hanako Yamada at 27-15 Jingumae 3-chome, Shibuya-ku, with the second lines, or,
// updated, Taro Sato at 1-1 Umeda 1-chome, Kita-ku, Osaka, every address member but the country
// changed. cleared leaves out the names and every address member but the country, which the
// probe `isolated/jp_unset` sends empty in one update and Stripe then holds as null. Every stage
// also sets the latin names the suite's JP company onboarding gives its person beside the kana
// and kanji ones (`Jenny Rosen`, internal/hardcase's PersonForm), which the probes leave out: on
// the dev sandbox Stripe fills unset latin names from the kanji names and moves them when those
// change, which the record does not show and the provider does not plan for.
func japanesePerson(updated, cleared bool) string {
	names := `first_name_kana  = "ハナコ"
  first_name_kanji = "花子"
  last_name_kana   = "ヤマダ"
  last_name_kanji  = "山田"`
	kana := `{ city = "ｼﾌﾞﾔ", country = "JP", line1 = "27-15", line2 = "ﾃｽﾄﾋﾞﾙ 2F", postal_code = "1500001", state = "ﾄｳｷﾖｳﾄ", town = "ｼﾞﾝｸﾞｳﾏｴ 3-" }`
	kanji := `{ city = "渋谷区", country = "JP", line1 = "２７－１５", line2 = "テストビル　２階", postal_code = "１５００００１", state = "東京都", town = "神宮前　３丁目" }`
	if updated {
		names = `first_name_kana  = "タロウ"
  first_name_kanji = "太郎"
  last_name_kana   = "サトウ"
  last_name_kanji  = "佐藤"`
		kana = `{ city = "ｵｵｻｶｼｷﾀｸ", country = "JP", line1 = "1-1", line2 = "ｻﾝﾌﾟﾙﾋﾞﾙ 3F", postal_code = "5300001", state = "ｵｵｻｶﾌ", town = "ｳﾒﾀﾞ 1-" }`
		kanji = `{ city = "大阪市北区", country = "JP", line1 = "１－１", line2 = "サンプルビル　３階", postal_code = "５３００００１", state = "大阪府", town = "梅田　１丁目" }`
	}
	if cleared {
		names, kana, kanji = "", `{ country = "JP" }`, `{ country = "JP" }`
	}
	return fmt.Sprintf(`
resource "stripe_account_person" "test" {
  account       = stripe_account.company.id
  first_name    = "Jenny"
  last_name     = "Rosen"
  address_kana  = %s
  address_kanji = %s
  %s
}
`, kana, kanji, names)
}

// A Japanese company's person takes its kana and kanji names and addresses on create and
// changes them in place, as the suite's probes do on its JP company account; an update sends
// each address whole. Removing the names and the address members but the country clears them,
// as the suite's unset of them shows.
func TestAccAccountPerson_japaneseNames(t *testing.T) {
	acct := acctest.NewAccount(t)
	var account, id string
	base := acct.ProviderBlock() + countryCompany("JP")
	resource.ParallelTest(t, resource.TestCase{
		ProtoV6ProviderFactories: tfacc.ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: base + japanesePerson(false, false),
				Check: resource.ComposeAggregateTestCheckFunc(
					ids(address, &account, &id),
					resource.TestCheckResourceAttr(address, "first_name_kana", "ハナコ"),
					resource.TestCheckResourceAttr(address, "last_name_kanji", "山田"),
					resource.TestCheckResourceAttr(address, "address_kana.town", "ｼﾞﾝｸﾞｳﾏｴ 3-"),
					resource.TestCheckResourceAttr(address, "address_kanji.line2", "テストビル　２階"),
					resource.TestCheckResourceAttr(address, "first_name", "Jenny"),
				),
				ConfigPlanChecks: resource.ConfigPlanChecks{PostApplyPostRefresh: []plancheck.PlanCheck{plancheck.ExpectEmptyPlan()}},
			},
			{
				Config: base + japanesePerson(true, false),
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply:             []plancheck.PlanCheck{plancheck.ExpectResourceAction(address, plancheck.ResourceActionUpdate)},
					PostApplyPostRefresh: []plancheck.PlanCheck{plancheck.ExpectEmptyPlan()},
				},
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttrPtr(address, "id", &id),
					resource.TestCheckResourceAttr(address, "first_name_kanji", "太郎"),
					resource.TestCheckResourceAttr(address, "last_name_kana", "サトウ"),
					resource.TestCheckResourceAttr(address, "address_kana.city", "ｵｵｻｶｼｷﾀｸ"),
					resource.TestCheckResourceAttr(address, "address_kanji.postal_code", "５３００００１"),
				),
			},
			{
				ResourceName:      address,
				ImportState:       true,
				ImportStateIdFunc: importID,
				ImportStateVerify: true,
			},
			{
				Config: base + japanesePerson(true, true),
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply:             []plancheck.PlanCheck{plancheck.ExpectResourceAction(address, plancheck.ResourceActionUpdate)},
					PostApplyPostRefresh: []plancheck.PlanCheck{plancheck.ExpectEmptyPlan()},
				},
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttrPtr(address, "id", &id),
					resource.TestCheckNoResourceAttr(address, "first_name_kana"),
					resource.TestCheckNoResourceAttr(address, "first_name_kanji"),
					resource.TestCheckNoResourceAttr(address, "last_name_kana"),
					resource.TestCheckNoResourceAttr(address, "last_name_kanji"),
					resource.TestCheckNoResourceAttr(address, "address_kana.city"),
					resource.TestCheckNoResourceAttr(address, "address_kanji.line2"),
					resource.TestCheckResourceAttr(address, "address_kana.country", "JP"),
					resource.TestCheckResourceAttr(address, "address_kanji.country", "JP"),
				),
			},
		},
	})
}

// thaiPerson is a person of the Thai company with only a secondary ID number, as the suite's
// probe `isolated/th_id_number_secondary` creates it, unless number is empty.
func thaiPerson(number string) string {
	line := ""
	if number != "" {
		line = fmt.Sprintf("  id_number_secondary = %q\n", number)
	}
	return fmt.Sprintf(`
resource "stripe_account_person" "test" {
  account = stripe_account.company.id
%s}
`, line)
}

// A Thai company's person takes a secondary ID number, a laser code, on create and changes it in
// place, as the suite's probe does with the laser codes it sends. Stripe never returns the
// number, only that it has one, so state keeps it from configuration, and removing it leaves it
// on the person.
func TestAccAccountPerson_thaiSecondaryID(t *testing.T) {
	acct := acctest.NewAccount(t)
	var account, id string
	base := acct.ProviderBlock() + countryCompany("TH")
	resource.ParallelTest(t, resource.TestCase{
		ProtoV6ProviderFactories: tfacc.ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: base + thaiPerson("AB0000000000"),
				Check: resource.ComposeAggregateTestCheckFunc(
					ids(address, &account, &id),
					resource.TestCheckResourceAttr(address, "id_number_secondary", "AB0000000000"),
					resource.TestCheckResourceAttr(address, "id_number_secondary_provided", "true"),
				),
				ConfigPlanChecks: resource.ConfigPlanChecks{PostApplyPostRefresh: []plancheck.PlanCheck{plancheck.ExpectEmptyPlan()}},
			},
			{
				Config: base + thaiPerson("CD1234567890"),
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply:             []plancheck.PlanCheck{plancheck.ExpectResourceAction(address, plancheck.ResourceActionUpdate)},
					PostApplyPostRefresh: []plancheck.PlanCheck{plancheck.ExpectEmptyPlan()},
				},
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttrPtr(address, "id", &id),
					resource.TestCheckResourceAttr(address, "id_number_secondary", "CD1234567890"),
					resource.TestCheckResourceAttr(address, "id_number_secondary_provided", "true"),
				),
			},
			{
				Config:   base + thaiPerson(""),
				PlanOnly: true,
			},
		},
	})
}
