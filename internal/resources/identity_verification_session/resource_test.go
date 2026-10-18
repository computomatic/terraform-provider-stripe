package identity_verification_session_test

import (
	"fmt"
	"os"
	"regexp"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/plancheck"
	"github.com/hashicorp/terraform-plugin-testing/terraform"

	"github.com/computomatic/terraform-provider-stripe/internal/acctest"
	"github.com/computomatic/terraform-provider-stripe/internal/acctest/tfacc"
	"github.com/computomatic/terraform-provider-stripe/internal/stripeapi"
	"github.com/computomatic/terraform-provider-stripe/internal/wire"
)

func TestMain(m *testing.M) { os.Exit(acctest.Main(m)) }

const address = "stripe_identity_verification_session.test"

// sessionConfig is a verification session of the given type. extra lands inside the session.
func sessionConfig(c acctest.Config, kind, extra string) string {
	return c.ProviderBlock() + fmt.Sprintf(`
resource "stripe_identity_verification_session" "test" {
  type = %q
%s}
`, kind, extra)
}

func idOf(dst *string) resource.TestCheckFunc {
	return resource.TestCheckResourceAttrWith(address, "id", func(v string) error {
		*dst = v
		return nil
	})
}

func status(t *testing.T, acct acctest.Config, id string) (string, error) {
	t.Helper()
	resp, err := tfacc.Client(acct).Do(t.Context(), stripeapi.Request{Method: "GET", Path: "/v1/identity/verification_sessions/" + id})
	if err != nil {
		return "", err
	}
	s, _ := resp.Body.(wire.Object).GetString("status")
	return s, nil
}

func TestAccIdentityVerificationSession_create(t *testing.T) {
	acct := acctest.NewAccount(t)
	resource.ParallelTest(t, resource.TestCase{
		ProtoV6ProviderFactories: tfacc.ProviderFactories,
		Steps: []resource.TestStep{{
			Config: sessionConfig(acct, "document", fmt.Sprintf(`  client_reference_id = %q
  metadata            = { env = "test" }
  options = {
    document = {
      allowed_types        = ["passport"]
      require_live_capture = true
    }
  }
`, acct.Unique("kyc-ref"))) + `
data "stripe_identity_verification_session" "test" {
  id = stripe_identity_verification_session.test.id
}
`,
			Check: resource.ComposeAggregateTestCheckFunc(
				resource.TestMatchResourceAttr(address, "id", regexp.MustCompile(`^vs_`)),
				resource.TestCheckResourceAttr(address, "type", "document"),
				resource.TestCheckResourceAttr(address, "status", "requires_input"),
				resource.TestMatchResourceAttr(address, "client_secret", regexp.MustCompile(`^vs_.+_secret_`)),
				resource.TestMatchResourceAttr(address, "url", regexp.MustCompile(`^https://verify\.stripe\.com/`)),
				resource.TestCheckResourceAttr(address, "client_reference_id", acct.Unique("kyc-ref")),
				resource.TestCheckResourceAttr(address, "metadata.env", "test"),
				resource.TestCheckResourceAttr(address, "options.document.allowed_types.#", "1"),
				resource.TestCheckResourceAttr(address, "options.document.allowed_types.0", "passport"),
				resource.TestCheckResourceAttr(address, "options.document.require_live_capture", "true"),
				resource.TestCheckResourceAttrPair("data.stripe_identity_verification_session.test", "id", address, "id"),
				resource.TestCheckResourceAttr("data.stripe_identity_verification_session.test", "status", "requires_input"),
				resource.TestCheckResourceAttrPair("data.stripe_identity_verification_session.test", "client_secret", address, "client_secret"),
			),
		}},
	})
}

// Stripe accepts `provided_details` and never returns it (suite: "create with optional
// fields"), so the configured value stays in state and the plan is empty.
func TestAccIdentityVerificationSession_providedDetails(t *testing.T) {
	acct := acctest.NewAccount(t)
	cfg := sessionConfig(acct, "document", "  provided_details = { email = \"person@example.com\" }\n")
	resource.ParallelTest(t, resource.TestCase{
		ProtoV6ProviderFactories: tfacc.ProviderFactories,
		Steps: []resource.TestStep{
			{Config: cfg, Check: resource.TestCheckResourceAttr(address, "provided_details.email", "person@example.com")},
			{Config: cfg, PlanOnly: true},
		},
	})
}

// The type and the metadata update in place (suite: "update", "update type"), and the client
// secret that only the create response carried is still in state.
func TestAccIdentityVerificationSession_update(t *testing.T) {
	acct := acctest.NewAccount(t)
	var first, secret string
	resource.ParallelTest(t, resource.TestCase{
		ProtoV6ProviderFactories: tfacc.ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: sessionConfig(acct, "document", "  metadata = { keep = \"me\", drop = \"me\" }\n"),
				Check: resource.ComposeAggregateTestCheckFunc(idOf(&first),
					resource.TestCheckResourceAttrWith(address, "client_secret", func(v string) error { secret = v; return nil })),
			},
			{
				Config: sessionConfig(acct, "id_number", "  metadata = { keep = \"me\", new = \"v\" }\n"),
				ConfigPlanChecks: resource.ConfigPlanChecks{PreApply: []plancheck.PlanCheck{
					plancheck.ExpectResourceAction(address, plancheck.ResourceActionUpdate),
				}},
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr(address, "type", "id_number"),
					resource.TestCheckResourceAttr(address, "metadata.%", "2"),
					resource.TestCheckResourceAttr(address, "metadata.new", "v"),
					resource.TestCheckResourceAttrWith(address, "id", func(v string) error {
						if v != first {
							return fmt.Errorf("the in-place update changed the ID from %s to %s", first, v)
						}
						return nil
					}),
					resource.TestCheckResourceAttrWith(address, "client_secret", func(v string) error {
						if v != secret {
							return fmt.Errorf("the client secret changed on update")
						}
						return nil
					}),
				),
			},
		},
	})
}

// `client_reference_id` is accepted on create only (suite: "update errors", "not updatable"),
// so changing it replaces the session, and so does a change to the options, which the suite
// shows accepted on update but not applied.
func TestAccIdentityVerificationSession_replace(t *testing.T) {
	acct := acctest.NewAccount(t)
	var first, second string
	replaced := func(prev *string) resource.TestCheckFunc {
		return resource.TestCheckResourceAttrWith(address, "id", func(v string) error {
			if v == *prev {
				return fmt.Errorf("the replacement kept the session %s", v)
			}
			return nil
		})
	}
	expectReplace := resource.ConfigPlanChecks{PreApply: []plancheck.PlanCheck{
		plancheck.ExpectResourceAction(address, plancheck.ResourceActionReplace),
	}}
	resource.ParallelTest(t, resource.TestCase{
		ProtoV6ProviderFactories: tfacc.ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: sessionConfig(acct, "document", fmt.Sprintf("  client_reference_id = %q\n  options = { document = { require_live_capture = false } }\n", acct.Unique("kyc-one"))),
				Check:  idOf(&first),
			},
			{
				Config:           sessionConfig(acct, "document", fmt.Sprintf("  client_reference_id = %q\n  options = { document = { require_live_capture = false } }\n", acct.Unique("kyc-two"))),
				ConfigPlanChecks: expectReplace,
				Check:            resource.ComposeAggregateTestCheckFunc(replaced(&first), idOf(&second)),
			},
			{
				Config:           sessionConfig(acct, "document", fmt.Sprintf("  client_reference_id = %q\n  options = { document = { require_live_capture = true } }\n", acct.Unique("kyc-two"))),
				ConfigPlanChecks: expectReplace,
				Check: resource.ComposeAggregateTestCheckFunc(replaced(&second),
					resource.TestCheckResourceAttr(address, "options.document.require_live_capture", "true")),
			},
		},
	})
}

// Stripe does not return `provided_details`, and the URL is once-returned, so the import
// verification leaves out the one and the other.
func TestAccIdentityVerificationSession_import(t *testing.T) {
	acct := acctest.NewAccount(t)
	cfg := sessionConfig(acct, "document", "  metadata = { team = \"a\" }\n")
	resource.ParallelTest(t, resource.TestCase{
		ProtoV6ProviderFactories: tfacc.ProviderFactories,
		Steps: []resource.TestStep{
			{Config: cfg},
			{
				ResourceName:            address,
				ImportState:             true,
				ImportStateVerify:       true,
				ImportStateVerifyIgnore: []string{"url"},
			},
			{
				ResourceName:    address,
				ImportState:     true,
				ImportStateKind: resource.ImportBlockWithID,
				Config:          cfg,
			},
		},
	})
}

// Stripe cannot delete a session; one that requires input cancels (suite: "cancel"), so
// destroy cancels it and the session stays in the account.
func TestAccIdentityVerificationSession_delete(t *testing.T) {
	acct := acctest.NewAccount(t)
	var id string
	resource.ParallelTest(t, resource.TestCase{
		ProtoV6ProviderFactories: tfacc.ProviderFactories,
		CheckDestroy: func(*terraform.State) error {
			got, err := status(t, acct, id)
			if err != nil {
				return fmt.Errorf("a canceled session still retrieves: %w", err)
			}
			if got != "canceled" {
				return fmt.Errorf("session %s is %q after destroy, want canceled", id, got)
			}
			return nil
		},
		Steps: []resource.TestStep{{Config: sessionConfig(acct, "document", ""), Check: idOf(&id)}},
	})
}

// A session canceled out of band reads as canceled, with the client secret and URL that
// Stripe now nulls still in state, and destroy has nothing to cancel (suite: "cancel", "already
// canceled is idempotent").
func TestAccIdentityVerificationSession_deleteAfterCancel(t *testing.T) {
	acct := acctest.NewAccount(t)
	var id string
	cfg := sessionConfig(acct, "document", "")
	resource.ParallelTest(t, resource.TestCase{
		ProtoV6ProviderFactories: tfacc.ProviderFactories,
		Steps: []resource.TestStep{
			{Config: cfg, Check: idOf(&id)},
			{
				PreConfig: func() { tfacc.Post(t, acct, "/v1/identity/verification_sessions/"+id+"/cancel", nil) },
				Config:    cfg,
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr(address, "status", "canceled"),
					resource.TestMatchResourceAttr(address, "client_secret", regexp.MustCompile(`^vs_.+_secret_`)),
				),
			},
		},
	})
}

// A verified session can no longer be canceled, so destroy leaves it as it is after a
// retrieve shows it verified. Only an admin seed reaches a verified session, and the suite
// does not prove what Stripe answers to the cancel: the fake answers `fake_unproven`, which
// the destroy treats as a refusal because the retrieve proves the status.
func TestAccIdentityVerificationSession_deleteVerified(t *testing.T) {
	acctest.RequireFake(t)
	acct := acctest.NewAccount(t)
	id := acctest.Seed(t, acct, wire.Object{
		{Name: "object", Value: wire.String("identity.verification_session")},
		{Name: "type", Value: wire.String("document")},
		{Name: "status", Value: wire.String("verified")},
		{Name: "livemode", Value: wire.Bool(false)},
		{Name: "metadata", Value: wire.Object{}},
		{Name: "client_reference_id", Value: wire.Null{}},
		{Name: "client_secret", Value: wire.Null{}},
		{Name: "last_error", Value: wire.Null{}},
		{Name: "last_verification_report", Value: wire.Null{}},
		{Name: "options", Value: wire.Object{}},
		{Name: "redaction", Value: wire.Null{}},
		{Name: "related_customer", Value: wire.Null{}},
		{Name: "related_customer_account", Value: wire.Null{}},
		{Name: "url", Value: wire.Null{}},
	})[0]
	resource.ParallelTest(t, resource.TestCase{
		ProtoV6ProviderFactories: tfacc.ProviderFactories,
		CheckDestroy: func(*terraform.State) error {
			if got, err := status(t, acct, id); err != nil || got != "verified" {
				return fmt.Errorf("session %s is %q (%v) after destroy, want verified", id, got, err)
			}
			return nil
		},
		Steps: []resource.TestStep{{
			Config:             sessionConfig(acct, "document", ""),
			ResourceName:       address,
			ImportState:        true,
			ImportStateId:      id,
			ImportStatePersist: true,
		}},
	})
}

func TestAccIdentityVerificationSession_drift(t *testing.T) {
	acct := acctest.NewAccount(t)
	var id string
	cfg := sessionConfig(acct, "document", "  metadata = { env = \"test\" }\n")
	resource.ParallelTest(t, resource.TestCase{
		ProtoV6ProviderFactories: tfacc.ProviderFactories,
		Steps: []resource.TestStep{
			{Config: cfg, Check: idOf(&id)},
			{
				PreConfig: func() {
					tfacc.Post(t, acct, "/v1/identity/verification_sessions/"+id, wire.Object{{Name: "metadata", Value: wire.Object{{Name: "env", Value: wire.String("changed")}}}})
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
