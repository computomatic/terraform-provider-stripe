package account_person_v2_test

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

// tokenAccount makes the account the suite's person token probes add their persons to, a US
// merchant (v2PersonAccount, v2MerchantValues), through the API: Stripe takes a person token only
// for the account it was made for, so the account exists before the configuration names a token.
// Stripe cannot delete it, so the test closes it.
func tokenAccount(t *testing.T, c acctest.Config) string {
	t.Helper()
	requested := wire.Object{{Name: "requested", Value: wire.Bool(true)}}
	id := tfacc.Create(t, c, "/v2/core/accounts", wire.Object{
		{Name: "contact_email", Value: wire.String(c.Name("person-tokens") + "@example.com")},
		{Name: "dashboard", Value: wire.String("none")},
		{Name: "identity", Value: wire.Object{{Name: "country", Value: wire.String("US")}}},
		{Name: "defaults", Value: wire.Object{{Name: "responsibilities", Value: wire.Object{
			{Name: "fees_collector", Value: wire.String("application")},
			{Name: "losses_collector", Value: wire.String("application")},
		}}}},
		{Name: "configuration", Value: wire.Object{{Name: "merchant", Value: wire.Object{
			{Name: "capabilities", Value: wire.Object{{Name: "card_payments", Value: requested}}},
		}}}},
	})
	t.Cleanup(func() {
		_, _ = tfacc.Client(c).Do(context.Background(), stripeapi.Request{Method: "POST", Path: "/v2/core/accounts/" + id + "/close", Params: wire.Object{
			{Name: "applied_configurations", Value: wire.Array{wire.String("merchant")}},
		}})
	})
	return id
}

// newPersonToken makes a person token for account holding a name, as the suite's fixture does
// (TestV2CoreAccountPersonAttributes, newV2PersonToken: `{"given_name": "Jenny", "surname":
// "Rosen"}`). Stripe takes a token once, so each person gets its own.
func newPersonToken(t *testing.T, c acctest.Config, account, given, surname string) string {
	t.Helper()
	return tfacc.Create(t, c, "/v2/core/accounts/"+account+"/person_tokens", wire.Object{
		{Name: "given_name", Value: wire.String(given)},
		{Name: "surname", Value: wire.String(surname)},
	})
}

// tokenPerson is a person of account made from token, alone, as the suite's probe
// `isolated/person_token_create` sends it, or with nothing, as the minimal person its probe
// `isolated/person_token_update` updates, with the data source that reads what the token gave it.
func tokenPerson(name, account, token string) string {
	body := ""
	if token != "" {
		body = fmt.Sprintf("  person_token = %q\n", token)
	}
	return fmt.Sprintf(`
resource "stripe_account_person_v2" %[1]q {
  account_id = %[2]q
%[3]s}

data "stripe_account_person_v2" %[1]q {
  account_id = %[2]q
  id         = stripe_account_person_v2.%[1]s.id
}
`, name, account, body)
}

// A person created from a token sent alone takes the name the token holds, and a minimal person
// takes a token sent alone on an update (probes `isolated/person_token_create` and
// `isolated/person_token_update`). Stripe never returns the token, so state keeps it, and the
// names the configuration leaves unset stay null while state holds a token, which the data source
// reads. Stripe refuses a person identical to one the account has (`duplicate_person_not_allowed`,
// dev sandbox, pin 19's smoke run), which the fake does not model, so the updated person's token
// holds another name the record sends a person, `Jane Roe` (probe `update/1`'s update), where the
// suite's probes each made their person on its own.
func TestAccAccountPersonV2_token(t *testing.T) {
	acct := acctest.NewAccount(t)
	account := tokenAccount(t, acct)
	created, updated := newPersonToken(t, acct, account, "Jenny", "Rosen"), newPersonToken(t, acct, account, "Jane", "Roe")
	const made, later = "stripe_account_person_v2.made", "stripe_account_person_v2.later"
	path := new(string)
	config := func(token string) string {
		return acct.ProviderBlock() + tokenPerson("made", account, created) + tokenPerson("later", account, token)
	}
	resource.ParallelTest(t, resource.TestCase{
		ProtoV6ProviderFactories: tfacc.ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: config(""),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr(made, "person_token", created),
					resource.TestCheckNoResourceAttr(made, "given_name"),
					resource.TestCheckNoResourceAttr(made, "surname"),
					resource.TestCheckResourceAttr("data."+made, "given_name", "Jenny"),
					resource.TestCheckResourceAttr("data."+made, "surname", "Rosen"),
					resource.TestCheckNoResourceAttr(later, "person_token"),
					resource.TestCheckNoResourceAttr("data."+later, "given_name"),
					resource.TestCheckResourceAttrWith(later, "id", func(v string) error {
						*path = "/v2/core/accounts/" + account + "/persons/" + v
						return nil
					}),
				),
				ConfigPlanChecks: resource.ConfigPlanChecks{PostApplyPostRefresh: []plancheck.PlanCheck{plancheck.ExpectEmptyPlan()}},
			},
			{
				Config: config(updated),
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{
						plancheck.ExpectResourceAction(made, plancheck.ResourceActionNoop),
						plancheck.ExpectResourceAction(later, plancheck.ResourceActionUpdate),
					},
					PostApplyPostRefresh: []plancheck.PlanCheck{plancheck.ExpectEmptyPlan()},
				},
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr(later, "person_token", updated),
					resource.TestCheckNoResourceAttr(later, "given_name"),
					resource.TestCheckResourceAttr("data."+later, "given_name", "Jane"),
					resource.TestCheckResourceAttr("data."+later, "surname", "Roe"),
					tfacc.Sent(t, acct, "POST", path, map[string]string{"person_token": updated}),
				),
			},
		},
	})
}

// tokenKept is a plan check that the plan keeps the person token of the resource at address as
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
		if before["person_token"] != k.token || after["person_token"] != k.token {
			resp.Error = fmt.Errorf("%s plans its token from %v to %v, want %q kept", k.address, before["person_token"], after["person_token"], k.token)
		}
		return
	}
	resp.Error = fmt.Errorf("%s is not in the plan", k.address)
}

// After a token, removing it from the configuration plans nothing and keeps it in state, and a
// later change beside it, such as metadata the configuration adds, plans an update that keeps
// the token, so the update does not send it again. Stripe takes a person made from a token only
// with another token, which the fake answers `fake_unproven`, so that update is only planned.
func TestAccAccountPersonV2_tokenKept(t *testing.T) {
	acct := acctest.NewAccount(t)
	account := tokenAccount(t, acct)
	token := newPersonToken(t, acct, account, "Jenny", "Rosen")
	const made = "stripe_account_person_v2.made"
	config := func(body string) string {
		return acct.ProviderBlock() + fmt.Sprintf(`
resource "stripe_account_person_v2" "made" {
  account_id = %q
%s}
`, account, body)
	}
	withToken := fmt.Sprintf("  person_token = %q\n", token)
	resource.ParallelTest(t, resource.TestCase{
		ProtoV6ProviderFactories: tfacc.ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config:           config(withToken),
				ConfigPlanChecks: resource.ConfigPlanChecks{PostApplyPostRefresh: []plancheck.PlanCheck{plancheck.ExpectEmptyPlan()}},
			},
			{
				Config:           config(""),
				ConfigPlanChecks: resource.ConfigPlanChecks{PreApply: []plancheck.PlanCheck{plancheck.ExpectEmptyPlan()}},
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr(made, "person_token", token),
					resource.TestCheckNoResourceAttr(made, "given_name"),
				),
			},
			{
				Config:             config(withToken + "  metadata     = { attrs_a = \"attrs\" }\n"),
				PlanOnly:           true,
				ExpectNonEmptyPlan: true,
				ConfigPlanChecks: resource.ConfigPlanChecks{PostApplyPreRefresh: []plancheck.PlanCheck{
					plancheck.ExpectResourceAction(made, plancheck.ResourceActionUpdate),
					tokenKept(made, token),
				}},
			},
		},
	})
}
