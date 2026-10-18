// Package file declares the stripe_file data source: a file uploaded to Stripe. There is no resource.
package file

import (
	"github.com/hashicorp/terraform-plugin-framework/datasource"

	"github.com/computomatic/terraform-provider-stripe/internal/engine"
	"github.com/computomatic/terraform-provider-stripe/internal/generated/file"
)

// Declaration is what the spec cannot say about a file. Stripe takes uploads on a separate
// multipart host and never deletes a file, so there is no resource.
var Declaration = engine.Declaration{
	TypeName:    "stripe_file",
	IDPrefix:    "file_",
	Description: "A file uploaded to Stripe. Look one up by its `id`. Files cannot be created or deleted through Terraform.",
	Paths:       engine.Paths{Retrieve: "/v1/files/{id}"},
	Delete:      engine.NoDelete,
}

// NewDataSource returns the data source constructor the provider registers. It looks a file up by `id` only.
func NewDataSource() (func() datasource.DataSource, error) {
	return engine.NewDataSource(Declaration, file.Table)
}
