// Package file declares the stripe_file resource and data source: a file uploaded to Stripe.
package file

import (
	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/resource"

	"github.com/computomatic/terraform-provider-stripe/internal/engine"
	"github.com/computomatic/terraform-provider-stripe/internal/generated/file"
)

// Declaration is what the spec cannot say about a file. Stripe takes uploads as a multipart form
// on the Files API host, which Terraform cannot send from HCL, so the resource uploads the local
// file `source` names, as the Files API's `file` part, with its `purpose` (suite: "upload",
// "upload for <purpose>"). Every other request goes to the API host. Stripe re-encodes images,
// so `size` is not the uploaded byte count and the content is tracked by its hash, a change of
// which replaces the file. Stripe has no update and no delete for a file (suite: "POST
// /v1/files/{id}" and "DELETE /v1/files/{id}" are 404 `Unrecognized request URL`), so destroy is
// refused. `file_link_data`, which makes a file link with the upload, is left out: the suite
// does not prove it, and `stripe_file_link` makes one.
var Declaration = engine.Declaration{
	TypeName: "stripe_file",
	IDPrefix: "file_",
	Description: "A file uploaded to Stripe, such as dispute evidence, a business logo or an Issuing card logo. " +
		"The provider uploads the local file that `source` names, and records the SHA-256 of its content. " +
		"Stripe cannot change a file once it is uploaded, so changing `purpose`, or the content of the file, fails the plan.",
	DataSourceDescription: "A file uploaded to Stripe. Look one up by its `id`.",
	ImportNote: "Stripe does not return a file's content, so an imported file has no `source` or `source_sha256` in state. " +
		"The first apply after the import records the configured `source` and the hash of its content without uploading it, so make sure `source` holds the content the file was uploaded with.",
	ImportNoteLevel: engine.NoteWarning,
	Paths: engine.Paths{
		Create:   "/v1/files",
		Retrieve: "/v1/files/{id}",
	},
	Upload:       &engine.Upload{Part: "file"},
	Delete:       engine.Refuse,
	RefuseReason: "Stripe cannot delete a file.",
}

// NewResource returns the resource constructor the provider registers.
func NewResource() (func() resource.Resource, error) {
	return engine.NewResource(Declaration, file.Table)
}

// NewDataSource returns the data source constructor. It looks a file up by `id` only.
func NewDataSource() (func() datasource.DataSource, error) {
	return engine.NewDataSource(Declaration, file.Table)
}
