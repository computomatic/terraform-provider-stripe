package setup_intent_test

import (
	"fmt"
	"maps"
	"slices"
	"strings"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/plancheck"

	"github.com/computomatic/terraform-provider-stripe/internal/acctest"
	"github.com/computomatic/terraform-provider-stripe/internal/acctest/tfacc"
)

// methodCase is a setup intent of one payment method, as a probe of the record's per-method
// variants (suite: TestSetupIntentAttributes) made it: with a payment method configuration of its
// own that turns the method on, and an inline payment method of the method's type. created and
// changed are what lands inside the method's `payment_method_options` hash on the create and on
// the update (no options where empty). A case marked once is not updated: its probe's update was
// refused, changes nothing a configuration can, or is sent otherwise by the provider.
type methodCase struct {
	method string
	// billing is what lands inside the inline payment method's `billing_details`, and data
	// what lands inside `payment_method_data` beside it.
	billing, data    string
	created, changed string
	once             bool
	// atFirst and atLast are the intent's attributes after the create and after the update, by
	// their path below the intent; sent is what the update sent of the method's options.
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

func intentAddress(key string) string { return "stripe_setup_intent." + key }

// sortedKeys keeps the rendered configuration in a stable order.
func sortedKeys[M ~map[string]V, V string | methodCase](m M) []string {
	return slices.Sorted(maps.Keys(m))
}

// methodBlocks renders each case's payment method configuration and setup intent, keyed by the
// case's name, with the options of the update where changed is true.
func methodBlocks(acct acctest.Config, cases map[string]methodCase, changed bool) string {
	var b strings.Builder
	b.WriteString(acct.ProviderBlock())
	for _, key := range sortedKeys(cases) {
		c := cases[key]
		options := c.created
		if changed {
			options = c.final()
		}
		opts := ""
		if options != "" {
			opts = fmt.Sprintf("  payment_method_options = {\n    %s = {\n%s    }\n  }\n", c.method, options)
		}
		fmt.Fprintf(&b, `
resource "stripe_payment_method_configuration" %[1]q {
  name = %[2]q
  %[3]s = {
    display_preference = { preference = "on" }
  }
}

resource "stripe_setup_intent" %[1]q {
  payment_method_configuration = stripe_payment_method_configuration.%[1]s.id
  payment_method_data = {
    type = %[3]q
    billing_details = {
%[4]s    }
%[5]s  }
%[6]s}
`, key, acct.Name("attrs-"+key), c.method, c.billing, c.data, opts)
	}
	return b.String()
}

// methodChecks checks each case's attributes, those after the update where changed is true, and
// what the update sent of its options.
func methodChecks(t *testing.T, acct acctest.Config, cases map[string]methodCase, changed bool) resource.TestCheckFunc {
	var checks []resource.TestCheckFunc
	for _, key := range sortedKeys(cases) {
		c := cases[key]
		want := c.atFirst
		if changed && !c.once {
			want = c.atLast
		}
		for _, p := range sortedKeys(want) {
			checks = append(checks, resource.TestCheckResourceAttr(intentAddress(key), p, want[p]))
		}
		if changed && len(c.sent) > 0 {
			sent := map[string]string{}
			for p, v := range c.sent {
				sent["payment_method_options."+c.method+"."+p] = v
			}
			path := new(string)
			checks = append(checks,
				resource.TestCheckResourceAttrWith(intentAddress(key), "id", func(v string) error {
					*path = "/v1/setup_intents/" + v
					return nil
				}),
				tfacc.Sent(t, acct, "POST", path, sent))
		}
	}
	return resource.ComposeAggregateTestCheckFunc(checks...)
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

// The options of Pix, Klarna and UPI change in place, and the inline payment methods of iDEAL
// and Naver Pay take the method's own data on create, each with the values of the probe named
// above it: every intent is created in one apply and updated in a second, in place, with an empty
// plan after each.
func TestAccSetupIntent_methodOptionsChangeInPlace(t *testing.T) {
	acct := acctest.NewAccount(t)
	pix := func(amount int, iof, amountType, end, schedule, reference, start string) string {
		return fmt.Sprintf(`      mandate_options = {
        amount              = %d
        amount_includes_iof = %q
        amount_type         = %q
        currency            = "brl"
        end_date            = %q
        payment_schedule    = %q
        reference           = %q
        start_date          = %q
      }
`, amount, iof, amountType, end, schedule, reference, start)
	}
	subscriptionRef := acct.Name("klarna-subscription")
	cases := map[string]methodCase{
		// update/33
		"pix": {
			method:  "pix",
			billing: jenny(acct) + "      tax_id = \"000.000.000-00\"\n",
			// The mandate reference is a generated value of at most 35 characters (`$gen` `unique`,
			// arg `35`), so it is fixed rather than the account's run-scoped name.
			created: pix(7, "always", "fixed", "2035-02-15", "halfyearly", "attrs-pix-ref-1", "2035-01-15"),
			changed: pix(8, "never", "maximum", "2035-03-15", "monthly", "attrs-pix-ref-2", "2035-01-20"),
			atFirst: map[string]string{
				"payment_method_options.pix.mandate_options.amount":              "7",
				"payment_method_options.pix.mandate_options.amount_includes_iof": "always",
				"payment_method_options.pix.mandate_options.amount_type":         "fixed",
				"payment_method_options.pix.mandate_options.currency":            "brl",
				"payment_method_options.pix.mandate_options.end_date":            "2035-02-15",
				"payment_method_options.pix.mandate_options.payment_schedule":    "halfyearly",
				"payment_method_options.pix.mandate_options.reference":           "attrs-pix-ref-1",
				"payment_method_options.pix.mandate_options.start_date":          "2035-01-15",
			},
			atLast: map[string]string{
				"payment_method_options.pix.mandate_options.amount":              "8",
				"payment_method_options.pix.mandate_options.amount_includes_iof": "never",
				"payment_method_options.pix.mandate_options.amount_type":         "maximum",
				"payment_method_options.pix.mandate_options.end_date":            "2035-03-15",
				"payment_method_options.pix.mandate_options.payment_schedule":    "monthly",
				"payment_method_options.pix.mandate_options.reference":           "attrs-pix-ref-2",
				"payment_method_options.pix.mandate_options.start_date":          "2035-01-20",
			},
		},
		// enums/182: the on-demand and subscription options, which Stripe never returns, are
		// kept from configuration, and the update sends them.
		"klarna": {
			method:  "klarna",
			billing: withAddress(acct, "354 Oyster Point Blvd", "South San Francisco", "CA", "94080", "US"),
			changed: fmt.Sprintf(`      on_demand        = { purchase_interval = "day" }
      preferred_locale = "cs-CZ"
      subscriptions = [{
        interval     = "day"
        next_billing = { amount = 7, date = "2035-01-15" }
        reference    = %q
      }]
`, subscriptionRef),
			atLast: map[string]string{
				"payment_method_options.klarna.preferred_locale":            "cs-CZ",
				"payment_method_options.klarna.on_demand.purchase_interval": "day",
				"payment_method_options.klarna.subscriptions.0.interval":    "day",
			},
			sent: map[string]string{
				"on_demand.purchase_interval":         "day",
				"preferred_locale":                    "cs-CZ",
				"subscriptions.0.interval":            "day",
				"subscriptions.0.next_billing.amount": "7",
				"subscriptions.0.next_billing.date":   "2035-01-15",
				"subscriptions.0.reference":           subscriptionRef,
			},
		},
		// enums/194: `setup_future_usage`, which Stripe never returns, is kept from
		// configuration, and the update sends it.
		"upi": {
			method:  "upi",
			billing: withAddress(acct, "1 MG Road", "Mumbai", "MH", "400001", "IN"),
			changed: "      setup_future_usage = \"on_session\"\n",
			atLast:  map[string]string{"payment_method_options.upi.setup_future_usage": "on_session"},
			sent:    map[string]string{"setup_future_usage": "on_session"},
		},
		// update/30
		"ideal": {
			method: "ideal", billing: jenny(acct), once: true,
			data:    "    ideal = { bank = \"abn_amro\" }\n",
			atFirst: map[string]string{"payment_method_data.ideal.bank": "abn_amro"},
		},
		// update/32
		"naver_pay": {
			method: "naver_pay", billing: jenny(acct), once: true,
			data:    "    naver_pay = { funding = \"card\" }\n",
			atFirst: map[string]string{"payment_method_data.naver_pay.funding": "card"},
		},
	}
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
		}, nil, "stripe_setup_intent"),
	})
}
