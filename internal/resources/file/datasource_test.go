package file_test

import (
	"os"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"

	"github.com/computomatic/terraform-provider-stripe/internal/acctest"
	"github.com/computomatic/terraform-provider-stripe/internal/acctest/tfacc"
	"github.com/computomatic/terraform-provider-stripe/internal/wire"
)

func TestMain(m *testing.M) { os.Exit(acctest.Main(m)) }

// A file cannot be uploaded through Terraform, so the test seeds one.
func TestAccFile_read(t *testing.T) {
	acctest.RequireFake(t)
	acct := acctest.NewAccount(t)
	id := acctest.Seed(t, acct, wire.Object{
		{Name: "object", Value: wire.String("file")},
		{Name: "filename", Value: wire.String("evidence.png")},
		{Name: "purpose", Value: wire.String("dispute_evidence")},
		{Name: "size", Value: wire.Int(70)},
		{Name: "type", Value: wire.String("png")},
		{Name: "title", Value: wire.String("Evidence")},
		{Name: "url", Value: wire.Null{}},
		{Name: "expires_at", Value: wire.Null{}},
	})[0]
	resource.ParallelTest(t, resource.TestCase{
		ProtoV6ProviderFactories: tfacc.ProviderFactories,
		Steps: []resource.TestStep{{
			Config: acct.ProviderBlock() + `
data "stripe_file" "test" {
  id = "` + id + `"
}
`,
			Check: resource.ComposeAggregateTestCheckFunc(
				resource.TestCheckResourceAttr("data.stripe_file.test", "filename", "evidence.png"),
				resource.TestCheckResourceAttr("data.stripe_file.test", "purpose", "dispute_evidence"),
				resource.TestCheckResourceAttr("data.stripe_file.test", "size", "70"),
				resource.TestCheckResourceAttr("data.stripe_file.test", "title", "Evidence"),
			),
		}},
	})
}
