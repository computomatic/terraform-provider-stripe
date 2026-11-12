package identity_verification_report_test

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

const address = "data.stripe_identity_verification_report.test"

func config(c acctest.Config, id string) string {
	return c.ProviderBlock() + `
data "stripe_identity_verification_report" "test" {
  id = "` + id + `"
}
`
}

// A report needs a person to complete a session's hosted flow, so nothing in the API makes one (suite:
// "Invoice rendering templates and verification reports are empty on the dev sandbox"). Against the
// fake the test seeds a verified session and a report of it in the forms the suite recorded for a
// verified session's report (TestIdentityVerificationReportAttributes, state `verified`), and the
// data source reads them back. In smoke mode it reads the first report the account has and skips
// when there is none, as on a new account.
func TestAccIdentityVerificationReport_read(t *testing.T) {
	acct := acctest.NewAccount(t)
	var id, session string
	if acct.Smoke {
		resp, err := tfacc.Client(acct).Do(t.Context(), stripeapi.Request{Method: "GET", Path: "/v1/identity/verification_reports"})
		if err != nil {
			t.Fatalf("listing: %v", err)
		}
		data, _ := resp.Body.(wire.Object).Get("data")
		items, _ := data.(wire.Array)
		if len(items) == 0 {
			t.Skip("the account has no verification reports, and one needs a person to complete the hosted flow")
		}
		id, _ = items[0].(wire.Object).GetString("id")
	} else {
		id, session = seededReport(t, acct)
	}
	checks := []resource.TestCheckFunc{
		resource.TestCheckResourceAttr(address, "id", id),
		resource.TestCheckResourceAttrSet(address, "type"),
		resource.TestCheckResourceAttrSet(address, "verification_session"),
	}
	if !acct.Smoke {
		checks = append(checks,
			resource.TestCheckResourceAttr(address, "type", "document"),
			resource.TestCheckResourceAttr(address, "verification_session", session),
			resource.TestCheckResourceAttr(address, "document.status", "verified"),
			resource.TestCheckResourceAttr(address, "document.type", "passport"),
			resource.TestCheckResourceAttr(address, "document.first_name", "Avery"),
			resource.TestCheckResourceAttr(address, "document.issuing_country", "US"),
			resource.TestCheckResourceAttr(address, "document.issued_date.year", "2020"),
			resource.TestCheckResourceAttr(address, "document.address.city", "Springfield"),
			resource.TestCheckResourceAttr(address, "document.files.#", "1"),
			resource.TestCheckNoResourceAttr(address, "document.error.code"),
			resource.TestCheckResourceAttr(address, "livemode", "false"),
		)
	}
	resource.ParallelTest(t, resource.TestCase{
		ProtoV6ProviderFactories: tfacc.ProviderFactories,
		Steps: []resource.TestStep{{
			Config: config(acct, id),
			Check:  resource.ComposeAggregateTestCheckFunc(checks...),
		}},
	})
}

// seededReport seeds, into the fake account, a verified document session and a report of it,
// as the fake's README gives them, and returns the report's and the session's IDs.
func seededReport(t *testing.T, acct acctest.Config) (report, session string) {
	t.Helper()
	acctest.RequireFake(t)
	null := wire.Null{}
	session = acctest.Seed(t, acct, wire.Object{
		{Name: "object", Value: wire.String("identity.verification_session")},
		{Name: "type", Value: wire.String("document")},
		{Name: "status", Value: wire.String("verified")},
		{Name: "livemode", Value: wire.Bool(false)},
		{Name: "metadata", Value: wire.Object{}},
		{Name: "client_reference_id", Value: null},
		{Name: "client_secret", Value: null},
		{Name: "last_error", Value: null},
		{Name: "last_verification_report", Value: null},
		{Name: "options", Value: wire.Object{}},
		{Name: "redaction", Value: null},
		{Name: "related_customer", Value: null},
		{Name: "related_customer_account", Value: null},
		{Name: "url", Value: null},
	})[0]
	report = acctest.Seed(t, acct, wire.Object{
		{Name: "object", Value: wire.String("identity.verification_report")},
		{Name: "type", Value: wire.String("document")},
		{Name: "livemode", Value: wire.Bool(false)},
		{Name: "client_reference_id", Value: null},
		{Name: "verification_session", Value: wire.String(session)},
		{Name: "options", Value: wire.Object{{Name: "id_number", Value: wire.Object{}}}},
		{Name: "document", Value: wire.Object{
			{Name: "status", Value: wire.String("verified")},
			{Name: "type", Value: wire.String("passport")},
			{Name: "files", Value: wire.Array{wire.String("file_seededpassport")}},
			{Name: "first_name", Value: wire.String("Avery")},
			{Name: "last_name", Value: wire.String("Example")},
			{Name: "issuing_country", Value: wire.String("US")},
			{Name: "issued_date", Value: wire.Object{{Name: "day", Value: wire.Int(1)}, {Name: "month", Value: wire.Int(6)}, {Name: "year", Value: wire.Int(2020)}}},
			{Name: "address", Value: wire.Object{
				{Name: "city", Value: wire.String("Springfield")},
				{Name: "country", Value: wire.String("US")},
				{Name: "line1", Value: wire.String("1 Example Street")},
				{Name: "line2", Value: wire.String("")},
				{Name: "postal_code", Value: wire.String("00001")},
				{Name: "state", Value: wire.String("CA")},
			}},
		}},
	})[0]
	return report, session
}

// An unknown report is a 404 `resource_missing` (suite: "retrieve unknown").
func TestAccIdentityVerificationReport_notFound(t *testing.T) {
	acct := acctest.NewAccount(t)
	resource.ParallelTest(t, resource.TestCase{
		ProtoV6ProviderFactories: tfacc.ProviderFactories,
		Steps: []resource.TestStep{{
			Config:      config(acct, "vr_doesnotexist"),
			ExpectError: regexp.MustCompile(`no stripe_identity_verification_report found for\s+id=vr_doesnotexist`),
		}},
	})
}
