package reporting_report_run_test

import (
	"os"
	"regexp"
	"strconv"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"

	"github.com/computomatic/terraform-provider-stripe/internal/acctest"
	"github.com/computomatic/terraform-provider-stripe/internal/acctest/tfacc"
	"github.com/computomatic/terraform-provider-stripe/internal/stripeapi"
	"github.com/computomatic/terraform-provider-stripe/internal/wire"
)

func TestMain(m *testing.M) { os.Exit(acctest.Main(m)) }

const (
	address    = "data.stripe_reporting_report_run.test"
	reportType = "balance.summary.1"
)

func config(c acctest.Config, id string) string {
	return c.ProviderBlock() + `
data "stripe_reporting_report_run" "test" {
  id = "` + id + `"
}
`
}

// run starts a balance summary run over the whole interval the report type has data for, and
// returns its ID and the interval (suite: "reportRunForm"). It skips the test when the
// interval is empty, because a run needs a start before its end. Stripe cannot delete a run.
func run(t *testing.T, acct acctest.Config) (id string, start, end int64) {
	t.Helper()
	resp, err := tfacc.Client(acct).Do(t.Context(), stripeapi.Request{Method: "GET", Path: "/v1/reporting/report_types/" + reportType})
	if err != nil {
		t.Fatalf("reading the report type: %v", err)
	}
	typ := resp.Body.(wire.Object)
	s, _ := typ.Get("data_available_start")
	e, _ := typ.Get("data_available_end")
	start, end = int64(s.(wire.Int)), int64(e.(wire.Int))
	if start >= end {
		t.Skipf("%s has no data interval yet (start %d, end %d)", reportType, start, end)
	}
	id = tfacc.Create(t, acct, "/v1/reporting/report_runs", wire.Object{
		{Name: "report_type", Value: wire.String(reportType)},
		{Name: "parameters", Value: wire.Object{
			{Name: "interval_start", Value: wire.Int(start)},
			{Name: "interval_end", Value: wire.Int(end)},
		}},
	})
	return id, start, end
}

// A run reads back with the parameters it was given. Stripe runs it in the background, so its
// status and result are not asserted.
func TestAccReportingReportRun_read(t *testing.T) {
	acct := acctest.NewAccount(t)
	id, start, end := run(t, acct)
	resource.ParallelTest(t, resource.TestCase{
		ProtoV6ProviderFactories: tfacc.ProviderFactories,
		Steps: []resource.TestStep{{
			Config: config(acct, id),
			Check: resource.ComposeAggregateTestCheckFunc(
				resource.TestCheckResourceAttr(address, "id", id),
				resource.TestCheckResourceAttr(address, "report_type", reportType),
				resource.TestCheckResourceAttr(address, "parameters.interval_start", strconv.FormatInt(start, 10)),
				resource.TestCheckResourceAttr(address, "parameters.interval_end", strconv.FormatInt(end, 10)),
				resource.TestCheckResourceAttrSet(address, "status"),
				resource.TestCheckResourceAttr(address, "livemode", "false"),
			),
		}},
	})
}

// An unknown run is a 404 `resource_missing` (suite: "retrieve unknown", an altered real ID).
func TestAccReportingReportRun_notFound(t *testing.T) {
	acct := acctest.NewAccount(t)
	id, _, _ := run(t, acct)
	missing := tfacc.AlteredID(id)
	resource.ParallelTest(t, resource.TestCase{
		ProtoV6ProviderFactories: tfacc.ProviderFactories,
		Steps: []resource.TestStep{{
			Config:      config(acct, missing),
			ExpectError: regexp.MustCompile(`no stripe_reporting_report_run found for\s+id=` + missing),
		}},
	})
}
