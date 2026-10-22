package treasury_financial_account_test

import (
	"errors"
	"fmt"
	"os"
	"regexp"
	"strings"
	"testing"

	"github.com/hashicorp/go-version"
	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/plancheck"
	"github.com/hashicorp/terraform-plugin-testing/tfversion"

	"github.com/computomatic/terraform-provider-stripe/internal/acctest"
	"github.com/computomatic/terraform-provider-stripe/internal/acctest/tfacc"
	"github.com/computomatic/terraform-provider-stripe/internal/stripeapi"
	"github.com/computomatic/terraform-provider-stripe/internal/wire"
)

func TestMain(m *testing.M) { os.Exit(acctest.Main(m)) }

const address = "stripe_treasury_financial_account.test"

// treasury returns the test's account and the configuration for its connected account with
// Treasury active, where financial accounts live.
func treasury(t *testing.T) (acct, conn acctest.Config) {
	t.Helper()
	acct = acctest.NewAccount(t)
	return acct, tfacc.IssuingAccount(t, acct)
}

// accountConfig is a financial account in the given currencies with the `card_issuing` feature
// requested, as the suite creates one (suite: "Issuing cards on a connected account need a
// Treasury financial account"). extra lands inside it.
func accountConfig(acct, conn acctest.Config, currencies, extra string) string {
	return acct.ProviderBlock() + conn.ConnectedProviderBlock() + fmt.Sprintf(`
resource "stripe_treasury_financial_account" "test" {
  provider             = stripe.connected
  supported_currencies = [%s]
  features = {
    card_issuing = { requested = true }
  }
%s}
`, currencies, extra)
}

// skipUpdate skips the test before it creates anything when the fake does not serve updating
// a financial account yet: nothing in the suite proves it, and a test that skipped after the
// create would leave a financial account its closing destroy refuses.
func skipUpdate(t *testing.T, conn acctest.Config) {
	t.Helper()
	tfacc.SkipUnlessFakeServes(t, conn, "POST", "/v1/treasury/financial_accounts/fa_probe", nil)
}

func idOf(dst *string) resource.TestCheckFunc {
	return resource.TestCheckResourceAttrWith(address, "id", func(v string) error {
		*dst = v
		return nil
	})
}

// pathOf records the financial account's API path.
func pathOf(dst *string) resource.TestCheckFunc {
	return resource.TestCheckResourceAttrWith(address, "id", func(v string) error {
		*dst = "/v1/treasury/financial_accounts/" + v
		return nil
	})
}

// A new financial account is open, holds nothing yet and has its `card_issuing` feature active
// at once (suite: "TestTreasuryFinancialAccount", "Issuing answers from the first card runs").
func TestAccTreasuryFinancialAccount_create(t *testing.T) {
	acct, conn := treasury(t)
	resource.ParallelTest(t, resource.TestCase{
		ProtoV6ProviderFactories: tfacc.ProviderFactories,
		Steps: acctest.Forget(t, []resource.TestStep{{
			Config: accountConfig(acct, conn, `"usd"`, fmt.Sprintf("  metadata = { run = %q }\n", acct.Name("create"))) + `
data "stripe_treasury_financial_account" "test" {
  provider = stripe.connected
  id       = stripe_treasury_financial_account.test.id
}
`,
			Check: resource.ComposeAggregateTestCheckFunc(
				resource.TestMatchResourceAttr(address, "id", regexp.MustCompile(`^fa_`)),
				resource.TestCheckResourceAttr(address, "status", "open"),
				resource.TestCheckResourceAttr(address, "country", "US"),
				resource.TestCheckResourceAttr(address, "supported_currencies.#", "1"),
				resource.TestCheckTypeSetElemAttr(address, "supported_currencies.*", "usd"),
				resource.TestCheckResourceAttr(address, "features.card_issuing.requested", "true"),
				resource.TestCheckTypeSetElemAttr(address, "active_features.*", "card_issuing"),
				resource.TestCheckResourceAttr(address, "balance.cash.usd", "0"),
				resource.TestCheckResourceAttr(address, "metadata.run", acct.Name("create")),
				resource.TestCheckResourceAttr(address, "livemode", "false"),
				resource.TestCheckResourceAttrPair("data.stripe_treasury_financial_account.test", "id", address, "id"),
				resource.TestCheckResourceAttr("data.stripe_treasury_financial_account.test", "status", "open"),
				resource.TestCheckTypeSetElemAttr("data.stripe_treasury_financial_account.test", "active_features.*", "card_issuing"),
			),
		}}, nil, "stripe_treasury_financial_account"),
	})
}

// A change to the requested features is refused at plan time, because nothing proves Stripe
// applies one on update. The error says to remove the financial account from the state, not
// to taint it, because its destroy is refused too.
func TestAccTreasuryFinancialAccount_refuseFeatures(t *testing.T) {
	acct, conn := treasury(t)
	resource.ParallelTest(t, resource.TestCase{
		ProtoV6ProviderFactories: tfacc.ProviderFactories,
		Steps: acctest.Forget(t, []resource.TestStep{
			{Config: accountConfig(acct, conn, `"usd"`, "")},
			{
				Config: acct.ProviderBlock() + conn.ConnectedProviderBlock() + `
resource "stripe_treasury_financial_account" "test" {
  provider             = stripe.connected
  supported_currencies = ["usd"]
  features = {
    card_issuing      = { requested = true }
    deposit_insurance = { requested = true }
  }
}
`,
				PlanOnly:    true,
				ExpectError: regexp.MustCompile(`(?s)not supported yet.*ignore_changes.*removed block`),
			},
		}, nil, "stripe_treasury_financial_account"),
	})
}

// Metadata changes in place; the fake does not serve that update yet.
func TestAccTreasuryFinancialAccount_update(t *testing.T) {
	acct, conn := treasury(t)
	skipUpdate(t, conn)
	var first string
	resource.ParallelTest(t, resource.TestCase{
		ProtoV6ProviderFactories: tfacc.ProviderFactories,
		Steps: acctest.Forget(t, []resource.TestStep{
			{Config: accountConfig(acct, conn, `"usd"`, "  metadata = { a = \"1\" }\n"), Check: idOf(&first)},
			{
				Config: accountConfig(acct, conn, `"usd"`, "  metadata = { b = \"2\" }\n"),
				ConfigPlanChecks: resource.ConfigPlanChecks{PreApply: []plancheck.PlanCheck{
					plancheck.ExpectResourceAction(address, plancheck.ResourceActionUpdate),
				}},
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckNoResourceAttr(address, "metadata.a"),
					resource.TestCheckResourceAttr(address, "metadata.b", "2"),
					resource.TestCheckResourceAttrWith(address, "id", func(v string) error {
						if v != first {
							return fmt.Errorf("the update replaced the financial account %s with %s", first, v)
						}
						return nil
					}),
				),
			},
		}, nil, "stripe_treasury_financial_account"),
	})
}

// `supported_currencies` is create-only, so changing it would replace the financial account,
// which Stripe cannot delete: the plan fails, and so does a taint, at apply.
func TestAccTreasuryFinancialAccount_replace(t *testing.T) {
	acct, conn := treasury(t)
	cfg := accountConfig(acct, conn, `"usd"`, "")
	resource.ParallelTest(t, resource.TestCase{
		ProtoV6ProviderFactories: tfacc.ProviderFactories,
		Steps: acctest.Forget(t, []resource.TestStep{
			{Config: cfg},
			{Config: accountConfig(acct, conn, `"usd", "eur"`, ""), PlanOnly: true, ExpectError: tfacc.RefusedReplace},
			{Config: cfg, Taint: []string{address}, ExpectError: tfacc.RefusedDestroy},
		}, nil, "stripe_treasury_financial_account"),
	})
}

// Stripe does not return the requested features, so an import has none, and a configuration
// ignores changes to them (see the import note).
func TestAccTreasuryFinancialAccount_import(t *testing.T) {
	acct, conn := treasury(t)
	extra := fmt.Sprintf("  metadata = { run = %q }\n", acct.Name("import"))
	cfg := accountConfig(acct, conn, `"usd"`, extra)
	imported := accountConfig(acct, conn, `"usd"`, extra+"  lifecycle {\n    ignore_changes = [features]\n  }\n")
	resource.ParallelTest(t, resource.TestCase{
		ProtoV6ProviderFactories: tfacc.ProviderFactories,
		Steps: acctest.Forget(t, []resource.TestStep{
			{Config: cfg},
			{
				Config:                  cfg,
				ResourceName:            address,
				ImportState:             true,
				ImportStateVerify:       true,
				ImportStateVerifyIgnore: []string{"features"},
			},
			{
				ResourceName:    address,
				ImportState:     true,
				ImportStateKind: resource.ImportBlockWithID,
				Config:          imported,
			},
		}, nil, "stripe_treasury_financial_account"),
	})
}

// The identity form needs Terraform or OpenTofu 1.12.0 or later.
func TestAccTreasuryFinancialAccount_importIdentity(t *testing.T) {
	acct, conn := treasury(t)
	cfg := accountConfig(acct, conn, `"usd"`, "  lifecycle {\n    ignore_changes = [features]\n  }\n")
	resource.ParallelTest(t, resource.TestCase{
		ProtoV6ProviderFactories: tfacc.ProviderFactories,
		TerraformVersionChecks:   []tfversion.TerraformVersionCheck{tfversion.RequireAbove(version.Must(version.NewVersion("1.12.0")))},
		Steps: acctest.Forget(t, []resource.TestStep{
			{Config: cfg},
			{
				ResourceName:    address,
				ImportState:     true,
				ImportStateKind: resource.ImportBlockWithResourceIdentity,
				Config:          cfg,
			},
		}, nil, "stripe_treasury_financial_account"),
	})
}

// Stripe cannot delete a financial account and the provider does not close one, so dropping
// it from the configuration, and `terraform destroy`, fail at plan time, and it stays open.
func TestAccTreasuryFinancialAccount_delete(t *testing.T) {
	acct, conn := treasury(t)
	var path string
	var mark int
	cfg := accountConfig(acct, conn, `"usd"`, "")
	resource.ParallelTest(t, resource.TestCase{
		ProtoV6ProviderFactories: tfacc.ProviderFactories,
		Steps: acctest.Forget(t, []resource.TestStep{
			{Config: cfg, Check: resource.ComposeTestCheckFunc(pathOf(&path), tfacc.LogMark(t, conn, &mark))},
			{Config: acctest.Without(t, cfg, "stripe_treasury_financial_account"), PlanOnly: true, ExpectError: tfacc.RefusedDestroy},
			{Config: cfg, Destroy: true, ExpectError: tfacc.RefusedDestroy},
		}, tfacc.Untouched(t, conn, &path, &mark, "status", wire.String("open")), "stripe_treasury_financial_account"),
	})
}

// Metadata changed out of band shows as a change, and the apply restores it. The fake does not
// serve updating a financial account yet, so the test skips there.
func TestAccTreasuryFinancialAccount_drift(t *testing.T) {
	acct, conn := treasury(t)
	skipUpdate(t, conn)
	var id string
	cfg := accountConfig(acct, conn, `"usd"`, "  metadata = { env = \"test\" }\n")
	resource.ParallelTest(t, resource.TestCase{
		ProtoV6ProviderFactories: tfacc.ProviderFactories,
		Steps: acctest.Forget(t, []resource.TestStep{
			{Config: cfg, Check: idOf(&id)},
			{
				PreConfig: func() {
					_, err := tfacc.Client(conn).Do(t.Context(), stripeapi.Request{Method: "POST", Path: "/v1/treasury/financial_accounts/" + id, Params: wire.Object{
						{Name: "metadata", Value: wire.Object{{Name: "env", Value: wire.String("drifted")}}},
					}})
					var apiErr *stripeapi.APIError
					if errors.As(err, &apiErr) && strings.HasPrefix(apiErr.Code, "fake_") {
						t.Skipf("the fake does not serve updating a financial account yet: %v", err)
					}
					if err != nil {
						t.Fatal(err)
					}
				},
				Config: cfg,
				ConfigPlanChecks: resource.ConfigPlanChecks{PreApply: []plancheck.PlanCheck{
					plancheck.ExpectNonEmptyPlan(),
					plancheck.ExpectResourceAction(address, plancheck.ResourceActionUpdate),
				}},
				Check: resource.TestCheckResourceAttr(address, "metadata.env", "test"),
			},
		}, nil, "stripe_treasury_financial_account"),
	})
}
