package invoice_rendering_template_test

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

const address = "data.stripe_invoice_rendering_template.test"

func config(c acctest.Config, id string) string {
	return c.ProviderBlock() + `
data "stripe_invoice_rendering_template" "test" {
  id = "` + id + `"
}
`
}

// Templates are made in the Dashboard, so nothing in the API makes one (suite: "Invoice rendering templates and verification
// reports are empty on the dev sandbox"). The test reads the first template the account has and
// skips when there is none, as on a new account.
func TestAccInvoiceRenderingTemplate_read(t *testing.T) {
	acct := acctest.NewAccount(t)
	resp, err := tfacc.Client(acct).Do(t.Context(), stripeapi.Request{Method: "GET", Path: "/v1/invoice_rendering_templates"})
	if err != nil {
		t.Fatalf("listing: %v", err)
	}
	data, _ := resp.Body.(wire.Object).Get("data")
	items, _ := data.(wire.Array)
	if len(items) == 0 {
		t.Skip("the account has no invoice rendering templates, and they are made in the Dashboard only")
	}
	id, _ := items[0].(wire.Object).GetString("id")
	resource.ParallelTest(t, resource.TestCase{
		ProtoV6ProviderFactories: tfacc.ProviderFactories,
		Steps: []resource.TestStep{{
			Config: config(acct, id),
			Check: resource.ComposeAggregateTestCheckFunc(
				resource.TestCheckResourceAttr(address, "id", id),
				resource.TestCheckResourceAttrSet(address, "status"),
				resource.TestCheckResourceAttrSet(address, "version"),
			),
		}},
	})
}

// An unknown template is a 404 `resource_missing` (suite: "retrieve unknown").
func TestAccInvoiceRenderingTemplate_notFound(t *testing.T) {
	acct := acctest.NewAccount(t)
	resource.ParallelTest(t, resource.TestCase{
		ProtoV6ProviderFactories: tfacc.ProviderFactories,
		Steps: []resource.TestStep{{
			Config:      config(acct, "inrtem_doesnotexist"),
			ExpectError: regexp.MustCompile(`no stripe_invoice_rendering_template found for\s+id=inrtem_doesnotexist`),
		}},
	})
}
