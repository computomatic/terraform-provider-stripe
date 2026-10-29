package file_test

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/plancheck"

	"github.com/computomatic/terraform-provider-stripe/internal/acctest"
	"github.com/computomatic/terraform-provider-stripe/internal/acctest/tfacc"
	"github.com/computomatic/terraform-provider-stripe/internal/wire"
)

const resAddress = "stripe_file.test"

// tinyPNG is the 1 by 1 PNG the suite uploads ("tinyPNGBase64").
const tinyPNG = "iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAADUlEQVR42mNkYPhfDwAChwGA60e6kgAAAABJRU5ErkJggg=="

// pngFile writes the tiny PNG to a new file named name in a directory of the test's and returns
// its path and the SHA-256 of its content.
func pngFile(t *testing.T, name string) (path, sum string) {
	t.Helper()
	content, err := base64.StdEncoding.DecodeString(tinyPNG)
	if err != nil {
		t.Fatal(err)
	}
	path = filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(path, content, 0o600); err != nil {
		t.Fatal(err)
	}
	h := sha256.Sum256(content)
	return path, hex.EncodeToString(h[:])
}

// fileConfig uploads the file at source for purpose. extra lands inside the resource.
func fileConfig(c acctest.Config, source, purpose, extra string) string {
	return c.ProviderBlock() + fmt.Sprintf(`
resource "stripe_file" "test" {
  source  = %q
  purpose = %q
%s}
`, source, purpose, extra)
}

func filePath(dst *string) resource.TestCheckFunc {
	return resource.TestCheckResourceAttrWith(resAddress, "id", func(v string) error {
		*dst = "/v1/files/" + v
		return nil
	})
}

func idOf(dst *string) resource.TestCheckFunc {
	return resource.TestCheckResourceAttrWith(resAddress, "id", func(v string) error {
		*dst = v
		return nil
	})
}

func sameID(first *string) resource.TestCheckFunc {
	return resource.TestCheckResourceAttrWith(resAddress, "id", func(v string) error {
		if v != *first {
			return fmt.Errorf("the file changed from %s to %s", *first, v)
		}
		return nil
	})
}

// The upload goes to the Files API host as a multipart form, named as the local file is, and a
// retrieve then returns it (suite: "upload").
func TestAccFile_create(t *testing.T) {
	acct := acctest.NewAccount(t)
	source, sum := pngFile(t, "evidence.png")
	resource.ParallelTest(t, resource.TestCase{
		ProtoV6ProviderFactories: tfacc.ProviderFactories,
		Steps: acctest.Forget(t, []resource.TestStep{{
			Config: fileConfig(acct, source, "dispute_evidence", "") + `
data "stripe_file" "by_id" {
  id = stripe_file.test.id
}
`,
			Check: resource.ComposeAggregateTestCheckFunc(
				resource.TestMatchResourceAttr(resAddress, "id", regexp.MustCompile(`^file_`)),
				resource.TestCheckResourceAttr(resAddress, "purpose", "dispute_evidence"),
				resource.TestCheckResourceAttr(resAddress, "filename", "evidence.png"),
				resource.TestCheckResourceAttr(resAddress, "type", "png"),
				resource.TestCheckResourceAttr(resAddress, "source", source),
				resource.TestCheckResourceAttr(resAddress, "source_sha256", sum),
				resource.TestCheckResourceAttrSet(resAddress, "size"),
				resource.TestCheckResourceAttrPair("data.stripe_file.by_id", "filename", resAddress, "filename"),
			),
		}}, nil, "stripe_file"),
	})
}

// Stripe never updates a file, so a new path to the same content changes nothing in Stripe:
// the plan is an update that keeps the file and records the new path.
func TestAccFile_update(t *testing.T) {
	acct := acctest.NewAccount(t)
	first, sum := pngFile(t, "logo.png")
	moved, _ := pngFile(t, "logo.png")
	var id string
	resource.ParallelTest(t, resource.TestCase{
		ProtoV6ProviderFactories: tfacc.ProviderFactories,
		Steps: acctest.Forget(t, []resource.TestStep{
			{Config: fileConfig(acct, first, "business_logo", ""), Check: idOf(&id)},
			{
				Config: fileConfig(acct, moved, "business_logo", ""),
				ConfigPlanChecks: resource.ConfigPlanChecks{PreApply: []plancheck.PlanCheck{
					plancheck.ExpectResourceAction(resAddress, plancheck.ResourceActionUpdate),
				}},
				Check: resource.ComposeAggregateTestCheckFunc(
					sameID(&id),
					resource.TestCheckResourceAttr(resAddress, "source", moved),
					resource.TestCheckResourceAttr(resAddress, "source_sha256", sum),
				),
			},
		}, nil, "stripe_file"),
	})
}

// `purpose` cannot change, and a file cannot be destroyed (suite: "POST /v1/files/{id}" and
// "DELETE /v1/files/{id}" are not served). Stripe refuses the tiny PNG as a `business_icon`, which
// must be at least 128 by 128 pixels, so the file is uploaded as dispute evidence.
func TestAccFile_replace(t *testing.T) {
	acct := acctest.NewAccount(t)
	source, _ := pngFile(t, "evidence.png")
	cfg := fileConfig(acct, source, "dispute_evidence", "")
	resource.ParallelTest(t, resource.TestCase{
		ProtoV6ProviderFactories: tfacc.ProviderFactories,
		Steps: acctest.Forget(t, []resource.TestStep{
			{Config: cfg},
			{Config: fileConfig(acct, source, "business_logo", ""), PlanOnly: true, ExpectError: tfacc.RefusedReplace},
			{Config: cfg, Taint: []string{resAddress}, ExpectError: tfacc.RefusedDestroy},
		}, nil, "stripe_file"),
	})
}

// An imported file has no `source` or `source_sha256`: the first apply after the import records
// them without uploading, an update in place.
func TestAccFile_import(t *testing.T) {
	acct := acctest.NewAccount(t)
	source, _ := pngFile(t, "evidence.png")
	cfg := fileConfig(acct, source, "dispute_evidence", "")
	resource.ParallelTest(t, resource.TestCase{
		ProtoV6ProviderFactories: tfacc.ProviderFactories,
		Steps: acctest.Forget(t, []resource.TestStep{
			{Config: cfg},
			{ResourceName: resAddress, ImportState: true, ImportStateVerify: true, ImportStateVerifyIgnore: []string{"source", "source_sha256"}},
			{
				ResourceName: resAddress, ImportState: true, ImportStateKind: resource.ImportBlockWithID, Config: cfg,
				ExpectNonEmptyPlan: true,
				ImportPlanChecks: resource.ImportPlanChecks{PreApply: []plancheck.PlanCheck{
					plancheck.ExpectResourceAction(resAddress, plancheck.ResourceActionUpdate),
				}},
			},
		}, nil, "stripe_file"),
	})
}

// Stripe cannot delete a file, so dropping it from the configuration, and `terraform destroy`,
// fail at plan time, and the file stays.
func TestAccFile_delete(t *testing.T) {
	acct := acctest.NewAccount(t)
	source, _ := pngFile(t, "evidence.png")
	var path string
	var mark int
	cfg := fileConfig(acct, source, "dispute_evidence", "")
	resource.ParallelTest(t, resource.TestCase{
		ProtoV6ProviderFactories: tfacc.ProviderFactories,
		Steps: acctest.Forget(t, []resource.TestStep{
			{Config: cfg, Check: resource.ComposeTestCheckFunc(filePath(&path), tfacc.LogMark(t, acct, &mark))},
			{Config: acctest.Without(t, cfg, "stripe_file"), PlanOnly: true, ExpectError: tfacc.RefusedDestroy},
			{Config: cfg, Destroy: true, ExpectError: tfacc.RefusedDestroy},
		}, tfacc.Untouched(t, acct, &path, &mark, "purpose", wire.String("dispute_evidence")), "stripe_file"),
	})
}

// Stripe never changes a file, so the drift is in the local file: new content plans a new upload,
// which fails because the file cannot be replaced, and the old content back plans nothing.
func TestAccFile_drift(t *testing.T) {
	acct := acctest.NewAccount(t)
	source, _ := pngFile(t, "evidence.png")
	original, err := os.ReadFile(source)
	if err != nil {
		t.Fatal(err)
	}
	cfg := fileConfig(acct, source, "dispute_evidence", "")
	resource.ParallelTest(t, resource.TestCase{
		ProtoV6ProviderFactories: tfacc.ProviderFactories,
		Steps: acctest.Forget(t, []resource.TestStep{
			{Config: cfg},
			{
				PreConfig: func() {
					if err := os.WriteFile(source, append(append([]byte(nil), original...), 0), 0o600); err != nil {
						t.Fatal(err)
					}
				},
				Config: cfg, PlanOnly: true, ExpectError: regexp.MustCompile("The content of `source` has changed"),
			},
			{
				PreConfig: func() {
					if err := os.WriteFile(source, original, 0o600); err != nil {
						t.Fatal(err)
					}
				},
				Config: cfg, PlanOnly: true,
			},
		}, nil, "stripe_file"),
	})
}
