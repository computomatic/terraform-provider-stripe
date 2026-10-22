package webhook_endpoint_test

import (
	"fmt"
	"os"
	"strings"
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

const address = "stripe_webhook_endpoint.test"

// endpointConfig is an endpoint with body inside its block. An endpoint has no name, so a
// body that sets no description gets one, which is how a smoke run recognises its objects.
func endpointConfig(c acctest.Config, body string) string {
	if !strings.Contains(body, "description") {
		body += fmt.Sprintf("  description = %q\n", c.Name("endpoint"))
	}
	return c.ProviderBlock() + "resource \"stripe_webhook_endpoint\" \"test\" {\n" + body + "}\n"
}

// describe is the description line of an endpoint body.
func describe(c acctest.Config, text string) string {
	return fmt.Sprintf("  description = %q\n", c.Name(text))
}

func idOf(dst *string) resource.TestCheckFunc {
	return resource.TestCheckResourceAttrWith(address, "id", func(v string) error {
		*dst = v
		return nil
	})
}

const basic = `
  url            = "https://example.com/hook"
  enabled_events = ["invoice.paid"]
`

func TestAccWebhookEndpoint_create(t *testing.T) {
	acct := acctest.NewAccount(t)
	resource.ParallelTest(t, resource.TestCase{
		ProtoV6ProviderFactories: tfacc.ProviderFactories,
		Steps: []resource.TestStep{{
			Config: endpointConfig(acct, `
  url            = "https://example.com/hook"
  enabled_events = ["invoice.paid", "customer.created"]
  metadata       = { team = "a" }
`+describe(acct, "Billing events")) + `
data "stripe_webhook_endpoint" "test" {
  id = stripe_webhook_endpoint.test.id
}
`,
			Check: resource.ComposeAggregateTestCheckFunc(
				resource.TestCheckResourceAttrSet(address, "id"),
				resource.TestCheckResourceAttr(address, "url", "https://example.com/hook"),
				resource.TestCheckResourceAttr(address, "enabled_events.#", "2"),
				resource.TestCheckResourceAttr(address, "description", acct.Name("Billing events")),
				resource.TestCheckResourceAttr(address, "status", "enabled"),
				resource.TestCheckResourceAttr(address, "metadata.team", "a"),
				// The secret is returned once, at creation, and state keeps it.
				resource.TestCheckResourceAttrSet(address, "secret"),
				resource.TestCheckResourceAttrPair("data.stripe_webhook_endpoint.test", "id", address, "id"),
				resource.TestCheckResourceAttr("data.stripe_webhook_endpoint.test", "url", "https://example.com/hook"),
				resource.TestCheckNoResourceAttr("data.stripe_webhook_endpoint.test", "secret"),
			),
		}},
	})
}

func TestAccWebhookEndpoint_update(t *testing.T) {
	acct := acctest.NewAccount(t)
	var secret string
	remember := resource.TestCheckResourceAttrWith(address, "secret", func(v string) error {
		secret = v
		return nil
	})
	resource.ParallelTest(t, resource.TestCase{
		ProtoV6ProviderFactories: tfacc.ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: endpointConfig(acct, basic+describe(acct, "one")+"  metadata = { team = \"a\", tier = \"1\" }\n"),
				Check:  remember,
			},
			{
				Config: endpointConfig(acct, `
  url            = "https://example.com/other"
  enabled_events = ["invoice.paid", "invoice.voided"]
  metadata       = { team = "b" }
  disabled       = true
`+describe(acct, "two")),
				ConfigPlanChecks: resource.ConfigPlanChecks{PreApply: []plancheck.PlanCheck{
					plancheck.ExpectResourceAction(address, plancheck.ResourceActionUpdate),
				}},
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr(address, "url", "https://example.com/other"),
					resource.TestCheckResourceAttr(address, "enabled_events.#", "2"),
					resource.TestCheckResourceAttr(address, "description", acct.Name("two")),
					resource.TestCheckResourceAttr(address, "status", "disabled"),
					resource.TestCheckResourceAttr(address, "metadata.%", "1"),
					// An update response omits the secret; state keeps the one from creation.
					resource.TestCheckResourceAttrWith(address, "secret", func(v string) error {
						if v == "" || v != secret {
							return fmt.Errorf("secret changed on update: %q was %q", v, secret)
						}
						return nil
					}),
				),
			},
		},
	})
}

// api_version is accepted on create only, so changing it replaces the endpoint.
func TestAccWebhookEndpoint_replace(t *testing.T) {
	acct := acctest.NewAccount(t)
	var first string
	resource.ParallelTest(t, resource.TestCase{
		ProtoV6ProviderFactories: tfacc.ProviderFactories,
		Steps: []resource.TestStep{
			{Config: endpointConfig(acct, basic+"  api_version = \"2020-08-27\"\n"), Check: idOf(&first)},
			{
				Config: endpointConfig(acct, basic+"  api_version = \"2022-11-15\"\n"),
				ConfigPlanChecks: resource.ConfigPlanChecks{PreApply: []plancheck.PlanCheck{
					plancheck.ExpectResourceAction(address, plancheck.ResourceActionReplace),
				}},
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr(address, "api_version", "2022-11-15"),
					resource.TestCheckResourceAttrSet(address, "secret"),
					resource.TestCheckResourceAttrWith(address, "id", func(v string) error {
						if v == first {
							return fmt.Errorf("the replacement kept the endpoint ID %s", v)
						}
						return nil
					}),
				),
			},
		},
	})
}

func TestAccWebhookEndpoint_import(t *testing.T) {
	acct := acctest.NewAccount(t)
	cfg := endpointConfig(acct, basic+describe(acct, "Billing")+"  metadata = { team = \"a\" }\n")
	resource.ParallelTest(t, resource.TestCase{
		ProtoV6ProviderFactories: tfacc.ProviderFactories,
		Steps: []resource.TestStep{
			{Config: cfg},
			{
				ResourceName: address,
				ImportState:  true,
				// Stripe returns the secret only at creation, so an imported endpoint has none.
				ImportStateVerify:       true,
				ImportStateVerifyIgnore: []string{"secret"},
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

func TestAccWebhookEndpoint_delete(t *testing.T) {
	acct := acctest.NewAccount(t)
	var id string
	resource.ParallelTest(t, resource.TestCase{
		ProtoV6ProviderFactories: tfacc.ProviderFactories,
		CheckDestroy: func(*terraform.State) error {
			_, err := tfacc.Client(acct).Do(t.Context(), stripeapi.Request{Method: "GET", Path: "/v1/webhook_endpoints/" + id})
			if !stripeapi.IsNotFound(err) {
				return fmt.Errorf("endpoint %s should be gone after destroy, got %v", id, err)
			}
			return nil
		},
		Steps: []resource.TestStep{{Config: endpointConfig(acct, basic), Check: idOf(&id)}},
	})
}

func TestAccWebhookEndpoint_drift(t *testing.T) {
	acct := acctest.NewAccount(t)
	var id string
	cfg := endpointConfig(acct, basic+describe(acct, "Billing"))
	resource.ParallelTest(t, resource.TestCase{
		ProtoV6ProviderFactories: tfacc.ProviderFactories,
		Steps: []resource.TestStep{
			{Config: cfg, Check: idOf(&id)},
			{
				PreConfig: func() {
					tfacc.Post(t, acct, "/v1/webhook_endpoints/"+id, wire.Object{
						{Name: "description", Value: wire.String("Renamed")},
					})
				},
				Config: cfg,
				ConfigPlanChecks: resource.ConfigPlanChecks{PreApply: []plancheck.PlanCheck{
					plancheck.ExpectNonEmptyPlan(),
					plancheck.ExpectResourceAction(address, plancheck.ResourceActionUpdate),
				}},
				Check: resource.TestCheckResourceAttr(address, "description", acct.Name("Billing")),
			},
		},
	})
}

// An endpoint created disabled is created, then updated. Stripe sends the secret on the
// create response only, so the provider must keep it through that follow-up update and
// through later ones.
func TestAccWebhookEndpoint_disabledAtCreation(t *testing.T) {
	acct := acctest.NewAccount(t)
	var secret string
	resource.ParallelTest(t, resource.TestCase{
		ProtoV6ProviderFactories: tfacc.ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: endpointConfig(acct, basic+"  disabled = true\n"),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr(address, "status", "disabled"),
					resource.TestCheckResourceAttrWith(address, "secret", func(v string) error {
						if v == "" {
							return fmt.Errorf("secret is empty after a create that also updated the endpoint")
						}
						secret = v
						return nil
					}),
				),
			},
			{
				Config: endpointConfig(acct, basic+"  disabled = true\n"+describe(acct, "later")),
				Check: resource.TestCheckResourceAttrWith(address, "secret", func(v string) error {
					if v == "" || v != secret {
						return fmt.Errorf("secret changed on update: %q was %q", v, secret)
					}
					return nil
				}),
			},
		},
	})
}
