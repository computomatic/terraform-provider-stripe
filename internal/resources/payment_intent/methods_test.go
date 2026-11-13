package payment_intent_test

import (
	"fmt"
	"maps"
	"regexp"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/plancheck"

	"github.com/computomatic/terraform-provider-stripe/internal/acctest"
	"github.com/computomatic/terraform-provider-stripe/internal/acctest/tfacc"
)

// methodCase is a payment intent of one payment method, as a probe of the record's per-method
// variants (suite: TestPaymentIntentAttributes) made it: in the method's currency and at an amount
// it takes, with a payment method configuration of its own that turns the method on, and an
// inline payment method of the method's type. created and changed are what lands inside the
// method's `payment_method_options` hash on the create and on the update (no options where
// empty). A case marked once is not updated: its probe's update was refused,
// changes nothing a configuration can, or is sent otherwise by the provider.
type methodCase struct {
	method   string
	currency string
	amount   int
	// customer says the intent is for a customer of its own, as a bank transfer needs.
	customer bool
	// billing is what lands inside the inline payment method's `billing_details`, and data
	// what lands inside `payment_method_data` beside it.
	billing, data    string
	created, changed string
	once             bool
	// atFirst and atLast are the intent's attributes after the create and after the update, by
	// their path below the method's options; sent is what the update sent of the options.
	atFirst, atLast map[string]string
	sent            map[string]string
}

// final is the options the case's intent holds at the end.
func (c methodCase) final() string {
	if c.once {
		return c.created
	}
	return c.changed
}

func intentAddress(key string) string { return "stripe_payment_intent." + key }

// sortedKeys keeps the rendered configuration in a stable order.
func sortedKeys[M ~map[string]V, V string | methodCase](m M) []string {
	return slices.Sorted(maps.Keys(m))
}

// methodBlocks renders each case's payment method configuration and payment intent, keyed by
// the case's name, with the options of the update where changed is true.
func methodBlocks(acct acctest.Config, cases map[string]methodCase, changed bool) string {
	var b strings.Builder
	b.WriteString(acct.ProviderBlock())
	for _, key := range sortedKeys(cases) {
		c := cases[key]
		options := c.created
		if changed {
			options = c.final()
		}
		fmt.Fprintf(&b, `
resource "stripe_payment_method_configuration" %[1]q {
  name = %[2]q
  %[3]s = {
    display_preference = { preference = "on" }
  }
}
`, key, acct.Name("attrs-"+key), c.method)
		customer := ""
		if c.customer {
			fmt.Fprintf(&b, "\nresource \"stripe_customer\" %q {}\n", key)
			customer = fmt.Sprintf("  customer                     = stripe_customer.%s.id\n", key)
		}
		opts := ""
		if options != "" {
			opts = fmt.Sprintf("  payment_method_options = {\n    %s = {\n%s    }\n  }\n", c.method, options)
		}
		fmt.Fprintf(&b, `
resource "stripe_payment_intent" %[1]q {
  amount                       = %[2]d
  currency                     = %[3]q
  payment_method_configuration = stripe_payment_method_configuration.%[1]s.id
%[4]s  payment_method_data = {
    type = %[5]q
    billing_details = {
%[6]s    }
%[7]s  }
%[8]s}
`, key, c.amount, c.currency, customer, c.method, c.billing, c.data, opts)
	}
	return b.String()
}

// methodChecks checks each case's options, those after the update where changed is true, and
// what the update sent of them.
func methodChecks(t *testing.T, acct acctest.Config, cases map[string]methodCase, changed bool) resource.TestCheckFunc {
	var checks []resource.TestCheckFunc
	for _, key := range sortedKeys(cases) {
		c := cases[key]
		want := c.atFirst
		if changed && !c.once {
			want = c.atLast
		}
		prefix := "payment_method_options." + c.method + "."
		for _, p := range sortedKeys(want) {
			checks = append(checks, resource.TestCheckResourceAttr(intentAddress(key), prefix+p, want[p]))
		}
		if changed && len(c.sent) > 0 {
			sent := map[string]string{}
			for p, v := range c.sent {
				sent[prefix+p] = v
			}
			path := new(string)
			checks = append(checks,
				resource.TestCheckResourceAttrWith(intentAddress(key), "id", func(v string) error {
					*path = "/v1/payment_intents/" + v
					return nil
				}),
				tfacc.Sent(t, acct, "POST", path, sent))
		}
	}
	return resource.ComposeAggregateTestCheckFunc(checks...)
}

// testMethods creates every case's intent in one apply and updates those not marked once in a
// second, in place, with an empty plan after each apply.
func testMethods(t *testing.T, acct acctest.Config, cases map[string]methodCase) {
	t.Helper()
	empty := []plancheck.PlanCheck{plancheck.ExpectEmptyPlan()}
	var actions []plancheck.PlanCheck
	for _, key := range sortedKeys(cases) {
		action := plancheck.ResourceActionUpdate
		if cases[key].once {
			action = plancheck.ResourceActionNoop
		}
		actions = append(actions, plancheck.ExpectResourceAction(intentAddress(key), action))
	}
	resource.ParallelTest(t, resource.TestCase{
		ProtoV6ProviderFactories: tfacc.ProviderFactories,
		Steps: acctest.Forget(t, []resource.TestStep{
			{
				Config:           methodBlocks(acct, cases, false),
				ConfigPlanChecks: resource.ConfigPlanChecks{PostApplyPostRefresh: empty},
				Check:            methodChecks(t, acct, cases, false),
			},
			{
				Config:           methodBlocks(acct, cases, true),
				ConfigPlanChecks: resource.ConfigPlanChecks{PreApply: actions, PostApplyPostRefresh: empty},
				Check:            methodChecks(t, acct, cases, true),
			},
		}, nil, "stripe_payment_intent"),
	})
}

// jenny is the billing details of the variants' inline payment methods: a name and a generated
// email.
func jenny(acct acctest.Config) string {
	return fmt.Sprintf("      name  = \"Jenny Rosen\"\n      email = %q\n", acct.Name("jenny")+"@example.com")
}

// withAddress is jenny with the billing address of a method that wants one, in the country it
// serves.
func withAddress(acct acctest.Config, line1, city, state, postal, country string) string {
	return jenny(acct) + fmt.Sprintf(`      address = {
        line1       = %q
        city        = %q
        state       = %q
        postal_code = %q
        country     = %q
      }
`, line1, city, state, postal, country)
}

// usAddress is the billing address of Klarna's and Afterpay's variants.
func usAddress(acct acctest.Config) string {
	return withAddress(acct, "354 Oyster Point Blvd", "South San Francisco", "CA", "94080", "US")
}

// inAddress is the billing address of UPI's variant.
func inAddress(acct acctest.Config) string {
	return withAddress(acct, "1 MG Road", "Mumbai", "MH", "400001", "IN")
}

// captureAndFuture is the options of a method that takes a capture method and a future usage.
// The record's update of each such method unsets `capture_method`, which Stripe defaults, so
// the update keeps it.
func captureAndFuture(future string) string {
	return fmt.Sprintf("      capture_method     = \"manual\"\n      setup_future_usage = %q\n", future)
}

// The options of Pix, Klarna, WeChat Pay and Affirm change in place, each with the values of the
// probe named above it, the create's and the update's.
func TestAccPaymentIntent_methodOptionsChangeInPlace(t *testing.T) {
	acct := acctest.NewAccount(t)
	expires := time.Now().Add(30 * 24 * time.Hour).Unix()
	pix := func(iof string, after, amount int, amountType, end, schedule, reference, start, future string) string {
		return fmt.Sprintf(`      amount_includes_iof   = %[1]q
      expires_after_seconds = %[2]d
      expires_at            = %[10]d
      mandate_options = {
        amount              = %[3]d
        amount_includes_iof = %[1]q
        amount_type         = %[4]q
        currency            = "brl"
        end_date            = %[5]q
        payment_schedule    = %[6]q
        reference           = %[7]q
        start_date          = %[8]q
      }
      setup_future_usage = %[9]q
`, iof, after, amount, amountType, end, schedule, reference, start, future, expires)
	}
	wechat := func(app, client string) string {
		return fmt.Sprintf("      app_id             = %q\n      client             = %q\n      setup_future_usage = \"none\"\n", app, client)
	}
	affirm := func(locale string) string {
		return fmt.Sprintf("      capture_method     = \"manual\"\n      preferred_locale   = %q\n      setup_future_usage = \"none\"\n", locale)
	}
	subscriptionRef := acct.Name("klarna-subscription")
	testMethods(t, acct, map[string]methodCase{
		// update/42
		"pix": {
			method: "pix", currency: "brl", amount: 2000,
			billing: jenny(acct) + "      tax_id = \"000.000.000-00\"\n",
			// The mandate reference is a generated value of at most 35 characters (`$gen` `unique`,
			// arg `35`), so it is fixed rather than the account's run-scoped name.
			created: pix("always", 3600, 7, "fixed", "2035-02-15", "halfyearly", "attrs-pix-ref-1", "2035-01-15", "none"),
			changed: pix("never", 7200, 8, "maximum", "2035-03-15", "monthly", "attrs-pix-ref-2", "2035-01-20", "off_session"),
			atFirst: map[string]string{
				"amount_includes_iof":              "always",
				"expires_after_seconds":            "3600",
				"expires_at":                       fmt.Sprint(expires),
				"mandate_options.amount":           "7",
				"mandate_options.currency":         "brl",
				"mandate_options.payment_schedule": "halfyearly",
				"mandate_options.reference":        "attrs-pix-ref-1",
				"setup_future_usage":               "none",
			},
			atLast: map[string]string{
				"amount_includes_iof":                 "never",
				"expires_after_seconds":               "7200",
				"mandate_options.amount":              "8",
				"mandate_options.amount_includes_iof": "never",
				"mandate_options.amount_type":         "maximum",
				"mandate_options.end_date":            "2035-03-15",
				"mandate_options.payment_schedule":    "monthly",
				"mandate_options.reference":           "attrs-pix-ref-2",
				"mandate_options.start_date":          "2035-01-20",
				"setup_future_usage":                  "off_session",
			},
		},
		// enums/212
		"klarna": {
			method: "klarna", currency: "usd", amount: 2000, billing: usAddress(acct),
			changed: "      capture_method     = \"manual\"\n      preferred_locale   = \"cs-CZ\"\n      setup_future_usage = \"none\"\n",
			atLast:  map[string]string{"capture_method": "manual", "preferred_locale": "cs-CZ", "setup_future_usage": "none"},
		},
		// enums/371: the on-demand and subscription options, which Stripe never returns, are
		// kept from configuration, and the update sends them.
		"klarna_on_demand": {
			method: "klarna", currency: "usd", amount: 2000, billing: usAddress(acct),
			changed: fmt.Sprintf(`      on_demand = { purchase_interval = "day" }
      subscriptions = [{
        interval     = "day"
        next_billing = { amount = 7, date = "2035-01-15" }
        reference    = %q
      }]
`, subscriptionRef),
			atLast: map[string]string{
				"on_demand.purchase_interval":       "day",
				"subscriptions.0.interval":          "day",
				"subscriptions.0.reference":         subscriptionRef,
				"subscriptions.0.next_billing.date": "2035-01-15",
			},
			sent: map[string]string{
				"on_demand.purchase_interval":         "day",
				"subscriptions.0.interval":            "day",
				"subscriptions.0.next_billing.amount": "7",
				"subscriptions.0.next_billing.date":   "2035-01-15",
				"subscriptions.0.reference":           subscriptionRef,
			},
		},
		// update/51
		"wechat_pay": {
			method: "wechat_pay", currency: "usd", amount: 2000, billing: jenny(acct),
			created: wechat(acct.Name("wechat-app-1"), "android"),
			changed: wechat(acct.Name("wechat-app-2"), "ios"),
			atFirst: map[string]string{"app_id": acct.Name("wechat-app-1"), "client": "android", "setup_future_usage": "none"},
			atLast:  map[string]string{"app_id": acct.Name("wechat-app-2"), "client": "ios"},
		},
		// update/24
		"affirm": {
			method: "affirm", currency: "usd", amount: 5000, billing: jenny(acct),
			created: affirm("en-US"), changed: affirm("en-CA"),
			atFirst: map[string]string{"capture_method": "manual", "preferred_locale": "en-US", "setup_future_usage": "none"},
			atLast:  map[string]string{"preferred_locale": "en-CA"},
		},
	})
}

// The options of the methods that take a capture method and a future usage, and those of
// Bancontact, Afterpay and UPI, change in place, each with the values of the probe named above
// it, the create's and the update's.
func TestAccPaymentIntent_methodFutureUsageChangesInPlace(t *testing.T) {
	acct := acctest.NewAccount(t)
	afterpay := func(reference string) string {
		return fmt.Sprintf("      capture_method     = \"manual\"\n      reference          = %q\n      setup_future_usage = \"none\"\n", reference)
	}
	bancontact := func(language, future string) string {
		return fmt.Sprintf("      preferred_language = %q\n      setup_future_usage = %q\n", language, future)
	}
	upiEnd := time.Now().Add(30 * 24 * time.Hour).Unix()
	// The suite sends a generated value of at most 20 characters (`$gen` `unique`, arg `20`), a
	// bound Stripe enforces, so the value is fixed rather than the account's run-scoped name.
	upiDescription := "attrs UPI mandate"
	cases := map[string]methodCase{
		// update/25
		"afterpay_clearpay": {
			method: "afterpay_clearpay", currency: "usd", amount: 2000, billing: usAddress(acct),
			created: afterpay(acct.Name("afterpay-ref-1")), changed: afterpay(acct.Name("afterpay-ref-2")),
			atFirst: map[string]string{"capture_method": "manual", "reference": acct.Name("afterpay-ref-1"), "setup_future_usage": "none"},
			atLast:  map[string]string{"reference": acct.Name("afterpay-ref-2")},
		},
		// update/28
		"bancontact": {
			method: "bancontact", currency: "eur", amount: 500, billing: "      name = \"Jenny Rosen\"\n",
			created: bancontact("de", "none"), changed: bancontact("en", "off_session"),
			atFirst: map[string]string{"preferred_language": "de", "setup_future_usage": "none"},
			atLast:  map[string]string{"preferred_language": "en", "setup_future_usage": "off_session"},
		},
		// enums/240
		"upi": {
			method: "upi", currency: "inr", amount: 10000, billing: inAddress(acct),
			changed: "      setup_future_usage = \"on_session\"\n",
			atLast:  map[string]string{"setup_future_usage": "on_session"},
		},
		// enums/372: the mandate options, which Stripe never returns, are kept from
		// configuration, and the update sends them.
		"upi_mandate": {
			method: "upi", currency: "inr", amount: 10000, billing: inAddress(acct),
			changed: fmt.Sprintf(`      mandate_options = {
        amount      = 7
        amount_type = "fixed"
        description = %q
        end_date    = %d
      }
`, upiDescription, upiEnd),
			atLast: map[string]string{"mandate_options.amount_type": "fixed", "mandate_options.description": upiDescription},
			sent: map[string]string{
				"mandate_options.amount":      "7",
				"mandate_options.amount_type": "fixed",
				"mandate_options.description": upiDescription,
				"mandate_options.end_date":    fmt.Sprint(upiEnd),
			},
		},
	}
	// update/27, update/29, update/34, update/36 and update/44
	for method, currency := range map[string]string{"amazon_pay": "usd", "cashapp": "usd", "kakao_pay": "krw", "kr_card": "krw", "satispay": "eur"} {
		amount := 2000
		if currency == "krw" {
			amount = 20000
		}
		cases[method] = methodCase{
			method: method, currency: currency, amount: amount, billing: jenny(acct),
			created: captureAndFuture("none"), changed: captureAndFuture("off_session"),
			atFirst: map[string]string{"capture_method": "manual", "setup_future_usage": "none"},
			atLast:  map[string]string{"capture_method": "manual", "setup_future_usage": "off_session"},
		}
	}
	testMethods(t, acct, cases)
}

// The options of the methods whose probe has no update a test can reproduce are taken on
// create and read back, each with the values of the create of the probe named above it.
func TestAccPaymentIntent_methodOptionsOnCreate(t *testing.T) {
	acct := acctest.NewAccount(t)
	future := "      setup_future_usage = \"none\"\n"
	subscriptionRef := acct.Name("klarna-subscription")
	cases := map[string]methodCase{
		// update/26
		"alipay": {
			method: "alipay", currency: "usd", amount: 2000, billing: jenny(acct), once: true,
			created: future, atFirst: map[string]string{"setup_future_usage": "none"},
		},
		// update/30
		"crypto": {
			method: "crypto", currency: "usd", amount: 2000, billing: jenny(acct), once: true,
			created: future, atFirst: map[string]string{"setup_future_usage": "none"},
		},
		// update/37
		"mb_way": {
			method: "mb_way", currency: "eur", amount: 2000, billing: jenny(acct) + "      phone = \"+351911111111\"\n", once: true,
			created: future, atFirst: map[string]string{"setup_future_usage": "none"},
		},
		// update/38
		"multibanco": {
			method: "multibanco", currency: "eur", amount: 2000, billing: jenny(acct), once: true,
			created: future, atFirst: map[string]string{"setup_future_usage": "none"},
		},
		// update/45
		"scalapay": {
			method: "scalapay", currency: "eur", amount: 5000, billing: jenny(acct), once: true,
			created: "      capture_method = \"manual\"\n", atFirst: map[string]string{"capture_method": "manual"},
		},
		// enums/367: the on-demand and subscription options, which Stripe never returns, are
		// kept from configuration.
		"klarna_on_demand": {
			method: "klarna", currency: "usd", amount: 2000, billing: usAddress(acct), once: true,
			created: fmt.Sprintf(`      on_demand = { purchase_interval = "month" }
      subscriptions = [{
        interval     = "month"
        next_billing = { amount = 7, date = "2035-01-15" }
        reference    = %q
      }]
`, subscriptionRef),
			atFirst: map[string]string{"on_demand.purchase_interval": "month", "subscriptions.0.interval": "month", "subscriptions.0.next_billing.amount": "7"},
		},
	}
	// update/41, update/43 and update/47
	for method, currency := range map[string]string{"payco": "krw", "samsung_pay": "krw", "sunbit": "usd"} {
		amount := 20000
		if method == "sunbit" {
			amount = 10000
		}
		cases[method] = methodCase{
			method: method, currency: currency, amount: amount, billing: jenny(acct), once: true,
			created: captureAndFuture("none"),
			atFirst: map[string]string{"capture_method": "manual", "setup_future_usage": "none"},
		}
	}
	testMethods(t, acct, cases)
}

// The inline payment methods of EPS, iDEAL, Naver Pay and Przelewy24 take the method's own data
// on create, beside the options of the probe named above each.
func TestAccPaymentIntent_methodDataOnCreate(t *testing.T) {
	acct := acctest.NewAccount(t)
	future := "      setup_future_usage = \"none\"\n"
	testMethods(t, acct, map[string]methodCase{
		// update/32
		"eps": {
			method: "eps", currency: "eur", amount: 2000, billing: jenny(acct), once: true,
			data:    "    eps = { bank = \"arzte_und_apotheker_bank\" }\n",
			created: future, atFirst: map[string]string{"setup_future_usage": "none"},
		},
		// update/33
		"ideal": {
			method: "ideal", currency: "eur", amount: 2000, billing: jenny(acct), once: true,
			data:    "    ideal = { bank = \"abn_amro\" }\n",
			created: future, atFirst: map[string]string{"setup_future_usage": "none"},
		},
		// update/39
		"naver_pay": {
			method: "naver_pay", currency: "krw", amount: 20000, billing: jenny(acct), once: true,
			data:    "    naver_pay = { funding = \"card\" }\n",
			created: captureAndFuture("none"), atFirst: map[string]string{"capture_method": "manual", "setup_future_usage": "none"},
		},
		// update/40: `tos_shown_and_accepted`, which Stripe never returns, is kept from
		// configuration.
		"p24": {
			method: "p24", currency: "eur", amount: 2000, billing: jenny(acct), once: true,
			data:    "    p24 = { bank = \"alior_bank\" }\n",
			created: future + "      tos_shown_and_accepted = true\n",
			atFirst: map[string]string{"setup_future_usage": "none", "tos_shown_and_accepted": "true"},
		},
	})
}

// A customer balance's bank transfer is taken on create, with the values of the create of
// `update/31`. The provider sends it on create only, so the change that probe's update makes
// fails the plan, as the intent cannot be replaced.
func TestAccPaymentIntent_customerBalanceBankTransfer(t *testing.T) {
	acct := acctest.NewAccount(t)
	transfer := func(country, types, kind string) string {
		return fmt.Sprintf(`      bank_transfer = {
        eu_bank_transfer        = { country = %q }
        requested_address_types = [%s]
        type                    = %q
      }
      funding_type       = "bank_transfer"
      setup_future_usage = "none"
`, country, types, kind)
	}
	cases := map[string]methodCase{
		// update/31
		"customer_balance": {
			method: "customer_balance", currency: "eur", amount: 2000, customer: true, billing: jenny(acct),
			created: transfer("DE", `"iban"`, "eu_bank_transfer"),
			changed: transfer("FR", `"aba", "iban"`, "gb_bank_transfer"),
			atFirst: map[string]string{
				"bank_transfer.eu_bank_transfer.country":  "DE",
				"bank_transfer.requested_address_types.0": "iban",
				"bank_transfer.type":                      "eu_bank_transfer",
				"funding_type":                            "bank_transfer",
				"setup_future_usage":                      "none",
			},
		},
	}
	resource.ParallelTest(t, resource.TestCase{
		ProtoV6ProviderFactories: tfacc.ProviderFactories,
		Steps: acctest.Forget(t, []resource.TestStep{
			{
				Config:           methodBlocks(acct, cases, false),
				ConfigPlanChecks: resource.ConfigPlanChecks{PostApplyPostRefresh: []plancheck.PlanCheck{plancheck.ExpectEmptyPlan()}},
				Check:            methodChecks(t, acct, cases, false),
			},
			{
				Config:      methodBlocks(acct, cases, true),
				PlanOnly:    true,
				ExpectError: regexp.MustCompile("(?s)Cannot replace stripe_payment_intent.*Changing `payment_method_options.customer_balance.bank_transfer`"),
			},
		}, nil, "stripe_payment_intent"),
	})
}
