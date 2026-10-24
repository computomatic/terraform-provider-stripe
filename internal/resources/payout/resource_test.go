package payout_test

import (
	"fmt"
	"regexp"
	"strings"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/plancheck"

	"github.com/computomatic/terraform-provider-stripe/internal/acctest"
	"github.com/computomatic/terraform-provider-stripe/internal/acctest/tfacc"
	"github.com/computomatic/terraform-provider-stripe/internal/wire"
)

const resAddress = "stripe_payout.test"

// bankAccount is the platform's own US bank account, which payouts in usd go to. Stripe adds
// one only in the Dashboard ("create without an external account"), and the fake only through
// its admin seed.
func bankAccount() wire.Object {
	return wire.Object{
		{Name: "object", Value: wire.String("bank_account")},
		{Name: "country", Value: wire.String("US")},
		{Name: "currency", Value: wire.String("usd")},
		{Name: "last4", Value: wire.String("6789")},
		{Name: "status", Value: wire.String("new")},
		{Name: "bank_name", Value: wire.String("STRIPE TEST BANK")},
		{Name: "routing_number", Value: wire.String("110000000")},
		{Name: "default_for_currency", Value: wire.Bool(true)},
	}
}

// payoutAccount is an account a payout in usd can be made from: it holds a bank account in usd
// and at least need available. The sandbox already has a bank account (BD: "Payouts on the US
// sandbox settle within seconds"), so smoke mode seeds nothing.
func payoutAccount(t *testing.T, need int64) acctest.Config {
	t.Helper()
	acct := acctest.NewAccount(t)
	if !acct.Smoke {
		acctest.Seed(t, acct, bankAccount())
	}
	tfacc.FundAvailableBalance(t, acct, "usd", need)
	return acct
}

// payoutConfig is a payout of amount cents. extra lands inside it.
func payoutConfig(c acctest.Config, amount int, extra string) string {
	return c.ProviderBlock() + fmt.Sprintf(`
resource "stripe_payout" "test" {
  amount   = %d
  currency = "usd"
%s}
`, amount, extra)
}

func payoutPath(dst *string) resource.TestCheckFunc {
	return resource.TestCheckResourceAttrWith(resAddress, "id", func(v string) error {
		*dst = "/v1/payouts/" + v
		return nil
	})
}

// A payout goes to the default bank account in its currency (suite: "create"). Stripe moves it
// on from `pending` within seconds.
func TestAccPayout_create(t *testing.T) {
	acct := payoutAccount(t, 500)
	resource.ParallelTest(t, resource.TestCase{
		ProtoV6ProviderFactories: tfacc.ProviderFactories,
		Steps: acctest.Forget(t, []resource.TestStep{{
			Config: payoutConfig(acct, 500, "  description = \"Test payout\"\n  metadata = { env = \"test\" }\n") + `
data "stripe_payout" "by_id" {
  id = stripe_payout.test.id
}
`,
			Check: resource.ComposeAggregateTestCheckFunc(
				resource.TestCheckResourceAttr(resAddress, "amount", "500"),
				resource.TestCheckResourceAttr(resAddress, "currency", "usd"),
				resource.TestCheckResourceAttr(resAddress, "description", "Test payout"),
				resource.TestCheckResourceAttr(resAddress, "metadata.env", "test"),
				resource.TestCheckResourceAttr(resAddress, "method", "standard"),
				resource.TestCheckResourceAttr(resAddress, "type", "bank_account"),
				resource.TestMatchResourceAttr(resAddress, "destination", regexp.MustCompile(`^ba_`)),
				resource.TestMatchResourceAttr(resAddress, "status", regexp.MustCompile(`^(pending|in_transit|paid)$`)),
				resource.TestCheckResourceAttrPair("data.stripe_payout.by_id", "id", resAddress, "id"),
			),
		}}, nil, "stripe_payout"),
	})
}

// `metadata` changes in place (suite: "update").
func TestAccPayout_update(t *testing.T) {
	acct := payoutAccount(t, 500)
	resource.ParallelTest(t, resource.TestCase{
		ProtoV6ProviderFactories: tfacc.ProviderFactories,
		Steps: acctest.Forget(t, []resource.TestStep{
			{Config: payoutConfig(acct, 500, "  metadata = { keep = \"me\", drop = \"me\" }\n")},
			{
				Config: payoutConfig(acct, 500, "  metadata = { keep = \"me\", new = \"v\" }\n"),
				ConfigPlanChecks: resource.ConfigPlanChecks{PreApply: []plancheck.PlanCheck{
					plancheck.ExpectResourceAction(resAddress, plancheck.ResourceActionUpdate),
				}},
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr(resAddress, "metadata.%", "2"),
					resource.TestCheckResourceAttr(resAddress, "metadata.new", "v"),
				),
			},
		}, nil, "stripe_payout"),
	})
}

// `description` cannot change (suite: "description is not updatable"), nor `amount` ("immutable
// amount"), and a payout cannot be destroyed, so a change to either and a taint are refused.
func TestAccPayout_replace(t *testing.T) {
	acct := payoutAccount(t, 500)
	cfg := payoutConfig(acct, 500, "  description = \"one\"\n")
	resource.ParallelTest(t, resource.TestCase{
		ProtoV6ProviderFactories: tfacc.ProviderFactories,
		Steps: acctest.Forget(t, []resource.TestStep{
			{Config: cfg},
			{Config: payoutConfig(acct, 500, "  description = \"two\"\n"), PlanOnly: true, ExpectError: tfacc.RefusedReplace},
			{Config: payoutConfig(acct, 600, "  description = \"one\"\n"), PlanOnly: true, ExpectError: tfacc.RefusedReplace},
			{Config: cfg, Taint: []string{resAddress}, ExpectError: tfacc.RefusedDestroy},
		}, nil, "stripe_payout"),
	})
}

func TestAccPayout_import(t *testing.T) {
	acct := payoutAccount(t, 500)
	cfg := payoutConfig(acct, 500, "  description = \"Test payout\"\n  metadata = { team = \"a\" }\n")
	resource.ParallelTest(t, resource.TestCase{
		ProtoV6ProviderFactories: tfacc.ProviderFactories,
		Steps: acctest.Forget(t, []resource.TestStep{
			{Config: cfg},
			// Stripe settles a payout within seconds, so the imported `status` and `trace_id` may
			// be later than the state's.
			{ResourceName: resAddress, ImportState: true, ImportStateVerify: true, ImportStateVerifyIgnore: []string{"status", "trace_id"}},
			{ResourceName: resAddress, ImportState: true, ImportStateKind: resource.ImportBlockWithID, Config: cfg},
		}, nil, "stripe_payout"),
	})
}

// Stripe cannot delete a payout, so dropping it from the configuration, and `terraform destroy`,
// fail at plan time, and the payout stays as it was.
func TestAccPayout_delete(t *testing.T) {
	acct := payoutAccount(t, 500)
	var path string
	var mark int
	cfg := payoutConfig(acct, 500, "  description = \"Test payout\"\n")
	resource.ParallelTest(t, resource.TestCase{
		ProtoV6ProviderFactories: tfacc.ProviderFactories,
		Steps: acctest.Forget(t, []resource.TestStep{
			{Config: cfg, Check: resource.ComposeTestCheckFunc(payoutPath(&path), tfacc.LogMark(t, acct, &mark))},
			{Config: acctest.Without(t, cfg, "stripe_payout"), PlanOnly: true, ExpectError: tfacc.RefusedDestroy},
			{Config: cfg, Destroy: true, ExpectError: tfacc.RefusedDestroy},
		}, tfacc.Untouched(t, acct, &path, &mark, "description", wire.String("Test payout")), "stripe_payout"),
	})
}

// `metadata` changed outside Terraform is set back.
func TestAccPayout_drift(t *testing.T) {
	acct := payoutAccount(t, 500)
	var path string
	cfg := payoutConfig(acct, 500, "  metadata = { env = \"test\" }\n")
	resource.ParallelTest(t, resource.TestCase{
		ProtoV6ProviderFactories: tfacc.ProviderFactories,
		Steps: acctest.Forget(t, []resource.TestStep{
			{Config: cfg, Check: payoutPath(&path)},
			{
				PreConfig: func() {
					tfacc.Post(t, acct, path, wire.Object{{Name: "metadata", Value: wire.Object{{Name: "env", Value: wire.String("changed")}}}})
				},
				Config: cfg,
				ConfigPlanChecks: resource.ConfigPlanChecks{PreApply: []plancheck.PlanCheck{
					plancheck.ExpectNonEmptyPlan(),
					plancheck.ExpectResourceAction(resAddress, plancheck.ResourceActionUpdate),
				}},
				Check: resource.TestCheckResourceAttr(resAddress, "metadata.env", "test"),
			},
		}, nil, "stripe_payout"),
	})
}

// A payout moves money, so in an account whose balance reports live mode it is refused at plan
// time until the provider allows live money movement, and so is a change to it (ADR
// 2026-10-22-money-moving-resources).
func TestAccPayout_liveMode(t *testing.T) {
	acctest.RequireFake(t)
	acct := acctest.NewAccount(t)
	funds := wire.Array{wire.Object{{Name: "amount", Value: wire.Int(10_000)}, {Name: "currency", Value: wire.String("usd")}}}
	acctest.Seed(t, acct, bankAccount(), wire.Object{
		{Name: "object", Value: wire.String("balance")},
		{Name: "livemode", Value: wire.Bool(true)},
		{Name: "available", Value: funds},
		{Name: "pending", Value: wire.Array{wire.Object{{Name: "amount", Value: wire.Int(0)}, {Name: "currency", Value: wire.String("usd")}}}},
	})
	refused := regexp.MustCompile(`Live-mode money movement is not allowed`)
	allow := strings.Replace(payoutConfig(acct, 500, ""), "provider \"stripe\" {\n", "provider \"stripe\" {\n  allow_live_money_movement = true\n", 1)
	resource.ParallelTest(t, resource.TestCase{
		ProtoV6ProviderFactories: tfacc.ProviderFactories,
		Steps: acctest.Forget(t, []resource.TestStep{
			{Config: payoutConfig(acct, 500, ""), PlanOnly: true, ExpectError: refused},
			{Config: allow, Check: resource.TestCheckResourceAttr(resAddress, "status", "pending")},
			{Config: payoutConfig(acct, 500, "  metadata = { env = \"live\" }\n"), PlanOnly: true, ExpectError: refused},
		}, nil, "stripe_payout"),
	})
}
