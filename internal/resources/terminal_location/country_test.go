package terminal_location_test

import (
	"context"
	"fmt"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/plancheck"

	"github.com/computomatic/terraform-provider-stripe/internal/acctest"
	"github.com/computomatic/terraform-provider-stripe/internal/acctest/tfacc"
	"github.com/computomatic/terraform-provider-stripe/internal/stripeapi"
	"github.com/computomatic/terraform-provider-stripe/internal/wire"
)

// japaneseAccount creates a custom individual account in Japan, as the fake's conformance run
// creates the suite's tagged `country_jp` account, on which the suite's probes make their
// Japanese locations, and deletes it when the test ends.
func japaneseAccount(t *testing.T, c acctest.Config) acctest.Config {
	t.Helper()
	requested := wire.Object{{Name: "requested", Value: wire.Bool(true)}}
	id := tfacc.Create(t, c, "/v1/accounts", wire.Object{
		{Name: "type", Value: wire.String("custom")},
		{Name: "country", Value: wire.String("JP")},
		{Name: "business_type", Value: wire.String("individual")},
		{Name: "capabilities", Value: wire.Object{{Name: "card_payments", Value: requested}, {Name: "transfers", Value: requested}}},
	})
	t.Cleanup(func() {
		if _, err := tfacc.Client(c).Do(context.Background(), stripeapi.Request{Method: "DELETE", Path: "/v1/accounts/" + id}); err != nil {
			t.Logf("deleting the Japanese account %s: %v", id, err)
		}
	})
	return c.Connected(id)
}

// locationStage is how far TestAccTerminalLocation_japanese has taken its locations.
type locationStage int

const (
	// created holds the creates of the suite's probes.
	created locationStage = iota
	// updated holds their updates.
	updated
	// line2Removed leaves the second lines out of the updated address.
	line2Removed
	// line2Emptied sets the second lines of the updated address to `""`.
	line2Emptied
)

// japaneseLocations are two locations of the Japanese account with the suite's probe values:
// `names` is the probe `isolated/jp`'s, display names and the kana and kanji addresses of 27-15
// Jingumae 3-chome, Shibuya-ku with no second lines, then other display names; `address` is
// `isolated/jp_address`'s, the same with second lines, then 1-1 Umeda 1-chome, Kita-ku, Osaka,
// every member but the country changed. Past the update, the address's second lines are left out
// or, as `isolated/jp_address_unset` sends them, empty.
func japaneseLocations(c acctest.Config, stage locationStage) string {
	names := `display_name_kana  = "ｺﾝﾌｫｰﾏﾝｽ ｶﾌﾞｼｷｶﾞｲｼﾔ"
  display_name_kanji = "コンフォーマンス株式会社"`
	kana := `city = "ｼﾌﾞﾔ", country = "JP", line1 = "27-15", postal_code = "1500001", state = "ﾄｳｷﾖｳﾄ", town = "ｼﾞﾝｸﾞｳﾏｴ 3-"`
	kanji := `city = "渋谷区", country = "JP", line1 = "２７－１５", postal_code = "１５００００１", state = "東京都", town = "神宮前　３丁目"`
	renamed := names
	kanaAddress, kanjiAddress := kana, kanji
	kanaLine2, kanjiLine2 := `, line2 = "ﾃｽﾄﾋﾞﾙ 2F"`, `, line2 = "テストビル　２階"`
	if stage >= updated {
		renamed = `display_name_kana  = "ﾃｽﾄ ﾛｹｰｼｮﾝ"
  display_name_kanji = "テスト店舗"`
		kanaAddress = `city = "ｵｵｻｶｼｷﾀｸ", country = "JP", line1 = "1-1", postal_code = "5300001", state = "ｵｵｻｶﾌ", town = "ｳﾒﾀﾞ 1-"`
		kanjiAddress = `city = "大阪市北区", country = "JP", line1 = "１－１", postal_code = "５３００００１", state = "大阪府", town = "梅田　１丁目"`
		kanaLine2, kanjiLine2 = `, line2 = "ｻﾝﾌﾟﾙﾋﾞﾙ 3F"`, `, line2 = "サンプルビル　３階"`
	}
	switch stage {
	case line2Removed:
		kanaLine2, kanjiLine2 = "", ""
	case line2Emptied:
		kanaLine2, kanjiLine2 = `, line2 = ""`, `, line2 = ""`
	}
	return c.ProviderBlock() + c.ConnectedProviderBlock() + fmt.Sprintf(`
resource "stripe_terminal_location" "names" {
  provider      = stripe.connected
  display_name  = %[1]q
  address_kana  = { %[3]s }
  address_kanji = { %[4]s }
  %[5]s
}

resource "stripe_terminal_location" "address" {
  provider      = stripe.connected
  display_name  = %[2]q
  address_kana  = { %[6]s%[7]s }
  address_kanji = { %[8]s%[9]s }
  %[10]s
}
`, c.Name("jp location"), c.Name("jp address"), kana, kanji, renamed, kanaAddress, kanaLine2, kanjiAddress, kanjiLine2, names)
}

// A Japanese account's location takes kana and kanji display names and addresses, with no
// latin address, and changes them in place, as the suite's probes do on its JP account; an
// update sends each address whole. A second line the configuration leaves unset is Stripe's to
// fill, `""` on the dev sandbox, so removing one plans nothing, and setting it to `""` clears it.
func TestAccTerminalLocation_japanese(t *testing.T) {
	acct := japaneseAccount(t, acctest.NewAccount(t))
	const names, addressed = "stripe_terminal_location.names", "stripe_terminal_location.address"
	var namesID, addressID string
	resource.ParallelTest(t, resource.TestCase{
		ProtoV6ProviderFactories: tfacc.ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: japaneseLocations(acct, created),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttrWith(names, "id", func(v string) error { namesID = v; return nil }),
					resource.TestCheckResourceAttrWith(addressed, "id", func(v string) error { addressID = v; return nil }),
					resource.TestCheckResourceAttr(names, "display_name_kanji", "コンフォーマンス株式会社"),
					resource.TestCheckResourceAttr(names, "address_kana.town", "ｼﾞﾝｸﾞｳﾏｴ 3-"),
					resource.TestCheckResourceAttr(addressed, "address_kanji.line2", "テストビル　２階"),
				),
				ConfigPlanChecks: resource.ConfigPlanChecks{PostApplyPostRefresh: []plancheck.PlanCheck{plancheck.ExpectEmptyPlan()}},
			},
			{
				Config: japaneseLocations(acct, updated),
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{
						plancheck.ExpectResourceAction(names, plancheck.ResourceActionUpdate),
						plancheck.ExpectResourceAction(addressed, plancheck.ResourceActionUpdate),
					},
					PostApplyPostRefresh: []plancheck.PlanCheck{plancheck.ExpectEmptyPlan()},
				},
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttrPtr(names, "id", &namesID),
					resource.TestCheckResourceAttrPtr(addressed, "id", &addressID),
					resource.TestCheckResourceAttr(names, "display_name_kana", "ﾃｽﾄ ﾛｹｰｼｮﾝ"),
					resource.TestCheckResourceAttr(names, "address_kanji.city", "渋谷区"),
					resource.TestCheckResourceAttr(addressed, "address_kana.city", "ｵｵｻｶｼｷﾀｸ"),
					resource.TestCheckResourceAttr(addressed, "address_kanji.line2", "サンプルビル　３階"),
					resource.TestCheckResourceAttr(addressed, "display_name_kana", "ｺﾝﾌｫｰﾏﾝｽ ｶﾌﾞｼｷｶﾞｲｼﾔ"),
				),
			},
			{
				Config:   japaneseLocations(acct, line2Removed),
				PlanOnly: true,
			},
			{
				Config: japaneseLocations(acct, line2Emptied),
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{
						plancheck.ExpectResourceAction(names, plancheck.ResourceActionNoop),
						plancheck.ExpectResourceAction(addressed, plancheck.ResourceActionUpdate),
					},
					PostApplyPostRefresh: []plancheck.PlanCheck{plancheck.ExpectEmptyPlan()},
				},
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttrPtr(addressed, "id", &addressID),
					resource.TestCheckResourceAttr(addressed, "address_kana.line2", ""),
					resource.TestCheckResourceAttr(addressed, "address_kanji.line2", ""),
					resource.TestCheckResourceAttr(addressed, "address_kana.city", "ｵｵｻｶｼｷﾀｸ"),
				),
			},
		},
	})
}
