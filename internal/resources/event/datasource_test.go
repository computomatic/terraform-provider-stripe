package event_test

import (
	"os"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"

	"github.com/computomatic/terraform-provider-stripe/internal/acctest"
	"github.com/computomatic/terraform-provider-stripe/internal/acctest/tfacc"
	"github.com/computomatic/terraform-provider-stripe/internal/wire"
)

func TestMain(m *testing.M) { os.Exit(acctest.Main(m)) }

// Events come from things that happen in the account, so the test seeds one.
func TestAccEvent_read(t *testing.T) {
	acct := acctest.NewAccount(t)
	id := acctest.Seed(t, acct, wire.Object{
		{Name: "object", Value: wire.String("event")},
		{Name: "type", Value: wire.String("customer.created")},
		{Name: "api_version", Value: wire.String("2026-09-30.endive")},
		{Name: "livemode", Value: wire.Bool(false)},
		{Name: "pending_webhooks", Value: wire.Int(0)},
		{Name: "request", Value: wire.Null{}},
		{Name: "data", Value: wire.Object{{Name: "object", Value: wire.Object{{Name: "object", Value: wire.String("customer")}}}}},
	})[0]
	resource.ParallelTest(t, resource.TestCase{
		ProtoV6ProviderFactories: tfacc.ProviderFactories,
		Steps: []resource.TestStep{{
			Config: acct.ProviderBlock() + `
data "stripe_event" "test" {
  id = "` + id + `"
}
`,
			Check: resource.ComposeAggregateTestCheckFunc(
				resource.TestCheckResourceAttr("data.stripe_event.test", "type", "customer.created"),
				resource.TestCheckResourceAttr("data.stripe_event.test", "pending_webhooks", "0"),
				resource.TestCheckResourceAttr("data.stripe_event.test", "api_version", "2026-09-30.endive"),
			),
		}},
	})
}
