// Package apps_install declares the stripe_apps_install data source. There is no resource.
package apps_install

import (
	"github.com/hashicorp/terraform-plugin-framework/datasource"

	"github.com/computomatic/terraform-provider-stripe/internal/engine"
	"github.com/computomatic/terraform-provider-stripe/internal/generated/apps_install"
)

// Declaration is what the spec cannot say about this object. Stripe installs an app only when
// the app exists, and no suite test can make one (suite: "App installs": an unknown app is a
// 404), so nothing proves an install, and uninstalling is a lifecycle action, not a delete.
// Only the data source exists. An unknown install is a 404.
var Declaration = engine.Declaration{
	TypeName:    "stripe_apps_install",
	IDPrefix:    "appinst_",
	Description: "An installation of a Stripe App on the account, with the permissions, endpoints and content security policy its installer granted. Look one up by its `id`. `auth_code` is sensitive and is stored in the state.",
	Paths:       engine.Paths{Retrieve: "/v1/apps/installs/{id}", List: "/v1/apps/installs"},
	Delete:      engine.NoDelete,
	Sensitive:   []string{"auth_code"},
}

// NewDataSource returns the data source constructor the provider registers. It looks the object up by `id` only: Stripe documents no field of it as unique.
func NewDataSource() (func() datasource.DataSource, error) {
	return engine.NewDataSource(Declaration, apps_install.Table)
}
