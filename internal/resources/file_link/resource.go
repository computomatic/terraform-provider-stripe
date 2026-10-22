// Package file_link declares the stripe_file_link resource and data source.
package file_link

import (
	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/resource"

	"github.com/computomatic/terraform-provider-stripe/internal/engine"
	"github.com/computomatic/terraform-provider-stripe/internal/generated/file_link"
)

// Declaration is what the spec cannot say about a file link. Stripe never deletes one, and an
// expired link cannot be modified, so expiring one is final and destroy is refused.
// `expires_at` ends a link at a time.
var Declaration = engine.Declaration{
	TypeName:              "stripe_file_link",
	IDPrefix:              "link_",
	Description:           "A link that lets anyone download a file. Stripe refuses to change an expired link.",
	DataSourceDescription: "A link that lets anyone download a file.",
	Paths: engine.Paths{
		Create:   "/v1/file_links",
		Retrieve: "/v1/file_links/{id}",
		Update:   "/v1/file_links/{id}",
		List:     "/v1/file_links",
	},
	Delete:       engine.Refuse,
	RefuseReason: "Stripe cannot delete a file link, and it serves the file until `expires_at`; set `expires_at` to end one.",
}

// NewResource returns the resource constructor the provider registers.
func NewResource() (func() resource.Resource, error) {
	return engine.NewResource(Declaration, file_link.Table)
}

// NewDataSource returns the data source constructor. No file link field is documented as unique, so the data source looks up by `id` only.
func NewDataSource() (func() datasource.DataSource, error) {
	return engine.NewDataSource(Declaration, file_link.Table)
}
