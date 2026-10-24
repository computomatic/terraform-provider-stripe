package account_test

import (
	"fmt"
	"regexp"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/terraform"

	"github.com/computomatic/terraform-provider-stripe/internal/acctest"
	"github.com/computomatic/terraform-provider-stripe/internal/acctest/tfacc"
	"github.com/computomatic/terraform-provider-stripe/internal/stripeapi"
	"github.com/computomatic/terraform-provider-stripe/internal/wire"
)

// The provider's `stripe_context` is sent as `Stripe-Context` on every request. With the
// platform's key on /v1, Stripe treats it as `Stripe-Account`, and naming the platform's own
// account as no header (suite: "TestAccountStripeContext"). These tests manage a customer,
// and check from outside, through the platform or the connected account, where it lives.

const contextCustomer = "stripe_customer.test"

func contextCustomerConfig(provider, name string) string {
	return provider + fmt.Sprintf(`
resource "stripe_customer" "test" {
  name = %q
}
`, name)
}

// customerIn checks that the customer in state lives in c's account: a retrieve through c
// finds it, and when other is set, a retrieve through other answers 404.
func customerIn(t *testing.T, c acctest.Config, other *acctest.Config) resource.TestCheckFunc {
	return func(s *terraform.State) error {
		id := s.RootModule().Resources[contextCustomer].Primary.ID
		if _, err := tfacc.Client(c).Do(t.Context(), stripeapi.Request{Method: "GET", Path: "/v1/customers/" + id}); err != nil {
			return fmt.Errorf("customer %s is not in the account %q: %w", id, c.StripeAccount, err)
		}
		if other == nil {
			return nil
		}
		_, err := tfacc.Client(*other).Do(t.Context(), stripeapi.Request{Method: "GET", Path: "/v1/customers/" + id})
		if !stripeapi.IsNotFound(err) {
			return fmt.Errorf("customer %s through the account %q: %v, want 404", id, other.StripeAccount, err)
		}
		return nil
	}
}

// customerGone checks after destroy that the customer is deleted in c's account.
func customerGone(t *testing.T, c acctest.Config, id *string) resource.TestCheckFunc {
	return func(*terraform.State) error {
		resp, err := tfacc.Client(c).Do(t.Context(), stripeapi.Request{Method: "GET", Path: "/v1/customers/" + *id})
		if stripeapi.IsNotFound(err) {
			return nil
		}
		if err != nil {
			return err
		}
		if deleted, _ := resp.Body.(wire.Object).GetBool("deleted"); !deleted {
			return fmt.Errorf("customer %s is still in the account %q", *id, c.StripeAccount)
		}
		return nil
	}
}

func customerID(dst *string) resource.TestCheckFunc {
	return resource.TestCheckResourceAttrWith(contextCustomer, "id", func(v string) error {
		*dst = v
		return nil
	})
}

// A customer managed through a `stripe_context` that names a connected account is created,
// updated, read and deleted inside that account, not the platform.
func TestAccAccount_stripeContextConnected(t *testing.T) {
	acct := acctest.NewAccount(t)
	conn := acct.Connected(tfacc.TransfersAccount(t, acct))
	provider := acct.ProviderBlockWith(fmt.Sprintf("stripe_context = %q", conn.StripeAccount))
	var id string
	resource.ParallelTest(t, resource.TestCase{
		ProtoV6ProviderFactories: tfacc.ProviderFactories,
		CheckDestroy:             customerGone(t, conn, &id),
		Steps: []resource.TestStep{
			{
				Config: contextCustomerConfig(provider, acct.Name("In context")),
				Check:  resource.ComposeAggregateTestCheckFunc(customerID(&id), customerIn(t, conn, &acct)),
			},
			{
				Config: contextCustomerConfig(provider, acct.Name("Renamed in context")),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr(contextCustomer, "name", acct.Name("Renamed in context")),
					customerIn(t, conn, &acct),
				),
			},
		},
	})
}

// A `stripe_context` that names the platform's own account acts as the platform.
func TestAccAccount_stripeContextPlatform(t *testing.T) {
	acct := acctest.NewAccount(t)
	platform := acct.AccountID
	if acct.Smoke {
		platform = platformID(t, acct)
	}
	var id string
	resource.ParallelTest(t, resource.TestCase{
		ProtoV6ProviderFactories: tfacc.ProviderFactories,
		CheckDestroy:             customerGone(t, acct, &id),
		Steps: []resource.TestStep{{
			Config: contextCustomerConfig(acct.ProviderBlockWith(fmt.Sprintf("stripe_context = %q", platform)), acct.Name("Platform context")),
			Check:  resource.ComposeAggregateTestCheckFunc(customerID(&id), customerIn(t, acct, nil)),
		}},
	})
}

// `stripe_account` and `stripe_context` together fail when the provider is configured,
// before any request.
func TestAccAccount_stripeContextConflict(t *testing.T) {
	acct := acctest.NewAccount(t)
	provider := acct.ProviderBlockWith(`stripe_account = "acct_1"`, `stripe_context = "acct_1"`)
	resource.ParallelTest(t, resource.TestCase{
		ProtoV6ProviderFactories: tfacc.ProviderFactories,
		Steps: []resource.TestStep{{
			Config:      contextCustomerConfig(provider, acct.Name("Never created")),
			ExpectError: regexp.MustCompile(`Conflicting account settings`),
		}},
	})
}

// `STRIPE_CONTEXT` sets the context when the provider block does not. Setting the environment
// keeps the test serial.
func TestAccAccount_stripeContextEnv(t *testing.T) {
	acct := acctest.NewAccount(t)
	conn := acct.Connected(tfacc.TransfersAccount(t, acct))
	t.Setenv("STRIPE_CONTEXT", conn.StripeAccount)
	var id string
	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: tfacc.ProviderFactories,
		CheckDestroy:             customerGone(t, conn, &id),
		Steps: []resource.TestStep{{
			Config: contextCustomerConfig(acct.ProviderBlock(), acct.Name("Context from the environment")),
			Check:  resource.ComposeAggregateTestCheckFunc(customerID(&id), customerIn(t, conn, &acct)),
		}},
	})
}

// platformID is the ID of the account c's key belongs to.
func platformID(t *testing.T, c acctest.Config) string {
	t.Helper()
	resp, err := tfacc.Client(c).Do(t.Context(), stripeapi.Request{Method: "GET", Path: "/v1/account"})
	if err != nil {
		t.Fatalf("GET /v1/account: %v", err)
	}
	id, _ := resp.Body.(wire.Object).GetString("id")
	if id == "" {
		t.Fatal("GET /v1/account returned no id")
	}
	return id
}
