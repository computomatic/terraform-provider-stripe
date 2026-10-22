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
	"github.com/hashicorp/terraform-plugin-testing/terraform"
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

// skipUpdate skips the test when the fake does not serve updating a financial account yet:
// nothing in the suite proves it.
func skipUpdate(t *testing.T) func(error) error {
	return tfacc.SkipWhenFakeLacks(t, "PostTreasuryFinancialAccountsFinancialAccount")
}

func idOf(dst *string) resource.TestCheckFunc {
	return resource.TestCheckResourceAttrWith(address, "id", func(v string) error {
		*dst = v
		return nil
	})
}

func status(t *testing.T, conn acctest.Config, id string) string {
	t.Helper()
	resp, err := tfacc.Client(conn).Do(t.Context(), stripeapi.Request{Method: "GET", Path: "/v1/treasury/financial_accounts/" + id})
	if err != nil {
		t.Fatalf("reading financial account %s: %v", id, err)
	}
	s, _ := resp.Body.(wire.Object).GetString("status")
	return s
}

// A new financial account is open, holds nothing yet and has its `card_issuing` feature active
// at once (suite: "TestTreasuryFinancialAccount", "Issuing answers from the first card runs").
func TestAccTreasuryFinancialAccount_create(t *testing.T) {
	acct, conn := treasury(t)
	resource.ParallelTest(t, resource.TestCase{
		ProtoV6ProviderFactories: tfacc.ProviderFactories,
		Steps: []resource.TestStep{{
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
		}},
	})
}

// A change to the requested features is refused at plan time, because nothing proves Stripe
// applies one on update. Metadata changes in place; the fake does not serve that update yet.
func TestAccTreasuryFinancialAccount_update(t *testing.T) {
	acct, conn := treasury(t)
	var first string
	resource.ParallelTest(t, resource.TestCase{
		ProtoV6ProviderFactories: tfacc.ProviderFactories,
		ErrorCheck:               skipUpdate(t),
		Steps: []resource.TestStep{
			{Config: accountConfig(acct, conn, `"usd"`, "  metadata = { a = \"1\" }\n"), Check: idOf(&first)},
			{
				Config: acct.ProviderBlock() + conn.ConnectedProviderBlock() + `
resource "stripe_treasury_financial_account" "test" {
  provider             = stripe.connected
  supported_currencies = ["usd"]
  features = {
    card_issuing      = { requested = true }
    deposit_insurance = { requested = true }
  }
  metadata = { a = "1" }
}
`,
				PlanOnly:    true,
				ExpectError: regexp.MustCompile(`(?s)not supported yet.*ignore_changes`),
			},
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
		},
	})
}

// A replacement leaves the old financial account open, because destroy does not close it.
// `supported_currencies` is create-only, so a change replaces the account; the fake does not
// serve a currency other than `usd` yet, so that step skips there.
func TestAccTreasuryFinancialAccount_replace(t *testing.T) {
	acct, conn := treasury(t)
	var first, second string
	resource.ParallelTest(t, resource.TestCase{
		ProtoV6ProviderFactories: tfacc.ProviderFactories,
		ErrorCheck:               tfacc.SkipWhenFakeLacks(t, "other than usd"),
		Steps: []resource.TestStep{
			{Config: accountConfig(acct, conn, `"usd"`, ""), Check: idOf(&first)},
			{
				Config: accountConfig(acct, conn, `"usd"`, ""),
				Taint:  []string{address},
				ConfigPlanChecks: resource.ConfigPlanChecks{PreApply: []plancheck.PlanCheck{
					plancheck.ExpectResourceAction(address, plancheck.ResourceActionReplace),
				}},
				Check: resource.ComposeAggregateTestCheckFunc(
					idOf(&second),
					resource.TestCheckResourceAttrWith(address, "id", func(v string) error {
						if v == first {
							return fmt.Errorf("the replacement kept the financial account %s", v)
						}
						if got := status(t, conn, first); got != "open" {
							return fmt.Errorf("the replaced financial account %s is %s, want open", first, got)
						}
						return nil
					}),
				),
			},
			{
				Config: accountConfig(acct, conn, `"usd", "eur"`, ""),
				ConfigPlanChecks: resource.ConfigPlanChecks{PreApply: []plancheck.PlanCheck{
					plancheck.ExpectResourceAction(address, plancheck.ResourceActionReplace),
				}},
				Check: resource.TestCheckResourceAttrWith(address, "id", func(v string) error {
					if v == second {
						return fmt.Errorf("the currency change kept the financial account %s", v)
					}
					return nil
				}),
			},
		},
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
		Steps: []resource.TestStep{
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
		},
	})
}

// The identity form needs Terraform or OpenTofu 1.12.0 or later.
func TestAccTreasuryFinancialAccount_importIdentity(t *testing.T) {
	acct, conn := treasury(t)
	cfg := accountConfig(acct, conn, `"usd"`, "  lifecycle {\n    ignore_changes = [features]\n  }\n")
	resource.ParallelTest(t, resource.TestCase{
		ProtoV6ProviderFactories: tfacc.ProviderFactories,
		TerraformVersionChecks:   []tfversion.TerraformVersionCheck{tfversion.RequireAbove(version.Must(version.NewVersion("1.12.0")))},
		Steps: []resource.TestStep{
			{Config: cfg},
			{
				ResourceName:    address,
				ImportState:     true,
				ImportStateKind: resource.ImportBlockWithResourceIdentity,
				Config:          cfg,
			},
		},
	})
}

// Destroy leaves the financial account open on the connected account, with a warning: nothing
// proves closing one.
func TestAccTreasuryFinancialAccount_delete(t *testing.T) {
	acct, conn := treasury(t)
	var id string
	resource.ParallelTest(t, resource.TestCase{
		ProtoV6ProviderFactories: tfacc.ProviderFactories,
		CheckDestroy: func(*terraform.State) error {
			if got := status(t, conn, id); got != "open" {
				return fmt.Errorf("financial account %s is %s after destroy, want open", id, got)
			}
			return nil
		},
		Steps: []resource.TestStep{{Config: accountConfig(acct, conn, `"usd"`, ""), Check: idOf(&id)}},
	})
}

// Metadata changed out of band shows as a change, and the apply restores it. The fake does not
// serve updating a financial account yet, so the test skips there.
func TestAccTreasuryFinancialAccount_drift(t *testing.T) {
	acct, conn := treasury(t)
	var id string
	cfg := accountConfig(acct, conn, `"usd"`, "  metadata = { env = \"test\" }\n")
	resource.ParallelTest(t, resource.TestCase{
		ProtoV6ProviderFactories: tfacc.ProviderFactories,
		ErrorCheck:               skipUpdate(t),
		Steps: []resource.TestStep{
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
		},
	})
}
