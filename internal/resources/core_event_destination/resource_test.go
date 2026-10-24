package core_event_destination_test

import (
	"fmt"
	"os"
	"regexp"
	"slices"
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

const (
	address   = "stripe_core_event_destination.test"
	thinEvent = "v1.billing.meter.error_report_triggered"
	otherThin = "v1.billing.meter.no_meter_found"
	basePath  = "/v2/core/event_destinations"
)

func destinationConfig(c acctest.Config, body string) string {
	return c.ProviderBlock() + "resource \"stripe_core_event_destination\" \"test\" {\n" + body + "}\n"
}

func idOf(dst *string) resource.TestCheckFunc {
	return resource.TestCheckResourceAttrWith(address, "id", func(v string) error {
		*dst = v
		return nil
	})
}

// basic is a thin webhook endpoint destination.
func basic(c acctest.Config) string {
	return `
  name           = "` + c.Name("billing") + `"
  type           = "webhook_endpoint"
  event_payload  = "thin"
  enabled_events = ["` + thinEvent + `"]
  webhook_endpoint = {
    url = "https://example.com/hook"
  }
`
}

func TestAccCoreEventDestination_create(t *testing.T) {
	acct := acctest.NewAccount(t)
	rfc3339 := regexp.MustCompile(`^\d{4}-\d{2}-\d{2}T`)
	resource.ParallelTest(t, resource.TestCase{
		ProtoV6ProviderFactories: tfacc.ProviderFactories,
		Steps: []resource.TestStep{{
			Config: destinationConfig(acct, basic(acct)+"  description = \"Billing events\"\n  metadata = { team = \"a\" }\n") + `
data "stripe_core_event_destination" "test" {
  id = stripe_core_event_destination.test.id
}
`,
			Check: resource.ComposeAggregateTestCheckFunc(
				resource.TestCheckResourceAttrWith(address, "id", func(v string) error {
					if len(v) < 4 || v[:3] != "ed_" {
						return fmt.Errorf("id %q does not start with ed_", v)
					}
					return nil
				}),
				resource.TestCheckResourceAttr(address, "name", acct.Name("billing")),
				resource.TestCheckResourceAttr(address, "type", "webhook_endpoint"),
				resource.TestCheckResourceAttr(address, "event_payload", "thin"),
				resource.TestCheckResourceAttr(address, "description", "Billing events"),
				resource.TestCheckResourceAttr(address, "enabled_events.#", "1"),
				resource.TestCheckResourceAttr(address, "metadata.team", "a"),
				resource.TestCheckResourceAttr(address, "status", "enabled"),
				resource.TestCheckResourceAttr(address, "livemode", "false"),
				resource.TestCheckResourceAttrWith(address, "created", func(v string) error {
					if !rfc3339.MatchString(v) {
						return fmt.Errorf("created %q is not an RFC 3339 time", v)
					}
					return nil
				}),
				resource.TestCheckResourceAttrSet(address, "updated"),
				// Stripe redacts the URL unless asked, so state holding it proves the
				// provider asks on create.
				resource.TestCheckResourceAttr(address, "webhook_endpoint.url", "https://example.com/hook"),
				resource.TestCheckResourceAttrPair("data.stripe_core_event_destination.test", "id", address, "id"),
				resource.TestCheckResourceAttr("data.stripe_core_event_destination.test", "webhook_endpoint.url", "https://example.com/hook"),
				resource.TestCheckResourceAttr("data.stripe_core_event_destination.test", "status", "enabled"),
			),
		}},
	})
}

func TestAccCoreEventDestination_update(t *testing.T) {
	acct := acctest.NewAccount(t)
	var id string
	resource.ParallelTest(t, resource.TestCase{
		ProtoV6ProviderFactories: tfacc.ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: destinationConfig(acct, basic(acct)+"  description = \"one\"\n  metadata = { keep = \"me\", drop = \"me\" }\n"),
				Check:  idOf(&id),
			},
			{
				Config: destinationConfig(acct, `
  name           = "`+acct.Name("billing 2")+`"
  type           = "webhook_endpoint"
  event_payload  = "thin"
  enabled_events = ["`+thinEvent+`", "`+otherThin+`"]
  description    = "two"
  metadata       = { keep = "me", new = "v" }
  webhook_endpoint = {
    url = "https://example.com/other"
  }
`),
				ConfigPlanChecks: resource.ConfigPlanChecks{PreApply: []plancheck.PlanCheck{
					plancheck.ExpectResourceAction(address, plancheck.ResourceActionUpdate),
				}},
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttrWith(address, "id", func(v string) error {
						if v != id {
							return fmt.Errorf("an update replaced the destination: %s was %s", v, id)
						}
						return nil
					}),
					resource.TestCheckResourceAttr(address, "name", acct.Name("billing 2")),
					resource.TestCheckResourceAttr(address, "description", "two"),
					resource.TestCheckResourceAttr(address, "enabled_events.#", "2"),
					resource.TestCheckResourceAttr(address, "metadata.%", "2"),
					resource.TestCheckResourceAttr(address, "metadata.keep", "me"),
					resource.TestCheckResourceAttr(address, "metadata.new", "v"),
					resource.TestCheckResourceAttr(address, "webhook_endpoint.url", "https://example.com/other"),
					func(*terraform.State) error { return checkUpdateRequests(t, acct, id) },
				),
			},
			{
				// Shrinking the list sends the whole new array.
				Config: destinationConfig(acct, `
  name           = "`+acct.Name("billing 2")+`"
  type           = "webhook_endpoint"
  event_payload  = "thin"
  enabled_events = ["`+otherThin+`"]
  description    = "two"
  metadata       = { keep = "me", new = "v" }
  webhook_endpoint = {
    url = "https://example.com/other"
  }
`),
				Check: resource.TestCheckResourceAttr(address, "enabled_events.#", "1"),
			},
		},
	})
}

// checkUpdateRequests reads the fake's request log: the create is JSON with the include,
// the update never sends a create-only field, sends the removed metadata key as null and
// the whole `enabled_events` array, and every POST carries an idempotency key. It checks
// nothing in smoke mode, where there is no log.
func checkUpdateRequests(t *testing.T, acct acctest.Config, id string) error {
	if acct.Smoke {
		return nil
	}
	log := acctest.RequestLog(t, acct)
	slices.Reverse(log)
	var sawCreate, sawUpdate, sawIncludedRead bool
	for _, entry := range log {
		if entry.Method == "POST" && entry.IdempotencyKey == "" {
			return fmt.Errorf("POST %s had no idempotency key", entry.Path)
		}
		params, _ := entry.Params.(wire.Object)
		switch {
		case entry.Method == "POST" && entry.Path == basePath:
			sawCreate = true
			if !includesURL(params) {
				return fmt.Errorf("create did not include webhook_endpoint.url: %v", entry.Params)
			}
		case entry.Method == "POST" && entry.Path == basePath+"/"+id:
			sawUpdate = true
			for _, createOnly := range []string{"type", "event_payload", "snapshot_api_version"} {
				if _, sent := params.Get(createOnly); sent {
					return fmt.Errorf("update sent the create-only field %s", createOnly)
				}
			}
			if meta, _ := params.GetObject("metadata"); !jsonIs(meta, `{"drop":null,"new":"v"}`) {
				return fmt.Errorf("update metadata = %v, want the diff with null for the removed key", meta)
			}
			if events, _ := params.Get("enabled_events"); !jsonIs(events, `["`+thinEvent+`","`+otherThin+`"]`) {
				return fmt.Errorf("update enabled_events = %v, want the whole array", events)
			}
		case entry.Method == "GET" && entry.Path == basePath+"/"+id:
			if includesURL(params) {
				sawIncludedRead = true
			}
		}
	}
	if !sawCreate || !sawUpdate || !sawIncludedRead {
		return fmt.Errorf("log lacks a create (%v), an update (%v) or a read that includes the url (%v)", sawCreate, sawUpdate, sawIncludedRead)
	}
	return nil
}

func includesURL(params wire.Object) bool {
	v, ok := params.Get("include")
	return ok && jsonIs(v, `["webhook_endpoint.url"]`)
}

func jsonIs(v wire.Value, want string) bool {
	if v == nil {
		return false
	}
	got, err := wire.EncodeJSON(v)
	return err == nil && string(got) == want
}

// event_payload is accepted on create only, so changing it replaces the destination.
func TestAccCoreEventDestination_replace(t *testing.T) {
	acct := acctest.NewAccount(t)
	var first string
	resource.ParallelTest(t, resource.TestCase{
		ProtoV6ProviderFactories: tfacc.ProviderFactories,
		Steps: []resource.TestStep{
			{Config: destinationConfig(acct, basic(acct)), Check: idOf(&first)},
			{
				Config: destinationConfig(acct, `
  name           = "`+acct.Name("billing")+`"
  type           = "webhook_endpoint"
  event_payload  = "snapshot"
  snapshot_api_version = "`+stripeapi.Version+`"
  enabled_events = ["customer.created"]
  webhook_endpoint = {
    url = "https://example.com/hook"
  }
`),
				ConfigPlanChecks: resource.ConfigPlanChecks{PreApply: []plancheck.PlanCheck{
					plancheck.ExpectResourceAction(address, plancheck.ResourceActionReplace),
				}},
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr(address, "event_payload", "snapshot"),
					resource.TestCheckResourceAttr(address, "snapshot_api_version", stripeapi.Version),
					resource.TestCheckResourceAttrWith(address, "id", func(v string) error {
						if v == first {
							return fmt.Errorf("the replacement kept the destination ID %s", v)
						}
						return nil
					}),
				),
			},
		},
	})
}

func TestAccCoreEventDestination_import(t *testing.T) {
	acct := acctest.NewAccount(t)
	cfg := destinationConfig(acct, basic(acct)+"  description = \"Billing\"\n  metadata = { team = \"a\" }\n")
	resource.ParallelTest(t, resource.TestCase{
		ProtoV6ProviderFactories: tfacc.ProviderFactories,
		Steps: []resource.TestStep{
			{Config: cfg},
			{
				ResourceName:      address,
				ImportState:       true,
				ImportStateVerify: true,
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

func TestAccCoreEventDestination_delete(t *testing.T) {
	acct := acctest.NewAccount(t)
	var id string
	resource.ParallelTest(t, resource.TestCase{
		ProtoV6ProviderFactories: tfacc.ProviderFactories,
		CheckDestroy: func(*terraform.State) error {
			_, err := tfacc.Client(acct).Do(t.Context(), stripeapi.Request{Method: "GET", Path: basePath + "/" + id})
			if !stripeapi.IsNotFound(err) {
				return fmt.Errorf("destination %s should be gone after destroy, got %v", id, err)
			}
			return nil
		},
		Steps: []resource.TestStep{
			{Config: destinationConfig(acct, basic(acct)), Check: idOf(&id)},
			{
				// A destination deleted out of band leaves the state at the next plan, and
				// destroying the then-empty configuration has nothing to do.
				PreConfig: func() {
					if _, err := tfacc.Client(acct).Do(t.Context(), stripeapi.Request{Method: "DELETE", Path: basePath + "/" + id}); err != nil {
						t.Fatal(err)
					}
				},
				Config: destinationConfig(acct, basic(acct)),
				ConfigPlanChecks: resource.ConfigPlanChecks{PreApply: []plancheck.PlanCheck{
					plancheck.ExpectResourceAction(address, plancheck.ResourceActionCreate),
				}},
				Check: idOf(&id),
			},
		},
	})
}

func TestAccCoreEventDestination_drift(t *testing.T) {
	acct := acctest.NewAccount(t)
	var id string
	cfg := destinationConfig(acct, basic(acct)+"  description = \"Billing\"\n")
	resource.ParallelTest(t, resource.TestCase{
		ProtoV6ProviderFactories: tfacc.ProviderFactories,
		Steps: []resource.TestStep{
			{Config: cfg, Check: idOf(&id)},
			{
				// The name changes behind the provider's back through the /v2 JSON API.
				PreConfig: func() {
					tfacc.Post(t, acct, basePath+"/"+id, wire.Object{
						{Name: "name", Value: wire.String("Renamed")},
					})
				},
				Config: cfg,
				ConfigPlanChecks: resource.ConfigPlanChecks{PreApply: []plancheck.PlanCheck{
					plancheck.ExpectNonEmptyPlan(),
					plancheck.ExpectResourceAction(address, plancheck.ResourceActionUpdate),
				}},
				Check: resource.TestCheckResourceAttr(address, "name", acct.Name("billing")),
			},
			{
				// The URL is redacted unless asked for. Because the provider asks, a URL
				// changed out of band is drift too.
				PreConfig: func() {
					tfacc.Post(t, acct, basePath+"/"+id, wire.Object{
						{Name: "webhook_endpoint", Value: wire.Object{{Name: "url", Value: wire.String("https://example.com/elsewhere")}}},
					})
				},
				Config: cfg,
				ConfigPlanChecks: resource.ConfigPlanChecks{PreApply: []plancheck.PlanCheck{
					plancheck.ExpectNonEmptyPlan(),
					plancheck.ExpectResourceAction(address, plancheck.ResourceActionUpdate),
				}},
				Check: resource.TestCheckResourceAttr(address, "webhook_endpoint.url", "https://example.com/hook"),
			},
		},
	})
}

// A /v2 error without a code or invalid_fields still reaches the user with the context
// fields Stripe adds to it.
func TestAccCoreEventDestination_rejectedWithContext(t *testing.T) {
	acct := acctest.NewAccount(t)
	resource.ParallelTest(t, resource.TestCase{
		ProtoV6ProviderFactories: tfacc.ProviderFactories,
		Steps: []resource.TestStep{{
			Config: destinationConfig(acct, `
  name           = "`+acct.Name("billing")+`"
  type           = "webhook_endpoint"
  event_payload  = "thin"
  enabled_events = ["v1.not.an.event"]
  webhook_endpoint = {
    url = "https://example.com/hook"
  }
`),
			ExpectError: regexp.MustCompile(`event_type`),
		}},
	})
}

// A data source for an unknown ID reports it.
func TestAccCoreEventDestination_dataSourceNotFound(t *testing.T) {
	acct := acctest.NewAccount(t)
	resource.ParallelTest(t, resource.TestCase{
		ProtoV6ProviderFactories: tfacc.ProviderFactories,
		Steps: []resource.TestStep{{
			Config:      acct.ProviderBlock() + "data \"stripe_core_event_destination\" \"test\" {\n  id = \"ed_test_missing\"\n}\n",
			ExpectError: regexp.MustCompile(`no stripe_core_event_destination found for id=ed_test_missing`),
		}},
	})
}

// An Optional collection configured empty applies cleanly and plans empty afterwards,
// whether Stripe returns it empty or leaves it out.
func TestAccCoreEventDestination_emptyMetadata(t *testing.T) {
	acct := acctest.NewAccount(t)
	cfg := destinationConfig(acct, basic(acct)+"  metadata = {}\n")
	resource.ParallelTest(t, resource.TestCase{
		ProtoV6ProviderFactories: tfacc.ProviderFactories,
		Steps: []resource.TestStep{
			{Config: cfg, Check: resource.TestCheckResourceAttr(address, "metadata.%", "0")},
			{Config: cfg, PlanOnly: true},
		},
	})
}
