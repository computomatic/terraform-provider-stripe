package account_person_v2_test

import (
	"fmt"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/plancheck"

	"github.com/computomatic/terraform-provider-stripe/internal/acctest"
	"github.com/computomatic/terraform-provider-stripe/internal/acctest/tfacc"
)

// scriptsConfig is a person of a Japanese merchant, the account the suite's persons of batch C1 go
// on (TestV2CoreAccountPersonAttributes, `v2JapaneseMerchantAccount`): created with an address of
// its country alone and the kana and kanji names and addresses of probe `update/12`, and, where
// updated, with those its update sends.
func scriptsConfig(c acctest.Config, updated bool) string {
	addresses := `{
    kana  = { city = "ｼﾌﾞﾔ", country = "JP", line1 = "27-15", line2 = "2ｶｲ", postal_code = "1500001", state = "ﾄｳｷﾖｳﾄ", town = "ｼﾞﾝｸﾞｳﾏｴ 3-" }
    kanji = { city = "渋谷区", country = "JP", line1 = "２７－１５", line2 = "２階", postal_code = "１５００００１", state = "東京都", town = "神宮前　３丁目" }
  }`
	names := `{ kana = { given_name = "ハナコ", surname = "ヤマダ" }, kanji = { given_name = "花子", surname = "山田" } }`
	if updated {
		addresses = `{
    kana  = { city = "ｵｵｻｶｼｷﾀｸ", country = "JP", line1 = "3-1-1", line2 = "5ｶｲ", postal_code = "5300001", state = "ｵｵｻｶﾌ", town = "ｳﾒﾀﾞ 3-" }
    kanji = { city = "大阪市北区", country = "JP", line1 = "３－１－１", line2 = "５階", postal_code = "５３００００１", state = "大阪府", town = "梅田　３丁目" }
  }`
		names = `{ kana = { given_name = "タロウ", surname = "スズキ" }, kanji = { given_name = "太郎", surname = "鈴木" } }`
	}
	return c.ProviderBlock() + fmt.Sprintf(`
resource "stripe_account_v2" "jp" {
  contact_email = %q
  dashboard     = "none"
  identity      = { country = "jp" }
  configuration = { merchant = { capabilities = { card_payments = { requested = true } } } }
  defaults      = { responsibilities = { fees_collector = "application", losses_collector = "application" } }
}

resource "stripe_account_person_v2" "test" {
  account_id       = stripe_account_v2.jp.id
  address          = { country = "JP" }
  script_addresses = %s
  script_names     = %s
}
`, c.Name("jp")+"@example.com", addresses, names)
}

// A Japanese person's kana and kanji names and addresses are taken on create and change in place,
// as the suite's probe `update/12` changes them. Stripe fills the address's postal code from the
// script addresses, and moves it with them.
func TestAccAccountPersonV2_japaneseScripts(t *testing.T) {
	acct := acctest.NewAccount(t)
	const person = "stripe_account_person_v2.test"
	resource.ParallelTest(t, resource.TestCase{
		ProtoV6ProviderFactories: tfacc.ProviderFactories,
		Steps: acctest.Forget(t, []resource.TestStep{
			{
				Config: scriptsConfig(acct, false),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr(person, "script_addresses.kana.town", "ｼﾞﾝｸﾞｳﾏｴ 3-"),
					resource.TestCheckResourceAttr(person, "script_addresses.kanji.postal_code", "１５００００１"),
					resource.TestCheckResourceAttr(person, "script_names.kana.given_name", "ハナコ"),
					resource.TestCheckResourceAttr(person, "address.postal_code", "1500001"),
				),
				ConfigPlanChecks: resource.ConfigPlanChecks{PostApplyPostRefresh: []plancheck.PlanCheck{plancheck.ExpectEmptyPlan()}},
			},
			{
				Config: scriptsConfig(acct, true),
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply:             []plancheck.PlanCheck{plancheck.ExpectResourceAction(person, plancheck.ResourceActionUpdate)},
					PostApplyPostRefresh: []plancheck.PlanCheck{plancheck.ExpectEmptyPlan()},
				},
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr(person, "script_addresses.kanji.city", "大阪市北区"),
					resource.TestCheckResourceAttr(person, "script_names.kanji.surname", "鈴木"),
					resource.TestCheckResourceAttr(person, "address.postal_code", "5300001"),
				),
			},
		}, nil, "stripe_account_v2"),
	})
}
