package billing_meter_event_summary_test

import (
	"os"
	"regexp"
	"strconv"
	"testing"
	"time"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"

	"github.com/computomatic/terraform-provider-stripe/internal/acctest"
	"github.com/computomatic/terraform-provider-stripe/internal/acctest/tfacc"
	"github.com/computomatic/terraform-provider-stripe/internal/wire"
)

func TestMain(m *testing.M) { os.Exit(acctest.Main(m)) }

const address = "data.stripe_billing_meter_event_summary.test"

func config(c acctest.Config, meter, customer string, start, end int64) string {
	return c.ProviderBlock() + `
data "stripe_billing_meter_event_summary" "test" {
  meter      = "` + meter + `"
  customer   = "` + customer + `"
  start_time = ` + strconv.FormatInt(start, 10) + `
  end_time   = ` + strconv.FormatInt(end, 10) + `
}
`
}

// sumMeter creates a meter that sums its events' values and returns its ID and event name.
func sumMeter(t *testing.T, acct acctest.Config, base string) (meter, eventName string) {
	t.Helper()
	eventName = acct.Unique(base)
	meter = tfacc.Create(t, acct, "/v1/billing/meters", wire.Object{
		{Name: "display_name", Value: wire.String(acct.Name(base))},
		{Name: "event_name", Value: wire.String(eventName)},
		{Name: "default_aggregation", Value: wire.Object{{Name: "formula", Value: wire.String("sum")}}},
	})
	return meter, eventName
}

func sendEvent(t *testing.T, acct acctest.Config, eventName, identifier, customer string, value int, ago time.Duration) {
	t.Helper()
	tfacc.Post(t, acct, "/v1/billing/meter_events", wire.Object{
		{Name: "event_name", Value: wire.String(eventName)},
		{Name: "identifier", Value: wire.String(acct.Unique(identifier))},
		{Name: "timestamp", Value: wire.Int(time.Now().Add(-ago).Unix())},
		{Name: "payload", Value: wire.Object{
			{Name: "stripe_customer_id", Value: wire.String(customer)},
			{Name: "value", Value: wire.String(strconv.Itoa(value))},
		}},
	})
}

// Two events of 3 and 4 reach one summary of the whole window, which echoes the window's
// times, once Stripe has aggregated them, about a minute later (suite: "Meter event
// summaries"). The window is whole hours around both events.
func TestAccBillingMeterEventSummary_read(t *testing.T) {
	acct := acctest.NewAccount(t)
	meter, eventName := sumMeter(t, acct, "tfacc_summary_read")
	customer := tfacc.Create(t, acct, "/v1/customers", nil)
	sendEvent(t, acct, eventName, "tfacc_summary_read_a", customer, 3, time.Minute)
	sendEvent(t, acct, eventName, "tfacc_summary_read_b", customer, 4, 91*time.Minute)
	now := time.Now().UTC()
	start := now.Add(-2 * time.Hour).Truncate(time.Hour).Unix()
	end := now.Truncate(time.Hour).Add(time.Hour).Unix()
	tfacc.AwaitListed(t, acct, "/v1/billing/meters/"+meter+"/event_summaries", wire.Object{
		{Name: "customer", Value: wire.String(customer)},
		{Name: "start_time", Value: wire.Int(start)},
		{Name: "end_time", Value: wire.Int(end)},
	}, func(items []wire.Object) bool {
		if len(items) != 1 {
			return false
		}
		v, _ := items[0].Get("aggregated_value")
		return v == wire.Int(7) || v == wire.Decimal("7")
	})
	resource.ParallelTest(t, resource.TestCase{
		ProtoV6ProviderFactories: tfacc.ProviderFactories,
		Steps: []resource.TestStep{{
			Config: config(acct, meter, customer, start, end),
			Check: resource.ComposeAggregateTestCheckFunc(
				resource.TestCheckResourceAttrSet(address, "id"),
				resource.TestCheckResourceAttr(address, "meter", meter),
				resource.TestCheckResourceAttr(address, "customer", customer),
				resource.TestCheckResourceAttr(address, "aggregated_value", "7"),
				resource.TestCheckResourceAttr(address, "start_time", strconv.FormatInt(start, 10)),
				resource.TestCheckResourceAttr(address, "end_time", strconv.FormatInt(end, 10)),
				resource.TestCheckResourceAttr(address, "livemode", "false"),
			),
		}},
	})
}

// An unknown meter is a 404 `resource_missing` that names the backing service, unlike the
// meter retrieve (suite: "Meter event summaries", an altered real ID). Whether a window with
// no events has a summary is not settled, so the test does not rely on one.
func TestAccBillingMeterEventSummary_notFound(t *testing.T) {
	acct := acctest.NewAccount(t)
	meter, _ := sumMeter(t, acct, "tfacc_summary_missing")
	missing := tfacc.AlteredID(meter)
	customer := tfacc.Create(t, acct, "/v1/customers", nil)
	end := time.Now().UTC().Truncate(time.Hour).Unix()
	resource.ParallelTest(t, resource.TestCase{
		ProtoV6ProviderFactories: tfacc.ProviderFactories,
		Steps: []resource.TestStep{{
			Config:      config(acct, missing, customer, end-3600, end),
			ExpectError: regexp.MustCompile(`Could not find\s+` + missing),
		}},
	})
}
