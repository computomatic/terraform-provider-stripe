package resources

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/datasource"
	dschema "github.com/hashicorp/terraform-plugin-framework/datasource/schema"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/hashicorp/terraform-plugin-go/tftypes"
	"github.com/hashicorp/terraform-plugin-log/tflogtest"

	"github.com/computomatic/terraform-provider-stripe/internal/engine"
	"github.com/computomatic/terraform-provider-stripe/internal/resources/apps_secret"
	"github.com/computomatic/terraform-provider-stripe/internal/resources/checkout_session"
	"github.com/computomatic/terraform-provider-stripe/internal/resources/commerce_product_catalog_import_v2"
	"github.com/computomatic/terraform-provider-stripe/internal/resources/identity_verification_report"
	"github.com/computomatic/terraform-provider-stripe/internal/resources/identity_verification_session"
	"github.com/computomatic/terraform-provider-stripe/internal/resources/invoice"
	"github.com/computomatic/terraform-provider-stripe/internal/resources/payment_intent"
	"github.com/computomatic/terraform-provider-stripe/internal/stripeapi"
)

const secret = "pi_1_secret_SHOULD_NOT_BE_LOGGED"

// clientSecrets are the members of a response that hold a client secret.
const clientSecrets = `,"client_secret":"` + secret + `","confirmation_secret":{"client_secret":"` + secret + `"}`

// A client secret finishes a payment, a verification session's secret and URL start a check,
// an app secret's payload is the secret itself, a verification report holds the person's
// identity data and a catalog import's upload URL grants access to its file, so the data
// sources that return one mark it sensitive in the schema and mask it in the trace log of the
// response body.
func TestClientSecretsAreSensitive(t *testing.T) {
	for name, tc := range map[string]struct {
		ctor func() (func() datasource.DataSource, error)
		path []string
		// extra is the members of the response body besides `id` and `object`.
		extra string
	}{
		"payment intent":   {payment_intent.NewDataSource, []string{"client_secret"}, clientSecrets},
		"checkout session": {checkout_session.NewDataSource, []string{"client_secret"}, clientSecrets},
		"invoice":          {invoice.NewDataSource, []string{"confirmation_secret", "client_secret"}, clientSecrets},
		"verification":     {identity_verification_session.NewDataSource, []string{"client_secret"}, clientSecrets},
		"verification url": {identity_verification_session.NewDataSource, []string{"url"}, clientSecrets + `,"url":"` + secret + `"`},
		"verified outputs": {identity_verification_session.NewDataSource, []string{"verified_outputs"}, clientSecrets + `,"verified_outputs":{"id_number":"` + secret + `"}`},
		"app secret":       {apps_secret.NewDataSource, []string{"payload"}, `,"payload":"` + secret + `"`},
		"report id number": {identity_verification_report.NewDataSource, []string{"id_number"}, `,"id_number":{"id_number":"` + secret + `"}`},
		"import upload url": {commerce_product_catalog_import_v2.NewDataSource, []string{"status_details", "awaiting_upload", "upload_url", "url"},
			`,"status_details":{"awaiting_upload":{"upload_url":{"url":"` + secret + `"}}}`},
	} {
		t.Run(name, func(t *testing.T) {
			ctor, err := tc.ctor()
			if err != nil {
				t.Fatal(err)
			}
			ds := ctor()
			ctx := context.Background()
			var sr datasource.SchemaResponse
			ds.Schema(ctx, datasource.SchemaRequest{}, &sr)
			if !sensitiveAt(t, sr.Schema.Attributes, tc.path) {
				t.Errorf("%v is not sensitive in the schema", tc.path)
			}

			body := `{"id":"x_1","object":"x"` + tc.extra + `}`
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				_, _ = w.Write([]byte(body))
			}))
			defer srv.Close()
			client := stripeapi.New(stripeapi.Config{APIKey: "sk_test_x", BaseURL: srv.URL}, stripeapi.Options{Version: "test"})
			var cr datasource.ConfigureResponse
			ds.(datasource.DataSourceWithConfigure).Configure(ctx, datasource.ConfigureRequest{ProviderData: &engine.ProviderData{Client: client}}, &cr)

			var logs bytes.Buffer
			ctx = tflogtest.RootLogger(ctx, &logs)
			attrs := map[string]tftypes.Value{}
			typ := sr.Schema.Type().TerraformType(ctx).(tftypes.Object)
			for n, at := range typ.AttributeTypes {
				attrs[n] = tftypes.NewValue(at, nil)
			}
			attrs["id"] = tftypes.NewValue(tftypes.String, "x_1")
			rr := datasource.ReadResponse{State: tfsdk.State{Schema: sr.Schema, Raw: tftypes.NewValue(typ, nil)}}
			ds.Read(ctx, datasource.ReadRequest{Config: tfsdk.Config{Schema: sr.Schema, Raw: tftypes.NewValue(typ, attrs)}}, &rr)
			if rr.Diagnostics.HasError() {
				t.Fatalf("read: %v", rr.Diagnostics)
			}
			if !strings.Contains(logs.String(), "stripe response body") {
				t.Fatalf("the trace log has no response body line: %s", logs.String())
			}
			if strings.Contains(logs.String(), secret) {
				t.Errorf("the trace log holds the secret: %s", logs.String())
			}
		})
	}
}

// sensitiveAt reports whether the attribute at the path is sensitive, or lies below one that is.
func sensitiveAt(t *testing.T, attrs map[string]dschema.Attribute, path []string) bool {
	t.Helper()
	a, ok := attrs[path[0]]
	if !ok {
		t.Fatalf("no attribute %s", path[0])
	}
	if a.IsSensitive() {
		return true
	}
	obj, nested := a.(dschema.SingleNestedAttribute)
	if len(path) == 1 || !nested {
		return false
	}
	return sensitiveAt(t, obj.Attributes, path[1:])
}
