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

	"github.com/computomatic/terraform-provider-stripe/internal/resources/checkout_session"
	"github.com/computomatic/terraform-provider-stripe/internal/resources/invoice"
	"github.com/computomatic/terraform-provider-stripe/internal/resources/payment_intent"
	"github.com/computomatic/terraform-provider-stripe/internal/stripeapi"
)

const secret = "pi_1_secret_SHOULD_NOT_BE_LOGGED"

// A client secret finishes a payment, so the data sources that return one mark it sensitive
// in the schema and mask it in the trace log of the response body.
func TestClientSecretsAreSensitive(t *testing.T) {
	for name, tc := range map[string]struct {
		ctor func() (func() datasource.DataSource, error)
		path []string
	}{
		"payment intent":   {payment_intent.NewDataSource, []string{"client_secret"}},
		"checkout session": {checkout_session.NewDataSource, []string{"client_secret"}},
		"invoice":          {invoice.NewDataSource, []string{"confirmation_secret", "client_secret"}},
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

			body := `{"id":"x_1","object":"x","client_secret":"` + secret + `","confirmation_secret":{"client_secret":"` + secret + `"}}`
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				_, _ = w.Write([]byte(body))
			}))
			defer srv.Close()
			client := stripeapi.New(stripeapi.Config{APIKey: "sk_test_x", BaseURL: srv.URL}, stripeapi.Options{Version: "test"})
			var cr datasource.ConfigureResponse
			ds.(datasource.DataSourceWithConfigure).Configure(ctx, datasource.ConfigureRequest{ProviderData: client}, &cr)

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
