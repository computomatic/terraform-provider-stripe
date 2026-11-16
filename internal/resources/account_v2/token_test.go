package account_v2_test

import (
	"context"
	"fmt"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/plancheck"

	"github.com/computomatic/terraform-provider-stripe/internal/acctest"
	"github.com/computomatic/terraform-provider-stripe/internal/acctest/tfacc"
	"github.com/computomatic/terraform-provider-stripe/internal/wire"
)

// newAccountToken makes an account token holding a display name, as the suite's fixture does
// (TestV2CoreAccountAttributes, newV2AccountToken: `{"display_name": "Attrs Token"}`). Stripe
// takes a token once, so each test makes its own.
func newAccountToken(t *testing.T, c acctest.Config) string {
	t.Helper()
	return tfacc.Create(t, c, "/v2/core/account_tokens", wire.Object{{Name: "display_name", Value: wire.String(c.Name("Attrs Token"))}})
}

// tokenConfig is an account made from token, alone, as the suite's probe
// `isolated/account_token_create` sends it, with the data source that reads what the token gave it.
func tokenConfig(c acctest.Config, name, token string) string {
	body := ""
	if token != "" {
		body = fmt.Sprintf("  account_token = %q\n", token)
	}
	return fmt.Sprintf(`
resource "stripe_account_v2" %[1]q {
%[2]s}

data "stripe_account_v2" %[1]q {
  id = stripe_account_v2.%[1]s.id
}
`, name, body)
}

// An account created from a token, sent alone (probe `isolated/account_token_create`), takes the
// display name the token holds. Stripe never returns the token, so state keeps it, and the display
// name the configuration leaves unset stays null while state holds a token, which the data source
// reads.
func TestAccAccountV2_tokenOnCreate(t *testing.T) {
	acct := acctest.NewAccount(t)
	token := newAccountToken(t, acct)
	const res, data = "stripe_account_v2.token", "data.stripe_account_v2.token"
	resource.ParallelTest(t, resource.TestCase{
		ProtoV6ProviderFactories: tfacc.ProviderFactories,
		Steps: acctest.Forget(t, []resource.TestStep{{
			Config: acct.ProviderBlock() + tokenConfig(acct, "token", token),
			Check: resource.ComposeAggregateTestCheckFunc(
				resource.TestCheckResourceAttr(res, "account_token", token),
				resource.TestCheckNoResourceAttr(res, "display_name"),
				resource.TestCheckResourceAttr(data, "display_name", acct.Name("Attrs Token")),
			),
			ConfigPlanChecks: resource.ConfigPlanChecks{PostApplyPostRefresh: []plancheck.PlanCheck{plancheck.ExpectEmptyPlan()}},
		}}, nil, "stripe_account_v2"),
	})
}

// An update of an account created without a token sends a token alone (probe
// `isolated/account_token_update`), and the account takes the display name it holds.
func TestAccAccountV2_tokenOnUpdate(t *testing.T) {
	acct := acctest.NewAccount(t)
	token := newAccountToken(t, acct)
	const res, data = "stripe_account_v2.token", "data.stripe_account_v2.token"
	path := new(string)
	resource.ParallelTest(t, resource.TestCase{
		ProtoV6ProviderFactories: tfacc.ProviderFactories,
		Steps: acctest.Forget(t, []resource.TestStep{
			{
				Config: acct.ProviderBlock() + tokenConfig(acct, "token", ""),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckNoResourceAttr(res, "account_token"),
					resource.TestCheckNoResourceAttr(data, "display_name"),
					resource.TestCheckResourceAttrWith(res, "id", func(v string) error {
						*path = basePath + "/" + v
						return nil
					}),
				),
				ConfigPlanChecks: resource.ConfigPlanChecks{PostApplyPostRefresh: []plancheck.PlanCheck{plancheck.ExpectEmptyPlan()}},
			},
			{
				Config: acct.ProviderBlock() + tokenConfig(acct, "token", token),
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply:             []plancheck.PlanCheck{plancheck.ExpectResourceAction(res, plancheck.ResourceActionUpdate)},
					PostApplyPostRefresh: []plancheck.PlanCheck{plancheck.ExpectEmptyPlan()},
				},
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr(res, "account_token", token),
					resource.TestCheckNoResourceAttr(res, "display_name"),
					resource.TestCheckResourceAttr(data, "display_name", acct.Name("Attrs Token")),
					tfacc.Sent(t, acct, "POST", path, map[string]string{"account_token": token}),
				),
			},
		}, nil, "stripe_account_v2"),
	})
}

// tokenKept is a plan check that the plan keeps the account token of the resource at address as
// the state holds it, so the update, which sends only what changed, does not send it again.
func tokenKept(address, token string) plancheck.PlanCheck { return keptToken{address, token} }

type keptToken struct{ address, token string }

func (k keptToken) CheckPlan(_ context.Context, req plancheck.CheckPlanRequest, resp *plancheck.CheckPlanResponse) {
	for _, rc := range req.Plan.ResourceChanges {
		if rc.Address != k.address {
			continue
		}
		before, _ := rc.Change.Before.(map[string]any)
		after, _ := rc.Change.After.(map[string]any)
		if before["account_token"] != k.token || after["account_token"] != k.token {
			resp.Error = fmt.Errorf("%s plans its token from %v to %v, want %q kept", k.address, before["account_token"], after["account_token"], k.token)
		}
		return
	}
	resp.Error = fmt.Errorf("%s is not in the plan", k.address)
}

// After a token, removing it from the configuration plans nothing and keeps it in state, and a
// later change beside it, such as metadata the configuration adds, plans an update that keeps
// the token, so the update does not send it again. Stripe takes an account made from a token only
// with another token, which the fake answers `fake_unproven`, so that update is only planned.
func TestAccAccountV2_tokenKept(t *testing.T) {
	acct := acctest.NewAccount(t)
	token := newAccountToken(t, acct)
	const res = "stripe_account_v2.token"
	withToken := acct.ProviderBlock() + tokenConfig(acct, "token", token)
	resource.ParallelTest(t, resource.TestCase{
		ProtoV6ProviderFactories: tfacc.ProviderFactories,
		Steps: acctest.Forget(t, []resource.TestStep{
			{
				Config:           withToken,
				ConfigPlanChecks: resource.ConfigPlanChecks{PostApplyPostRefresh: []plancheck.PlanCheck{plancheck.ExpectEmptyPlan()}},
			},
			{
				Config:           acct.ProviderBlock() + tokenConfig(acct, "token", ""),
				ConfigPlanChecks: resource.ConfigPlanChecks{PreApply: []plancheck.PlanCheck{plancheck.ExpectEmptyPlan()}},
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr(res, "account_token", token),
					resource.TestCheckNoResourceAttr(res, "display_name"),
				),
			},
			{
				Config: acct.ProviderBlock() + fmt.Sprintf(`
resource "stripe_account_v2" "token" {
  account_token = %q
  metadata      = { attrs_a = "attrs" }
}
`, token),
				PlanOnly:           true,
				ExpectNonEmptyPlan: true,
				ConfigPlanChecks: resource.ConfigPlanChecks{PostApplyPreRefresh: []plancheck.PlanCheck{
					plancheck.ExpectResourceAction(res, plancheck.ResourceActionUpdate),
					tokenKept(res, token),
				}},
			},
		}, nil, "stripe_account_v2"),
	})
}
