package customer_test

import (
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/resource"

	"github.com/computomatic/terraform-provider-stripe/internal/resources/customer"
)

// Stripe fills these in when the configuration leaves them out (a minimal create returns
// them), so they must be Optional and Computed or every apply is inconsistent.
func TestServerFilledAttributesAreComputed(t *testing.T) {
	ctor, err := customer.NewResource()
	if err != nil {
		t.Fatal(err)
	}
	var resp resource.SchemaResponse
	ctor().Schema(t.Context(), resource.SchemaRequest{}, &resp)
	for _, name := range []string{"balance", "invoice_prefix", "invoice_settings", "next_invoice_sequence", "tax_exempt"} {
		a, ok := resp.Schema.Attributes[name]
		if !ok {
			t.Errorf("no attribute %s", name)
			continue
		}
		if !a.IsOptional() || !a.IsComputed() {
			t.Errorf("%s must be optional and computed", name)
		}
	}
}
