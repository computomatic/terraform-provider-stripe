// Package apps_secret declares the stripe_apps_secret data source. There is no resource.
package apps_secret

import (
	"github.com/hashicorp/terraform-plugin-framework/datasource"

	"github.com/computomatic/terraform-provider-stripe/internal/engine"
	"github.com/computomatic/terraform-provider-stripe/internal/generated/apps_secret"
)

// Declaration is what the spec cannot say about this object. The provider reads secrets only. Stripe has no retrieve by ID: `find` reads a secret by its name, which is unique within its scope, and returns the payload only when expanded (suite: "The Secret Store"). An unknown name is a 404.
var Declaration = engine.Declaration{
	TypeName:    "stripe_apps_secret",
	Description: "A secret a Stripe App keeps in the Secret Store. Look one up by its `name` and `scope`, such as `{ type = \"account\" }`. The `payload` is sensitive and is stored in the state.",
	Paths:       engine.Paths{Retrieve: "/v1/apps/secrets/find"},
	FindBy:      []engine.FindParam{{Name: "name"}, {Name: "scope"}},
	Expand:      []string{"payload"},
	Sensitive:   []string{"payload"},
	Delete:      engine.NoDelete,
}

// NewDataSource returns the data source constructor the provider registers. It reads the secret by `name` and `scope`, which Stripe's `find` takes in place of an ID.
func NewDataSource() (func() datasource.DataSource, error) {
	return engine.NewDataSource(Declaration, apps_secret.Table)
}
