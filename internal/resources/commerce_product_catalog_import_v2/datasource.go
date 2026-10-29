// Package commerce_product_catalog_import_v2 declares the stripe_commerce_product_catalog_import_v2 data source. There is no resource.
package commerce_product_catalog_import_v2

import (
	"github.com/hashicorp/terraform-plugin-framework/datasource"

	"github.com/computomatic/terraform-provider-stripe/internal/engine"
	"github.com/computomatic/terraform-provider-stripe/internal/generated/commerce_product_catalog_import_v2"
)

// Declaration is what the spec cannot say about this object. An import is a one-off upload of a product catalog that Stripe processes in the background and cannot delete (suite: "v2 events and product catalog imports"), so only the data source exists. Its upload and error file URLs grant access to the files, so they are sensitive.
var Declaration = engine.Declaration{
	TypeName:    "stripe_commerce_product_catalog_import_v2",
	Description: "An import of a product catalog from an uploaded file, and its progress. Look one up by its `id`. The upload URL and the error file's download URL in `status_details` are sensitive.",
	Paths:       engine.Paths{Retrieve: "/v2/commerce/product_catalog/imports/{id}", List: "/v2/commerce/product_catalog/imports"},
	Sensitive: []string{
		"status_details.awaiting_upload.upload_url.url",
		"status_details.succeeded_with_errors.error_file.download_url.url",
	},
	Delete: engine.NoDelete,
}

// NewDataSource returns the data source constructor the provider registers. It looks the object up by `id` only: Stripe documents no field of it as unique.
func NewDataSource() (func() datasource.DataSource, error) {
	return engine.NewDataSource(Declaration, commerce_product_catalog_import_v2.Table)
}
