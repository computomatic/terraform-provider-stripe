package core_event_test

import (
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

const address = "data.stripe_core_event.test"

func config(c acctest.Config, id string) string {
	return c.ProviderBlock() + `
data "stripe_core_event" "test" {
  id = "` + id + `"
}
`
}

// pinged creates a thin webhook event destination, pings it and returns the destination's ID
// and the ID of the ping event, once Stripe lists it, some tens of milliseconds later (suite:
// "v2 events and product catalog imports"). The destination's URL is example.com, so the
// delivery fails harmlessly. The destination is deleted when the test ends.
func pinged(t *testing.T, acct acctest.Config, base string) (destination, event string) {
	t.Helper()
	destination = tfacc.Create(t, acct, "/v2/core/event_destinations", wire.Object{
		{Name: "name", Value: wire.String(acct.Name(base))},
		{Name: "type", Value: wire.String("webhook_endpoint")},
		{Name: "event_payload", Value: wire.String("thin")},
		{Name: "enabled_events", Value: wire.Array{wire.String("v1.billing.meter.error_report_triggered")}},
		{Name: "webhook_endpoint", Value: wire.Object{{Name: "url", Value: wire.String("https://example.com/hook")}}},
	})
	t.Cleanup(func() {
		_, _ = tfacc.Client(acct).Do(t.Context(), stripeapi.Request{Method: "DELETE", Path: "/v2/core/event_destinations/" + destination})
	})
	event = tfacc.Create(t, acct, "/v2/core/event_destinations/"+destination+"/ping", nil)
	tfacc.AwaitListed(t, acct, "/v2/core/events", wire.Object{{Name: "object_id", Value: wire.String(destination)}}, func(items []wire.Object) bool {
		for _, item := range items {
			if id, _ := item.GetString("id"); id == event {
				return true
			}
		}
		return false
	})
	return destination, event
}

// A retrieve names the related object's type in full, unlike the ping's reply.
func TestAccCoreEvent_read(t *testing.T) {
	acct := acctest.NewAccount(t)
	destination, id := pinged(t, acct, "core event read")
	resource.ParallelTest(t, resource.TestCase{
		ProtoV6ProviderFactories: tfacc.ProviderFactories,
		Steps: []resource.TestStep{{
			Config: config(acct, id),
			Check: resource.ComposeAggregateTestCheckFunc(
				resource.TestCheckResourceAttr(address, "id", id),
				resource.TestCheckResourceAttr(address, "type", "v2.core.event_destination.ping"),
				resource.TestCheckResourceAttr(address, "related_object.id", destination),
				resource.TestCheckResourceAttr(address, "related_object.type", "v2.core.event_destination"),
				resource.TestCheckResourceAttrSet(address, "created"),
				resource.TestCheckResourceAttr(address, "livemode", "false"),
			),
		}},
	})
}

// An unknown event is a 404 `not_found` (suite: "retrieve unknown", an altered real ID).
func TestAccCoreEvent_notFound(t *testing.T) {
	acct := acctest.NewAccount(t)
	_, id := pinged(t, acct, "core event missing")
	missing := tfacc.AlteredID(id)
	resource.ParallelTest(t, resource.TestCase{
		ProtoV6ProviderFactories: tfacc.ProviderFactories,
		Steps: []resource.TestStep{{
			Config:      config(acct, missing),
			ExpectError: regexp.MustCompile(`no stripe_core_event found for\s+id=` + missing),
		}},
	})
}
