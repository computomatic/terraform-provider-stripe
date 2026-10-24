package file_link_test

import (
	"fmt"
	"os"
	"strconv"
	"testing"
	"time"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/plancheck"

	"github.com/computomatic/terraform-provider-stripe/internal/acctest"
	"github.com/computomatic/terraform-provider-stripe/internal/acctest/tfacc"
	"github.com/computomatic/terraform-provider-stripe/internal/wire"
)

func TestMain(m *testing.M) { os.Exit(acctest.Main(m)) }

const address = "stripe_file_link.test"

// seedFile puts a file in the account, which Terraform cannot upload. Only the fake can seed
// one, so it skips the test in smoke mode before the test creates anything.
func seedFile(t *testing.T, acct acctest.Config) string {
	t.Helper()
	acctest.RequireFake(t)
	return acctest.Seed(t, acct, wire.Object{
		{Name: "object", Value: wire.String("file")},
		{Name: "filename", Value: wire.String("report.png")},
		{Name: "purpose", Value: wire.String("dispute_evidence")},
		{Name: "size", Value: wire.Int(70)},
		{Name: "type", Value: wire.String("png")},
		{Name: "title", Value: wire.Null{}},
		{Name: "url", Value: wire.Null{}},
		{Name: "expires_at", Value: wire.Null{}},
	})[0]
}

func inOneDay(days int) string {
	return strconv.FormatInt(time.Now().Add(time.Duration(days)*24*time.Hour).Unix(), 10)
}

// linkConfig is a file link. extra lands inside the resource block.
func linkConfig(c acctest.Config, file string, extra string) string {
	return c.ProviderBlock() + fmt.Sprintf(`
resource "stripe_file_link" "test" {
  file = %q
%s}
`, file, extra)
}

func idOf(dst *string) resource.TestCheckFunc {
	return resource.TestCheckResourceAttrWith(address, "id", func(v string) error {
		*dst = v
		return nil
	})
}

// pathOf records the object's API path.
func pathOf(dst *string) resource.TestCheckFunc {
	return resource.TestCheckResourceAttrWith(address, "id", func(v string) error {
		*dst = "/v1/file_links/" + v
		return nil
	})
}

func TestAccFileLink_create(t *testing.T) {
	acct := acctest.NewAccount(t)
	file := seedFile(t, acct)
	expires := inOneDay(1)
	resource.ParallelTest(t, resource.TestCase{
		ProtoV6ProviderFactories: tfacc.ProviderFactories,
		Steps: acctest.Forget(t, []resource.TestStep{{
			Config: linkConfig(acct, file, fmt.Sprintf("  expires_at = %q\n  metadata   = { team = \"a\" }\n", expires)) + `
data "stripe_file_link" "test" {
  id = stripe_file_link.test.id
}
`,
			Check: resource.ComposeAggregateTestCheckFunc(
				resource.TestCheckResourceAttrSet(address, "id"),
				resource.TestCheckResourceAttr(address, "file", file),
				resource.TestCheckResourceAttr(address, "expires_at", expires),
				resource.TestCheckResourceAttr(address, "expired", "false"),
				resource.TestCheckResourceAttrSet(address, "url"),
				resource.TestCheckResourceAttr(address, "metadata.team", "a"),
				resource.TestCheckResourceAttrPair("data.stripe_file_link.test", "id", address, "id"),
				resource.TestCheckResourceAttr("data.stripe_file_link.test", "file", file),
			),
		}}, nil, "stripe_file_link"),
	})
}

func TestAccFileLink_update(t *testing.T) {
	acct := acctest.NewAccount(t)
	file := seedFile(t, acct)
	var first string
	later := inOneDay(2)
	resource.ParallelTest(t, resource.TestCase{
		ProtoV6ProviderFactories: tfacc.ProviderFactories,
		Steps: acctest.Forget(t, []resource.TestStep{
			{Config: linkConfig(acct, file, fmt.Sprintf("  expires_at = %q\n  metadata = { team = \"a\", tier = \"1\" }\n", inOneDay(1))), Check: idOf(&first)},
			{
				Config: linkConfig(acct, file, fmt.Sprintf("  expires_at = %q\n  metadata = { team = \"b\" }\n", later)),
				ConfigPlanChecks: resource.ConfigPlanChecks{PreApply: []plancheck.PlanCheck{
					plancheck.ExpectResourceAction(address, plancheck.ResourceActionUpdate),
				}},
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr(address, "expires_at", later),
					resource.TestCheckResourceAttr(address, "metadata.%", "1"),
					resource.TestCheckResourceAttr(address, "metadata.team", "b"),
					resource.TestCheckResourceAttrWith(address, "id", func(v string) error {
						if v != first {
							return fmt.Errorf("the in-place update changed the ID from %s to %s", first, v)
						}
						return nil
					}),
				),
			},
		}, nil, "stripe_file_link"),
	})
}

// `file` is create-only, so changing it would replace the link, which Stripe cannot delete:
// the plan fails, and so does a taint, at apply.
func TestAccFileLink_replace(t *testing.T) {
	acct := acctest.NewAccount(t)
	one, two := seedFile(t, acct), seedFile(t, acct)
	cfg := linkConfig(acct, one, "")
	resource.ParallelTest(t, resource.TestCase{
		ProtoV6ProviderFactories: tfacc.ProviderFactories,
		Steps: acctest.Forget(t, []resource.TestStep{
			{Config: cfg},
			{Config: linkConfig(acct, two, ""), PlanOnly: true, ExpectError: tfacc.RefusedReplace},
			{Config: cfg, Taint: []string{address}, ExpectError: tfacc.RefusedDestroy},
		}, nil, "stripe_file_link"),
	})
}

func TestAccFileLink_import(t *testing.T) {
	acct := acctest.NewAccount(t)
	cfg := linkConfig(acct, seedFile(t, acct), fmt.Sprintf("  expires_at = %q\n  metadata = { team = \"a\" }\n", inOneDay(1)))
	resource.ParallelTest(t, resource.TestCase{
		ProtoV6ProviderFactories: tfacc.ProviderFactories,
		Steps: acctest.Forget(t, []resource.TestStep{
			{Config: cfg},
			{
				ResourceName:      address,
				ImportState:       true,
				ImportStateVerify: true,
			},
			{
				ResourceName:    address,
				ImportState:     true,
				ImportStateKind: resource.ImportBlockWithID,
				Config:          cfg,
			},
		}, nil, "stripe_file_link"),
	})
}

// Stripe cannot delete a file link, so dropping it from the configuration, and `terraform
// destroy`, fail at plan time, and the file link stays as it was.
func TestAccFileLink_delete(t *testing.T) {
	acct := acctest.NewAccount(t)
	file := seedFile(t, acct)
	var path string
	var mark int
	cfg := linkConfig(acct, file, fmt.Sprintf("  expires_at = %q\n", inOneDay(1)))
	resource.ParallelTest(t, resource.TestCase{
		ProtoV6ProviderFactories: tfacc.ProviderFactories,
		Steps: acctest.Forget(t, []resource.TestStep{
			{Config: cfg, Check: resource.ComposeTestCheckFunc(pathOf(&path), tfacc.LogMark(t, acct, &mark))},
			{Config: acctest.Without(t, cfg, "stripe_file_link"), PlanOnly: true, ExpectError: tfacc.RefusedDestroy},
			{Config: cfg, Destroy: true, ExpectError: tfacc.RefusedDestroy},
		}, tfacc.Untouched(t, acct, &path, &mark, "expired", wire.Bool(false)), "stripe_file_link"),
	})
}

func TestAccFileLink_drift(t *testing.T) {
	acct := acctest.NewAccount(t)
	file := seedFile(t, acct)
	var id string
	cfg := linkConfig(acct, file, "  metadata = { team = \"a\" }\n")
	resource.ParallelTest(t, resource.TestCase{
		ProtoV6ProviderFactories: tfacc.ProviderFactories,
		Steps: acctest.Forget(t, []resource.TestStep{
			{Config: cfg, Check: idOf(&id)},
			{
				PreConfig: func() {
					tfacc.Post(t, acct, "/v1/file_links/"+id, wire.Object{
						{Name: "metadata", Value: wire.Object{{Name: "team", Value: wire.String("changed")}}},
					})
				},
				Config: cfg,
				ConfigPlanChecks: resource.ConfigPlanChecks{PreApply: []plancheck.PlanCheck{
					plancheck.ExpectNonEmptyPlan(),
					plancheck.ExpectResourceAction(address, plancheck.ResourceActionUpdate),
				}},
				Check: resource.TestCheckResourceAttr(address, "metadata.team", "a"),
			},
		}, nil, "stripe_file_link"),
	})
}
