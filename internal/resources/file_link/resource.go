// Package file_link declares the stripe_file_link resource and data source.
package file_link

import (
	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/resource"

	"github.com/computomatic/terraform-provider-stripe/internal/engine"
	"github.com/computomatic/terraform-provider-stripe/internal/generated/file_link"
)

// Declaration is what the spec cannot say about a file link. Stripe never deletes one, so
// destroy expires it (`expires_at` set to now) and drops it from state. An expired link
// cannot be modified, so destroy retrieves first.
var Declaration = engine.Declaration{
	TypeName:    "stripe_file_link",
	IDPrefix:    "link_",
	Description: "A link that lets anyone download a file. `terraform destroy` expires the link, because Stripe cannot delete one; it stays in the account. Stripe refuses to change an expired link.",
	Paths: engine.Paths{
		Create:   "/v1/file_links",
		Retrieve: "/v1/file_links/{id}",
		Update:   "/v1/file_links/{id}",
		List:     "/v1/file_links",
	},
	Delete:  engine.Archive,
	Archive: &engine.ArchiveAction{Field: "expires_at", Now: true},
}

// NewResource returns the resource constructor the provider registers.
func NewResource() (func() resource.Resource, error) {
	return engine.NewResource(Declaration, file_link.Table)
}

// NewDataSource returns the data source constructor. No file link field is documented as unique, so the data source looks up by `id` only.
func NewDataSource() (func() datasource.DataSource, error) {
	return engine.NewDataSource(Declaration, file_link.Table)
}
