package identity_verification_session_test

import (
	"fmt"
	"os"
	"regexp"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/plancheck"

	"github.com/computomatic/terraform-provider-stripe/internal/acctest"
	"github.com/computomatic/terraform-provider-stripe/internal/acctest/tfacc"
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

// pathOf records the session's API path.
func pathOf(dst *string) resource.TestCheckFunc {
	return resource.TestCheckResourceAttrWith(address, "id", func(v string) error {
		*dst = "/v1/identity/verification_sessions/" + v
		return nil
	})
}

func TestAccIdentityVerificationSession_create(t *testing.T) {
	acct := acctest.NewAccount(t)
	resource.ParallelTest(t, resource.TestCase{
		ProtoV6ProviderFactories: tfacc.ProviderFactories,
		Steps: acctest.Forget(t, []resource.TestStep{{
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
		}}, nil, "stripe_identity_verification_session"),
	})
}

// Stripe accepts `provided_details` and never returns it (suite: "create with optional
// fields"), so the configured value stays in state and the plan is empty.
func TestAccIdentityVerificationSession_providedDetails(t *testing.T) {
	acct := acctest.NewAccount(t)
	cfg := sessionConfig(acct, "document", "  provided_details = { email = \"person@example.com\" }\n")
	resource.ParallelTest(t, resource.TestCase{
		ProtoV6ProviderFactories: tfacc.ProviderFactories,
		Steps: acctest.Forget(t, []resource.TestStep{
			{Config: cfg, Check: resource.TestCheckResourceAttr(address, "provided_details.email", "person@example.com")},
			{Config: cfg, PlanOnly: true},
		}, nil, "stripe_identity_verification_session"),
	})
}

// The type and the metadata update in place (suite: "update", "update type"), and the client
// secret that only the create response carried is still in state.
func TestAccIdentityVerificationSession_update(t *testing.T) {
	acct := acctest.NewAccount(t)
	var first, secret string
	resource.ParallelTest(t, resource.TestCase{
		ProtoV6ProviderFactories: tfacc.ProviderFactories,
		Steps: acctest.Forget(t, []resource.TestStep{
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
		}, nil, "stripe_identity_verification_session"),
	})
}

// `client_reference_id` is create-only and `options` replaces too, so changing either would
// replace the session, which Stripe cannot delete: the plan fails, and so does a taint, at
// apply.
func TestAccIdentityVerificationSession_replace(t *testing.T) {
	acct := acctest.NewAccount(t)
	cfg := func(reference string, liveCapture bool) string {
		return sessionConfig(acct, "document", fmt.Sprintf("  client_reference_id = %q\n  options = { document = { require_live_capture = %t } }\n", acct.Unique(reference), liveCapture))
	}
	resource.ParallelTest(t, resource.TestCase{
		ProtoV6ProviderFactories: tfacc.ProviderFactories,
		Steps: acctest.Forget(t, []resource.TestStep{
			{Config: cfg("kyc-one", false)},
			{Config: cfg("kyc-two", false), PlanOnly: true, ExpectError: tfacc.RefusedReplace},
			{Config: cfg("kyc-one", true), PlanOnly: true, ExpectError: tfacc.RefusedReplace},
			{Config: cfg("kyc-one", false), Taint: []string{address}, ExpectError: tfacc.RefusedDestroy},
		}, nil, "stripe_identity_verification_session"),
	})
}

// Stripe does not return `provided_details`, and the URL is once-returned, so the import
// verification leaves out the one and the other.
func TestAccIdentityVerificationSession_import(t *testing.T) {
	acct := acctest.NewAccount(t)
	cfg := sessionConfig(acct, "document", "  metadata = { team = \"a\" }\n")
	resource.ParallelTest(t, resource.TestCase{
		ProtoV6ProviderFactories: tfacc.ProviderFactories,
		Steps: acctest.Forget(t, []resource.TestStep{
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
		}, nil, "stripe_identity_verification_session"),
	})
}

// Stripe cannot delete a session, so dropping it from the configuration, and `terraform
// destroy`, fail at plan time, and the session stays as it was.
func TestAccIdentityVerificationSession_delete(t *testing.T) {
	acct := acctest.NewAccount(t)
	var path string
	var mark int
	cfg := sessionConfig(acct, "document", "")
	resource.ParallelTest(t, resource.TestCase{
		ProtoV6ProviderFactories: tfacc.ProviderFactories,
		Steps: acctest.Forget(t, []resource.TestStep{
			{Config: cfg, Check: resource.ComposeTestCheckFunc(pathOf(&path), tfacc.LogMark(t, acct, &mark))},
			{Config: acctest.Without(t, cfg, "stripe_identity_verification_session"), PlanOnly: true, ExpectError: tfacc.RefusedDestroy},
			{Config: cfg, Destroy: true, ExpectError: tfacc.RefusedDestroy},
		}, tfacc.Untouched(t, acct, &path, &mark, "status", wire.String("requires_input")), "stripe_identity_verification_session"),
	})
}

// Setting `status` to `canceled` cancels the session through its action endpoint (suite:
// "cancel"). It reads back canceled, with the client secret Stripe now nulls still in state.
func TestAccIdentityVerificationSession_cancel(t *testing.T) {
	acct := acctest.NewAccount(t)
	var path string
	resource.ParallelTest(t, resource.TestCase{
		ProtoV6ProviderFactories: tfacc.ProviderFactories,
		Steps: acctest.Forget(t, []resource.TestStep{
			{Config: sessionConfig(acct, "document", ""), Check: pathOf(&path)},
			{
				Config: sessionConfig(acct, "document", "  status = \"canceled\"\n"),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr(address, "status", "canceled"),
					resource.TestMatchResourceAttr(address, "client_secret", regexp.MustCompile(`^vs_.+_secret_`)),
					tfacc.Holds(t, acct, &path, "status", wire.String("canceled")),
				),
			},
		}, nil, "stripe_identity_verification_session"),
	})
}

func TestAccIdentityVerificationSession_drift(t *testing.T) {
	acct := acctest.NewAccount(t)
	var id string
	cfg := sessionConfig(acct, "document", "  metadata = { env = \"test\" }\n")
	resource.ParallelTest(t, resource.TestCase{
		ProtoV6ProviderFactories: tfacc.ProviderFactories,
		Steps: acctest.Forget(t, []resource.TestStep{
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
		}, nil, "stripe_identity_verification_session"),
	})
}
