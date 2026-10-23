package financial_connections_account_owner_test

import (
	"fmt"
	"os"
	"regexp"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"

	"github.com/computomatic/terraform-provider-stripe/internal/acctest"
	"github.com/computomatic/terraform-provider-stripe/internal/acctest/tfacc"
	"github.com/computomatic/terraform-provider-stripe/internal/stripeapi"
	"github.com/computomatic/terraform-provider-stripe/internal/wire"
)

func TestMain(m *testing.M) { os.Exit(acctest.Main(m)) }

const address = "data.stripe_financial_connections_account_owner.test"

// config reads the owner id of account, of ownership when it is not empty and of the account's
// latest otherwise.
func config(c acctest.Config, account, id, ownership string) string {
	attr := ""
	if ownership != "" {
		attr = fmt.Sprintf("  ownership = %q\n", ownership)
	}
	return c.ProviderBlock() + fmt.Sprintf(`
data "stripe_financial_connections_account_owner" "test" {
  account = %q
  id      = %q
%s}
`, account, id, attr)
}

// ownerOf returns the ID of the one owner the ownership of account lists.
func ownerOf(t *testing.T, c acctest.Config, account, ownership string) string {
	t.Helper()
	path := "/v1/financial_connections/accounts/" + account + "/owners"
	resp, err := tfacc.Client(c).Do(t.Context(), stripeapi.Request{Method: "GET", Path: path, Params: wire.Object{{Name: "ownership", Value: wire.String(ownership)}}})
	if err != nil {
		t.Fatalf("GET %s: %v", path, err)
	}
	data, _ := resp.Body.(wire.Object).Get("data")
	items, _ := data.(wire.Array)
	if len(items) != 1 {
		t.Fatalf("GET %s lists %d owners, want 1", path, len(items))
	}
	id, _ := items[0].(wire.Object).GetString("id")
	return id
}

// An ownership refresh lists one owner, `Jane Smith` at the test institution (suite: "Financial
// Connections ownership refresh and owners"). The account is refreshed twice: without
// `ownership` the data source reads the latest, and with it an earlier one.
func TestAccFinancialConnectionsAccountOwner_read(t *testing.T) {
	acct := acctest.NewAccount(t)
	account := tfacc.LinkedAccount(t, acct)
	earlier := tfacc.RefreshedOwnership(t, acct, account)
	earlierOwner := ownerOf(t, acct, account, earlier)
	latest := tfacc.RefreshedOwnership(t, acct, account)
	latestOwner := ownerOf(t, acct, account, latest)
	resource.ParallelTest(t, resource.TestCase{
		ProtoV6ProviderFactories: tfacc.ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: config(acct, account, latestOwner, ""),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr(address, "id", latestOwner),
					resource.TestCheckResourceAttr(address, "account", account),
					resource.TestCheckResourceAttr(address, "ownership", latest),
					resource.TestCheckResourceAttr(address, "name", "Jane Smith"),
					resource.TestCheckResourceAttr(address, "phone", "617 555-3082"),
					resource.TestCheckResourceAttr(address, "raw_address", "123 MAIN STREET ANYTOWN USA US"),
					resource.TestMatchResourceAttr(address, "email", regexp.MustCompile(`^nobody\+.+@stripe\.com$`)),
					resource.TestCheckResourceAttrSet(address, "refreshed_at"),
				),
			},
			{
				Config: config(acct, account, earlierOwner, earlier),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr(address, "id", earlierOwner),
					resource.TestCheckResourceAttr(address, "ownership", earlier),
					resource.TestCheckResourceAttr(address, "name", "Jane Smith"),
				),
			},
		},
	})
}

// An owner of an earlier ownership is not in the latest, which the data source reads without
// `ownership`, and an account never refreshed has no ownership to read. An unknown account is
// Stripe's 404 `resource_missing` (suite: "Financial Connections ownership refresh and
// owners").
func TestAccFinancialConnectionsAccountOwner_notFound(t *testing.T) {
	acct := acctest.NewAccount(t)
	unrefreshed := tfacc.LinkedAccount(t, acct)
	account := tfacc.LinkedAccount(t, acct)
	earlierOwner := ownerOf(t, acct, account, tfacc.RefreshedOwnership(t, acct, account))
	tfacc.RefreshedOwnership(t, acct, account)
	resource.ParallelTest(t, resource.TestCase{
		ProtoV6ProviderFactories: tfacc.ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config:      config(acct, account, earlierOwner, ""),
				ExpectError: regexp.MustCompile(`no stripe_financial_connections_account_owner found for\s+id=` + earlierOwner),
			},
			{
				Config:      config(acct, unrefreshed, "fcaown_doesnotexist", ""),
				ExpectError: regexp.MustCompile(`account\s+` + unrefreshed + `\s+has\s+no\s+` + "`ownership`" + `:\s+refresh\s+the\s+account's\s+ownership\s+first`),
			},
			{
				Config:      config(acct, "fca_doesnotexist", "fcaown_doesnotexist", ""),
				ExpectError: regexp.MustCompile(`(?s)Stripe API error: resource_missing.*fca_doesnotexist`),
			},
		},
	})
}
