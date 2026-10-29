package commerce_product_catalog_import_v2_test

import (
	"os"
	"regexp"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"

	"github.com/computomatic/terraform-provider-stripe/internal/acctest"
	"github.com/computomatic/terraform-provider-stripe/internal/acctest/tfacc"
	"github.com/computomatic/terraform-provider-stripe/internal/wire"
)

func TestMain(m *testing.M) { os.Exit(acctest.Main(m)) }

const address = "data.stripe_commerce_product_catalog_import_v2.test"

func config(c acctest.Config, id string) string {
	return c.ProviderBlock() + `
data "stripe_commerce_product_catalog_import_v2" "test" {
  id = "` + id + `"
}
`
}

// An import made without a file waits for its upload, with an upload URL (suite: "v2 events
// and product catalog imports"). Stripe cannot delete one.
func TestAccCommerceProductCatalogImportV2_read(t *testing.T) {
	acct := acctest.NewAccount(t)
	id := tfacc.Create(t, acct, "/v2/commerce/product_catalog/imports", wire.Object{
		{Name: "feed_type", Value: wire.String("product")},
		{Name: "mode", Value: wire.String("upsert")},
		{Name: "metadata", Value: wire.Object{{Name: "team", Value: wire.String("a")}}},
	})
	resource.ParallelTest(t, resource.TestCase{
		ProtoV6ProviderFactories: tfacc.ProviderFactories,
		Steps: []resource.TestStep{{
			Config: config(acct, id),
			Check: resource.ComposeAggregateTestCheckFunc(
				resource.TestCheckResourceAttr(address, "id", id),
				resource.TestCheckResourceAttr(address, "feed_type", "product"),
				resource.TestCheckResourceAttr(address, "mode", "upsert"),
				resource.TestCheckResourceAttr(address, "metadata.team", "a"),
				resource.TestCheckResourceAttr(address, "status", "awaiting_upload"),
				resource.TestCheckResourceAttrSet(address, "status_details.awaiting_upload.upload_url.url"),
				resource.TestCheckResourceAttrSet(address, "created"),
			),
		}},
	})
}

// An unknown import, well formed or not, is a 404 `invalid_object_id` (suite: "retrieve
// unknown").
func TestAccCommerceProductCatalogImportV2_notFound(t *testing.T) {
	acct := acctest.NewAccount(t)
	resource.ParallelTest(t, resource.TestCase{
		ProtoV6ProviderFactories: tfacc.ProviderFactories,
		Steps: []resource.TestStep{{
			Config:      config(acct, "doesnotexist"),
			ExpectError: regexp.MustCompile(`no stripe_commerce_product_catalog_import_v2 found for\s+id=doesnotexist`),
		}},
	})
}
