package account_v2_test

import (
	"fmt"
	"regexp"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/plancheck"

	"github.com/computomatic/terraform-provider-stripe/internal/acctest"
	"github.com/computomatic/terraform-provider-stripe/internal/acctest/tfacc"
)

// countryCapabilities are the suite's probes `isolated/capability_<name>_create`
// (TestV2CoreAccountAttributes): a company merchant in a country that offers the capability,
// created requesting it beside `card_payments`.
var countryCapabilities = []struct{ capability, country string }{
	{"alma_payments", "FR"}, {"au_becs_debit_payments", "AU"}, {"bacs_debit_payments", "GB"}, {"blik_payments", "PL"},
	{"fpx_payments", "MY"}, {"grabpay_payments", "SG"}, {"jp_bank_transfer_payments", "JP"}, {"mobilepay_payments", "DK"},
	{"oxxo_payments", "CA"}, {"pay_by_bank_payments", "GB"}, {"paynow_payments", "SG"}, {"promptpay_payments", "TH"},
	{"revolut_pay_payments", "GB"}, {"sepa_bank_transfer_payments", "DE"}, {"sepa_debit_payments", "DE"},
	{"sequra_payments", "ES"}, {"twint_payments", "CH"}, {"zip_payments", "AU"},
}

// countryMerchant is the suite's merchant base in a country: no dashboard, the platform collecting
// fees and covering losses, and the given identity and merchant configuration members. Each
// depends on the one before, because Stripe refuses more than five account creates a second with
// a test key.
func countryMerchant(c acctest.Config, name, previous, identity, merchant string) string {
	dependsOn := ""
	if previous != "" {
		dependsOn = fmt.Sprintf("  depends_on    = [stripe_account_v2.%s]\n", previous)
	}
	return fmt.Sprintf(`
resource "stripe_account_v2" %q {
%s  contact_email = %q
  dashboard     = "none"
  defaults      = { responsibilities = { fees_collector = "application", losses_collector = "application" } }
  identity      = %s
  configuration = { merchant = %s }
}
`, name, dependsOn, c.Name(name)+"@example.com", identity, merchant)
}

// Each country's payment method capability is requested on create, as the suite's probes request
// it, and the account reads its status, which Stripe moves on its own, so only its form is checked.
// The request is kept from the configuration, since Stripe returns only the status.
func TestAccAccountV2_countryCapabilities(t *testing.T) {
	acct := acctest.NewAccount(t)
	status := regexp.MustCompile(`^(active|pending|restricted|unsupported)$`)
	config := acct.ProviderBlock()
	var checks []resource.TestCheckFunc
	previous := ""
	for _, cc := range countryCapabilities {
		merchant := fmt.Sprintf(`{ capabilities = { card_payments = { requested = true }, %s = { requested = true } } }`, cc.capability)
		config += countryMerchant(acct, cc.capability, previous, fmt.Sprintf(`{ country = %q, entity_type = "company" }`, cc.country), merchant)
		previous = cc.capability
		at := "configuration.merchant.capabilities." + cc.capability
		checks = append(checks,
			resource.TestCheckResourceAttr("stripe_account_v2."+cc.capability, at+".requested", "true"),
			resource.TestMatchResourceAttr("stripe_account_v2."+cc.capability, at+".status", status),
		)
	}
	resource.ParallelTest(t, resource.TestCase{
		ProtoV6ProviderFactories: tfacc.ProviderFactories,
		Steps: acctest.Forget(t, []resource.TestStep{{
			Config:           config,
			Check:            resource.ComposeAggregateTestCheckFunc(checks...),
			ConfigPlanChecks: resource.ConfigPlanChecks{PostApplyPostRefresh: []plancheck.PlanCheck{plancheck.ExpectEmptyPlan()}},
		}}, nil, "stripe_account_v2"),
	})
}

// The Japanese script forms of the suite's probes: `every_field/106` and `every_field/107` create
// an individual and a company with them, and `update/90` and `update/91` change them.
const (
	kanaAddress        = `{ city = "ｼﾌﾞﾔ", country = "JP", line1 = "27-15", line2 = "2ｶｲ", postal_code = "1500001", state = "ﾄｳｷﾖｳﾄ", town = "ｼﾞﾝｸﾞｳﾏｴ 3-" }`
	kanjiAddress       = `{ city = "渋谷区", country = "JP", line1 = "２７－１５", line2 = "２階", postal_code = "１５００００１", state = "東京都", town = "神宮前　３丁目" }`
	kanaAddressUpdate  = `{ city = "ｵｵｻｶｼｷﾀｸ", country = "JP", line1 = "3-1-1", line2 = "5ｶｲ", postal_code = "5300001", state = "ｵｵｻｶﾌ", town = "ｳﾒﾀﾞ 3-" }`
	kanjiAddressUpdate = `{ city = "大阪市北区", country = "JP", line1 = "３－１－１", line2 = "５階", postal_code = "５３００００１", state = "大阪府", town = "梅田　３丁目" }`
)

func scriptsConfig(c acctest.Config, updated bool) string {
	kana, kanji := kanaAddress, kanjiAddress
	names := `{ kana = { given_name = "ハナコ", surname = "ヤマダ" }, kanji = { given_name = "花子", surname = "山田" } }`
	company := `{ kana = { registered_name = "ｺﾝﾌｫｰﾏﾝｽ ｶﾌﾞｼｷｶﾞｲｼﾔ" }, kanji = { registered_name = "コンフォーマンス株式会社" } }`
	if updated {
		kana, kanji = kanaAddressUpdate, kanjiAddressUpdate
		names = `{ kana = { given_name = "タロウ", surname = "スズキ" }, kanji = { given_name = "太郎", surname = "鈴木" } }`
		company = `{ kana = { registered_name = "ｱﾄﾘﾋﾞﾕｰﾄ ｶﾌﾞｼｷｶﾞｲｼﾔ" }, kanji = { registered_name = "アトリビュート株式会社" } }`
	}
	merchant := `{ capabilities = { card_payments = { requested = true } } }`
	addresses := fmt.Sprintf("{\n      kana  = %s\n      kanji = %s\n    }", kana, kanji)
	return c.ProviderBlock() +
		countryMerchant(c, "individual", "", fmt.Sprintf(`{
    country     = "JP"
    entity_type = "individual"
    individual = {
      script_addresses = %s
      script_names     = %s
    }
  }`, addresses, names), merchant) +
		countryMerchant(c, "company", "individual", fmt.Sprintf(`{
    country     = "JP"
    entity_type = "company"
    business_details = {
      script_addresses = %s
      script_names     = %s
    }
  }`, addresses, company), merchant)
}

// A Japanese individual's and company's kana and kanji names and addresses are taken on create and
// change in place, the individual and the business details sent whole, as the suite's probes send
// them. Stripe fills the address's country and postal code from the script addresses.
func TestAccAccountV2_japaneseScripts(t *testing.T) {
	acct := acctest.NewAccount(t)
	const individual, company = "stripe_account_v2.individual", "stripe_account_v2.company"
	resource.ParallelTest(t, resource.TestCase{
		ProtoV6ProviderFactories: tfacc.ProviderFactories,
		Steps: acctest.Forget(t, []resource.TestStep{
			{
				Config: scriptsConfig(acct, false),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr(individual, "identity.individual.script_addresses.kana.city", "ｼﾌﾞﾔ"),
					resource.TestCheckResourceAttr(individual, "identity.individual.script_addresses.kanji.town", "神宮前　３丁目"),
					resource.TestCheckResourceAttr(individual, "identity.individual.script_names.kanji.surname", "山田"),
					resource.TestCheckResourceAttr(individual, "identity.individual.address.postal_code", "1500001"),
					resource.TestCheckResourceAttr(company, "identity.business_details.script_addresses.kana.postal_code", "1500001"),
					resource.TestCheckResourceAttr(company, "identity.business_details.script_names.kana.registered_name", "ｺﾝﾌｫｰﾏﾝｽ ｶﾌﾞｼｷｶﾞｲｼﾔ"),
					resource.TestCheckResourceAttr(company, "identity.business_details.address.country", "JP"),
				),
				ConfigPlanChecks: resource.ConfigPlanChecks{PostApplyPostRefresh: []plancheck.PlanCheck{plancheck.ExpectEmptyPlan()}},
			},
			{
				Config: scriptsConfig(acct, true),
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{
						plancheck.ExpectResourceAction(individual, plancheck.ResourceActionUpdate),
						plancheck.ExpectResourceAction(company, plancheck.ResourceActionUpdate),
					},
					PostApplyPostRefresh: []plancheck.PlanCheck{plancheck.ExpectEmptyPlan()},
				},
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr(individual, "identity.individual.script_addresses.kana.city", "ｵｵｻｶｼｷﾀｸ"),
					resource.TestCheckResourceAttr(individual, "identity.individual.script_names.kana.given_name", "タロウ"),
					resource.TestCheckResourceAttr(company, "identity.business_details.script_addresses.kanji.state", "大阪府"),
					resource.TestCheckResourceAttr(company, "identity.business_details.script_names.kanji.registered_name", "アトリビュート株式会社"),
				),
			},
		}, nil, "stripe_account_v2"),
	})
}

// A Japanese merchant's Konbini support details (probe `every_field/108`) and a British merchant's
// Bacs Direct Debit display name (`every_field/109`) are taken on create. Stripe gives a merchant a
// SEPA creditor ID of its own.
func TestAccAccountV2_merchantCountrySettings(t *testing.T) {
	acct := acctest.NewAccount(t)
	const jp, gb = "stripe_account_v2.konbini", "stripe_account_v2.bacs"
	config := acct.ProviderBlock() +
		countryMerchant(acct, "konbini", "", `{ country = "JP" }`, `{
      capabilities     = { card_payments = { requested = true } }
      konbini_payments = { support = { email = "jenny.rosen@example.com", hours = { end_time = "17:00", start_time = "09:00" }, phone = "0312345678" } }
    }`) +
		countryMerchant(acct, "bacs", "konbini", `{ country = "GB" }`, `{
      bacs_debit_payments = { display_name = "ATTRS CONFORMANCE" }
      capabilities        = { card_payments = { requested = true } }
    }`)
	resource.ParallelTest(t, resource.TestCase{
		ProtoV6ProviderFactories: tfacc.ProviderFactories,
		Steps: acctest.Forget(t, []resource.TestStep{{
			Config: config,
			Check: resource.ComposeAggregateTestCheckFunc(
				resource.TestCheckResourceAttr(jp, "configuration.merchant.konbini_payments.support.email", "jenny.rosen@example.com"),
				resource.TestCheckResourceAttr(jp, "configuration.merchant.konbini_payments.support.hours.start_time", "09:00"),
				resource.TestCheckResourceAttr(jp, "configuration.merchant.konbini_payments.support.phone", "0312345678"),
				resource.TestMatchResourceAttr(jp, "configuration.merchant.sepa_debit_payments.creditor_id", regexp.MustCompile(`^[A-Z]{2}[0-9]{2}[A-Z0-9]{3}[A-Z0-9]{1,28}$`)),
				resource.TestCheckResourceAttr(gb, "configuration.merchant.bacs_debit_payments.display_name", "ATTRS CONFORMANCE"),
			),
			ConfigPlanChecks: resource.ConfigPlanChecks{PostApplyPostRefresh: []plancheck.PlanCheck{plancheck.ExpectEmptyPlan()}},
		}}, nil, "stripe_account_v2"),
	})
}

// The plan refuses what Stripe refused for the US platform: the JCB capability of a Japanese
// company (probe `isolated/capability_jcb_create`) and a custom SEPA creditor ID in a sandbox
// (`isolated/sepa_creditor_create`), each sent as the probe sent it.
func TestAccAccountV2_countryRefusals(t *testing.T) {
	acct := acctest.NewAccount(t)
	jcb := acct.ProviderBlock() + countryMerchant(acct, "jcb", "", `{ country = "JP", entity_type = "company" }`,
		`{ capabilities = { card_payments = { requested = true }, jcb_payments = { requested = true } } }`)
	sepa := acct.ProviderBlock() + countryMerchant(acct, "sepa", "", `{ country = "DE" }`, `{
      capabilities        = { card_payments = { requested = true } }
      sepa_debit_payments = { creditor_id = "DE98ZZZ09999999999" }
    }`)
	resource.ParallelTest(t, resource.TestCase{
		ProtoV6ProviderFactories: tfacc.ProviderFactories,
		Steps: []resource.TestStep{
			{Config: jcb, PlanOnly: true, ExpectError: regexp.MustCompile(`Capability jcb_payments is not available for platforms in country\s+US`)},
			{Config: sepa, PlanOnly: true, ExpectError: regexp.MustCompile(`custom\s+SEPA\s+Creditor\s+Identifier`)},
		},
	})
}
